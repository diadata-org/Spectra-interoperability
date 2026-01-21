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
import { DeployOptions, DeploymentRecord } from "../types";
import { getTemplate } from "../utils/templates";
import { timestampNow } from "../utils/dates";

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

  let constructorArgs = options.constructorArgs;
  if (constructorArgs.length === 0) {
    const template = getTemplate(options.alias);
    if (template?.args) {
      constructorArgs = template.args;
    }
  }

  constructorArgs = constructorArgs.map((arg) => arg.trim()).filter((arg) => arg.length > 0);

  if (!displayArgs.includes("--broadcast")) {
    displayArgs.push("--broadcast");
  }

  if (constructorArgs.length > 0) {
    displayArgs.push("--constructor-args", ...constructorArgs);
  }

  if (options.salt) {
    displayArgs.push("--salt", options.salt);
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
    .option("--salt <salt>", "Deterministic deployment salt")
    .option("--dry-run", "Print command without executing")
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
