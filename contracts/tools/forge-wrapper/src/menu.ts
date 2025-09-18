import prompts from "prompts";
import chalk from "chalk";
import path from "path";
import { promises as fs } from "fs";
import {
  generatePrivateKey,
  listKeyAliases,
  storePrivateKey,
  readStoredWallet,
} from "./services/keys";
import { prepareCustomerEnvironment, loadNetworkConfig, resolveAccountPrivateKey } from "./config";
import { getDefaultCustomer, getDefaultNetwork, getProjectRoot } from "./utils/paths";
import { listNetworkNames, createNetworkConfig } from "./services/networks";
import { listDeployments } from "./services/deployments";
import { executeDeploy } from "./commands/deploy";
import { buildVerifyContext, verifyDeployment } from "./commands/verify";
import { recordDeployment } from "./deployments";
import { buildPresetChoices, getPreset } from "./utils/contracts";
import { getTemplate } from "./utils/templates";
import { loadDeployments } from "./deployments";
import { NetworkConfig } from "./types";

function logEquivalent(command: string): void {
  // eslint-disable-next-line no-console
  console.log(chalk.blue(`Direct CLI: ${command}`));
}

export async function runInteractiveMenu(): Promise<void> {
  let currentCustomer = getDefaultCustomer();
  let currentNetwork = getDefaultNetwork();
  await prepareCustomerEnvironment(currentCustomer);

  // eslint-disable-next-line no-constant-condition
  while (true) {
    const choices = [
      {
        title: "Create wallet",
        description: "Generate and store a new private key",
        value: "createWallet",
      },
      {
        title: "Deploy contract",
        description: "Run forge deploy interactively",
        value: "deploy",
      },
      {
        title: "Verify contract",
        description: "Submit verification for a deployment",
        value: "verify",
      },
      {
        title: "List networks",
        value: "listNetworks",
      },
      {
        title: "Add network",
        value: "addNetwork",
      },
      {
        title: "List keys",
        value: "listKeys",
      },
      {
        title: "List deployments",
        value: "listDeployments",
      },
      {
        title: `Switch customer (current: ${currentCustomer})`,
        value: "switchCustomer",
      },
      currentNetwork
        ? {
            title: `Switch network (current: ${currentNetwork})`,
            value: "switchNetwork",
          }
        : {
            title: "Set default network",
            value: "switchNetwork",
          },
      {
        title: "Exit",
        value: "exit",
      },
    ].filter(Boolean) as { title: string; value: string; description?: string }[];

    if (choices.length === 0) {
      // eslint-disable-next-line no-console
      console.log(chalk.gray("No actions available."));
      return;
    }

    const { action } = await prompts({
      type: "select",
      name: "action",
      message: "Forge Wrapper",
      choices,
      initial: 0,
    });

    if (!action || action === "exit") {
      // eslint-disable-next-line no-console
      console.log(chalk.gray("Goodbye"));
      return;
    }

    try {
      switch (action) {
        case "createWallet": {
          const name = await promptWalletName(currentCustomer);
          const key = generatePrivateKey();
          const info = await storePrivateKey(currentCustomer, name, key, false);
          // eslint-disable-next-line no-console
          console.log(chalk.green(`Stored wallet '${name}' for ${currentCustomer}.`));
          // eslint-disable-next-line no-console
          console.log(chalk.gray(`metadata: ${info.metadataPath}`));
          if (info.address) {
            // eslint-disable-next-line no-console
            console.log(chalk.gray(`address: ${info.address}`));
          }
          logEquivalent(
            `forge-wrapper keys import --customer ${currentCustomer} --name ${name} --value <PRIVATE_KEY_HEX>`
          );
          break;
        }
        case "deploy": {
          const selectedNetwork = await promptSelectNetwork(currentNetwork);
          if (!selectedNetwork) {
            // eslint-disable-next-line no-console
            console.log(chalk.yellow("No network selected"));
            break;
          }
          currentNetwork = selectedNetwork;
          await interactiveDeploy(currentCustomer, currentNetwork);
          break;
        }
        case "verify": {
          const network = currentNetwork || (await promptSelectNetwork());
          if (!network) {
            // eslint-disable-next-line no-console
            console.log(chalk.yellow("No network selected"));
            break;
          }

          currentNetwork = network;

          const deploymentsFile = await loadDeployments(currentCustomer, network);
          const records = Object.values(deploymentsFile.current ?? {});
          if (records.length === 0) {
            // eslint-disable-next-line no-console
            console.log(chalk.gray(`No deployments found for ${currentCustomer} on ${network}`));
            break;
          }

          const aliasChoices = records.map((record) => {
            const status = record.verification?.status;
            const statusLabel =
              status === "success" ? " [verified]" : status === "failed" ? " [failed]" : "";
            return {
              title: `${record.alias} -> ${record.address}${statusLabel}`,
              value: record.alias,
            };
          });

          const aliasAnswer = await prompts({
            type: "select",
            name: "alias",
            message: "Select deployment to verify",
            choices: aliasChoices,
            initial: 0,
          });
          const alias = aliasAnswer.alias ? String(aliasAnswer.alias) : undefined;
          if (!alias) {
            // eslint-disable-next-line no-console
            console.log(chalk.gray("Verification cancelled (no alias selected)."));
            break;
          }

          const selectedRecord = deploymentsFile.current[alias];
          if (!selectedRecord) {
            // eslint-disable-next-line no-console
            console.log(chalk.red(`Deployment record for ${alias} not found`));
            break;
          }

          const networkConfig = await loadNetworkConfig(network);

          const watchInitial =
            typeof networkConfig.verification?.watch === "boolean"
              ? networkConfig.verification.watch
              : undefined;

          const verificationAnswers = await prompts([
            {
              type: "text",
              name: "apiKey",
              message: "Explorer API key override (leave blank to use config)",
            },
            {
              type: "text",
              name: "chain",
              message: "Explorer chain override (leave blank to use config)",
              initial: networkConfig.verification?.chain ?? "",
            },
            {
              type: "confirm",
              name: "watch",
              message: "Enable --watch during verification?",
              initial: watchInitial ?? false,
            },
            {
              type: "confirm",
              name: "confirm",
              message: `Verify ${alias} on ${network}?`,
              initial: true,
            },
          ]);

          if (verificationAnswers.confirm === false) {
            // eslint-disable-next-line no-console
            console.log(chalk.gray("Verification cancelled"));
            break;
          }

          const apiKeyOverride = verificationAnswers.apiKey?.trim()
            ? String(verificationAnswers.apiKey).trim()
            : undefined;
          const chainOverride = verificationAnswers.chain?.trim()
            ? String(verificationAnswers.chain).trim()
            : undefined;

          let watchOverride: boolean | undefined;
          if (typeof verificationAnswers.watch === "boolean") {
            if (watchInitial === undefined) {
              watchOverride = verificationAnswers.watch ? true : undefined;
            } else if (verificationAnswers.watch !== watchInitial) {
              watchOverride = verificationAnswers.watch;
            }
          }

          try {
            const context = await buildVerifyContext({
              alias,
              customer: currentCustomer,
              network,
              apiKey: apiKeyOverride,
              chain: chainOverride,
              watch: watchOverride,
            });

            const cliParts = [
              `forge-wrapper verify ${alias}`,
              `--customer ${currentCustomer}`,
              `--network ${network}`,
            ];
            if (apiKeyOverride) {
              cliParts.push("--api-key <provided>");
            }
            if (chainOverride) {
              cliParts.push(`--chain ${chainOverride}`);
            }
            const shouldWatch =
              typeof watchOverride === "boolean"
                ? watchOverride
                : watchInitial === true;
            if (shouldWatch) {
              cliParts.push("--watch");
            }
            logEquivalent(cliParts.join(" "));

            await verifyDeployment(context);
            // eslint-disable-next-line no-console
            console.log(
              chalk.green(
                `Verification submitted for ${selectedRecord.alias} (${selectedRecord.address})`
              )
            );
          } catch (error: any) {
            // eslint-disable-next-line no-console
            console.error(chalk.red(error?.message ?? error));
          }
          break;
        }
        case "listNetworks": {
          const networks = await listNetworkNames();
          if (networks.length === 0) {
            // eslint-disable-next-line no-console
            console.log(chalk.gray("No networks defined"));
          } else {
            for (const net of networks) {
              try {
                const config = await loadNetworkConfig(net);
                const classification = classifyNetwork(config);
                const suffix = classification === "unknown" ? "" : ` (${classification})`;
                // eslint-disable-next-line no-console
                console.log(`- ${net} :: chainId=${config.chain_id}${suffix}`);
              } catch (error) {
                // eslint-disable-next-line no-console
                console.log(`- ${net}`);
              }
            }
          }
          logEquivalent("forge-wrapper networks list --details");
          break;
        }
        case "addNetwork": {
          await interactiveAddNetwork();
          logEquivalent("Add/edit YAML under networks/<name>.yaml (no CLI helper yet)");
          break;
        }
        case "listKeys": {
          const keys = await listKeyAliases(currentCustomer);
          if (keys.length === 0) {
            // eslint-disable-next-line no-console
            console.log(chalk.gray(`No keys for ${currentCustomer}`));
          } else {
            // eslint-disable-next-line no-console
            console.log(keys.map((k) => `- ${k}`).join("\n"));
          }
          logEquivalent(`forge-wrapper keys list --customer ${currentCustomer}`);
          break;
        }
        case "listDeployments": {
          const network = currentNetwork || (await promptSelectNetwork());
          if (!network) {
            // eslint-disable-next-line no-console
            console.log(chalk.gray("No network selected"));
            break;
          }
          const deployments = await listDeployments(currentCustomer, network);
          if (deployments.length === 0) {
            // eslint-disable-next-line no-console
            console.log(chalk.gray(`No deployments for ${currentCustomer}`));
          } else {
            for (const entry of deployments) {
              // eslint-disable-next-line no-console
              console.log(`${entry.network} :: ${entry.alias} -> ${entry.address} (${entry.deployedAt})`);
            }
          }
          logEquivalent(`forge-wrapper deployments list --customer ${currentCustomer} --network ${network ?? "<network>"}`);
          break;
        }
        case "switchCustomer": {
          const next = await promptCustomer(currentCustomer);
          if (next) {
            currentCustomer = next;
            await prepareCustomerEnvironment(currentCustomer);
            // eslint-disable-next-line no-console
            console.log(chalk.green(`Customer set to ${currentCustomer}`));
            logEquivalent(`FORGE_WRAPPER_CUSTOMER=${currentCustomer} forge-wrapper ...`);
          }
          break;
        }
        case "switchNetwork": {
          currentNetwork = await promptSelectNetwork();
          if (currentNetwork) {
            // eslint-disable-next-line no-console
            console.log(chalk.green(`Network set to ${currentNetwork}`));
            logEquivalent(`FORGE_WRAPPER_NETWORK=${currentNetwork} forge-wrapper ...`);
          }
          break;
        }
        default:
          break;
      }
    } catch (error: any) {
      // eslint-disable-next-line no-console
      console.error(chalk.red(error?.message ?? error));
    }
  }
}

