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
import { registerConfigureCommand } from "./commands/configure";
import { registerIntentCommands } from "./commands/intents";
import {
  getProjectRoot,
  getDefaultCustomer,
  getDeploymentsDir,
  getKeysDir,
  setStorageOverrides,
  getDeploymentsRoot,
  getKeysRoot,
} from "./utils/paths";
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

  program
    .name("forge-wrapper")
    .description("Utility CLI wrapping forge and cast")
    .version(loadPackageVersion())
    .option("--storage-root <path>", "Base directory to store keys/ deployments")
    .option("--deployments-root <path>", "Directory to store deployment records (overrides storage root)")
    .option("--keys-root <path>", "Directory to store keys (overrides storage root)");

  registerDeployCommand(program);
  registerCallCommand(program);
  registerDebugCommand(program);
  registerKeysCommand(program);
  registerNetworksCommand(program);
  registerDeploymentsCommand(program);
  registerVerifyCommand(program);
  registerConfigureCommand(program);
  registerIntentCommands(program);

  let storageLogged = false;

  const applyStorageOptions = (opts: StorageOptionFlags): void => {
    setStorageOverrides({
      storageRoot: opts.storageRoot,
      deploymentsRoot: opts.deploymentsRoot,
      keysRoot: opts.keysRoot,
    });
    if (!storageLogged) {
      logStorageLocations();
      storageLogged = true;
    }
  };

  program.hook("preAction", (thisCommand) => {
    applyStorageOptions(thisCommand.optsWithGlobals() as StorageOptionFlags);
  });

  program.action(async () => {
    await runInteractiveMenu();
  });

  program.configureOutput({
    outputError: (str) => {
      // eslint-disable-next-line no-console
      console.error(chalk.red(str.trim()));
    },
  });

  await program.parseAsync(process.argv);
}

main().catch((error) => {
  // eslint-disable-next-line no-console
  console.error(chalk.red(error?.message ?? error));
  process.exit(1);
});

function logStorageLocations(): void {
  const defaultCustomer = getDefaultCustomer();
  const deploymentsRoot = getDeploymentsRoot();
  const keysRoot = getKeysRoot();
  const defaultDeployments = getDeploymentsDir(defaultCustomer);
  const defaultKeys = getKeysDir(defaultCustomer);

  // eslint-disable-next-line no-console
  console.log(
    chalk.gray(
      `Storage directories:\n  deployments root: ${deploymentsRoot}\n  keys root        : ${keysRoot}\n  default customer (${defaultCustomer}) deployments: ${defaultDeployments}\n  default customer (${defaultCustomer}) keys        : ${defaultKeys}`
    )
  );
}

interface StorageOptionFlags {
  storageRoot?: string;
  deploymentsRoot?: string;
  keysRoot?: string;
}
