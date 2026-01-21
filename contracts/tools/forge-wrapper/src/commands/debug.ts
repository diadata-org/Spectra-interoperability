import chalk from "chalk";
import { Command } from "commander";
import { getDeployment, loadDeployments } from "../deployments";
import { formatDeploymentRecord } from "../deployments";
import { getDefaultCustomer, getDefaultNetwork } from "../utils/paths";
import { prepareCustomerEnvironment } from "../config";

export function registerDebugCommand(program: Command): void {
  program
    .command("debug <alias>")
    .description("Inspect stored deployment information for an alias")
    .option("-n, --network <network>")
    .option("-c, --customer <customer>")
    .option("--history", "Show history entries as well")
    .action(async (alias: string, options) => {
      const network = options.network ?? getDefaultNetwork();
      if (!network) {
        throw new Error("Network is required (use --network or FORGE_WRAPPER_NETWORK)");
      }
      const customer = options.customer ?? getDefaultCustomer();
      await prepareCustomerEnvironment(customer);

      const record = await getDeployment(customer, network, alias);
      if (!record) {
        // eslint-disable-next-line no-console
        console.error(chalk.red(`No deployment stored for alias '${alias}' on network '${network}'`));
        process.exitCode = 1;
        return;
      }

      // eslint-disable-next-line no-console
      console.log(chalk.green("Current deployment"));
      // eslint-disable-next-line no-console
      console.log(formatDeploymentRecord(record));

      if (options.history) {
        const file = await loadDeployments(customer, network);
        const historical = file.history.filter((entry) => entry.alias === alias);
        if (historical.length) {
          // eslint-disable-next-line no-console
          console.log(chalk.gray("\nHistory:"));
          for (const entry of historical) {
            // eslint-disable-next-line no-console
            console.log(chalk.gray(formatDeploymentRecord(entry)));
            // eslint-disable-next-line no-console
            console.log(chalk.gray("---"));
          }
        } else {
          // eslint-disable-next-line no-console
          console.log(chalk.gray("No historical deployments stored."));
        }
      }
    });
}
