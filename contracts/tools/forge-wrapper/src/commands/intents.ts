import chalk from "chalk";
import { Command } from "commander";
import { getAddress } from "ethers";
import { getDefaultCustomer, getDefaultNetwork } from "../utils/paths";
import { prepareCustomerEnvironment, loadNetworkConfig } from "../config";
import { getDeployment } from "../deployments";
import { readStoredPrivateKey, readStoredWallet } from "../services/keys";
import { executeContractSend } from "./configure";
import { signOracleIntent, defaultOracleIntentInput, OracleIntentInput } from "../utils/intents";
import {
  fetchIntentByHash,
  toOracleIntentInput,
  intentToPrintable,
  fetchDomainSeparator,
} from "../services/registry";
import { formatCommand, runCast } from "../utils/forge";

interface RegisterOptions {
  alias?: string;
  customer?: string;
  network?: string;
  signer: string;
  txSigner?: string;
  symbol: string;
  price?: string;
  timestamp?: string;
  intentType?: string;
  version?: string;
  nonce?: string;
  expiry?: string;
  source?: string;
  dryRun?: boolean;
  showOnly?: boolean;
  rpcUrl?: string;
  intentHash?: string;
}

interface HandleOptions extends RegisterOptions {
  receiverAlias?: string;
  receiverAddress?: string;
  registryAddress?: string;
  rpcUrl?: string;
  intentHash?: string;
  registryNetwork?: string;
  registryRpcUrl?: string;
}

interface CompareDomainOptions {
  registryAlias?: string;
  receiverAlias?: string;
  registryNetwork?: string;
  receiverNetwork?: string;
  registryRpcUrl?: string;
  receiverRpcUrl?: string;
  customer?: string;
}

async function getPrivateKey(customer: string, alias: string): Promise<string> {
  try {
    return await readStoredPrivateKey(customer, alias);
  } catch (error) {
    try {
      return await readStoredPrivateKey("master", alias);
    } catch (masterError) {
      throw new Error(`Private key for alias '${alias}' not found in ${customer} or master keystores`);
    }
  }
}

function parseBigint(value: string | undefined, label: string, fallback: bigint): bigint {
  if (!value || value.trim().length === 0) {
    return fallback;
  }
  try {
    if (value.startsWith("0x")) {
      return BigInt(value);
    }
    return BigInt(value);
  } catch (error) {
    throw new Error(`Invalid numeric value for ${label}: ${value}`);
  }
}

function formatRegisterParams(intent: OracleIntentInput, signature: string, signer: string): string[] {
  return [
    intent.intentType,
    intent.version,
    intent.chainId.toString(),
    intent.nonce.toString(),
    intent.expiry.toString(),
    intent.symbol,
    intent.price.toString(),
    intent.timestamp.toString(),
    intent.source,
    signature,
    signer,
  ];
}

function buildHandleTuple(intent: OracleIntentInput, signature: string, signer: string): string {
  const tuple = `("${escapeString(intent.intentType)}","${escapeString(intent.version)}",${intent.chainId},${intent.nonce},${intent.expiry},"${escapeString(intent.symbol)}",${intent.price},${intent.timestamp},"${escapeString(intent.source)}",${signature},${signer})`;
  return tuple;
}