async function promptCustomer(current: string): Promise<string | undefined> {
  const customers = await listCustomers();
  const unique = new Set(customers);
  if (current) {
    unique.add(current);
  }
  const options = Array.from(unique).sort();

  if (options.length === 0) {
    const text = await prompts({
      type: "text",
      name: "customer",
      message: "Customer namespace",
      initial: current,
    });
    return text.customer ? String(text.customer).trim() : undefined;
  }

  const select = await prompts({
    type: "select",
    name: "choice",
    message: "Select customer",
    choices: [
      ...options.map((name) => ({ title: name, value: name })),
      { title: "Create new customer", value: "__new__" },
    ],
    initial: Math.max(options.indexOf(current), 0),
  });

  if (!select.choice) {
    return undefined;
  }

  if (select.choice === "__new__") {
    const text = await prompts({
      type: "text",
      name: "customer",
      message: "New customer name",
      validate: (value: string) => (value && value.trim() ? true : "Required"),
    });
    return text.customer ? String(text.customer).trim() : undefined;
  }

  return String(select.choice);
}

async function promptSelectNetwork(initial?: string): Promise<string | undefined> {
  const networks = await listNetworkNames();
  if (networks.length === 0) {
    // eslint-disable-next-line no-console
    console.log(chalk.yellow("No networks defined yet. Use 'Add network' first."));
    return undefined;
  }
  const { network } = await prompts({
    type: "select",
    name: "network",
    message: "Select network",
    choices: networks.map((name) => ({ title: name, value: name })),
    initial: Math.max(networks.indexOf(initial ?? ""), 0),
  });
  return network;
}

