import chalk from "chalk";
import { Command } from "commander";
import {
  loadNetworkConfig,
  prepareCustomerEnvironment,
  resolveAccountPrivateKey,
} from "../config";
import { getDeployment } from "../deployments";
import { getDefaultCustomer, getDefaultNetwork } from "../utils/paths";
import { DeploymentRecord } from "../types";
import { formatCommand, runCast, runForge } from "../utils/forge";

function maskCastArgs(args: string[]): string[] {
  const masked: string[] = [];
  for (let i = 0; i < args.length; i += 1) {
    masked.push(args[i]);
    if (args[i] === "--private-key" && i + 1 < args.length) {
      masked.push("***hidden***");
      i += 1;
    }
  }
  return masked;
}

function getContractName(record: DeploymentRecord): string {
  const artifact = record.artifact ?? "";
  const parts = artifact.split(":");
  const name = parts[parts.length - 1]?.trim();
  if (!name) {
    throw new Error(`Unable to determine contract name from artifact '${artifact}'`);
  }
  return name;
}

function requireContract(record: DeploymentRecord, allowed: string[]): string {
  const name = getContractName(record);
  if (!allowed.includes(name)) {
    throw new Error(
      `Action not supported for contract '${name}'. Supported contracts: ${allowed.join(", ")}`
    );
  }
  return name;
}

function parseBoolean(value: string, label: string): boolean {
  const normalized = value.trim().toLowerCase();
  if (["true", "1", "yes", "y"].includes(normalized)) {
    return true;
  }
  if (["false", "0", "no", "n"].includes(normalized)) {
    return false;
  }
  throw new Error(`Invalid boolean for ${label}: '${value}'. Use true/false.`);
}

function assertAddress(value: string, label: string): string {
  const normalized = value.trim();
  if (!/^0x[0-9a-fA-F]{40}$/.test(normalized)) {
    throw new Error(`Invalid address for ${label}: '${value}'`);
  }
  return normalized;
}

function buildCastArgs(
  rpcUrl: string,
  privateKey: string,
  to: string,
  signature: string,
  params: string[],
  value?: string,
  useLegacy?: boolean
): string[] {
  const args = ["send", to, signature, ...params];
  const trimmedValue = value?.trim();
  if (trimmedValue && trimmedValue.length > 0) {
    args.push("--value", trimmedValue);
  }
  args.push("--rpc-url", rpcUrl, "--private-key", privateKey);
  if (useLegacy) {
    args.push("--legacy");
  }
  return args;
}

export async function executeContractSend(
  options: {
    network: string;
    customer: string;
    alias: string;
    account: string;
    rpcUrl?: string;
    signature: string;
    params: string[];
    dryRun?: boolean;
    value?: string;
  }
): Promise<void> {
  await prepareCustomerEnvironment(options.customer);

  const record = await getDeployment(options.customer, options.network, options.alias);
  if (!record) {
    throw new Error(
      `No deployment found for alias '${options.alias}' on network '${options.network}'`
    );
  }

  const networkConfig = await loadNetworkConfig(options.network);
  const rpcUrl = options.rpcUrl ?? networkConfig.rpc_url;
  const privateKey = await resolveAccountPrivateKey(networkConfig, options.account, options.customer);
  if (!privateKey) {
    throw new Error(
      `No private key configured for account '${options.account}'. Update network config or use --account/--private-key override.`
    );
  }

  const castArgs = buildCastArgs(
    rpcUrl,
    privateKey,
    record.address,
    options.signature,
    options.params,
    options.value,
    networkConfig.legacy
  );
  const printable = formatCommand("cast", maskCastArgs(castArgs));
  // eslint-disable-next-line no-console
  console.log(chalk.gray(printable));

  if (options.dryRun) {
    // eslint-disable-next-line no-console
    console.log(chalk.yellow("Dry run enabled, not executing cast send."));
    return;
  }

  try {
    const result = await runCast(castArgs);
    const output = result.stdout.trim();
    // eslint-disable-next-line no-console
    console.log(output.length ? output : chalk.gray("(tx submitted)"));
  } catch (error: any) {
    // eslint-disable-next-line no-console
    console.error(chalk.red(error?.message ?? error));
    if (error?.stdout) {
      // eslint-disable-next-line no-console
      console.error(chalk.red(error.stdout));
    }
    if (error?.stderr) {
      // eslint-disable-next-line no-console
      console.error(chalk.red(error.stderr));
    }
    throw error;
  }
}

