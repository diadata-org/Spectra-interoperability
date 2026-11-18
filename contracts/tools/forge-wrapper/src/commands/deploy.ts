import chalk from "chalk";
import { Command } from "commander";
import { recordDeployment } from "../deployments";
import {
  loadNetworkConfig,
  prepareCustomerEnvironment,
  resolveAccountPrivateKey,
} from "../config";
import {
  getDefaultCustomer,
  getDefaultNetwork,
} from "../utils/paths";
import { formatCommand, runForge } from "../utils/forge";
import {
  buildCreate2Plan,
  checkFactoryPresence,
  executeCreate2DeployWithInitCode,
  getContractsRoot,
  normalizeSalt,
} from "../utils/create2";
import {
  ensureReceiverFactory,
  predictReceiverAddress,
} from "../utils/receiver-factory";
import { DeployOptions, DeploymentRecord } from "../types";
import { getTemplate } from "../utils/templates";
import { timestampNow } from "../utils/dates";
import path from "path";
import { promises as fs } from "fs";
import { ethers } from "ethers";

function collectArgs(value: string, previous: string[]): string[] {
  return [...previous, value];
}

function inferArtifact(alias: string, networkDefaults: Record<string, string>, explicit?: string): string {
  if (explicit) {
    return explicit;
  }
  const inferred = networkDefaults[alias];
  if (!inferred) {
    throw new Error(
      `No artifact provided for alias '${alias}'. Pass --artifact or add a mapping in default_contracts.`
    );
  }
  return inferred;
}

function maskSensitiveArgs(args: string[]): string[] {
  const masked: string[] = [];
  for (let i = 0; i < args.length; i += 1) {
    const value = args[i];
    masked.push(value);
    if (value === "--private-key" && i + 1 < args.length) {
      masked.push("***hidden***");
      i += 1;
    }
  }
  return masked;
}

/** initCodeHash of artifact+args (no salt dependence) for receiver-address prediction. */
async function plan0InitCodeHash(
  artifact: string,
  constructorArgs: string[],
  forgeProfile?: string
): Promise<string> {
  const plan = await buildCreate2Plan({
    artifact,
    constructorArgs,
    salt: "0x" + "0".repeat(64),
    forgeProfile,
  });
  return plan.initCodeHash;
}

/** Dry-run DeploymentRecord for the receiver-factory path. */
function dryRecord(
  options: DeployOptions,
  artifact: string,
  constructorArgs: string[],
  accountName: string,
  predicted: string,
  receiverFactory: string,
  salt: string,
  initCodeHash: string
): DeploymentRecord {
  return {
    alias: options.alias,
    address: predicted,
    txHash: undefined,
    deployedAt: timestampNow(),
    artifact,
    constructorArgs,
    deployer: { alias: accountName, address: options.deployerAddress },
    deployMethod: "create2",
    create2: { factory: receiverFactory, salt, initCodeHash },
  };
}

/** Broadcast deployReceiver(...) on the DIAReceiverFactory. */
async function executeReceiverDeploy(options: {
  rpcUrl: string;
  chainId: number;
  privateKey: string;
  receiverFactory: string;
  userSalt: string;
  constructorArgs: string[];
  legacy?: boolean;
  gasPrice?: string;
}): Promise<{ address: string; txHash: string }> {
  const { ethers } = await import("ethers");
  const provider = new ethers.JsonRpcProvider(options.rpcUrl);
  const wallet = new ethers.Wallet(options.privateKey, provider);
  const iface = new ethers.Interface([
    "function deployReceiver(bytes32 userSalt, string domainName, string domainVersion, uint256 sourceChainId, address verifyingContract) returns (address)",
    "event ReceiverDeployed(address indexed receiver, address indexed owner, bytes32 indexed userSalt)",
  ]);
  const [domainName, domainVersion, sourceChainId, verifyingContract] = options.constructorArgs;
  const data = iface.encodeFunctionData("deployReceiver", [
    options.userSalt,
    domainName,
    domainVersion,
    BigInt(sourceChainId),
    verifyingContract,
  ]);
  const tx: any = { to: options.receiverFactory, data, chainId: options.chainId };
  if (options.legacy || options.gasPrice) {
    tx.type = 0;
    if (options.gasPrice) tx.gasPrice = ethers.parseUnits(options.gasPrice, "wei");
  } else {
    const fee = await provider.getFeeData();
    if (fee.maxFeePerGas) tx.maxFeePerGas = fee.maxFeePerGas + (fee.maxFeePerGas / 4n);
    if (fee.maxPriorityFeePerGas) tx.maxPriorityFeePerGas = fee.maxPriorityFeePerGas;
  }
  const sent = await wallet.sendTransaction(tx);
  // eslint-disable-next-line no-console
  console.log(chalk`{gray tx: ${sent.hash}}`);
  const receipt = await sent.wait();
  if (!receipt || receipt.status !== 1) {
    throw new Error(`DIAReceiverFactory.deployReceiver tx reverted: ${sent.hash}`);
  }
  const event = receipt.logs
    .map((log: any) => {
      try {
        return iface.parseLog({ topics: log.topics as string[], data: log.data });
      } catch {
        return undefined;
      }
    })
    .find((parsed: any) => parsed && parsed.name === "ReceiverDeployed");
  if (!event) {
    throw new Error(`ReceiverDeployed event not found in receipt ${sent.hash}`);
  }
  return { address: String(event.args.receiver), txHash: sent.hash };
}