async function listCustomers(): Promise<string[]> {
  const customers = new Set<string>();
  const projectRoot = getProjectRoot();
  const candidateDirs = [path.join(projectRoot, "keys"), path.join(projectRoot, "deployments")];

  for (const dir of candidateDirs) {
    try {
      const entries = await fs.readdir(dir, { withFileTypes: true });
      for (const entry of entries) {
        if (entry.isDirectory()) {
          customers.add(entry.name);
        }
      }
    } catch (error: any) {
      if (error && error.code === "ENOENT") {
        continue;
      }
      throw error;
    }
  }

  return Array.from(customers).sort();
}

const ADDRESS_REGEX = /^0x[0-9a-fA-F]{40}$/;
const TESTNET_KEYWORDS = [
  "test",
  "dev",
  "sepolia",
  "goerli",
  "holesky",
  "mumbai",
  "chiado",
  "fuji",
  "optimism-sepolia",
  "arbitrum-sepolia",
  "zkevm-test",
  "sandbox",
  "staging",
];
const MAINNET_KEYWORDS = ["mainnet", "l1", "production"];

function classifyNetwork(config: NetworkConfig): "mainnet" | "testnet" | "unknown" {
  const name = config.name.toLowerCase();
  if (config.chain_id === 1 || MAINNET_KEYWORDS.some((keyword) => name.includes(keyword))) {
    return "mainnet";
  }
  if (
    TESTNET_KEYWORDS.some((keyword) => name.includes(keyword)) ||
    String(config.chain_id).startsWith("10") ||
    String(config.chain_id).startsWith("42")
  ) {
    return "testnet";
  }
  return "unknown";
}

