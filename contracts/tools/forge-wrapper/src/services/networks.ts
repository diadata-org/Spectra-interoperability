import { promises as fs } from "fs";
import path from "path";
import { getNetworksDir, normalizeNetworkFileName } from "../utils/paths";
import { loadNetworkConfig } from "../config";
import { NetworkConfig, NetworkEnvironment } from "../types";
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
  environment?: NetworkEnvironment;
  verification?: {
    chain?: string;
    verifier?: string;
    verifierUrl?: string;
    explorerUrl?: string;
    apiKeyEnv?: string;
    apiKeyValue?: string;
    watch?: boolean;
  };
}

export async function createNetworkConfig(input: NewNetworkInput): Promise<string> {
  const fileSafeName = normalizeNetworkFileName(input.name);
  const filePath = path.join(getNetworksDir(), `${fileSafeName}.yaml`);
  if (await pathExists(filePath)) {
    throw new Error(`Network config ${fileSafeName} already exists`);
  }

  const verificationCandidate = input.verification
    ? {
        chain: input.verification.chain || undefined,
        verifier: input.verification.verifier || undefined,
        verifier_url: input.verification.verifierUrl || undefined,
        explorer_url: input.verification.explorerUrl || undefined,
        api_key_env: input.verification.apiKeyEnv || undefined,
        api_key_value: input.verification.apiKeyValue || undefined,
        watch: typeof input.verification.watch === "boolean" ? input.verification.watch : undefined,
      }
    : undefined;

  const hasVerification = verificationCandidate
    ? Object.values(verificationCandidate).some((value) => value !== undefined)
    : false;

  const content = {
    name: fileSafeName,
    chain_id: input.chainId,
    rpc_url: input.rpcUrl,
    forge_profile: input.forgeProfile || undefined,
    environment: input.environment || undefined,
    accounts: input.defaultAccountAlias
      ? {
          [input.defaultAccountAlias]: {
            type: "alias",
            name: input.defaultAccountAlias,
          },
        }
      : undefined,
    default_contracts: {},
    verification: hasVerification ? verificationCandidate : undefined,
  };

  await writeYamlFile(filePath, content);
  return filePath;
}

export async function loadNetworkYaml(name: string): Promise<Record<string, unknown>> {
  const filePath = path.join(getNetworksDir(), `${name}.yaml`);
  return readYamlFile<Record<string, unknown>>(filePath, {});
}