function parseForgeCreateOutput(stdout: string): { address: string; txHash?: string } {
  const trimmed = stdout.trim();

  const jsonMatch = trimmed.match(/\{[\s\S]*\}$/);
  if (jsonMatch) {
    const jsonPayload = jsonMatch[0];
    try {
      const parsed = JSON.parse(jsonPayload);
      const lower = Object.fromEntries(
        Object.entries(parsed).map(([key, value]) => [key.toLowerCase(), value])
      );
      const address = (lower.deployedto || lower.contractaddress || "").toString();
      const txHash = lower.transactionhash ? lower.transactionhash.toString() : undefined;
      if (/^0x[a-fA-F0-9]{40}$/.test(address)) {
        return { address, txHash };
      }
    } catch (error) {
      // fall back to regex parsing below
    }
  }

  const addressMatch = stdout.match(/Deployed to:\s*(0x[a-fA-F0-9]{40})/);
  if (!addressMatch) {
    throw new Error(`Failed to parse deployment address from forge output:\n${stdout}`);
  }
  const hashMatch = stdout.match(/Transaction hash:\s*(0x[a-fA-F0-9]+)/);
  return {
    address: addressMatch[1],
    txHash: hashMatch ? hashMatch[1] : undefined,
  };
}

export async function executeDeploy(options: DeployOptions): Promise<DeploymentRecord> {
  const networkConfig = await loadNetworkConfig(options.network);
  const rpcUrl = options.rpcUrl ?? networkConfig.rpc_url;
  const artifact = inferArtifact(options.alias, networkConfig.default_contracts, options.artifact);
  const accountName = options.account || "deployer";
  const forgeProfile = networkConfig.forge_profile;
  let privateKey = options.privateKeyOverride;
  if (!privateKey) {
    privateKey = await resolveAccountPrivateKey(networkConfig, accountName, options.customer);
  }

  if (!privateKey) {
    throw new Error(
      `No private key available for account '${accountName}'. Provide --account mapping or --private-key override.`
    );
  }

  const displayArgs: string[] = [
    "create",
    artifact,
    "--rpc-url",
    rpcUrl,
    "--private-key",
    privateKey,
    "--chain-id",
    String(networkConfig.chain_id),
  ];

  if (networkConfig.evm_version) {
    displayArgs.push("--evm-version", networkConfig.evm_version);
  }

  // Add legacy flag early if needed
  if (options.legacy || networkConfig.legacy) {
    displayArgs.push("--legacy");
  }

  // Apply gas configuration when the network declares one. Some chains
  // (e.g. Cosmos-SDK EVMs like goat-testnet) enforce a minimum priority
  // fee that forge's auto-estimation underestimates, so the override is
  // required for the transaction to be accepted.
  if (networkConfig.gas) {
    if (networkConfig.gas.max_fee_per_gas) {
      displayArgs.push("--gas-price", networkConfig.gas.max_fee_per_gas);
    }
    if (networkConfig.gas.priority_fee) {
      displayArgs.push("--priority-gas-price", networkConfig.gas.priority_fee);
    }
  }

  let constructorArgs = options.constructorArgs;
  if (constructorArgs.length === 0) {
    const template = getTemplate(options.alias);
    if (template?.args) {
      constructorArgs = template.args;
    }
  }

  constructorArgs = constructorArgs.map((arg) => arg.trim()).filter((arg) => arg.length > 0);

  // ------------------------------------------------------------------
  // CREATE2 path: deterministic deployment.
  // PushOracleReceiverV2 routes through DIAReceiverFactory (owner-correct:
  // factory deploys + atomically transfers ownership to the tx sender).
  // Anything else deploys directly via the Arachnid proxy.
  // ------------------------------------------------------------------
  if (options.deployMethod === "create2") {
    if (!options.salt) {
      throw new Error("CREATE2 deployment requires a salt (--salt).");
    }
    const salt = normalizeSalt(options.salt);
    const isReceiver = artifact.endsWith("PushOracleReceiverV2.sol:PushOracleReceiverV2");

    let receiverFactory: string | undefined;
    if (isReceiver) {
      const ownerAddr = options.deployerAddress ?? new ethers.Wallet(privateKey).address;
      const ensured = await ensureReceiverFactory({
        rpcUrl,
        chainId: networkConfig.chain_id,
        networkName: options.network,
        privateKey,
        legacy: options.legacy || networkConfig.legacy,
        gasPrice: networkConfig.gas?.max_fee_per_gas,
      });
      receiverFactory = ensured.address;
      // eslint-disable-next-line no-console
      console.log(ensured.reused
        ? chalk.gray(`Reusing DIAReceiverFactory at ${ensured.address}`)
        : chalk.green(`DIAReceiverFactory deployed at ${ensured.address}`));

      const receiverInitCodeHash = await plan0InitCodeHash(artifact, constructorArgs, networkConfig.forge_profile);
      const predicted = predictReceiverAddress(ensured.address, ownerAddr, salt, receiverInitCodeHash);
      // eslint-disable-next-line no-console
      console.log(chalk.gray(`CREATE2 plan (via DIAReceiverFactory, owner ${ownerAddr}):`));
      // eslint-disable-next-line no-console
      console.log(chalk.gray(`  • predicted address: ${predicted}`));
      // eslint-disable-next-line no-console
      console.log(chalk.gray(`  • user salt: ${salt}`));
      // eslint-disable-next-line no-console
      console.log(chalk.gray(`  • final salt: keccak(owner ++ userSalt)`));

      if (options.dryRun) {
        // eslint-disable-next-line no-console
        console.log(chalk.yellow("Dry run enabled, not broadcasting CREATE2 deployment."));
        return dryRecord(options, artifact, constructorArgs, accountName, predicted, receiverFactory!, salt, receiverInitCodeHash);
      }

      // Idempotency: refuse if predicted address already has code
      const provider = new ethers.JsonRpcProvider(rpcUrl);
      const existing = await provider.getCode(predicted);
      if (existing && existing !== "0x") {
        throw new Error(
          `Address ${predicted} already has code — the same owner+salt was deployed before. Use a different salt.`
        );
      }

      const result = await executeReceiverDeploy({
        rpcUrl,
        chainId: networkConfig.chain_id,
        privateKey,
        receiverFactory: ensured.address,
        userSalt: salt,
        constructorArgs,
        legacy: options.legacy || networkConfig.legacy,
        gasPrice: networkConfig.gas?.max_fee_per_gas,
      });
      if (result.address.toLowerCase() !== predicted.toLowerCase()) {
        throw new Error(`Deployed ${result.address} != predicted ${predicted} — investigate.`);
      }
      return {
        alias: options.alias,
        address: result.address,
        txHash: result.txHash,
        deployedAt: timestampNow(),
        artifact,
        constructorArgs,
        deployer: { alias: accountName, address: options.deployerAddress },
        deployMethod: "create2",
        create2: { factory: ensured.address, salt, initCodeHash: receiverInitCodeHash },
      };
    }

    // Non-receiver contracts: Arachnid direct (owner WILL be the factory —
    // only safe for non-Ownable contracts)
    const plan = await buildCreate2Plan({
      artifact,
      constructorArgs,
      salt,
      forgeProfile: networkConfig.forge_profile,
      evmVersion: networkConfig.evm_version,
    });

    const presence = await checkFactoryPresence(rpcUrl);
    if (presence === "missing") {
      throw new Error(
        `CREATE2 factory ${plan.factory} not deployed on ${options.network}. ` +
          `Broadcast the canonical deterministic-deployment-proxy tx first, or use the standard deploy method.`
      );
    }
    if (presence === "unknown") {
      // eslint-disable-next-line no-console
      console.log(chalk.yellow(`Warning: could not verify factory presence on ${options.network} — continuing.`));
    }

    // eslint-disable-next-line no-console
    console.log(chalk.gray(`CREATE2 plan:`));
    // eslint-disable-next-line no-console
    console.log(chalk.gray(`  • predicted address: ${plan.address}`));
    // eslint-disable-next-line no-console
    console.log(chalk.gray(`  • salt: ${plan.salt}`));
    // eslint-disable-next-line no-console
    console.log(chalk.gray(`  • initCodeHash: ${plan.initCodeHash}`));
    // eslint-disable-next-line no-console
    console.log(chalk.gray(`  • initCode size: ${plan.initCodeByteLength} bytes`));

    if (options.dryRun) {
      // eslint-disable-next-line no-console
      console.log(chalk.yellow("Dry run enabled, not broadcasting CREATE2 deployment."));
      return {
        alias: options.alias,
        address: plan.address,
        txHash: undefined,
        deployedAt: timestampNow(),
        artifact,
        constructorArgs,
        deployer: {
          alias: accountName,
          address: options.deployerAddress,
        },
        deployMethod: "create2",
        create2: {
          factory: plan.factory,
          salt: plan.salt,
          initCodeHash: plan.initCodeHash,
        },
      };
    }

    // Already deployed at the predicted address? Make it idempotent.
    const provider = new ethers.JsonRpcProvider(rpcUrl);
    const existing = await provider.getCode(plan.address);
    if (existing && existing !== "0x") {
      throw new Error(
        `Address ${plan.address} already has code — the same salt+initCode was deployed before. ` +
          `Use a different salt for a fresh deployment.`
      );
    }

    // Rebuild initCode for the tx data (creation code ++ encoded args)
    const [sourcePath, contractName] = artifact.split(":");
    const artifactRel = sourcePath.replace(/^contracts\//, "");
    const artifactPath = path.join(getContractsRoot(), "out", artifactRel, `${contractName}.json`);
    const artifactJson = JSON.parse(await fs.readFile(artifactPath, "utf-8"));
    let initCode: string = artifactJson.bytecode.object;
    if (constructorArgs.length > 0) {
      const iface = new ethers.Interface(artifactJson.abi);
      const typed = constructorArgs.map((arg) => (/^-?\d+$/.test(arg) ? BigInt(arg) : arg));
      initCode = initCode + iface.encodeDeploy(typed as any[]).slice(2);
    }

    const result = await executeCreate2DeployWithInitCode({
      rpcUrl,
      chainId: networkConfig.chain_id,
      privateKey,
      salt,
      initCode,
      legacy: options.legacy || networkConfig.legacy,
      gasPrice: networkConfig.gas?.max_fee_per_gas,
    });

    if (result.address.toLowerCase() !== plan.address.toLowerCase()) {
      throw new Error(
        `Deployed address ${result.address} != predicted ${plan.address}. Record NOT consistent — investigate.`
      );
    }

    return {
      alias: options.alias,
      address: result.address,
      txHash: result.txHash,
      deployedAt: timestampNow(),
      artifact,
      constructorArgs,
      deployer: {
        alias: accountName,
        address: options.deployerAddress,
      },
      deployMethod: "create2",
      create2: {
        factory: plan.factory,
        salt: plan.salt,
        initCodeHash: plan.initCodeHash,
      },
    };
  }

  // ------------------------------------------------------------------
  // Standard path: forge create
  // ------------------------------------------------------------------

  if (!displayArgs.includes("--broadcast")) {
    displayArgs.push("--broadcast");
  }

  if (constructorArgs.length > 0) {
    displayArgs.push("--constructor-args", ...constructorArgs);
  }

  const actualForgeArgs = [...displayArgs, "--json"];

  const maskedCommand = formatCommand("forge", maskSensitiveArgs(displayArgs));
  // eslint-disable-next-line no-console
  if (forgeProfile) {
    console.log(chalk.gray(`FOUNDRY_PROFILE=${forgeProfile} ${maskedCommand}`));
  } else {
    console.log(chalk.gray(maskedCommand));
  }

  if (options.dryRun) {
    // eslint-disable-next-line no-console
    console.log(chalk.yellow("Dry run enabled, not executing forge command."));
    return {
      alias: options.alias,
      address: "0x0000000000000000000000000000000000000000",
      txHash: undefined,
      deployedAt: timestampNow(),
      artifact,
      constructorArgs: constructorArgs,
      deployer: {
        alias: accountName,
        address: options.deployerAddress,
      },
    };
  }

  const runTx = async (force: boolean) => {
    const args = [...actualForgeArgs];
    if (force && !args.includes("--force")) {
      args.splice(1, 0, "--force");
    }
    try {
      return await runForge(args, {
        env: forgeProfile ? { FOUNDRY_PROFILE: forgeProfile } : undefined,
      });
    } catch (error: any) {
      const stderr = (error?.stderr ?? "").trim();
      const stdout = (error?.stdout ?? "").trim();
      const pieces = [
        `forge exited with code ${error?.code ?? "unknown"}`,
        stderr.length ? stderr : undefined,
        stdout.length ? stdout : undefined,
      ].filter(Boolean);
      throw new Error(pieces.join("\n"));
    }
  };

  let result = await runTx(false);
  let parsed: { address: string; txHash?: string } | undefined;
  const attemptParse = () => {
    try {
      return parseForgeCreateOutput(result.stdout);
    } catch (err) {
      return undefined;
    }
  };

  parsed = attemptParse();

  if (!parsed) {
    const stdout = result.stdout.trim();
    if (stdout.includes("No files changed")) {
      result = await runTx(true);
      parsed = attemptParse();
    }
  }

  if (!parsed) {
    const stdout = result.stdout.trim();
    const stderr = "";
    const pieces = [
      "forge create did not return a deployment address.",
      stdout.length ? `stdout:\n${stdout}` : undefined,
      stderr.length ? `stderr:\n${stderr}` : undefined,
    ].filter(Boolean);
    throw new Error(pieces.join("\n\n"));
  }
  const record: DeploymentRecord = {
    alias: options.alias,
    address: parsed.address,
    txHash: parsed.txHash,
    deployedAt: timestampNow(),
    artifact,
    constructorArgs: constructorArgs,
    deployer: {
      alias: accountName,
      address: options.deployerAddress,
    },
  };

  return record;
}

export function registerDeployCommand(program: Command): void {
  program
    .command("deploy <alias>")
    .description("Deploy a contract alias using forge")
    .option("-n, --network <network>", "Network name (matches networks/<network>.yaml)")
    .option("-c, --customer <customer>", "Customer namespace for keys/deployments")
    .option("-a, --artifact <artifact>", "Forge artifact, e.g. contracts/Contract.sol:Contract")
    .option("--account <account>", "Account alias from network config", "deployer")
    .option("--rpc-url <url>", "Override RPC URL")
    .option("--constructor-arg <value>", "Constructor argument", collectArgs, [])
    .option("--create2", "Deploy deterministically via the Arachnid CREATE2 factory (requires --salt)")
    .option("--salt <salt>", "CREATE2 salt (hex 0x64-chars or arbitrary string, hashed with keccak256)")
    .option("--dry-run", "Print command without executing")
    .option("--legacy", "Use legacy transactions instead of EIP1559")
  
  .action(async (alias: string, cmdOptions) => {
      const network = cmdOptions.network ?? getDefaultNetwork();
      if (!network) {
        throw new Error("Network is required (pass --network or set FORGE_WRAPPER_NETWORK)");
      }

      const customer = cmdOptions.customer ?? getDefaultCustomer();
      await prepareCustomerEnvironment(customer);

      try {
        const record = await executeDeploy({
          alias,
          artifact: cmdOptions.artifact,
          constructorArgs: cmdOptions.constructorArg ?? [],
          customer,
          network,
          account: cmdOptions.account ?? "deployer",
          rpcUrl: cmdOptions.rpcUrl,
          dryRun: Boolean(cmdOptions.dryRun),
          salt: cmdOptions.salt,
          deployMethod: cmdOptions.create2 ? "create2" : undefined,
          legacy: Boolean(cmdOptions.legacy),
        });

        if (!cmdOptions.dryRun) {
          await recordDeployment(customer, network, record);
          // eslint-disable-next-line no-console
          console.log(chalk.green(`Deployment successful: ${record.address}`));
          if (record.txHash) {
            // eslint-disable-next-line no-console
            console.log(chalk.gray(`tx: ${record.txHash}`));
          }
        }
      } catch (error: any) {
        // eslint-disable-next-line no-console
        console.error(chalk.red(`Deployment failed: ${error?.message ?? error}`));
        if (error?.stdout) {
          // eslint-disable-next-line no-console
          console.error(chalk.red(error.stdout));
        }
        if (error?.stderr) {
          // eslint-disable-next-line no-console
          console.error(chalk.red(error.stderr));
        }
        process.exitCode = 1;
      }
    });
}
