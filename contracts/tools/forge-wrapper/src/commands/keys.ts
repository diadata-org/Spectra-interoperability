import chalk from "chalk";
import { Command } from "commander";
import { readFileSync } from "fs";
import { getDefaultCustomer } from "../utils/paths";
import { prepareCustomerEnvironment } from "../config";
import {
  listKeyAliases,
  listKeySummaries,
  storePrivateKey,
  normalizePrivateKey,
} from "../services/keys";

function resolveKeyValue(options: {
  fromEnv?: string;
  fromFile?: string;
  value?: string;
}): string {
  const provided = [options.fromEnv, options.fromFile, options.value].filter(Boolean).length;
  if (provided !== 1) {
    throw new Error("Provide exactly one of --from-env, --from-file, or --value");
  }

  if (options.value) {
    return options.value.trim();
  }

  if (options.fromEnv) {
    const envVal = process.env[options.fromEnv];
    if (!envVal) {
      throw new Error(`Environment variable ${options.fromEnv} is not set`);
    }
    return envVal.trim();
  }

  if (options.fromFile) {
    const raw = readFileSync(options.fromFile, "utf8");
    return raw.trim();
  }

  throw new Error("Unable to resolve key value");
}

export function registerKeysCommand(program: Command): void {
  const keys = program.command("keys").description("Manage private keys stored under keys/<customer>");

  keys
    .command("list")
    .description("List stored key aliases")
    .option("-c, --customer <customer>")
    .action(async (cmdOptions) => {
      const customer = cmdOptions.customer ?? getDefaultCustomer();
      await prepareCustomerEnvironment(customer);
      const summaries = await listKeySummaries(customer);
      if (summaries.length === 0) {
        // eslint-disable-next-line no-console
        console.log(chalk.gray(`No keys stored for customer '${customer}'.`));
      } else {
        // eslint-disable-next-line no-console
        console.log(chalk.green(`Keys for ${customer}:`));
        for (const entry of summaries) {
          const addressLabel = entry.address ?? chalk.gray("(address unknown)");
          // eslint-disable-next-line no-console
          console.log(`- ${entry.alias}  ( ${addressLabel} )`);
        }
      }
    });

  keys
    .command("import")
    .description("Import a private key into the keystore")
    .requiredOption("--name <alias>", "Key alias")
    .option("-c, --customer <customer>")
    .option("--from-env <VAR>", "Read key from environment variable")
    .option("--from-file <path>", "Read key from file")
    .option("--value <hex>", "Provide key as literal value")
    .option("--overwrite", "Overwrite existing key")
    .action(async (cmdOptions) => {
      const customer = cmdOptions.customer ?? getDefaultCustomer();
      await prepareCustomerEnvironment(customer);

      try {
        const keyValue = resolveKeyValue({
          fromEnv: cmdOptions.fromEnv,
          fromFile: cmdOptions.fromFile,
          value: cmdOptions.value,
        });

        const normalized = normalizePrivateKey(keyValue);
        const info = await storePrivateKey(
          customer,
          cmdOptions.name,
          normalized,
          Boolean(cmdOptions.overwrite)
        );
        // eslint-disable-next-line no-console
        console.log(chalk.green(`Stored key '${cmdOptions.name}' for customer '${customer}'.`));
        // eslint-disable-next-line no-console
        console.log(chalk.gray(`metadata: ${info.metadataPath}`));
        if (info.address) {
          // eslint-disable-next-line no-console
          console.log(chalk.gray(`address: ${info.address}`));
        }
      } catch (error: any) {
        // eslint-disable-next-line no-console
        console.error(chalk.red(`Failed to import key: ${error?.message ?? error}`));
        process.exitCode = 1;
      }
    });
}