async function buildPushOracleArgs(
  args: string[],
  networkConfig: NetworkConfig,
  customer: string,
  network: string,
  aliasLabel: string
): Promise<string[]> {
  const template = getTemplate("PushOracleReceiverV2");
  let domainName = args[0] ?? template?.args?.[0] ?? "DIA Oracle";
  let domainVersion = args[1] ?? template?.args?.[1] ?? "1.0";

  const defaultChain = networkConfig.chain_id ? String(networkConfig.chain_id) : "";
  let chainId = args[2] ?? defaultChain;
  const chainResponse = await prompts({
    type: "text",
    name: "chainId",
    message: `Source chain id for ${aliasLabel}`,
    initial: chainId,
    validate: (value: string) => (value && /^\d+$/.test(value.trim()) ? true : "Enter numeric chain id"),
  });
  if (!chainResponse.chainId) {
    throw new Error("Deployment cancelled (no chain id provided).");
  }
  chainId = String(chainResponse.chainId).trim();

  let registryAddress = args[3] && ADDRESS_REGEX.test(args[3]) ? args[3] : undefined;
  if (!registryAddress) {
    registryAddress = await selectOracleRegistryAddress(customer, network);
  }

  return [domainName, domainVersion, chainId, registryAddress];
}

async function selectOracleRegistryAddress(customer: string, targetNetwork: string): Promise<string> {
  const candidates = await collectOracleRegistryCandidates(customer);
  const ordered = [
    ...candidates.filter((candidate) => candidate.network === targetNetwork),
    ...candidates.filter((candidate) => candidate.network !== targetNetwork),
  ];

  if (ordered.length === 0) {
    return await promptForAddress("OracleIntentRegistry address (no deployments found)");
  }

  const { registry } = await prompts({
    type: "select",
    name: "registry",
    message: "Select OracleIntentRegistry address",
    choices: [
      ...ordered.map((candidate) => ({
        title: `${candidate.address} (${candidate.network})`,
        value: candidate.address,
      })),
      { title: "Enter manually", value: "__manual__" },
    ],
    initial: 0,
  });

  if (!registry) {
    throw new Error("Deployment cancelled (no OracleIntentRegistry selected).");
  }

  if (registry === "__manual__") {
    return await promptForAddress("OracleIntentRegistry address");
  }

  return String(registry);
}

