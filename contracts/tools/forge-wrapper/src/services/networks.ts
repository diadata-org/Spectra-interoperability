import { promises as fs } from "fs";
import path from "path";
import { getNetworksDir } from "../utils/paths";
import { loadNetworkConfig } from "../config";
import { NetworkConfig } from "../types";
import { writeYamlFile, pathExists, readYamlFile } from "../utils/fs";

export async function listNetworkNames(): Promise<string[]> {
  const dir = getNetworksDir();
  try {
    const entries = await fs.readdir(dir, { withFileTypes: true });
    return entries
      .filter((entry) => entry.isFile() && entry.name.endsWith(".yaml"))
      .map((entry) => entry.name.replace(/\.yaml$/, ""))
      .sort();
  } catch (err: any) {
    if (err && err.code === "ENOENT") {
      return [];
    }
    throw err;
  }
}

export async function getNetworkByChainId(chainId: number): Promise<NetworkConfig | undefined> {
  const names = await listNetworkNames();
  for (const name of names) {
    const config = await loadNetworkConfig(name);
    if (config.chain_id === chainId) {
      return config;
    }
  }
  return undefined;
}

export interface NewNetworkInput {
  name: string;
  chainId: number;
  rpcUrl: string;
  forgeProfile?: string;
  defaultAccountAlias?: string;
}

export async function createNetworkConfig(input: NewNetworkInput): Promise<string> {
  const filePath = path.join(getNetworksDir(), `${input.name}.yaml`);
  if (await pathExists(filePath)) {
    throw new Error(`Network config ${input.name} already exists`);
  }

  const content = {
    name: input.name,
    chain_id: input.chainId,
    rpc_url: input.rpcUrl,
    forge_profile: input.forgeProfile || undefined,
    accounts: input.defaultAccountAlias
      ? {
          [input.defaultAccountAlias]: {
            type: "alias",
            name: input.defaultAccountAlias,
          },
        }
      : undefined,
    default_contracts: {},
  };

  await writeYamlFile(filePath, content);
  return filePath;
}

export async function loadNetworkYaml(name: string): Promise<Record<string, unknown>> {
  const filePath = path.join(getNetworksDir(), `${name}.yaml`);
  return readYamlFile<Record<string, unknown>>(filePath, {});
}
