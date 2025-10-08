import prompts from "prompts";
import type { PromptObject } from "prompts";
import chalk from "chalk";
import path from "path";
import { promises as fs } from "fs";
import {
  generatePrivateKey,
  listKeyAliases,
  listKeySummaries,
  storePrivateKey,
  readStoredWallet,
  StoredWalletMeta,
} from "./services/keys";
import { prepareCustomerEnvironment, loadNetworkConfig } from "./config";
import {
  getDefaultCustomer,
  getDefaultNetwork,
  getProjectRoot,
  normalizeNetworkFileName,
} from "./utils/paths";
import { listNetworkNames, createNetworkConfig } from "./services/networks";
import { listDeployments } from "./services/deployments";
import { executeDeploy } from "./commands/deploy";
import { buildVerifyContext, verifyDeployment } from "./commands/verify";
import {
  executeContractSend,
  loadContractFunctions,
  ContractFunctionFragment,
} from "./commands/configure";
import { registerOracleIntent, submitIntentToReceiver, compareDomainSeparators } from "./commands/intents";
import { getDeployment, recordDeployment } from "./deployments";
import { buildPresetChoices, getPreset } from "./utils/contracts";
import { getTemplate } from "./utils/templates";
import { loadDeployments } from "./deployments";
import { NetworkConfig, DeploymentRecord } from "./types";
import { formatCommand, runCast } from "./utils/forge";
import { defaultOracleIntentInput } from "./utils/intents";

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

type PromptInput = PromptObject | PromptObject[];

function formatContextValue(value?: string | null): string {
  if (typeof value === "string" && value.trim().length > 0) {
    return value;
  }
  return "(not set)";
}

function deriveScope(questions: PromptInput): string | undefined {
  const first = Array.isArray(questions) ? questions[0] : questions;
  if (typeof first?.message === "string" && first.message.trim().length > 0) {
    return first.message.trim();
  }
  return undefined;
}

function displayContext(
  customer: string | undefined,
  network: string | undefined,
  scope?: string
): void {
  const contextLine = [
    `customer: ${formatContextValue(customer)}`,
    `network: ${formatContextValue(network)}`,
  ].join(" | ");
  const suffix = scope ? ` — ${scope}` : "";
  // eslint-disable-next-line no-console
  console.log(chalk.gray(`[${contextLine}]${suffix}`));
}