async function promptForAddress(message: string): Promise<string> {
  const response = await prompts({
    type: "text",
    name: "address",
    message,
    validate: (value: string) => (ADDRESS_REGEX.test(value.trim()) ? true : "Enter a 0x-prefixed address"),
  });
  if (!response.address) {
    throw new Error("Deployment cancelled (no address provided).");
  }
  return String(response.address).trim();
}

async function collectOracleRegistryCandidates(customer: string): Promise<
  Array<{ network: string; address: string }>
> {
  const networks = await listNetworkNames();
  const seen = new Map<string, { network: string; address: string }>();

  for (const net of networks) {
    const { current, history } = await loadDeployments(customer, net);
    const record = current["OracleIntentRegistry"];
    if (record?.address && ADDRESS_REGEX.test(record.address)) {
      seen.set(record.address.toLowerCase(), { network: net, address: record.address });
    }
    for (const entry of history) {
      if (entry.alias === "OracleIntentRegistry" && entry.address && ADDRESS_REGEX.test(entry.address)) {
        if (!seen.has(entry.address.toLowerCase())) {
          seen.set(entry.address.toLowerCase(), { network: net, address: entry.address });
        }
      }
    }
  }

  if (seen.size === 0 && customer !== "master") {
    const masterNetworks = await listNetworkNames();
    for (const net of masterNetworks) {
      const { current, history } = await loadDeployments("master", net);
      const record = current["OracleIntentRegistry"];
      if (record?.address && ADDRESS_REGEX.test(record.address)) {
        seen.set(record.address.toLowerCase(), { network: `${net} (master)`, address: record.address });
      }
      for (const entry of history) {
        if (entry.alias === "OracleIntentRegistry" && entry.address && ADDRESS_REGEX.test(entry.address)) {
          if (!seen.has(entry.address.toLowerCase())) {
            seen.set(entry.address.toLowerCase(), { network: `${net} (master)`, address: entry.address });
          }
        }
      }
    }
  }

  return Array.from(seen.values());
}

async function promptWalletName(customer: string): Promise<string> {
  const { name } = await prompts({
    type: "text",
    name: "name",
    message: "Wallet alias (leave blank for autogenerated)",
  });
  if (name && String(name).trim()) {
    return String(name).trim();
  }
  return `wallet-${Date.now()}`;
}