function escapeString(value: string): string {
  return value.replace(/"/g, '\\"');
}

async function resolveIntentInput(options: RegisterOptions, networkChainId: number): Promise<OracleIntentInput> {
  const now = BigInt(Math.floor(Date.now() / 1000));
  const base = defaultOracleIntentInput(options.symbol);
  return {
    intentType: options.intentType ?? base.intentType,
    version: options.version ?? base.version,
    chainId: networkChainId,
    nonce: parseBigint(options.nonce, "nonce", base.nonce),
    expiry: parseBigint(options.expiry, "expiry", now + 3600n),
    symbol: options.symbol,
    price: parseBigint(options.price, "price", 0n),
    timestamp: parseBigint(options.timestamp, "timestamp", now),
    source: options.source ?? base.source,
  };
}

export async function registerOracleIntent(options: RegisterOptions): Promise<void> {
  const customer = options.customer ?? getDefaultCustomer();
  const network = options.network ?? getDefaultNetwork();
  if (!network) {
    throw new Error("Network is required (use --network or FORGE_WRAPPER_NETWORK)");
  }
  const alias = options.alias ?? "OracleIntentRegistry";
  await prepareCustomerEnvironment(customer);

  const deployment = await getDeployment(customer, network, alias);
  if (!deployment) {
    throw new Error(`Deployment for alias '${alias}' not found on ${network}`);
  }

  if (!options.signer) {
    throw new Error("--signer is required");
  }

  const networkConfig = await loadNetworkConfig(network);
  const rpcUrl = options.rpcUrl ?? networkConfig.rpc_url;

  let intent: OracleIntentInput;
  let signature: string;
  let signerAddress: string;

  if (options.intentHash) {
    const record = await fetchIntentByHash(rpcUrl, deployment.address, options.intentHash);
    intent = toOracleIntentInput(record);
    signature = record.signature;
    signerAddress = record.signer;
    // eslint-disable-next-line no-console
    console.log(chalk.gray(`Loaded intent signer from registry: ${signerAddress}`));
    // eslint-disable-next-line no-console
    console.log(chalk.gray("intent payload:"));
    // eslint-disable-next-line no-console
    console.log(JSON.stringify(intentToPrintable(record), null, 2));
  } else {
    const resolved = await resolveIntentInput(options, networkConfig.chain_id);
    intent = resolved;
    const privateKey = await getPrivateKey(customer, options.signer);
    const domainSeparator = await fetchDomainSeparator(rpcUrl, deployment.address);
    const signed = await signOracleIntent(privateKey, domainSeparator, intent);
    signature = signed.signature;
    signerAddress = signed.signer;
    // eslint-disable-next-line no-console
    console.log(chalk.gray(`intent signer: ${options.signer} (${signerAddress})`));
    // eslint-disable-next-line no-console
    console.log(chalk.gray(`signature: ${signature}`));
    // eslint-disable-next-line no-console
    console.log(chalk.gray("intent payload:"));
    // eslint-disable-next-line no-console
    console.log(JSON.stringify(intentToPrintable({
      intentType: intent.intentType,
      version: intent.version,
      chainId: BigInt(intent.chainId),
      nonce: intent.nonce,
      expiry: intent.expiry,
      symbol: intent.symbol,
      price: intent.price,
      timestamp: intent.timestamp,
      source: intent.source,
      signer: signerAddress,
      signature,
    }), null, 2));
  }

  const txSignerAlias = options.txSigner ?? options.signer;
  let txSignerAddress: string | undefined;
  try {
    const wallet = await readStoredWallet(customer, txSignerAlias);
    txSignerAddress = wallet.address;
  } catch {
    try {
      const wallet = await readStoredWallet("master", txSignerAlias);
      txSignerAddress = wallet.address;
    } catch {
      txSignerAddress = undefined;
    }
  }

  // eslint-disable-next-line no-console
  console.log(
    chalk.gray(
      `tx signer: ${txSignerAlias}${txSignerAddress ? ` (${txSignerAddress})` : ""}`
    )
  );

  if (options.showOnly) {
    // eslint-disable-next-line no-console
    console.log(JSON.stringify({ intent, signature, signer: signerAddress }, null, 2));
    return;
  }

  await executeContractSend({
    network,
    customer,
    alias,
    account: txSignerAlias,
    signature: "registerIntent(string,string,uint256,uint256,uint256,string,uint256,uint256,string,bytes,address)",
    params: formatRegisterParams(intent, signature, signerAddress),
    dryRun: Boolean(options.dryRun),
    rpcUrl,
  });
}

export async function submitIntentToReceiver(options: HandleOptions): Promise<void> {
  const customer = options.customer ?? getDefaultCustomer();
  const receiverNetwork = options.network ?? getDefaultNetwork();
  if (!receiverNetwork) {
    throw new Error("Network is required (use --network or FORGE_WRAPPER_NETWORK)");
  }
  const registryNetwork = options.registryNetwork ?? receiverNetwork;
  const receiverAlias = options.receiverAlias ?? options.alias ?? "PushOracleReceiverV2";
  await prepareCustomerEnvironment(customer);

  const normalizeAddressOption = (value: string | undefined, label: string): string | undefined => {
    if (!value) {
      return undefined;
    }
    const trimmed = value.trim();
    try {
      return getAddress(trimmed);
    } catch (error) {
      throw new Error(`Invalid ${label}: ${value}`);
    }
  };

  const receiverAddressOverride = normalizeAddressOption(options.receiverAddress, "receiver contract address");
  const registryAddressOverride = normalizeAddressOption(options.registryAddress, "registry contract address");

  const receiverDeployment = receiverAddressOverride
    ? undefined
    : await getDeployment(customer, receiverNetwork, receiverAlias);
  if (!receiverAddressOverride && !receiverDeployment) {
    throw new Error(`Receiver deployment for alias '${receiverAlias}' not found on ${receiverNetwork}`);
  }

  const registryAlias = options.alias ?? "OracleIntentRegistry";
  const registryDeployment = registryAddressOverride
    ? undefined
    : await getDeployment(customer, registryNetwork, registryAlias);
  if (!registryAddressOverride && !registryDeployment) {
    throw new Error(`Registry deployment '${registryAlias}' not found on ${registryNetwork}`);
  }

  const ensureAddress = (value: string | undefined, label: string): string => {
    if (!value) {
      throw new Error(`${label} is required`);
    }
    try {
      return getAddress(value);
    } catch (error) {
      throw new Error(`Invalid ${label}: ${value}`);
    }
  };

  const receiverAddress = ensureAddress(receiverAddressOverride ?? receiverDeployment?.address, "Receiver contract address");
  const registryAddress = ensureAddress(registryAddressOverride ?? registryDeployment?.address, "Registry contract address");

  const receiverConfig = await loadNetworkConfig(receiverNetwork);
  const registryConfig = registryNetwork === receiverNetwork
    ? receiverConfig
    : await loadNetworkConfig(registryNetwork);
  const receiverRpcUrl = options.rpcUrl ?? receiverConfig.rpc_url;
  const registryRpcUrl = options.registryRpcUrl ?? (registryNetwork === receiverNetwork
    ? receiverRpcUrl
    : registryConfig.rpc_url);

  // eslint-disable-next-line no-console
  console.log(chalk.gray(`registry network: ${registryNetwork}`));
  // eslint-disable-next-line no-console
  console.log(chalk.gray(`receiver network: ${receiverNetwork}`));
  // eslint-disable-next-line no-console
  console.log(chalk.gray(`registry address: ${registryAddress}`));
  // eslint-disable-next-line no-console
  console.log(chalk.gray(`receiver address: ${receiverAddress}`));

  let intent: OracleIntentInput;
  let signature: string;
  let signerAddress: string;
  let intentSignerLabel: string;

  if (options.intentHash) {
    const record = await fetchIntentByHash(registryRpcUrl, registryAddress, options.intentHash);
    intent = toOracleIntentInput(record);
    signature = record.signature;
    signerAddress = record.signer;
    intentSignerLabel = `registry (${signerAddress})`;
    // eslint-disable-next-line no-console
    console.log(chalk.gray(`Loaded intent '${options.intentHash}' from registry.`));
    // eslint-disable-next-line no-console
    console.log(chalk.gray("intent payload:"));
    // eslint-disable-next-line no-console
    console.log(JSON.stringify(intentToPrintable(record), null, 2));
  } else {
    if (!options.signer) {
      throw new Error("--signer is required when intent hash is not provided");
    }

    intent = await resolveIntentInput(options, registryConfig.chain_id);
    const signerKey = await getPrivateKey(customer, options.signer);
    const domainSeparator = await fetchDomainSeparator(registryRpcUrl, registryAddress);
    const signed = await signOracleIntent(signerKey, domainSeparator, intent);
    signature = signed.signature;
    signerAddress = signed.signer;
    intentSignerLabel = `${options.signer} (${signerAddress})`;
    // eslint-disable-next-line no-console
    console.log(chalk.gray(`intent signer: ${intentSignerLabel}`));
    // eslint-disable-next-line no-console
    console.log(chalk.gray(`signature: ${signature}`));
    // eslint-disable-next-line no-console
    console.log(chalk.gray("intent payload:"));
    // eslint-disable-next-line no-console
    console.log(
      JSON.stringify(
        intentToPrintable({
          intentType: intent.intentType,
          version: intent.version,
          chainId: BigInt(intent.chainId),
          nonce: intent.nonce,
          expiry: intent.expiry,
          symbol: intent.symbol,
          price: intent.price,
          timestamp: intent.timestamp,
          source: intent.source,
          signer: signerAddress,
          signature,
        }),
        null,
        2
      )
    );
  }

  const txSignerAlias = options.txSigner ?? options.signer;
  if (!txSignerAlias) {
    throw new Error("Provide --tx-signer when intent signer alias is not available");
  }
  const txKey = await getPrivateKey(customer, txSignerAlias);

  let txSignerAddress: string | undefined;
  try {
    const wallet = await readStoredWallet(customer, txSignerAlias);
    txSignerAddress = wallet.address;
  } catch {
    try {
      const wallet = await readStoredWallet("master", txSignerAlias);
      txSignerAddress = wallet.address;
    } catch {
      txSignerAddress = undefined;
    }
  }

  const tupleValue = buildHandleTuple(intent, signature, signerAddress);
  const castArgs = [
    "send",
    receiverAddress,
    "handleIntentUpdate((string,string,uint256,uint256,uint256,string,uint256,uint256,string,bytes,address))",
    tupleValue,
    "--rpc-url",
    receiverRpcUrl,
    "--private-key",
    txKey,
  ];

  // eslint-disable-next-line no-console
  console.log(chalk.gray(`intent signer: ${intentSignerLabel}`));
  // eslint-disable-next-line no-console
  console.log(
    chalk.gray(`tx signer: ${txSignerAlias}${txSignerAddress ? ` (${txSignerAddress})` : ""}`)
  );
  // eslint-disable-next-line no-console
  console.log(chalk.gray(`signature: ${signature}`));
  const printable = formatCommand("cast", [
    "send",
    receiverAddress,
    "handleIntentUpdate((string,string,uint256,uint256,uint256,string,uint256,uint256,string,bytes,address))",
    tupleValue,
  ]);
  // eslint-disable-next-line no-console
  console.log(chalk.gray(printable));
  if (options.dryRun) {
    // eslint-disable-next-line no-console
    console.log(chalk.yellow("Dry run enabled, not executing cast command."));
    return;
  }

  try {
    const result = await runCast(castArgs);
    // eslint-disable-next-line no-console
    console.log(chalk.green("Transaction submitted successfully"));
    if (result.stdout.trim()) {
      // eslint-disable-next-line no-console
      console.log(chalk.gray(`stdout: ${result.stdout.trim()}`));
    }
  } catch (error: any) {
    // eslint-disable-next-line no-console
    console.log(chalk.red(`cast exited with code ${error.code || 'unknown'}`));
    if (error.stderr?.trim()) {
      // eslint-disable-next-line no-console
      console.log(chalk.red(`Error: ${error.stderr.trim()}`));
    }
    if (error.stdout?.trim()) {
      // eslint-disable-next-line no-console
      console.log(chalk.yellow(`stdout: ${error.stdout.trim()}`));
    }
    throw new Error(`Transaction failed: ${error.message}`);
  }
}

export async function compareDomainSeparators(options: CompareDomainOptions): Promise<void> {
  const customer = options.customer ?? getDefaultCustomer();
  const registryNetwork = options.registryNetwork ?? getDefaultNetwork();
  if (!registryNetwork) {
    throw new Error("Registry network is required (use --registry-network or FORGE_WRAPPER_NETWORK)");
  }
  const receiverNetwork = options.receiverNetwork ?? registryNetwork;

  await prepareCustomerEnvironment(customer);

  const registryAlias = options.registryAlias ?? "OracleIntentRegistry";
  const receiverAlias = options.receiverAlias ?? "PushOracleReceiverV2";

  const registryDeployment = await getDeployment(customer, registryNetwork, registryAlias);
  if (!registryDeployment) {
    throw new Error(`Registry deployment '${registryAlias}' not found on ${registryNetwork}`);
  }

  const receiverDeployment = await getDeployment(customer, receiverNetwork, receiverAlias);
  if (!receiverDeployment) {
    throw new Error(`Receiver deployment '${receiverAlias}' not found on ${receiverNetwork}`);
  }

  const registryConfig = await loadNetworkConfig(registryNetwork);
  const receiverConfig = receiverNetwork === registryNetwork
    ? registryConfig
    : await loadNetworkConfig(receiverNetwork);

  const registryRpcUrl = options.registryRpcUrl ?? registryConfig.rpc_url;
  const receiverRpcUrl = options.receiverRpcUrl ?? receiverConfig.rpc_url;

  const registryDomain = await fetchDomainSeparator(registryRpcUrl, registryDeployment.address);
  const receiverDomain = await fetchDomainSeparator(receiverRpcUrl, receiverDeployment.address);

  // eslint-disable-next-line no-console
  console.log(chalk.gray(`Registry (${registryNetwork}) ${registryDeployment.address}`));
  // eslint-disable-next-line no-console
  console.log(chalk.gray(`domain separator: ${registryDomain}`));
  // eslint-disable-next-line no-console
  console.log(chalk.gray(`Receiver (${receiverNetwork}) ${receiverDeployment.address}`));
  // eslint-disable-next-line no-console
  console.log(chalk.gray(`domain separator: ${receiverDomain}`));

  if (registryDomain === receiverDomain) {
    // eslint-disable-next-line no-console
    console.log(chalk.green("Domain separators match."));
  } else {
    // eslint-disable-next-line no-console
    console.log(chalk.red("Domain separators differ."));
  }
}

export function registerIntentCommands(program: Command): void {
  const intents = program.command("intents").description("Oracle intent helpers");

  intents
    .command("register")
    .description("Sign and register an oracle intent")
    .requiredOption("--symbol <symbol>", "Oracle symbol")
    .requiredOption("--signer <alias>", "Key alias for signing and submission")
    .option("--price <wei>", "Oracle price value")
    .option("--timestamp <seconds>", "Intent timestamp")
    .option("--nonce <value>", "Intent nonce (defaults to current time)")
    .option("--expiry <seconds>", "Intent expiry timestamp (defaults now+3600)")
    .option("--intent-type <value>", "Intent type", "OracleUpdate")
    .option("--version <value>", "Intent version", "1.0")
    .option("--source <value>", "Intent source", "cli")
    .option("--alias <deployment>", "Registry deployment alias", "OracleIntentRegistry")
    .option("-n, --network <network>", "Network name")
    .option("-c, --customer <customer>", "Customer namespace")
    .option("--rpc-url <url>", "Override RPC URL")
    .option("--tx-signer <alias>", "Transaction signer alias (defaults to intent signer)")
    .option("--dry-run", "Show forge command without executing")
    .option("--show-only", "Only output signed payload without submitting")
    .action(async (cmdOptions: RegisterOptions) => {
      try {
        await registerOracleIntent(cmdOptions);
      } catch (error: any) {
        // eslint-disable-next-line no-console
        console.error(chalk.red(error?.message ?? error));
        process.exitCode = 1;
      }
    });

  intents
    .command("handle")
    .description("Sign (optional) and submit intent to PushOracleReceiverV2")
    .requiredOption("--symbol <symbol>", "Oracle symbol")
    .option("--signer <alias>", "Key alias for signing when generating a new intent")
    .option("--price <wei>", "Oracle price value")
    .option("--timestamp <seconds>", "Intent timestamp")
    .option("--nonce <value>", "Intent nonce")
    .option("--expiry <seconds>", "Intent expiry timestamp")
    .option("--intent-type <value>", "Intent type", "OracleUpdate")
    .option("--version <value>", "Intent version", "1.0")
    .option("--source <value>", "Intent source", "cli")
    .option("--alias <registry>", "Registry deployment alias", "OracleIntentRegistry")
    .option("--receiver-alias <receiver>", "Receiver deployment alias", "PushOracleReceiverV2")
    .option("--registry-address <address>", "Override registry contract address")
    .option("--receiver-address <address>", "Override receiver contract address")
    .option("-n, --network <network>", "Network name")
    .option("-c, --customer <customer>", "Customer namespace")
    .option("--rpc-url <url>", "Override RPC URL")
    .option("--tx-signer <alias>", "Transaction signer alias (defaults to intent signer)")
    .option("--registry-network <network>", "Registry network (defaults to receiver network)")
    .option("--registry-rpc-url <url>", "Override registry RPC URL")
    .option("--intent-hash <hash>", "Fetch existing intent by hash from registry")
    .action(async (cmdOptions: HandleOptions) => {
      try {
        await submitIntentToReceiver(cmdOptions);
      } catch (error: any) {
        // eslint-disable-next-line no-console
        console.error(chalk.red(error?.message ?? error));
        process.exitCode = 1;
      }
    });

  intents
    .command("compare-domain")
    .description("Compare domain separators between registry and receiver")
    .option("--registry-alias <alias>", "Registry deployment alias", "OracleIntentRegistry")
    .option("--receiver-alias <alias>", "Receiver deployment alias", "PushOracleReceiverV2")
    .option("--registry-network <network>", "Registry network")
    .option("--receiver-network <network>", "Receiver network")
    .option("--registry-rpc-url <url>", "Override registry RPC URL")
    .option("--receiver-rpc-url <url>", "Override receiver RPC URL")
    .option("-c, --customer <customer>", "Customer namespace")
    .action(async (cmdOptions: CompareDomainOptions) => {
      try {
        await compareDomainSeparators(cmdOptions);
      } catch (error: any) {
        // eslint-disable-next-line no-console
        console.error(chalk.red(error?.message ?? error));
        process.exitCode = 1;
      }
    });
}
