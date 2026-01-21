import path from "path";
import { existsSync } from "fs";
import { ensureDir } from "./fs";

const PROJECT_ROOT = path.resolve(__dirname, "..", "..");
const CONFIG_PRIVATE_SUBMODULE = path.join(PROJECT_ROOT, "config-private");

let storageRootOverride: string | undefined;
let deploymentsRootOverride: string | undefined;
let keysRootOverride: string | undefined;

function hasSubmodule(): boolean {
  return existsSync(path.join(CONFIG_PRIVATE_SUBMODULE, ".git"));
}

function sanitizeSegment(input: string): string {
  return input.replace(/[^a-zA-Z0-9_-]/g, "-");
}

export function normalizeNetworkFileName(name: string): string {
  return sanitizeSegment(name).toLowerCase();
}

function resolvedOverride(value: string | undefined): string | undefined {
  return value ? path.resolve(value) : undefined;
}

export function setStorageOverrides(options: {
  storageRoot?: string;
  deploymentsRoot?: string;
  keysRoot?: string;
}): void {
  storageRootOverride = resolvedOverride(options.storageRoot);
  deploymentsRootOverride = resolvedOverride(options.deploymentsRoot);
  keysRootOverride = resolvedOverride(options.keysRoot);
}

export function getProjectRoot(): string {
  return PROJECT_ROOT;
}

function getEnvOverride(envKey: string): string | undefined {
  const value = process.env[envKey];
  return value && value.trim().length > 0 ? path.resolve(value.trim()) : undefined;
}

function resolveStorageRoot(subdir: "deployments" | "keys"): string {
  const envStorageRoot = getEnvOverride("FORGE_WRAPPER_STORAGE_ROOT");
  const baseFromOverride = storageRootOverride ?? envStorageRoot;

  if (baseFromOverride) {
    return path.join(baseFromOverride, subdir);
  }

  // Check submodule first
  if (hasSubmodule()) {
    return path.join(CONFIG_PRIVATE_SUBMODULE, subdir);
  }

  // Fall back to local directories
  return path.join(PROJECT_ROOT, subdir);
}

export function getDeploymentsRoot(): string {
  const envDeploymentsRoot = getEnvOverride("FORGE_WRAPPER_DEPLOYMENTS_DIR");
  return deploymentsRootOverride ?? envDeploymentsRoot ?? resolveStorageRoot("deployments");
}

export function getKeysRoot(): string {
  const envKeysRoot = getEnvOverride("FORGE_WRAPPER_KEYS_DIR");
  return keysRootOverride ?? envKeysRoot ?? resolveStorageRoot("keys");
}

export function getNetworksDir(): string {
  const envNetworksDir = getEnvOverride("FORGE_WRAPPER_NETWORKS_DIR");

  if (envNetworksDir) {
    return envNetworksDir;
  }

  // Check submodule first
  if (hasSubmodule()) {
    return path.join(CONFIG_PRIVATE_SUBMODULE, "networks");
  }

  // Fall back to local directory
  return path.join(getProjectRoot(), "networks");
}

export function getNetworkConfigPath(network: string): string {
  const dir = getNetworksDir();
  const normalized = normalizeNetworkFileName(network);
  const preferred = path.join(dir, `${normalized}.yaml`);
  if (existsSync(preferred)) {
    return preferred;
  }

  const legacy = path.join(dir, `${sanitizeSegment(network)}.yaml`);
  if (existsSync(legacy)) {
    return legacy;
  }

  const raw = path.join(dir, `${network}.yaml`);
  return raw;
}

export function getDeploymentsDir(customer: string): string {
  return path.join(getDeploymentsRoot(), sanitizeSegment(customer));
}

export function getDeploymentFilePath(customer: string, network: string): string {
  return path.join(getDeploymentsDir(customer), `${sanitizeSegment(network)}.yaml`);
}

export function getKeysDir(customer: string): string {
  return path.join(getKeysRoot(), sanitizeSegment(customer));
}

export async function ensureCustomerDirs(customer: string): Promise<void> {
  await ensureDir(getDeploymentsDir(customer));
  await ensureDir(getKeysDir(customer));
}

export function getDefaultCustomer(): string {
  return process.env.FORGE_WRAPPER_CUSTOMER?.trim() || "master";
}

export function getDefaultNetwork(): string | undefined {
  const env = process.env.FORGE_WRAPPER_NETWORK?.trim();
  return env && env.length > 0 ? env : undefined;
}
