import chalk from "chalk";
import { Command } from "commander";
import { listNetworkNames } from "../services/networks";
import { loadNetworkConfig } from "../config";
import { NetworkConfig } from "../types";

export function registerNetworksCommand(program: Command): void {
  const networks = program.command("networks").description("Inspect configured networks");

  networks
    .command("list")
    .description("List available networks")
    .option("--details", "Show chain ID and RPC URL")
    .option("--filter <environment>", "Filter by environment (mainnet|testnet)")
    .action(async (options) => {
      const names = await listNetworkNames();
      if (names.length === 0) {
        // eslint-disable-next-line no-console
        console.log(chalk.gray("No networks configured."));
        return;
      }
      for (const name of names) {
        const config = await loadNetworkConfig(name);
        const classification = classifyNetwork(config);
        const filter = options.filter ? String(options.filter).toLowerCase() : undefined;
        if (filter === "mainnet" && classification !== "mainnet") {
          continue;
        }
        if (filter === "testnet" && classification !== "testnet") {
          continue;
        }
        const suffix = classification === "unknown" ? "" : ` (${classification})`;
        if (options.details) {
          // eslint-disable-next-line no-console
          console.log(`${name}${suffix} :: chainId=${config.chain_id} rpc=${config.rpc_url}`);
        } else {
          // eslint-disable-next-line no-console
          console.log(`- ${name} :: chainId=${config.chain_id}${suffix}`);
        }
      }
    });
}

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

function classifyNetwork(config: NetworkConfig): "mainnet" | "testnet" | "unknown" {
  if (config.environment === "mainnet") {
    return "mainnet";
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
