import path from "path";
import { ensureDir } from "./fs";

const PROJECT_ROOT = path.resolve(__dirname, "..", "..");

let storageRootOverride: string | undefined;
let deploymentsRootOverride: string | undefined;
let keysRootOverride: string | undefined;

function sanitizeSegment(input: string): string {
  return input.replace(/[^a-zA-Z0-9_-]/g, "-");
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
  return baseFromOverride ? path.join(baseFromOverride, subdir) : path.join(PROJECT_ROOT, subdir);
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
  return path.join(getProjectRoot(), "networks");
}

export function getNetworkConfigPath(network: string): string {
  const dir = getNetworksDir();
  return path.join(dir, `${sanitizeSegment(network)}.yaml`);
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
