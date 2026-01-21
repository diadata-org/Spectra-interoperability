import chalk from "chalk";
import { Command } from "commander";
import { getDefaultCustomer } from "../utils/paths";
import { prepareCustomerEnvironment } from "../config";
import { listDeployments } from "../services/deployments";

export function registerDeploymentsCommand(program: Command): void {
  const deployments = program
    .command("deployments")
    .description("Inspect stored deployment addresses");

  deployments
    .command("list")
    .description("List deployments for a customer")
    .option("-c, --customer <customer>")
    .option("-n, --network <network>")
    .action(async (options) => {
      const customer = options.customer ?? getDefaultCustomer();
      await prepareCustomerEnvironment(customer);
      const entries = await listDeployments(customer, options.network);
      if (entries.length === 0) {
        // eslint-disable-next-line no-console
        console.log(chalk.gray(`No deployments recorded for ${customer}.`));
        return;
      }
      for (const entry of entries) {
        // eslint-disable-next-line no-console
        console.log(`${entry.network} :: ${entry.alias} -> ${entry.address} (${entry.deployedAt})`);
      }
    });
}