async function interactiveAddNetwork(): Promise<void> {
  const answers = await prompts([
    {
      type: "text",
      name: "name",
      message: "Network name",
      validate: (value: string) => (value && value.trim() ? true : "Required"),
    },
    {
      type: "number",
      name: "chainId",
      message: "Chain ID",
      validate: (value: number) => (Number.isInteger(value) ? true : "Enter a valid integer"),
    },
    {
      type: "text",
      name: "rpcUrl",
      message: "RPC URL",
      validate: (value: string) => (value && value.trim() ? true : "Required"),
    },
    {
      type: "text",
      name: "forgeProfile",
      message: "Forge profile (optional)",
    },
    {
      type: "confirm",
      name: "addDefaultAccount",
      message: "Add default deployer account alias?",
      initial: true,
    },
    {
      type: (prev: boolean) => (prev ? "text" : null),
      name: "defaultAlias",
      message: "Default account alias",
      initial: "deployer",
    },
  ]);

  if (!answers.name) {
    return;
  }

  const filePath = await createNetworkConfig({
    name: answers.name,
    chainId: Number(answers.chainId),
    rpcUrl: String(answers.rpcUrl),
    forgeProfile: answers.forgeProfile ? String(answers.forgeProfile) : undefined,
    defaultAccountAlias: answers.addDefaultAccount ? String(answers.defaultAlias || "deployer") : undefined,
  });
  // eslint-disable-next-line no-console
  console.log(chalk.green(`Created network config at ${filePath}`));
}