async function promptWithContext(
  customer: string | undefined,
  network: string | undefined,
  questions: PromptInput,
  scope?: string
): Promise<any> {
  const derivedScope = scope ?? deriveScope(questions);
  displayContext(customer, network, derivedScope);
  return prompts(questions as any);
}

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
        title: "Configure contract",
        description: "Run post-deployment actions",
        value: "configure",
      },
      {
        title: "Intent tools",
        description: "Register / relay oracle intents",
        value: "intentTools",
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

    const { action } = await promptWithContext(currentCustomer, currentNetwork, {
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
          const name = await promptWalletName(currentCustomer, currentNetwork);
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
          const selectedNetwork = await promptSelectNetwork({
            customer: currentCustomer,
            currentNetwork,
            scope: "Select network for deployment",
          });
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
          const network =
            currentNetwork ||
            (await promptSelectNetwork({
              customer: currentCustomer,
              currentNetwork,
              scope: "Select network for verification",
            }));
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

          const aliasAnswer = await promptWithContext(currentCustomer, network, {
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

          const verificationAnswers = await promptWithContext(currentCustomer, network, [
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
        case "configure": {
          const nextNetwork = await interactiveConfigure(currentCustomer, currentNetwork);
          if (nextNetwork) {
            currentNetwork = nextNetwork;
          }
          break;
        }
        case "intentTools": {
          const nextNetwork = await interactiveIntentTools(currentCustomer, currentNetwork);
          if (nextNetwork) {
            currentNetwork = nextNetwork;
          }
          break;
        }
        case "listNetworks": {
          const networks = await listNetworkNames();
          if (networks.length === 0) {
            // eslint-disable-next-line no-console
            console.log(chalk.gray("No networks defined"));
          } else {
            const filterAnswer = await promptWithContext(currentCustomer, currentNetwork, {
              type: "select",
              name: "env",
              message: "Select network environment",
              choices: [
                { title: "All", value: "all" },
                { title: "Mainnet", value: "mainnet" },
                { title: "Testnet", value: "testnet" },
              ],
              initial: 0,
            });
            const filterEnv = filterAnswer.env ?? "all";
            for (const net of networks) {
              try {
                const config = await loadNetworkConfig(net);
                const classification = classifyNetwork(config);
                if (filterEnv === "mainnet" && classification !== "mainnet") {
                  continue;
                }
                if (filterEnv === "testnet" && classification !== "testnet") {
                  continue;
                }
                const suffix = classification === "unknown" ? "" : ` (${classification})`;
                // eslint-disable-next-line no-console
                console.log(`- ${net} :: chainId=${config.chain_id}${suffix}`);
              } catch (error) {
                // eslint-disable-next-line no-console
                console.log(`- ${net}`);
              }
            }
            const filterFlag = filterEnv !== "all" ? ` --filter ${filterEnv}` : "";
            logEquivalent(`forge-wrapper networks list${filterFlag}`);
            break;
          }
          logEquivalent("forge-wrapper networks list");
          break;
        }
        case "addNetwork": {
          await interactiveAddNetwork(currentCustomer, currentNetwork);
          logEquivalent("Add/edit YAML under networks/<name>.yaml (no CLI helper yet)");
          break;
        }
        case "listKeys": {
          const keys = await listKeySummaries(currentCustomer);
          if (keys.length === 0) {
            // eslint-disable-next-line no-console
            console.log(chalk.gray(`No keys for ${currentCustomer}`));
          } else {
            for (const key of keys) {
              const addressLabel = key.address ?? chalk.gray("(address unknown)");
              // eslint-disable-next-line no-console
              console.log(`- ${key.alias}   ( ${addressLabel} )`);
            }
          }
          logEquivalent(`forge-wrapper keys list --customer ${currentCustomer}`);
          break;
        }
        case "listDeployments": {
          const network =
            currentNetwork ||
            (await promptSelectNetwork({
              customer: currentCustomer,
              currentNetwork,
              scope: "Select network for deployments list",
            }));
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
          const next = await promptCustomer(currentCustomer, currentNetwork);
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
          currentNetwork = await promptSelectNetwork({
            customer: currentCustomer,
            currentNetwork,
            scope: "Switch network",
          });
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

async function promptCustomer(current: string, network?: string): Promise<string | undefined> {
  const customers = await listCustomers();
  const unique = new Set(customers);
  if (current) {
    unique.add(current);
  }
  const options = Array.from(unique).sort();

  if (options.length === 0) {
    const text = await promptWithContext(current, network, {
      type: "text",
      name: "customer",
      message: "Customer namespace",
      initial: current,
    });
    return text.customer ? String(text.customer).trim() : undefined;
  }

  const select = await promptWithContext(current, network, {
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
    const text = await promptWithContext(current, network, {
      type: "text",
      name: "customer",
      message: "New customer name",
      validate: (value: string) => (value && value.trim() ? true : "Required"),
    });
    return text.customer ? String(text.customer).trim() : undefined;
  }

  return String(select.choice);
}

interface PromptSelectNetworkOptions {
  customer?: string;
  currentNetwork?: string;
  scope?: string;
}

async function promptSelectNetwork(
  options: PromptSelectNetworkOptions = {}
): Promise<string | undefined> {
  const { customer, currentNetwork, scope } = options;
  const networks = await listNetworkNames();
  if (networks.length === 0) {
    // eslint-disable-next-line no-console
    console.log(chalk.yellow("No networks defined yet. Use 'Add network' first."));
    return undefined;
  }
  const { network } = await promptWithContext(customer, currentNetwork, {
    type: "select",
    name: "network",
    message: "Select network",
    choices: networks.map((name) => ({ title: name, value: name })),
    initial: Math.max(networks.indexOf(currentNetwork ?? ""), 0),
  }, scope ?? "Select network");
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

function classifyNetwork(config: NetworkConfig): "mainnet" | "testnet" | "unknown" {
  if (config.environment === "mainnet") {
    return "mainnet";
  }
  if (config.environment === "testnet") {
    return "testnet";
  }
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
  return "testnet";
}

function contractNameFromArtifact(artifact: string): string {
  const parts = (artifact || "").split(":");
  const name = parts[parts.length - 1]?.trim();
  if (!name) {
    throw new Error(`Unable to resolve contract name from artifact '${artifact}'`);
  }
  return name;
}

async function interactiveConfigure(
  customer: string,
  currentNetwork?: string
): Promise<string | undefined> {
  const network =
    currentNetwork ||
    (await promptSelectNetwork({
      customer,
      currentNetwork,
      scope: "Select network for configuration",
    }));
  if (!network) {
    // eslint-disable-next-line no-console
    console.log(chalk.yellow("No network selected"));
    return currentNetwork;
  }

  await prepareCustomerEnvironment(customer);
  const deploymentsFile = await loadDeployments(customer, network);
  const records = Object.values(deploymentsFile.current ?? {});
  if (records.length === 0) {
    // eslint-disable-next-line no-console
    console.log(chalk.gray(`No deployments for ${customer} on ${network}`));
    return network;
  }

  const aliasChoices = records.map((record) => ({
    title: `${record.alias} -> ${record.address}`,
    description: record.artifact,
    value: record.alias,
  }));

  const aliasAnswer = await promptWithContext(customer, network, {
    type: "select",
    name: "alias",
    message: "Select contract to configure",
    choices: aliasChoices,
    initial: 0,
  });
  const alias = aliasAnswer.alias ? String(aliasAnswer.alias) : undefined;
  if (!alias) {
    // eslint-disable-next-line no-console
    console.log(chalk.gray("Configuration cancelled (no alias selected)."));
    return network;
  }

  const deployment = deploymentsFile.current[alias];
  if (!deployment) {
    // eslint-disable-next-line no-console
    console.log(chalk.red(`Deployment record for ${alias} not found.`));
    return network;
  }

  let contractName: string;
  try {
    contractName = contractNameFromArtifact(deployment.artifact);
  } catch (error: any) {
    // eslint-disable-next-line no-console
    console.error(chalk.red(error?.message ?? error));
    return network;
  }

  let functions: ContractFunctionFragment[];
  try {
    functions = await loadContractFunctions(deployment.artifact);
  } catch (error: any) {
    // eslint-disable-next-line no-console
    console.error(chalk.red(error?.message ?? error));
    return network;
  }

  const readFunctions = functions.filter((fn) => fn.constant);
  const writeFunctions = functions.filter((fn) => !fn.constant);

  if (readFunctions.length === 0 && writeFunctions.length === 0) {
    // eslint-disable-next-line no-console
    console.log(chalk.gray(`No callable functions discovered for ${contractName}.`));
    return network;
  }

  const functionChoices: { title: string; description?: string; value: { kind: "read" | "write"; fn: ContractFunctionFragment } }[] = [];

  for (const fn of writeFunctions) {
    functionChoices.push({
      title: `Write · ${formatFunctionLabel(fn)}`,
      description: formatFunctionDescription(fn),
      value: { kind: "write", fn },
    });
  }
  for (const fn of readFunctions) {
    functionChoices.push({
      title: `Read · ${formatFunctionLabel(fn)}`,
      description: formatFunctionDescription(fn),
      value: { kind: "read", fn },
    });
  }

  const functionAnswer = await promptWithContext(customer, network, {
    type: "select",
    name: "selection",
    message: `Select function on ${alias} (${contractName})`,
    choices: functionChoices,
    initial: 0,
  });

  const selection = functionAnswer.selection as { kind: "read" | "write"; fn: ContractFunctionFragment } | undefined;
  if (!selection) {
    // eslint-disable-next-line no-console
    console.log(chalk.gray("Configuration cancelled (no function selected)."));
    return network;
  }

  try {
    if (selection.kind === "read") {
      await handleReadFunction({ customer, network, deployment, fragment: selection.fn });
    } else {
      await handleWriteFunction({ customer, network, alias, fragment: selection.fn });
    }
  } catch (error: any) {
    // eslint-disable-next-line no-console
    console.error(chalk.red(error?.message ?? error));
  }

  return network;
}

async function interactiveRegisterIntent(
  customer: string,
  currentNetwork?: string
): Promise<string | undefined> {
  const network =
    currentNetwork ||
    (await promptSelectNetwork({
      customer,
      currentNetwork,
      scope: "Select network for intent registration",
    }));
  if (!network) {
    // eslint-disable-next-line no-console
    console.log(chalk.yellow("No network selected"));
    return currentNetwork;
  }

  await prepareCustomerEnvironment(customer);
  const deploymentsFile = await loadDeployments(customer, network);
  const registryAliases = Object.keys(deploymentsFile.current ?? {});

  const defaultAlias = registryAliases.includes("OracleIntentRegistry")
    ? "OracleIntentRegistry"
    : registryAliases[0] ?? "OracleIntentRegistry";

  const aliasAnswer = await promptWithContext(customer, network, {
    type: "select",
    name: "alias",
    message: "OracleIntentRegistry alias",
    choices: registryAliases.length
      ? registryAliases.map((alias) => ({ title: alias, value: alias }))
      : [{ title: defaultAlias, value: defaultAlias }],
    initial: Math.max(registryAliases.indexOf(defaultAlias), 0),
  });

  const registryAlias = aliasAnswer.alias ? String(aliasAnswer.alias) : defaultAlias;

  const nowSeconds = Math.floor(Date.now() / 1000);
  const defaults = defaultOracleIntentInput("BTC");

  const answers = await promptWithContext(customer, network, [
    {
      type: "text",
      name: "symbol",
      message: "Oracle symbol",
      initial: "BTC",
      validate: (value: string) => (value && value.trim().length > 0 ? true : "Required"),
    },
    {
      type: "text",
      name: "price",
      message: "Price (wei)",
      initial: "0",
    },
    {
      type: "text",
      name: "timestamp",
      message: "Timestamp (seconds)",
      initial: String(nowSeconds),
    },
    {
      type: "text",
      name: "expiry",
      message: "Expiry timestamp (seconds)",
      initial: String(nowSeconds + 3600),
    },
    {
      type: "text",
      name: "nonce",
      message: "Nonce",
      initial: String(nowSeconds),
    },
    {
      type: "text",
      name: "intentType",
      message: "Intent type",
      initial: defaults.intentType,
    },
    {
      type: "text",
      name: "version",
      message: "Version",
      initial: defaults.version,
    },
    {
      type: "text",
      name: "source",
      message: "Source label",
      initial: defaults.source,
    },
  ]);

  if (!answers.symbol) {
    // eslint-disable-next-line no-console
    console.log(chalk.gray("Intent registration cancelled."));
    return network;
  }

  const networkConfig = await loadNetworkConfig(network);
  const intentSignerAlias = await promptAccountAlias(
    customer,
    networkConfig,
    "Intent signer alias",
    network
  );
  if (!intentSignerAlias) {
    // eslint-disable-next-line no-console
    console.log(chalk.gray("Intent registration cancelled (no signer)."));
    return network;
  }

  const txSameAnswer = await promptWithContext(customer, network, {
    type: "confirm",
    name: "same",
    message: "Use same signer for transaction submission?",
    initial: true,
  });

  let txSignerAlias: string = intentSignerAlias;
  if (txSameAnswer.same === false) {
    const altTxAlias = await promptAccountAlias(
      customer,
      networkConfig,
      "Transaction signer alias",
      network
    );
    if (!altTxAlias) {
      // eslint-disable-next-line no-console
      console.log(chalk.gray("Intent registration cancelled (no transaction signer)."));
      return network;
    }
    txSignerAlias = altTxAlias;
  }

  try {
    await registerOracleIntent({
      alias: registryAlias,
      customer,
      network,
      signer: intentSignerAlias,
      txSigner: txSignerAlias,
      symbol: String(answers.symbol).trim(),
      price: answers.price ? String(answers.price).trim() : undefined,
      timestamp: answers.timestamp ? String(answers.timestamp).trim() : undefined,
      expiry: answers.expiry ? String(answers.expiry).trim() : undefined,
      nonce: answers.nonce ? String(answers.nonce).trim() : undefined,
      intentType: answers.intentType ? String(answers.intentType).trim() : undefined,
      version: answers.version ? String(answers.version).trim() : undefined,
      source: answers.source ? String(answers.source).trim() : undefined,
    });
  } catch (error: any) {
    // eslint-disable-next-line no-console
    console.error(chalk.red(error?.message ?? error));
  }

  return network;
}

async function interactiveHandleIntent(
  customer: string,
  currentNetwork?: string
): Promise<string | undefined> {
  let registryNetworkCandidate = currentNetwork;
  if (!registryNetworkCandidate) {
    registryNetworkCandidate = await promptSelectNetwork({
      customer,
      currentNetwork,
      scope: "Select registry network",
    });
  }
  if (!registryNetworkCandidate) {
    // eslint-disable-next-line no-console
    console.log(chalk.yellow("No registry network selected"));
    return currentNetwork;
  }
  const registryNetwork: string = registryNetworkCandidate!;

  const differentAnswer = await promptWithContext(customer, registryNetworkCandidate, {
    type: "confirm",
    name: "different",
    message: "Use different network for receiver?",
    initial: false,
  });

  let receiverNetworkCandidate: string | undefined = registryNetwork;
  if (differentAnswer.different) {
    receiverNetworkCandidate = await promptSelectNetwork({
      customer,
      currentNetwork: registryNetwork,
      scope: "Select receiver network",
    });
    if (!receiverNetworkCandidate) {
      // eslint-disable-next-line no-console
      console.log(chalk.yellow("No receiver network selected"));
      return registryNetwork;
    }
  }
  if (!receiverNetworkCandidate) {
    return registryNetwork;
  }
  const receiverNetwork = receiverNetworkCandidate;

  await prepareCustomerEnvironment(customer);
  const registryDeployments = await loadDeployments(customer, registryNetwork);
  const receiverDeployments = registryNetwork === receiverNetwork
    ? registryDeployments
    : await loadDeployments(customer, receiverNetwork);

  const registryAliases = Object.keys(registryDeployments.current ?? {});
  const receiverAliases = Object.keys(receiverDeployments.current ?? {});

  const defaultRegistry = registryAliases.includes("OracleIntentRegistry")
    ? "OracleIntentRegistry"
    : registryAliases[0];
  const defaultReceiver = receiverAliases.includes("PushOracleReceiverV2")
    ? "PushOracleReceiverV2"
    : receiverAliases[0];

  const registryManualValue = "__manual_registry__";
  const receiverManualValue = "__manual_receiver__";

  const registryChoices = registryAliases.length
    ? registryAliases.map((alias) => ({ title: alias, value: alias }))
    : [];
  registryChoices.push({ title: "Enter address manually", value: registryManualValue });

  const receiverChoices = receiverAliases.length
    ? receiverAliases.map((alias) => ({ title: alias, value: alias }))
    : [];
  receiverChoices.push({ title: "Enter address manually", value: receiverManualValue });

  const aliasAnswers = await promptWithContext(customer, registryNetwork, [
    {
      type: "select",
      name: "registryAlias",
      message: `Registry alias (${registryNetwork})`,
      choices: registryChoices,
      initial: Math.max(registryAliases.indexOf(defaultRegistry ?? ""), 0),
    },
    {
      type: "select",
      name: "receiverAlias",
      message: `Receiver alias (${receiverNetwork})`,
      choices: receiverChoices,
      initial: Math.max(receiverAliases.indexOf(defaultReceiver ?? ""), 0),
    },
  ]);

  const fallbackRegistryAlias = defaultRegistry ?? "OracleIntentRegistry";
  const fallbackReceiverAlias = defaultReceiver ?? "PushOracleReceiverV2";

  const registrySelection = typeof aliasAnswers.registryAlias === "string"
    ? aliasAnswers.registryAlias
    : fallbackRegistryAlias;
  const receiverSelection = typeof aliasAnswers.receiverAlias === "string"
    ? aliasAnswers.receiverAlias
    : fallbackReceiverAlias;

  let registryAlias = fallbackRegistryAlias;
  let registryAddressOverride: string | undefined;
  if (registrySelection === registryManualValue) {
    const manualAddress = await promptAddressValue(customer, `OracleIntentRegistry address (${registryNetwork})`);
    if (!manualAddress) {
      // eslint-disable-next-line no-console
      console.log(chalk.gray("Handle intent cancelled (no registry address)."));
      return receiverNetwork;
    }
    registryAddressOverride = manualAddress;
  } else if (registrySelection) {
    registryAlias = registrySelection;
  }

  let receiverAlias = fallbackReceiverAlias;
  let receiverAddressOverride: string | undefined;
  if (receiverSelection === receiverManualValue) {
    const manualAddress = await promptAddressValue(customer, `PushOracleReceiverV2 address (${receiverNetwork})`);
    if (!manualAddress) {
      // eslint-disable-next-line no-console
      console.log(chalk.gray("Handle intent cancelled (no receiver address)."));
      return receiverNetwork;
    }
    receiverAddressOverride = manualAddress;
  } else if (receiverSelection) {
    receiverAlias = receiverSelection;
  }

  const nowSeconds = Math.floor(Date.now() / 1000);
  const defaults = defaultOracleIntentInput("BTC");

  const hashAnswer = await promptWithContext(customer, receiverNetwork, {
    type: "confirm",
    name: "useHash",
    message: "Use existing intent hash?",
    initial: false,
  });

  let intentHash: string | undefined;
  let answers: Record<string, string | undefined> = {};

  if (hashAnswer.useHash) {
    const hashInput = await promptWithContext(customer, receiverNetwork, {
      type: "text",
      name: "hash",
      message: "Intent hash (0x...)",
      validate: (value: string) => (/^0x[0-9a-fA-F]{64}$/.test(value.trim()) ? true : "Enter 32-byte hash"),
    });
    if (!hashInput.hash) {
      // eslint-disable-next-line no-console
      console.log(chalk.gray("Handle intent cancelled (no hash)."));
      return receiverNetwork;
    }
    intentHash = String(hashInput.hash).trim();
  } else {
    const detailAnswers = await promptWithContext(customer, receiverNetwork, [
      {
        type: "text",
        name: "symbol",
        message: "Oracle symbol",
        initial: "BTC",
        validate: (value: string) => (value && value.trim().length > 0 ? true : "Required"),
      },
      {
        type: "text",
        name: "price",
        message: "Price (wei)",
        initial: "0",
      },
      {
        type: "text",
        name: "timestamp",
        message: "Timestamp (seconds)",
        initial: String(nowSeconds),
      },
      {
        type: "text",
        name: "expiry",
        message: "Expiry timestamp (seconds)",
        initial: String(nowSeconds + 3600),
      },
      {
        type: "text",
        name: "nonce",
        message: "Nonce",
        initial: String(nowSeconds),
      },
      {
        type: "text",
        name: "intentType",
        message: "Intent type",
        initial: defaults.intentType,
      },
      {
        type: "text",
        name: "version",
        message: "Version",
        initial: defaults.version,
      },
      {
        type: "text",
        name: "source",
        message: "Source label",
        initial: defaults.source,
      },
    ]);

    answers = detailAnswers as Record<string, string | undefined>;

    if (!detailAnswers.symbol) {
      // eslint-disable-next-line no-console
      console.log(chalk.gray("Handle intent cancelled (no symbol)."));
      return receiverNetwork;
    }
  }

  const registryConfig = await loadNetworkConfig(registryNetwork);
  const receiverConfig = registryNetwork === receiverNetwork
    ? registryConfig
    : await loadNetworkConfig(receiverNetwork);
  let intentSignerAlias: string | undefined;
  let txSignerAlias: string;

  if (intentHash) {
    const txAlias = await promptAccountAlias(
      customer,
      receiverConfig,
      "Transaction signer alias",
      receiverNetwork
    );
    if (!txAlias) {
      // eslint-disable-next-line no-console
      console.log(chalk.gray("Handle intent cancelled (no transaction signer)."));
      return receiverNetwork;
    }
    txSignerAlias = txAlias;
    intentSignerAlias = txAlias;
  } else {
    const intentAlias = await promptAccountAlias(
      customer,
      registryConfig,
      "Intent signer alias",
      registryNetwork
    );
    if (!intentAlias) {
      // eslint-disable-next-line no-console
      console.log(chalk.gray("Handle intent cancelled (no signer)."));
      return receiverNetwork;
    }
    const txSameAnswer = await promptWithContext(customer, receiverNetwork, {
      type: "confirm",
      name: "same",
      message: "Use same signer for transaction submission?",
      initial: true,
    });

    let txAlias: string = intentAlias;
    if (txSameAnswer.same === false) {
      const altTxAlias = await promptAccountAlias(
        customer,
        receiverConfig,
        "Transaction signer alias",
        receiverNetwork
      );
      if (!altTxAlias) {
        // eslint-disable-next-line no-console
        console.log(chalk.gray("Handle intent cancelled (no transaction signer)."));
        return receiverNetwork;
      }
      txAlias = altTxAlias;
    }

    intentSignerAlias = intentAlias;
    txSignerAlias = txAlias;
  }

  try {
    await submitIntentToReceiver({
      alias: registryAlias,
      receiverAlias,
      registryAddress: registryAddressOverride,
      receiverAddress: receiverAddressOverride,
      customer,
      network: receiverNetwork,
      registryNetwork,
      signer: intentSignerAlias ?? txSignerAlias,
      txSigner: txSignerAlias,
      intentHash,
      symbol: answers.symbol ? String(answers.symbol).trim() : intentHash ?? "",
      price: answers.price ? String(answers.price).trim() : undefined,
      timestamp: answers.timestamp ? String(answers.timestamp).trim() : undefined,
      expiry: answers.expiry ? String(answers.expiry).trim() : undefined,
      nonce: answers.nonce ? String(answers.nonce).trim() : undefined,
      intentType: answers.intentType ? String(answers.intentType).trim() : undefined,
      version: answers.version ? String(answers.version).trim() : undefined,
      source: answers.source ? String(answers.source).trim() : undefined,
    });
  } catch (error: any) {
    // eslint-disable-next-line no-console
    console.error(chalk.red(error?.message ?? error));
  }

  return receiverNetwork;
}

async function interactiveCompareIntentDomains(
  customer: string,
  currentNetwork?: string
): Promise<void> {
  let registryNetworkCandidate = currentNetwork;
  if (!registryNetworkCandidate) {
    registryNetworkCandidate = await promptSelectNetwork({
      customer,
      currentNetwork,
      scope: "Select registry network",
    });
  }
  if (!registryNetworkCandidate) {
    // eslint-disable-next-line no-console
    console.log(chalk.yellow("No registry network selected"));
    return;
  }
  const registryNetwork: string = registryNetworkCandidate;

  const differentAnswer = await promptWithContext(customer, registryNetwork, {
    type: "confirm",
    name: "different",
    message: "Compare against different receiver network?",
    initial: false,
  });

  let receiverNetworkCandidate: string | undefined = registryNetwork;
  if (differentAnswer.different) {
    receiverNetworkCandidate = await promptSelectNetwork({
      customer,
      currentNetwork: registryNetwork,
      scope: "Select receiver network",
    });
    if (!receiverNetworkCandidate) {
      // eslint-disable-next-line no-console
      console.log(chalk.yellow("No receiver network selected"));
      return;
    }
  }
  const receiverNetwork = receiverNetworkCandidate!;

  await prepareCustomerEnvironment(customer);
  const registryDeployments = await loadDeployments(customer, registryNetwork);
  const receiverDeployments = registryNetwork === receiverNetwork
    ? registryDeployments
    : await loadDeployments(customer, receiverNetwork);

  const registryAliases = Object.keys(registryDeployments.current ?? {});
  const receiverAliases = Object.keys(receiverDeployments.current ?? {});

  const defaultRegistry = registryAliases.includes("OracleIntentRegistry")
    ? "OracleIntentRegistry"
    : registryAliases[0];
  const defaultReceiver = receiverAliases.includes("PushOracleReceiverV2")
    ? "PushOracleReceiverV2"
    : receiverAliases[0];

  const aliasAnswers = await promptWithContext(customer, registryNetwork, [
    {
      type: "select",
      name: "registryAlias",
      message: `Registry alias (${registryNetwork})`,
      choices: registryAliases.length
        ? registryAliases.map((alias) => ({ title: alias, value: alias }))
        : [{ title: defaultRegistry ?? "OracleIntentRegistry", value: defaultRegistry ?? "OracleIntentRegistry" }],
      initial: Math.max(registryAliases.indexOf(defaultRegistry ?? ""), 0),
    },
    {
      type: "select",
      name: "receiverAlias",
      message: `Receiver alias (${receiverNetwork})`,
      choices: receiverAliases.length
        ? receiverAliases.map((alias) => ({ title: alias, value: alias }))
        : [{ title: defaultReceiver ?? "PushOracleReceiverV2", value: defaultReceiver ?? "PushOracleReceiverV2" }],
      initial: Math.max(receiverAliases.indexOf(defaultReceiver ?? ""), 0),
    },
  ]);

  await compareDomainSeparators({
    customer,
    registryNetwork,
    receiverNetwork,
    registryAlias: aliasAnswers.registryAlias ? String(aliasAnswers.registryAlias) : defaultRegistry,
    receiverAlias: aliasAnswers.receiverAlias ? String(aliasAnswers.receiverAlias) : defaultReceiver,
  });
}

async function interactiveIntentTools(
  customer: string,
  currentNetwork?: string
): Promise<string | undefined> {
  let activeNetwork = currentNetwork;

  // eslint-disable-next-line no-constant-condition
  while (true) {
    const { action } = await promptWithContext(customer, activeNetwork, {
      type: "select",
      name: "action",
      message: "Intent tools",
      choices: [
        { title: "Register intent", value: "register" },
        { title: "Handle intent", value: "handle" },
        { title: "Compare domain separators", value: "compare" },
        { title: "Back", value: "back" },
      ],
      initial: 0,
    });

    if (!action || action === "back") {
      return activeNetwork;
    }

    try {
      switch (action) {
        case "register": {
          const next = await interactiveRegisterIntent(customer, activeNetwork);
          if (next) {
            activeNetwork = next;
          }
          break;
        }
        case "handle": {
          const next = await interactiveHandleIntent(customer, activeNetwork);
          if (next) {
            activeNetwork = next;
          }
          break;
        }
        case "compare": {
          await interactiveCompareIntentDomains(customer, activeNetwork);
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

async function handleReadFunction(args: {
  customer: string;
  network: string;
  deployment: DeploymentRecord;
  fragment: ContractFunctionFragment;
}): Promise<void> {
  const params = await promptFunctionParams(args.customer, args.fragment, args.network);
  if (!params) {
    // eslint-disable-next-line no-console
    console.log(chalk.gray("Operation cancelled."));
    return;
  }

  const networkConfig = await loadNetworkConfig(args.network);
  const rpcUrl = networkConfig.rpc_url;
  const callArgs = [
    "call",
    args.deployment.address,
    args.fragment.signature,
    ...params,
    "--rpc-url",
    rpcUrl,
  ];

  const printable = formatCommand("cast", callArgs);
  // eslint-disable-next-line no-console
  console.log(chalk.gray("call: read-only (no signer)"));
  // eslint-disable-next-line no-console
  console.log(chalk.gray(printable));
  logEquivalent(printable);

  try {
    const result = await runCast(callArgs);
    const output = result.stdout.trim();
    // eslint-disable-next-line no-console
    console.log(output.length ? output : chalk.gray("(no output)"));
  } catch (error: any) {
    const stderr = error?.stderr ? error.stderr.trim() : "";
    const stdout = error?.stdout ? error.stdout.trim() : "";
    const pieces = [error?.message ?? String(error), stderr, stdout].filter((piece) => piece && piece.length);
    throw new Error(pieces.join("\n"));
  }
}

async function handleWriteFunction(args: {
  customer: string;
  network: string;
  alias: string;
  fragment: ContractFunctionFragment;
}): Promise<void> {
  const params = await promptFunctionParams(args.customer, args.fragment, args.network);
  if (!params) {
    // eslint-disable-next-line no-console
    console.log(chalk.gray("Operation cancelled."));
    return;
  }

  const networkConfig = await loadNetworkConfig(args.network);
  const account = await promptAccountAlias(args.customer, networkConfig, undefined, args.network);
  if (!account) {
    // eslint-disable-next-line no-console
    console.log(chalk.gray("Operation cancelled."));
    return;
  }

  let value: string | undefined;
  if (args.fragment.payable) {
    const valueAnswer = await promptWithContext(args.customer, args.network, {
      type: "text",
      name: "value",
      message: "Value to send (wei, leave blank for 0)",
    });
    value = valueAnswer.value ? String(valueAnswer.value).trim() : undefined;
  }

  const confirm = await promptWithContext(args.customer, args.network, {
    type: "confirm",
    name: "confirm",
    message: `Call ${args.fragment.signature} as ${account}?`,
    initial: true,
  });
  if (confirm.confirm === false) {
    // eslint-disable-next-line no-console
    console.log(chalk.gray("Operation cancelled."));
    return;
  }

  let accountDisplay = account;
  try {
    const wallet = await readStoredWallet(args.customer, account);
    if (wallet.address) {
      accountDisplay = `${account} (${wallet.address})`;
    }
  } catch (error) {
    // ignore lookup failures; alias already validated earlier
  }

  const callSummary = [
    `network: ${args.network}`,
    `contract: ${args.alias}`,
    `function: ${args.fragment.signature}`,
    params.length ? `params: [${params.map((value) => JSON.stringify(value)).join(", ")}]` : undefined,
    value && value.length ? `value: ${value}` : undefined,
    `account: ${accountDisplay}`,
  ].filter(Boolean) as string[];

  const deploymentForSummary = await getDeployment(args.customer, args.network, args.alias);
  const commandArgs = [
    "send",
    deploymentForSummary?.address ?? "<resolved-address>",
    args.fragment.signature,
    ...params,
  ];
  if (value && value.length) {
    commandArgs.push("--value", value);
  }
  commandArgs.push("--rpc-url", networkConfig.rpc_url, "--private-key", "***hidden***");
  const printable = formatCommand("cast", commandArgs);

  // eslint-disable-next-line no-console
  console.log(chalk.gray("Configuration plan:"));
  for (const line of callSummary) {
    // eslint-disable-next-line no-console
    console.log(chalk.gray(`  • ${line}`));
  }
  // eslint-disable-next-line no-console
  console.log(chalk.gray(`  • command: ${printable}`));

  const confirmPlan = await promptWithContext(args.customer, args.network, {
    type: "confirm",
    name: "confirm",
    message: "Proceed with transaction?",
    initial: true,
  });

  if (confirmPlan.confirm === false) {
    // eslint-disable-next-line no-console
    console.log(chalk.gray("Configuration cancelled."));
    return;
  }

  await executeContractSend({
    network: args.network,
    customer: args.customer,
    alias: args.alias,
    account,
    signature: args.fragment.signature,
    params,
    value,
  });
}

function formatFunctionLabel(fragment: ContractFunctionFragment): string {
  return fragment.signature;
}

function formatFunctionDescription(fragment: ContractFunctionFragment): string {
  const inputs = fragment.inputs ?? [];
  const params = inputs
    .map((input, index) => {
      const label = input.name && input.name.length > 0 ? input.name : `arg${index}`;
      return `${input.type} ${label}`;
    })
    .join(", ");
  const mutability = fragment.stateMutability || "nonpayable";
  return params.length ? `Mutability: ${mutability} · Inputs: ${params}` : `Mutability: ${mutability} · Inputs: (none)`;
}

async function promptFunctionParams(
  customer: string,
  fragment: ContractFunctionFragment,
  network?: string
): Promise<string[] | undefined> {
  const inputs = fragment.inputs ?? [];
  const values: string[] = [];

  for (let i = 0; i < inputs.length; i += 1) {
    const input = inputs[i];
    const value = await promptParameterValue(customer, input, i, network);
    if (value === undefined) {
      return undefined;
    }
    values.push(value);
  }

  return values;
}

async function promptParameterValue(
  customer: string,
  input: { type: string; name?: string },
  index: number,
  network?: string
): Promise<string | undefined> {
  const labelName = input.name && input.name.length > 0 ? input.name : `arg${index}`;
  const label = `${labelName} (${input.type})`;
  const normalizedType = input.type.trim();

  if (normalizedType === "address" || normalizedType === "address payable") {
    return promptAddressValue(customer, label, network);
}

  const answer = await promptWithContext(customer, network, {
    type: "text",
    name: "value",
    message: `Parameter ${label}`,
  });

  if (typeof answer.value === "undefined") {
    return undefined;
  }

  return String(answer.value);
}

async function promptAddressValue(
  customer: string,
  label: string,
  network?: string
): Promise<string | undefined> {
  const walletAliases = await listKeyAliases(customer);
  const walletChoices: { title: string; value: { kind: "wallet"; alias: string; address: string } }[] = [];

  for (const alias of walletAliases) {
    try {
      const wallet = await readStoredWallet(customer, alias);
      if (wallet.address && ADDRESS_REGEX.test(wallet.address)) {
        walletChoices.push({
          title: `${alias} (${wallet.address})`,
          value: { kind: "wallet", alias, address: wallet.address },
        });
      }
    } catch (error: any) {
      // ignore malformed entries
    }
  }

  const deploymentChoices = await collectDeploymentAddressChoices(customer);

  const selectionChoices = [
    { title: "Enter address manually", value: { kind: "manual" as const } },
    ...deploymentChoices,
    ...walletChoices,
    { title: "Create new wallet", value: { kind: "new" as const } },
  ];

  const { source } = await promptWithContext(customer, network, {
    type: "select",
    name: "source",
    message: `${label}: choose address source`,
    choices: selectionChoices,
    initial: 0,
  });

  if (!source) {
    return undefined;
  }

  if (source.kind === "manual") {
    const manual = await promptWithContext(customer, network, {
      type: "text",
      name: "address",
      message: label,
      validate: (value: string) =>
        value && ADDRESS_REGEX.test(value.trim()) ? true : "Enter a valid address",
    });
    const address = manual.address ? String(manual.address).trim() : undefined;
    return address;
  }

  if (source.kind === "wallet") {
    return source.address;
  }

  if (source.kind === "deployment") {
    return source.address;
  }

  const newAlias = await promptWalletName(customer, network);
  const newKey = generatePrivateKey();
  try {
    const info = await storePrivateKey(customer, newAlias, newKey, false);
    // eslint-disable-next-line no-console
    console.log(chalk.green(`Stored wallet '${newAlias}' for ${customer}.`));
    // eslint-disable-next-line no-console
    console.log(chalk.gray(`metadata: ${info.metadataPath}`));
    const wallet = await readStoredWallet(customer, newAlias);
    if (!wallet.address || !ADDRESS_REGEX.test(wallet.address)) {
      throw new Error("Failed to derive wallet address.");
    }
    // eslint-disable-next-line no-console
    console.log(chalk.gray(`address: ${wallet.address}`));
    return wallet.address;
  } catch (error: any) {
    // eslint-disable-next-line no-console
    console.error(chalk.red(error?.message ?? error));
    return undefined;
  }
}

async function collectDeploymentAddressChoices(
  customer: string
): Promise<{ title: string; value: { kind: "deployment"; address: string } }[]> {
  const seen = new Set<string>();
  const choices: { title: string; value: { kind: "deployment"; address: string } }[] = [];

  const networks = await listNetworkNames();
  const customersToCheck = customer === "master" ? ["master"] : [customer, "master"];

  for (const customerToCheck of customersToCheck) {
    for (const network of networks) {
      try {
        const { current } = await loadDeployments(customerToCheck, network);
        for (const [alias, record] of Object.entries(current)) {
          if (!record?.address || !ADDRESS_REGEX.test(record.address)) {
            continue;
          }
          const key = `${network}:${record.address.toLowerCase()}`;
          if (seen.has(key)) {
            continue;
          }
          seen.add(key);
          const customerLabel = customerToCheck === "master" && customer !== "master" ? ` (master)` : "";
          choices.push({
            title: `${alias} on ${network}${customerLabel} (${record.address})`,
            value: { kind: "deployment", address: record.address },
          });
        }
      } catch (error: any) {
        // ignore networks we cannot read
      }
    }
  }

  return choices;
}

async function promptAccountAlias(
  customer: string,
  networkConfig: NetworkConfig,
  label = "Select account alias",
  network?: string
): Promise<string | undefined> {
  const accountAliases = Object.keys(networkConfig.accounts ?? {});
  const masterAliases = await listKeyAliases("master");
  const defaultAlias = accountAliases.includes("admin")
    ? "admin"
    : accountAliases[0] ?? (accountAliases.length ? accountAliases[0] : "deployer");

  let attempt = 0;
  // eslint-disable-next-line no-constant-condition
  while (true) {
    attempt += 1;
    const selectionChoices = [
      ...accountAliases.map((alias) => ({
        title: alias,
        value: { kind: "alias" as const, alias },
      })),
      ...masterAliases
        .filter((alias) => !accountAliases.includes(alias))
        .map((alias) => ({
          title: `${alias} (master)`,
          value: { kind: "alias" as const, alias },
        })),
      { title: "Custom alias", value: { kind: "manual" as const } },
      { title: "Cancel", value: { kind: "cancel" as const } },
    ];

    const accountAnswer = await promptWithContext(customer, network, {
      type: "select",
      name: "selection",
      message: attempt === 1 ? label : `${label} (reselect)`,
      choices: selectionChoices,
      initial: Math.max(accountAliases.indexOf(defaultAlias), 0),
    });

    const choice = accountAnswer.selection as
      | { kind: "alias"; alias: string }
      | { kind: "manual" }
      | { kind: "cancel" }
      | undefined;

    if (!choice || choice.kind === "cancel") {
      return undefined;
    }

    let alias: string | undefined;
    if (choice.kind === "alias") {
      alias = choice.alias;
    } else {
      const manual = await promptWithContext(customer, network, {
        type: "text",
        name: "alias",
        message: "Account alias",
        validate: (value: string) => (value && value.trim().length > 0 ? true : "Required"),
      });
      alias = manual.alias ? String(manual.alias).trim() : undefined;
    }

    if (!alias) {
      return undefined;
    }

    const ensured = await ensureAccountWallet(customer, alias, network);
    if (ensured) {
      return alias;
    }

    const retry = await promptWithContext(customer, network, {
      type: "confirm",
      name: "retry",
      message: "Account is unavailable. Choose a different alias?",
      initial: true,
    });
    if (!retry.retry) {
      return undefined;
    }
  }
}

async function ensureAccountWallet(
  customer: string,
  alias: string,
  network?: string
): Promise<boolean> {
  try {
    await readStoredWallet(customer, alias);
    return true;
  } catch (error: any) {
    const masterWallet = await tryReadWallet("master", alias);
    if (masterWallet) {
      // eslint-disable-next-line no-console
      console.log(chalk.gray(`Using master key '${alias}'.`));
      return true;
    }
    const create = await promptWithContext(customer, network, {
      type: "confirm",
      name: "create",
      message: `No key stored for '${alias}'. Create one now?`,
      initial: true,
    });
    if (!create.create) {
      return false;
    }

    const methodAnswer = await promptWithContext(customer, network, {
      type: "select",
      name: "method",
      message: `Provide private key for '${alias}'`,
      choices: [
        { title: "Generate new key", value: "generate" },
        { title: "Enter existing private key", value: "manual" },
        { title: "Cancel", value: "cancel" },
      ],
      initial: 0,
    });

    if (!methodAnswer.method || methodAnswer.method === "cancel") {
      return false;
    }

    try {
      let privateKey: string | undefined;
      if (methodAnswer.method === "generate") {
        privateKey = generatePrivateKey();
      } else {
        const keyAnswer = await promptWithContext(customer, network, {
          type: "password",
          name: "pk",
          message: "Enter 32-byte private key (prefixed with 0x or hex)",
          validate: (value: string) => (value && value.trim().length > 0 ? true : "Required"),
        });
        privateKey = keyAnswer.pk ? String(keyAnswer.pk).trim() : undefined;
        if (!privateKey) {
          return false;
        }
      }

      const info = await storePrivateKey(customer, alias, privateKey!, true);
      // eslint-disable-next-line no-console
      console.log(chalk.green(`Stored key for '${alias}'.`));
      // eslint-disable-next-line no-console
      console.log(chalk.gray(`metadata: ${info.metadataPath}`));
      if (info.address) {
        // eslint-disable-next-line no-console
        console.log(chalk.gray(`address: ${info.address}`));
      }
      return true;
    } catch (storeError: any) {
      // eslint-disable-next-line no-console
      console.error(chalk.red(storeError?.message ?? storeError));
      return false;
    }
  }
}

async function tryReadWallet(customer: string, alias: string): Promise<StoredWalletMeta | undefined> {
  try {
    return await readStoredWallet(customer, alias);
  } catch (error) {
    return undefined;
  }
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
  const chainResponse = await promptWithContext(customer, network, {
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
    return await promptForAddress(customer, targetNetwork, "OracleIntentRegistry address (no deployments found)");
  }

  const { registry } = await promptWithContext(customer, targetNetwork, {
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
    return await promptForAddress(customer, targetNetwork, "OracleIntentRegistry address");
  }

  return String(registry);
}

async function promptForAddress(
  customer: string,
  network: string,
  message: string
): Promise<string> {
  const response = await promptWithContext(customer, network, {
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

async function promptWalletName(customer: string, network?: string): Promise<string> {
  const customerPrefix = sanitizeAlias(customer) || "CUSTOMER";
  const { name } = await promptWithContext(customer, network, {
    type: "text",
    name: "name",
    message: `Wallet alias (stored as ${customerPrefix}-<alias>-key)`,
    validate: (value: string) => (value && value.trim().length > 0 ? true : "Required"),
  });

  const raw = typeof name === "string" ? name.trim() : "";
  const suffix = raw.length > 0 ? sanitizeAlias(raw) : `wallet-${Date.now()}`;
  return `${customerPrefix}-${suffix}-KEY`;
}

function sanitizeAlias(value: string): string {
  const trimmed = (value ?? "").trim();
  return trimmed.replace(/[^a-zA-Z0-9_-]/g, "-");
}

async function interactiveAddNetwork(customer?: string, network?: string): Promise<void> {
  const answers = await promptWithContext(customer, network, [
    {
      type: "text",
      name: "name",
      message: "Network name",
      validate: (value: string) => (value && value.trim() ? true : "Required"),
    },
    {
      type: "select",
      name: "environment",
      message: "Network environment",
      choices: [
        { title: "Testnet", value: "testnet" },
        { title: "Mainnet", value: "mainnet" },
      ],
      initial: 0,
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
    {
      type: "confirm",
      name: "addVerification",
      message: "Configure block explorer verification settings?",
      initial: false,
    },
    {
      type: (prev: boolean, values: Record<string, unknown>) => (values.addVerification ? "text" : null),
      name: "verificationVerifier",
      message: "Verifier name (e.g. etherscan, blockscout)",
    },
    {
      type: (prev: boolean, values: Record<string, unknown>) => (values.addVerification ? "text" : null),
      name: "verificationVerifierUrl",
      message: "Verifier API URL",
    },
    {
      type: (prev: boolean, values: Record<string, unknown>) => (values.addVerification ? "text" : null),
      name: "verificationExplorerUrl",
      message: "Explorer URL template (use {address})",
    },
    {
      type: (prev: boolean, values: Record<string, unknown>) => (values.addVerification ? "text" : null),
      name: "verificationChain",
      message: "Verifier chain identifier (optional)",
    },
    {
      type: (prev: boolean, values: Record<string, unknown>) => (values.addVerification ? "confirm" : null),
      name: "verificationWatch",
      message: "Enable --watch by default?",
      initial: false,
    },
    {
      type: (prev: boolean, values: Record<string, unknown>) => (values.addVerification ? "text" : null),
      name: "verificationApiKeyEnv",
      message: "API key environment variable (optional)",
    },
    {
      type: (prev: boolean, values: Record<string, unknown>) => (values.addVerification ? "text" : null),
      name: "verificationApiKeyValue",
      message: "Inline API key value (optional)",
    },
  ]);

  if (!answers.name) {
    return;
  }

  const rawName = String(answers.name).trim();
  const normalizedName = normalizeNetworkFileName(rawName);

  const filePath = await createNetworkConfig({
    name: rawName,
    chainId: Number(answers.chainId),
    rpcUrl: String(answers.rpcUrl),
    forgeProfile: answers.forgeProfile ? String(answers.forgeProfile) : undefined,
    defaultAccountAlias: answers.addDefaultAccount ? String(answers.defaultAlias || "deployer") : undefined,
    environment: answers.environment,
    verification: answers.addVerification
      ? {
          verifier: answers.verificationVerifier ? String(answers.verificationVerifier).trim() || undefined : undefined,
          verifierUrl: answers.verificationVerifierUrl
            ? String(answers.verificationVerifierUrl).trim() || undefined
            : undefined,
          explorerUrl: answers.verificationExplorerUrl
            ? String(answers.verificationExplorerUrl).trim() || undefined
            : undefined,
          chain: answers.verificationChain ? String(answers.verificationChain).trim() || undefined : undefined,
          apiKeyEnv: answers.verificationApiKeyEnv
            ? String(answers.verificationApiKeyEnv).trim() || undefined
            : undefined,
          apiKeyValue: answers.verificationApiKeyValue
            ? String(answers.verificationApiKeyValue).trim() || undefined
            : undefined,
          watch: typeof answers.verificationWatch === "boolean" ? answers.verificationWatch : undefined,
        }
      : undefined,
  });
  // eslint-disable-next-line no-console
  console.log(chalk.green(`Created network config '${normalizedName}' at ${filePath}`));
  if (normalizedName !== rawName) {
    // eslint-disable-next-line no-console
    console.log(chalk.gray(`Note: input '${rawName}' normalized to '${normalizedName}' for file naming.`));
  }
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

  const aliasAnswer = await promptWithContext(customer, network, {
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
    const customAlias = await promptWithContext(customer, network, {
      type: "text",
      name: "customAlias",
      message: "Enter deployment alias",
      validate: (value: string) => (value && value.trim() ? true : "Required"),
    });
    aliasName = String(customAlias.customAlias || "").trim();
    artifactPreset = undefined;
  }

  const aliasLabel = aliasName || "(custom)";

  const answers = (await promptWithContext(customer, network, [
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
      const { artifactInput } = await promptWithContext(customer, network, {
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
  const masterKeys = await listKeyAliases("master");
  const allAvailableKeys = [...availableKeys, ...masterKeys.filter(key => !availableKeys.includes(key))];

  if (allAvailableKeys.length === 0) {
    const createKeyAnswer = await promptWithContext(customer, network, {
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
    const newName = await promptWalletName(customer, network);
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
    const refreshedMasterKeys = await listKeyAliases("master");
    allAvailableKeys.length = 0;
    allAvailableKeys.push(...availableKeys, ...refreshedMasterKeys.filter(key => !availableKeys.includes(key)));
  }

  const keyChoices = allAvailableKeys.map((key) => {
    const isMasterKey = masterKeys.includes(key) && !availableKeys.includes(key);
    return {
      title: isMasterKey ? `${key} (master)` : key,
      value: key
    };
  });

  const { chosenKey } = await promptWithContext(customer, network, {
    type: "select",
    name: "chosenKey",
    message: "Select a key to use",
    choices: keyChoices,
    initial: Math.max(allAvailableKeys.indexOf("deployer"), 0),
  });
  if (!chosenKey) {
    // eslint-disable-next-line no-console
    console.log(chalk.gray("Deployment cancelled (no key selected)."));
    return;
  }

  const resolvedAccount = String(chosenKey);
  let walletMeta;
  try {
    walletMeta = await readStoredWallet(customer, resolvedAccount);
  } catch (error) {
    // Try master keys as fallback
    if (masterKeys.includes(resolvedAccount)) {
      walletMeta = await readStoredWallet("master", resolvedAccount);
    } else {
      throw error;
    }
  }
  const privateKey = walletMeta.privateKey;
  const deployerAddress = walletMeta.address;

  const classification = classifyNetwork(networkConfig);
  const previewArgs = [
    "create",
    artifact,
    "--rpc-url",
    networkConfig.rpc_url,
    "--private-key",
    "***hidden***",
    "--chain-id",
    String(networkConfig.chain_id),
    "--broadcast",
  ];
  if (sanitizedArgs.length > 0) {
    previewArgs.push("--constructor-args", ...sanitizedArgs);
  }
  const previewCommand = formatCommand("forge", previewArgs);

  const summaryLines = [
    `network: ${network} (chainId ${networkConfig.chain_id}, ${classification})`,
    `contract: ${alias} -> ${artifact}`,
    sanitizedArgs.length
      ? `constructor args: [${sanitizedArgs.map((arg) => JSON.stringify(arg)).join(", ")}]`
      : undefined,
    `account: ${resolvedAccount}${
      deployerAddress ? ` (${deployerAddress})` : ""
    }`,
    forgeProfile ? `forge profile: ${forgeProfile}` : undefined,
    `command: ${previewCommand}`,
  ].filter(Boolean) as string[];

  // eslint-disable-next-line no-console
  console.log(chalk.gray("Deployment plan:"));
  for (const line of summaryLines) {
    // eslint-disable-next-line no-console
    console.log(chalk.gray(`  • ${line}`));
  }

  const finalConfirm = await promptWithContext(customer, network, {
    type: "confirm",
    name: "confirm",
    message: "Proceed with deployment?",
    initial: true,
  });

  if (finalConfirm.confirm === false) {
    // eslint-disable-next-line no-console
    console.log(chalk.gray("Deployment cancelled"));
    return;
  }

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
    const { verify } = await promptWithContext(customer, network, {
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