export interface ConfigureTransactionOptions {
  network: string;
  customer: string;
  alias: string;
  account: string;
  rpcUrl?: string;
  dryRun?: boolean;
}

export async function configureSetSignerAuthorization(
  base: ConfigureTransactionOptions,
  params: { signer: string; status: boolean }
): Promise<void> {
  const record = await getDeployment(base.customer, base.network, base.alias);
  if (!record) {
    throw new Error(
      `No deployment found for alias '${base.alias}' on network '${base.network}'`
    );
  }
  requireContract(record, ["OracleIntentRegistry", "PushOracleReceiverV2"]);

  const signer = assertAddress(params.signer, "signer");
  await executeContractSend({
    ...base,
    signature: "setSignerAuthorization(address,bool)",
    params: [signer, params.status ? "true" : "false"],
  });
}

export async function configureOracleTriggerAddChain(
  base: ConfigureTransactionOptions,
  params: { chainId: number; recipient: string }
): Promise<void> {
  const record = await getDeployment(base.customer, base.network, base.alias);
  if (!record) {
    throw new Error(
      `No deployment found for alias '${base.alias}' on network '${base.network}'`
    );
  }
  requireContract(record, ["OracleTriggerV2"]);

  if (!Number.isInteger(params.chainId) || params.chainId < 0 || params.chainId > 0xffffffff) {
    throw new Error(`chainId must be a 32-bit unsigned integer.`);
  }
  const recipient = assertAddress(params.recipient, "recipient");

  await executeContractSend({
    ...base,
    signature: "addChain(uint32,address)",
    params: [String(params.chainId), recipient],
  });
}

export async function configureOracleTriggerUpdateRegistry(
  base: ConfigureTransactionOptions,
  params: { registry: string }
): Promise<void> {
  const record = await getDeployment(base.customer, base.network, base.alias);
  if (!record) {
    throw new Error(
      `No deployment found for alias '${base.alias}' on network '${base.network}'`
    );
  }
  requireContract(record, ["OracleTriggerV2"]);

  const registry = assertAddress(params.registry, "registry");
  await executeContractSend({
    ...base,
    signature: "updateIntentRegistryContract(address)",
    params: [registry],
  });
}

export async function configurePushOracleSetIsm(
  base: ConfigureTransactionOptions,
  params: { ism: string }
): Promise<void> {
  const record = await getDeployment(base.customer, base.network, base.alias);
  if (!record) {
    throw new Error(
      `No deployment found for alias '${base.alias}' on network '${base.network}'`
    );
  }
  requireContract(record, ["PushOracleReceiverV2"]);

  const ism = assertAddress(params.ism, "ism");
  await executeContractSend({
    ...base,
    signature: "setInterchainSecurityModule(address)",
    params: [ism],
  });
}

export async function configurePushOracleSetMailbox(
  base: ConfigureTransactionOptions,
  params: { mailbox: string }
): Promise<void> {
  const record = await getDeployment(base.customer, base.network, base.alias);
  if (!record) {
    throw new Error(
      `No deployment found for alias '${base.alias}' on network '${base.network}'`
    );
  }
  requireContract(record, ["PushOracleReceiverV2"]);

  const mailbox = assertAddress(params.mailbox, "mailbox");
  await executeContractSend({
    ...base,
    signature: "setTrustedMailBox(address)",
    params: [mailbox],
  });
}

export async function configureIsmAddSender(
  base: ConfigureTransactionOptions,
  params: { originDomain: number; sender: string }
): Promise<void> {
  const record = await getDeployment(base.customer, base.network, base.alias);
  if (!record) {
    throw new Error(
      `No deployment found for alias '${base.alias}' on network '${base.network}'`
    );
  }
  requireContract(record, ["Ism"]);

  const originDomain = Number(params.originDomain);
  if (!Number.isInteger(originDomain) || originDomain < 0 || originDomain > 0xffffffff) {
    throw new Error("originDomain must be a uint32 value");
  }
  const sender = assertAddress(params.sender, "sender");

  await executeContractSend({
    ...base,
    signature: "addSenderShouldBe(uint32,address)",
    params: [String(originDomain), sender],
  });
}

