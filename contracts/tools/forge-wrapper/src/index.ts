#!/usr/bin/env node
import { Command } from "commander";
import chalk from "chalk";
import { registerDeployCommand } from "./commands/deploy";
import { registerCallCommand } from "./commands/call";
import { registerDebugCommand } from "./commands/debug";
import { registerKeysCommand } from "./commands/keys";
import { runInteractiveMenu } from "./menu";
import { registerNetworksCommand } from "./commands/networks";
import { registerDeploymentsCommand } from "./commands/deployments";
import { registerVerifyCommand } from "./commands/verify";
import { getProjectRoot } from "./utils/paths";
import path from "path";
import { readFileSync } from "fs";

function loadPackageVersion(): string {
  try {
    const pkgPath = path.join(getProjectRoot(), "package.json");
    const pkgRaw = readFileSync(pkgPath, "utf8");
    const pkg = JSON.parse(pkgRaw);
    return pkg.version ?? "0.0.0";
  } catch {
    return "0.0.0";
  }
}

async function main(): Promise<void> {
  const program = new Command();
  program.name("forge-wrapper").description("Utility CLI wrapping forge and cast").version(loadPackageVersion());

  registerDeployCommand(program);
  registerCallCommand(program);
  registerDebugCommand(program);
  registerKeysCommand(program);
  registerNetworksCommand(program);
  registerDeploymentsCommand(program);
  registerVerifyCommand(program);

  program.configureOutput({
    outputError: (str) => {
      // eslint-disable-next-line no-console
      console.error(chalk.red(str.trim()));
    },
  });

  const args = process.argv.slice(2);
  if (args.length === 0) {
    await runInteractiveMenu();
    return;
  }

  await program.parseAsync(process.argv);
}

main().catch((error) => {
  // eslint-disable-next-line no-console
  console.error(chalk.red(error?.message ?? error));
  process.exit(1);
});
