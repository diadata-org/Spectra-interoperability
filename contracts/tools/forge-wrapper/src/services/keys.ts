import chalk from "chalk";
import { promises as fs } from "fs";
import path from "path";
import crypto from "crypto";
import { getKeysDir } from "../utils/paths";
import { writeYamlFile, pathExists, readYamlFile } from "../utils/fs";
import { runCast } from "../utils/forge";

export async function listKeyAliases(customer: string): Promise<string[]> {
  const dir = getKeysDir(customer);
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

export interface StoredKeyInfo {
  metadataPath: string;
  address?: string;
}

export interface StoredWalletMeta {
  name: string;
  customer: string;
  privateKey: string;
  address?: string;
  metadataPath: string;
}

export async function storePrivateKey(
  customer: string,
  name: string,
  privateKey: string,
  overwrite = false
): Promise<StoredKeyInfo> {
  const sanitized = normalizePrivateKey(privateKey);
  if (!/^0x[0-9a-fA-F]{64}$/.test(sanitized)) {
    throw new Error("Private key must be a 32-byte hex string");
  }
  const dir = getKeysDir(customer);
  const filePath = path.join(dir, `${name}.yaml`);

  if (!overwrite && (await pathExists(filePath))) {
    throw new Error(`Metadata file already exists for key '${name}'`);
  }

  let address: string | undefined;
  try {
    address = await deriveAddressFromPrivateKey(sanitized);
    await writeYamlFile(filePath, {
      name,
      customer,
      address,
      private_key: sanitized,
    });
  } catch (error) {
    // eslint-disable-next-line no-console
    console.warn(
      `Failed to derive address for key '${name}': ${error instanceof Error ? error.message : error}`
    );
  }

  return { metadataPath: filePath, address };
}

export async function readStoredPrivateKey(customer: string, name: string): Promise<string> {
  return (await readStoredWallet(customer, name)).privateKey;
}

export async function readStoredWallet(customer: string, name: string): Promise<StoredWalletMeta> {
  const metadataPath = path.join(getKeysDir(customer), `${name}.yaml`);
  const data = await readYamlFile<Record<string, unknown> | null>(metadataPath, null);
  if (!data || typeof data.private_key !== "string") {
    throw new Error(`Private key not found in ${metadataPath}`);
  }
  const privateKey = normalizePrivateKey(data.private_key);
  const address = typeof data.address === "string" ? data.address : undefined;

  return {
    name,
    customer,
    privateKey,
    address,
    metadataPath,
  };
}

export function normalizePrivateKey(value: string): string {
  const trimmed = value.trim();
  if (trimmed.startsWith("0x")) {
    return trimmed;
  }
  return `0x${trimmed}`;
}

export function generatePrivateKey(): string {
  return `0x${crypto.randomBytes(32).toString("hex")}`;
}

export function formatKeyList(keys: string[]): string {
  if (keys.length === 0) {
    return chalk.gray("(none)");
  }
  return keys.map((key) => `- ${key}`).join("\n");
}

async function deriveAddressFromPrivateKey(privateKey: string): Promise<string> {
  const result = await runCast(["wallet", "address", "--private-key", privateKey]);
  const address = result.stdout.trim();
  if (!/^0x[0-9a-fA-F]{40}$/.test(address)) {
    throw new Error(`Unexpected address output: ${address}`);
  }
  return address;
}