export async function configureIsmRemoveSender(
  base: ConfigureTransactionOptions,
  params: { originDomain: number; sender: string }
): Promise<void> {
  const record = await getDeployment(base.customer, base.network, base.alias);
  if (!record) {
    throw new Error(
      `No deployment found for alias '${base.alias}' on network '${base.network}'`
    );
  }
  requireContract(record, ["Ism"]);

  const originDomain = Number(params.originDomain);
  if (!Number.isInteger(originDomain) || originDomain < 0 || originDomain > 0xffffffff) {
    throw new Error("originDomain must be a uint32 value");
  }
  const sender = assertAddress(params.sender, "sender");

  await executeContractSend({
    ...base,
    signature: "removeSenderShouldBe(uint32,address)",
    params: [String(originDomain), sender],
  });
}

export async function configureIsmSetMailbox(
  base: ConfigureTransactionOptions,
  params: { mailbox: string }
): Promise<void> {
  const record = await getDeployment(base.customer, base.network, base.alias);
  if (!record) {
    throw new Error(
      `No deployment found for alias '${base.alias}' on network '${base.network}'`
    );
  }
  requireContract(record, ["Ism"]);

  const mailbox = assertAddress(params.mailbox, "mailbox");
  await executeContractSend({
    ...base,
    signature: "setTrustedMailBox(address)",
    params: [mailbox],
  });
}

interface AbiInput {
  name?: string;
  type: string;
}

interface AbiFunctionItem {
  type?: string;
  name?: string;
  stateMutability?: string;
  inputs?: AbiInput[];
  outputs?: AbiInput[];
}

export interface ContractFunctionFragment {
  name: string;
  inputs: AbiInput[];
  outputs: AbiInput[];
  stateMutability: string;
  signature: string;
  payable: boolean;
  constant: boolean;
}

function buildFunctionSignature(name: string, inputs: AbiInput[]): string {
  const params = (inputs ?? []).map((input) => input.type ?? "");
  return `${name}(${params.join(",")})`;
}

export async function loadContractFunctions(artifact: string): Promise<ContractFunctionFragment[]> {
  let result;
  try {
    result = await runForge(["inspect", artifact, "abi", "--json"]);
  } catch (error: any) {
    const message = error?.message ?? error;
    throw new Error(`Failed to inspect ABI for ${artifact}: ${message}`);
  }

  let parsed: unknown;
  try {
    const output = (result.stdout ?? "").trim();
    parsed = output.length ? JSON.parse(output) : [];
  } catch (error: any) {
    throw new Error(`Unable to parse ABI for ${artifact}: ${error instanceof Error ? error.message : String(error)}`);
  }

  if (!Array.isArray(parsed)) {
    throw new Error(`Unexpected ABI format for ${artifact}`);
  }

  const fragments: ContractFunctionFragment[] = [];
  for (const item of parsed as AbiFunctionItem[]) {
    if (!item || item.type !== "function" || !item.name) {
      continue;
    }
    const name = item.name;
    const inputs = Array.isArray(item.inputs) ? item.inputs : [];
    const outputs = Array.isArray(item.outputs) ? item.outputs : [];
    const stateMutability = item.stateMutability ?? "nonpayable";
    const signature = buildFunctionSignature(name, inputs);
    const constant = stateMutability === "view" || stateMutability === "pure";
    const payable = stateMutability === "payable";

    fragments.push({
      name,
      inputs,
      outputs,
      stateMutability,
      signature,
      constant,
      payable,
    });
  }

  return fragments.sort((a, b) => a.signature.localeCompare(b.signature));
}