async function interactiveDeploy(customer: string, network: string): Promise<void> {
  await prepareCustomerEnvironment(customer);
  const networkConfig = await loadNetworkConfig(network);
  const forgeProfile = networkConfig.forge_profile;
  const keys = await listKeyAliases(customer);
  const accountChoices = Object.keys(networkConfig.accounts ?? {});

  interface DeployAnswers {
    account?: string;
    key?: string;
    artifact?: string;
    constructorArgs?: string[] | string;
    confirm?: boolean;
  }

  const aliasChoices = buildPresetChoices(networkConfig.default_contracts);

  const aliasAnswer = await prompts({
    type: "select",
    name: "alias",
    message: "Deployment alias",
    choices: aliasChoices,
    initial: 0,
  });

  let selectedAlias = String(aliasAnswer.alias || "").trim();
  let aliasName = selectedAlias;
  let artifactPreset = aliasName && aliasName !== "__custom__" ? networkConfig.default_contracts?.[aliasName] : undefined;

  if (selectedAlias === "__custom__" || !aliasName) {
    const customAlias = await prompts({
      type: "text",
      name: "customAlias",
      message: "Enter deployment alias",
      validate: (value: string) => (value && value.trim() ? true : "Required"),
    });
    aliasName = String(customAlias.customAlias || "").trim();
    artifactPreset = undefined;
  }

  const aliasLabel = aliasName || "(custom)";

  const answers = (await prompts([
    {
      type: "text",
      name: "artifact",
      message: () => `Forge artifact for ${aliasLabel} (optional)`,
      initial: artifactPreset,
    },
    {
      type: "list",
      name: "constructorArgs",
      message: "Constructor arguments (comma separated, leave blank for none)",
      separator: ",",
      initial: (getTemplate(aliasName)?.args ?? []).join(","),
    },
    {
      type: "confirm",
      name: "confirm",
      message: () => `Deploy ${aliasLabel} to ${network}?`,
      initial: true,
    },
  ])) as DeployAnswers;

  if (answers.confirm === false) {
    // eslint-disable-next-line no-console
    console.log(chalk.gray("Deployment cancelled"));
    return;
  }

  const alias = aliasName;

  let artifact = answers.artifact ? String(answers.artifact).trim() : "";
  if (!artifact) {
    const preset = getPreset(alias);
    if (preset) {
      artifact = preset.artifact;
    } else {
      const { artifactInput } = await prompts({
        type: "text",
        name: "artifactInput",
        message: `Forge artifact for ${aliasLabel} (e.g. contracts/Path.sol:Contract)`,
        validate: (value: string) => (value && value.includes(":") ? true : "Format should be path:Contract"),
      });
      if (!artifactInput) {
        // eslint-disable-next-line no-console
        console.log(chalk.gray("Deployment cancelled (no artifact provided)."));
        return;
      }
      artifact = String(artifactInput).trim();
    }
  }

  let sanitizedArgs = Array.isArray(answers.constructorArgs)
    ? answers.constructorArgs
    : typeof answers.constructorArgs === "string" && answers.constructorArgs.length > 0
    ? [answers.constructorArgs]
    : [];
  sanitizedArgs = sanitizedArgs.map((arg: string) => arg.trim()).filter((arg) => arg.length > 0);

  if (alias === "PushOracleReceiverV2") {
    sanitizedArgs = await buildPushOracleArgs(
      sanitizedArgs,
      networkConfig,
      customer,
      network,
      aliasLabel
    );
  }

  let availableKeys = await listKeyAliases(customer);
  if (availableKeys.length === 0) {
    const createKeyAnswer = await prompts({
      type: "confirm",
      name: "createKey",
      message: `No keys found for ${customer}. Create a new wallet now?`,
      initial: true,
    });
    if (!createKeyAnswer.createKey) {
      // eslint-disable-next-line no-console
      console.log(chalk.gray("Deployment cancelled (no keys available)."));
      return;
    }
    const newName = await promptWalletName(customer);
    const newKey = generatePrivateKey();
    const info = await storePrivateKey(customer, newName, newKey, false);
    // eslint-disable-next-line no-console
    console.log(chalk.green(`Stored wallet '${newName}' for ${customer}.`));
    // eslint-disable-next-line no-console
    console.log(chalk.gray(`metadata: ${info.metadataPath}`));
    if (info.address) {
      // eslint-disable-next-line no-console
      console.log(chalk.gray(`address: ${info.address}`));
    }
    logEquivalent(
      `forge-wrapper keys import --customer ${customer} --name ${newName} --value <PRIVATE_KEY_HEX>`
    );
    availableKeys = await listKeyAliases(customer);
  }

  const { chosenKey } = await prompts({
    type: "select",
    name: "chosenKey",
    message: "Select a key to use",
    choices: availableKeys.map((key) => ({ title: key, value: key })),
    initial: Math.max(availableKeys.indexOf("deployer"), 0),
  });
  if (!chosenKey) {
    // eslint-disable-next-line no-console
    console.log(chalk.gray("Deployment cancelled (no key selected)."));
    return;
  }

  const resolvedAccount = String(chosenKey);
  const walletMeta = await readStoredWallet(customer, resolvedAccount);
  const privateKey = walletMeta.privateKey;
  const deployerAddress = walletMeta.address;

  const deployRecord = await executeDeploy({
    alias,
    artifact,
    constructorArgs: sanitizedArgs,
    customer,
    network,
    account: resolvedAccount,
    rpcUrl: undefined,
    dryRun: false,
    salt: undefined,
    privateKeyOverride: privateKey,
    deployerAddress,
  });

  await recordDeployment(customer, network, deployRecord);
  // eslint-disable-next-line no-console
  console.log(chalk.green(`Deployment successful: ${deployRecord.address}`));
  if (deployRecord.txHash) {
    // eslint-disable-next-line no-console
    console.log(chalk.gray(`tx: ${deployRecord.txHash}`));
  }
  if (deployerAddress) {
    // eslint-disable-next-line no-console
    console.log(chalk.gray(`deployer ${resolvedAccount}: ${deployerAddress}`));
  }
  const argsList = deployRecord.constructorArgs.length
    ? deployRecord.constructorArgs
        .map((arg) => ` --constructor-arg ${JSON.stringify(arg)}`)
        .join("")
    : "";
  const envPrefix = forgeProfile ? `FOUNDRY_PROFILE=${forgeProfile} ` : "";
  logEquivalent(
    `${envPrefix}forge-wrapper deploy ${deployRecord.alias} --customer ${customer} --network ${network} --account ${resolvedAccount} --artifact ${artifact}${argsList}`
  );

  if (networkConfig.verification) {
    const { verify } = await prompts({
      type: "confirm",
      name: "verify",
      message: "Verify contract on explorer?",
      initial: true,
    });
    if (verify) {
      try {
        const context = await buildVerifyContext({
          alias,
          customer,
          network,
        });
        await verifyDeployment(context);
      } catch (error: any) {
        // eslint-disable-next-line no-console
        console.error(chalk.red(`Verification failed: ${error?.message ?? error}`));
      }
    }
  }
}
