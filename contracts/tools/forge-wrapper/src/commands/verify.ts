import chalk from "chalk";
import { Command } from "commander";
import { loadNetworkConfig, resolveVerificationConfig } from "../config";
import { getDefaultCustomer, getDefaultNetwork } from "../utils/paths";
import { getDeployment, loadDeployments, saveDeployments } from "../deployments";
import { DeploymentRecord, NetworkConfig } from "../types";
import { runForge, runCast, formatCommand } from "../utils/forge";
import { timestampNow } from "../utils/dates";
import { getTemplate } from "../utils/templates";

interface VerifyOptions {
  alias?: string;
  customer?: string;
  network?: string;
  apiKey?: string;
  chain?: string;
  watch?: boolean;
  dryRun?: boolean;
}

export interface VerifyContext {
  customer: string;
  network: string;
  alias: string;
  record: DeploymentRecord;
  networkConfig: NetworkConfig;
  apiKey?: string;
  chain?: string;
  watch?: boolean;
  verifier?: string;
  verifierUrl?: string;
}

export function registerVerifyCommand(program: Command): void {
  program
    .command("verify <alias>")
    .description("Verify a deployed contract on the configured block explorer")
    .option("-c, --customer <customer>", "Customer namespace", getDefaultCustomer())
    .option("-n, --network <network>", "Network name", getDefaultNetwork())
    .option("--api-key <apiKey>", "Explorer API key override")
    .option("--chain <chain>", "Explorer chain identifier override")
    .option("--watch", "Pass --watch to forge verify-contract")
    .option("--dry-run", "Print command without executing")
    .action(async (alias: string, options: VerifyOptions) => {
      const network = options.network ?? getDefaultNetwork();
      if (!network) {
        throw new Error("Network is required (use --network or FORGE_WRAPPER_NETWORK)");
      }
      const customer = options.customer ?? getDefaultCustomer();

      try {
        const context = await buildVerifyContext({
          alias,
          customer,
          network,
          apiKey: options.apiKey,
          chain: options.chain,
          watch: options.watch,
        });

        if (options.dryRun) {
          const { args, env, masked } = await buildForgeVerifyArgs(context);
          // eslint-disable-next-line no-console
          console.log(chalk.gray(masked));
          return;
        }

        await verifyDeployment(context);
        // eslint-disable-next-line no-console
        console.log(chalk.green(`Verification submitted for ${context.alias} (${context.record.address})`));
      } catch (error: any) {
        // eslint-disable-next-line no-console
        console.error(chalk.red(error?.message ?? error));
        process.exitCode = 1;
      }
    });
}

export async function buildVerifyContext(options: {
  alias: string;
  customer: string;
  network: string;
  apiKey?: string;
  chain?: string;
  watch?: boolean;
}): Promise<VerifyContext> {
  const networkConfig = await loadNetworkConfig(options.network);
  const record = await getDeployment(options.customer, options.network, options.alias);
  if (!record) {
    throw new Error(`Deployment for alias '${options.alias}' not found on ${options.network}`);
  }

  const verification = resolveVerificationConfig(networkConfig, {
    apiKey: options.apiKey,
    chain: options.chain,
    watch: options.watch,
  });

  if (!verification) {
    throw new Error(
      `No verification configuration found for network '${options.network}'. Update networks/${options.network}.yaml`
    );
  }

  return {
    customer: options.customer,
    network: options.network,
    alias: options.alias,
    record,
    networkConfig,
    apiKey: verification.apiKey,
    chain: verification.config.chain,
    watch: verification.config.watch,
    verifier: verification.config.verifier,
    verifierUrl: verification.config.verifier_url,
  };
}

export async function verifyDeployment(context: VerifyContext): Promise<void> {
  const { args, env, masked } = await buildForgeVerifyArgs(context);
  // eslint-disable-next-line no-console
  console.log(chalk.gray(masked));

  const result = await runForge(args, {
    env,
  });

  const stdout = result.stdout.trim();
  if (stdout.length) {
    // eslint-disable-next-line no-console
    console.log(stdout);
  }

  const deployments = await loadDeployments(context.customer, context.network);
  const existing = deployments.current[context.alias];
  if (!existing) {
    throw new Error(`Deployment record for ${context.alias} disappeared during verification`);
  }

  existing.verification = {
    status: "success",
    timestamp: timestampNow(),
    explorerUrl: context.networkConfig.verification?.explorer_url,
  };

  deployments.current[context.alias] = existing;
  await saveDeployments(context.customer, context.network, deployments);
}

interface ForgeVerifyCommand {
  args: string[];
  env: Record<string, string>;
  masked: string;
}

async function buildForgeVerifyArgs(context: VerifyContext): Promise<ForgeVerifyCommand> {
  const { record, networkConfig } = context;
  const args: string[] = ["verify-contract"];

  if (context.chain) {
    args.push("--chain", context.chain);
  } else {
    args.push("--chain-id", String(networkConfig.chain_id));
  }

  args.push("--rpc-url", networkConfig.rpc_url);

  const encodedConstructorArgs = await encodeConstructorArgs(record);
  if (encodedConstructorArgs) {
    args.push("--constructor-args", encodedConstructorArgs);
  }

  if (context.watch) {
    args.push("--watch");
  } else if (networkConfig.verification?.watch) {
    args.push("--watch");
  }

  if (context.verifier) {
    args.push("--verifier", context.verifier);
  }

  if (context.verifierUrl) {
    args.push("--verifier-url", context.verifierUrl);
  }

  if (context.apiKey) {
    args.push("--etherscan-api-key", context.apiKey);
  }
  args.push(record.address, record.artifact);

  const env: Record<string, string> = {};
  if (networkConfig.forge_profile) {
    env.FOUNDRY_PROFILE = networkConfig.forge_profile;
  }

  const maskedArgs = maskArgs(args);
  const masked = formatCommand("forge", maskedArgs);

  return { args, env, masked };
}

function maskArgs(args: string[]): string[] {
  const masked: string[] = [];
  for (let i = 0; i < args.length; i += 1) {
    const value = args[i];
    masked.push(value);
    if (value === "--etherscan-api-key" && i + 1 < args.length) {
      masked.push("***hidden***");
      i += 1;
    }
  }
  return masked;
}

async function encodeConstructorArgs(record: DeploymentRecord): Promise<string | undefined> {
  if (!record.constructorArgs.length) {
    return undefined;
  }

  const template = getTemplate(record.alias);
  const signature = template?.constructorSignature;
  if (!signature) {
    throw new Error(
      `Constructor signature not found for ${record.alias}. Update templates/contracts.yaml (constructorSignature).`
    );
  }

  const encodeArgs = [signature, ...record.constructorArgs];
  let result;
  try {
    result = await runCast(["abi-encode", ...encodeArgs]);
  } catch (error: any) {
    const stderr = (error?.stderr ?? "").trim();
    throw new Error(
      `Failed to encode constructor args for ${record.alias}. Ensure templates/contracts.yaml has the correct signature.\n${stderr}`
    );
  }
  return result.stdout.trim();
}
