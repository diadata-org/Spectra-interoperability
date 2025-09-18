import path from "path";
import { ensureDir } from "./fs";

const PROJECT_ROOT = path.resolve(__dirname, "..", "..");

function sanitizeSegment(input: string): string {
  return input.replace(/[^a-zA-Z0-9_-]/g, "-");
}

export function getProjectRoot(): string {
  return PROJECT_ROOT;
}

export function getNetworksDir(): string {
  return path.join(PROJECT_ROOT, "networks");
}

export function getNetworkConfigPath(network: string): string {
  const dir = getNetworksDir();
  return path.join(dir, `${sanitizeSegment(network)}.yaml`);
}

export function getDeploymentsDir(customer: string): string {
  return path.join(PROJECT_ROOT, "deployments", sanitizeSegment(customer));
}

export function getDeploymentFilePath(customer: string, network: string): string {
  return path.join(getDeploymentsDir(customer), `${sanitizeSegment(network)}.yaml`);
}

export function getKeysDir(customer: string): string {
  return path.join(PROJECT_ROOT, "keys", sanitizeSegment(customer));
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
