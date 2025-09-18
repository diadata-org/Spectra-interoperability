import chalk from "chalk";
import { Command } from "commander";
import { loadNetworkConfig, prepareCustomerEnvironment, resolveAccountPrivateKey } from "../config";
import { getDeployment } from "../deployments";
import { getDefaultCustomer, getDefaultNetwork } from "../utils/paths";
import { formatCommand, runCast } from "../utils/forge";

function maskArgs(args: string[]): string[] {
  const masked: string[] = [];
  for (let i = 0; i < args.length; i += 1) {
    masked.push(args[i]);
    if ((args[i] === "--private-key" || args[i] === "-p") && i + 1 < args.length) {
      masked.push("***hidden***");
      i += 1;
    }
  }
  return masked;
}

export function registerCallCommand(program: Command): void {
  program
    .command("call <alias> <signature> [params...]")
    .description("Invoke a contract function using cast call/send")
    .option("-n, --network <network>", "Network name")
    .option("-c, --customer <customer>", "Customer namespace")
    .option("--rpc-url <url>", "Override RPC URL")
    .option("--write", "Use cast send (transaction) instead of cast call")
    .option("--account <account>", "Account alias for write calls", "deployer")
    .option("--dry-run", "Print cast command without executing")
    .action(async (alias: string, signature: string, params: string[], options) => {
      const network = options.network ?? getDefaultNetwork();
      if (!network) {
        throw new Error("Network is required (use --network or FORGE_WRAPPER_NETWORK)");
      }
      const customer = options.customer ?? getDefaultCustomer();
      await prepareCustomerEnvironment(customer);

      try {
        const record = await getDeployment(customer, network, alias);
        if (!record) {
          throw new Error(`No deployment found for alias '${alias}' on network '${network}'`);
        }

        const networkConfig = await loadNetworkConfig(network);
        const rpcUrl = options.rpcUrl ?? networkConfig.rpc_url;

        const baseArgs = [record.address, signature, ...params];
        const castArgs: string[] = [];

        if (options.write) {
          const accountAlias = options.account ?? "deployer";
          const privateKey = await resolveAccountPrivateKey(networkConfig, accountAlias, customer);
          if (!privateKey) {
            throw new Error(
              `No private key configured for account '${accountAlias}'. Provide --account with alias or use --dry-run.`
            );
          }
          castArgs.push("send", ...baseArgs, "--rpc-url", rpcUrl, "--private-key", privateKey);
        } else {
          castArgs.push("call", ...baseArgs, "--rpc-url", rpcUrl);
        }

        const printable = formatCommand("cast", maskArgs(castArgs));
        // eslint-disable-next-line no-console
        console.log(chalk.gray(printable));

        if (options.dryRun) {
          // eslint-disable-next-line no-console
          console.log(chalk.yellow("Dry run enabled, not executing cast command."));
          return;
        }

        const result = await runCast(castArgs);
        const output = result.stdout.trim();
        // eslint-disable-next-line no-console
        console.log(output.length ? output : chalk.gray("(no output)"));
      } catch (error: any) {
        // eslint-disable-next-line no-console
        console.error(chalk.red(`Call failed: ${error?.message ?? error}`));
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