export function registerConfigureCommand(program: Command): void {
  const configure = program.command("configure").description("Configure deployed contracts");

  configure
    .command("set-signer-authorization <alias>")
    .description("Authorize or revoke signers on OracleIntentRegistry/PushOracleReceiverV2")
    .requiredOption("--signer <address>", "Signer address")
    .option("--status <status>", "Authorization status", "true")
    .option("-n, --network <network>", "Network name")
    .option("-c, --customer <customer>", "Customer namespace")
    .option("--account <account>", "Account alias", "admin")
    .option("--rpc-url <url>", "Override RPC URL")
    .option("--dry-run", "Print cast command without executing")
    .action(async (alias: string, cmdOptions) => {
      const network = cmdOptions.network ?? getDefaultNetwork();
      if (!network) {
        throw new Error("Network is required (use --network or FORGE_WRAPPER_NETWORK)");
      }
      const customer = cmdOptions.customer ?? getDefaultCustomer();
      const status = parseBoolean(String(cmdOptions.status), "status");
      await configureSetSignerAuthorization(
        {
          network,
          customer,
          alias,
          account: cmdOptions.account ?? "admin",
          rpcUrl: cmdOptions.rpcUrl,
          dryRun: Boolean(cmdOptions.dryRun),
        },
        { signer: String(cmdOptions.signer), status }
      );
    });

  configure
    .command("add-chain <alias>")
    .description("Add a destination chain on OracleTriggerV2")
    .requiredOption("--chain-id <id>", "Destination chain id", (value: string) => parseInt(value, 10))
    .requiredOption("--recipient <address>", "Recipient contract address")
    .option("-n, --network <network>", "Network name")
    .option("-c, --customer <customer>", "Customer namespace")
    .option("--account <account>", "Account alias", "admin")
    .option("--rpc-url <url>", "Override RPC URL")
    .option("--dry-run", "Print cast command without executing")
    .action(async (alias: string, cmdOptions) => {
      const network = cmdOptions.network ?? getDefaultNetwork();
      if (!network) {
        throw new Error("Network is required (use --network or FORGE_WRAPPER_NETWORK)");
      }
      const customer = cmdOptions.customer ?? getDefaultCustomer();
      await configureOracleTriggerAddChain(
        {
          network,
          customer,
          alias,
          account: cmdOptions.account ?? "admin",
          rpcUrl: cmdOptions.rpcUrl,
          dryRun: Boolean(cmdOptions.dryRun),
        },
        {
          chainId: cmdOptions.chainId,
          recipient: String(cmdOptions.recipient),
        }
      );
    });

  configure
    .command("update-intent-registry <alias>")
    .description("Update OracleTriggerV2's intent registry address")
    .requiredOption("--registry <address>", "Registry contract address")
    .option("-n, --network <network>", "Network name")
    .option("-c, --customer <customer>", "Customer namespace")
    .option("--account <account>", "Account alias", "admin")
    .option("--rpc-url <url>", "Override RPC URL")
    .option("--dry-run", "Print cast command without executing")
    .action(async (alias: string, cmdOptions) => {
      const network = cmdOptions.network ?? getDefaultNetwork();
      if (!network) {
        throw new Error("Network is required (use --network or FORGE_WRAPPER_NETWORK)");
      }
      const customer = cmdOptions.customer ?? getDefaultCustomer();
      await configureOracleTriggerUpdateRegistry(
        {
          network,
          customer,
          alias,
          account: cmdOptions.account ?? "admin",
          rpcUrl: cmdOptions.rpcUrl,
          dryRun: Boolean(cmdOptions.dryRun),
        },
        {
          registry: String(cmdOptions.registry),
        }
      );
    });

  configure
    .command("set-ism <alias>")
    .description("Configure PushOracleReceiverV2 interchain security module")
    .requiredOption("--ism <address>", "ISM contract address")
    .option("-n, --network <network>", "Network name")
    .option("-c, --customer <customer>", "Customer namespace")
    .option("--account <account>", "Account alias", "admin")
    .option("--rpc-url <url>", "Override RPC URL")
    .option("--dry-run", "Print cast command without executing")
    .action(async (alias: string, cmdOptions) => {
      const network = cmdOptions.network ?? getDefaultNetwork();
      if (!network) {
        throw new Error("Network is required (use --network or FORGE_WRAPPER_NETWORK)");
      }
      const customer = cmdOptions.customer ?? getDefaultCustomer();
      await configurePushOracleSetIsm(
        {
          network,
          customer,
          alias,
          account: cmdOptions.account ?? "admin",
          rpcUrl: cmdOptions.rpcUrl,
          dryRun: Boolean(cmdOptions.dryRun),
        },
        {
          ism: String(cmdOptions.ism),
        }
      );
    });

  configure
    .command("set-mailbox <alias>")
    .description("Configure PushOracleReceiverV2 trusted mailbox")
    .requiredOption("--mailbox <address>", "Mailbox contract address")
    .option("-n, --network <network>", "Network name")
    .option("-c, --customer <customer>", "Customer namespace")
    .option("--account <account>", "Account alias", "admin")
    .option("--rpc-url <url>", "Override RPC URL")
    .option("--dry-run", "Print cast command without executing")
    .action(async (alias: string, cmdOptions) => {
      const network = cmdOptions.network ?? getDefaultNetwork();
      if (!network) {
        throw new Error("Network is required (use --network or FORGE_WRAPPER_NETWORK)");
      }
      const customer = cmdOptions.customer ?? getDefaultCustomer();
      await configurePushOracleSetMailbox(
        {
          network,
          customer,
          alias,
          account: cmdOptions.account ?? "admin",
          rpcUrl: cmdOptions.rpcUrl,
          dryRun: Boolean(cmdOptions.dryRun),
        },
        {
          mailbox: String(cmdOptions.mailbox),
        }
      );
    });

  configure
    .command("ism-add-sender <alias>")
    .description("Allow a sender for an origin domain on an Ism deployment")
    .requiredOption(
      "--origin-domain <id>",
      "Origin domain identifier",
      (value: string) => parseInt(value, 10)
    )
    .requiredOption("--sender <address>", "Sender contract address")
    .option("-n, --network <network>", "Network name")
    .option("-c, --customer <customer>", "Customer namespace")
    .option("--account <account>", "Account alias", "admin")
    .option("--rpc-url <url>", "Override RPC URL")
    .option("--dry-run", "Print cast command without executing")
    .action(async (alias: string, cmdOptions) => {
      const network = cmdOptions.network ?? getDefaultNetwork();
      if (!network) {
        throw new Error("Network is required (use --network or FORGE_WRAPPER_NETWORK)");
      }
      const customer = cmdOptions.customer ?? getDefaultCustomer();
      await configureIsmAddSender(
        {
          network,
          customer,
          alias,
          account: cmdOptions.account ?? "admin",
          rpcUrl: cmdOptions.rpcUrl,
          dryRun: Boolean(cmdOptions.dryRun),
        },
        {
          originDomain: cmdOptions.originDomain,
          sender: String(cmdOptions.sender),
        }
      );
    });

  configure
    .command("ism-remove-sender <alias>")
    .description("Remove an allowed sender for an origin domain on an Ism deployment")
    .requiredOption(
      "--origin-domain <id>",
      "Origin domain identifier",
      (value: string) => parseInt(value, 10)
    )
    .requiredOption("--sender <address>", "Sender contract address")
    .option("-n, --network <network>", "Network name")
    .option("-c, --customer <customer>", "Customer namespace")
    .option("--account <account>", "Account alias", "admin")
    .option("--rpc-url <url>", "Override RPC URL")
    .option("--dry-run", "Print cast command without executing")
    .action(async (alias: string, cmdOptions) => {
      const network = cmdOptions.network ?? getDefaultNetwork();
      if (!network) {
        throw new Error("Network is required (use --network or FORGE_WRAPPER_NETWORK)");
      }
      const customer = cmdOptions.customer ?? getDefaultCustomer();
      await configureIsmRemoveSender(
        {
          network,
          customer,
          alias,
          account: cmdOptions.account ?? "admin",
          rpcUrl: cmdOptions.rpcUrl,
          dryRun: Boolean(cmdOptions.dryRun),
        },
        {
          originDomain: cmdOptions.originDomain,
          sender: String(cmdOptions.sender),
        }
      );
    });

  configure
    .command("ism-set-mailbox <alias>")
    .description("Set the trusted mailbox on an Ism deployment")
    .requiredOption("--mailbox <address>", "Mailbox contract address")
    .option("-n, --network <network>", "Network name")
    .option("-c, --customer <customer>", "Customer namespace")
    .option("--account <account>", "Account alias", "admin")
    .option("--rpc-url <url>", "Override RPC URL")
    .option("--dry-run", "Print cast command without executing")
    .action(async (alias: string, cmdOptions) => {
      const network = cmdOptions.network ?? getDefaultNetwork();
      if (!network) {
        throw new Error("Network is required (use --network or FORGE_WRAPPER_NETWORK)");
      }
      const customer = cmdOptions.customer ?? getDefaultCustomer();
      await configureIsmSetMailbox(
        {
          network,
          customer,
          alias,
          account: cmdOptions.account ?? "admin",
          rpcUrl: cmdOptions.rpcUrl,
          dryRun: Boolean(cmdOptions.dryRun),
        },
        {
          mailbox: String(cmdOptions.mailbox),
        }
      );
    });

}
