import { promises as fs } from "fs";
import path from "path";
import { ethers } from "ethers";
import { getProjectRoot } from "./paths";
import { CREATE2_FACTORY, checkFactoryPresence, executeCreate2DeployWithInitCode } from "./create2";

/**
 * Per-chain registry of deployed DIAReceiverFactory instances.
 * Layout: { "<chainId>": { "address": "0x..", "txHash": "0x..", "deployedAt": "ISO" } }
 * Lives in config-private so it stays out of the public repo.
 */
export interface FactoryRegistryEntry {
  address: string;
  txHash?: string;
  deployedAt: string;
}

export type FactoryRegistry = Record<string, FactoryRegistryEntry>;

export const RECEIVER_FACTORY_ARTIFACT = "contracts/DIAReceiverFactory.sol:DIAReceiverFactory";

export function getFactoryRegistryPath(): string {
  return path.join(getProjectRoot(), "config-private", "dia-receiver-factories.json");
}

export async function loadFactoryRegistry(): Promise<FactoryRegistry> {
  try {
    const raw = await fs.readFile(getFactoryRegistryPath(), "utf-8");
    const parsed = JSON.parse(raw);
    return parsed && typeof parsed === "object" ? parsed : {};
  } catch {
    return {};
  }
}

export async function saveFactoryRegistry(registry: FactoryRegistry): Promise<void> {
  await fs.writeFile(getFactoryRegistryPath(), JSON.stringify(registry, null, 2) + "\n", "utf-8");
}

/**
 * The initCode for deploying DIAReceiverFactory itself via the Arachnid proxy.
 * Factory has no constructor args.
 */
export async function buildReceiverFactoryInitCode(): Promise<string> {
  const { getContractsRoot } = await import("./create2");
  const contractsRoot = getContractsRoot();
  const artifactPath = path.join(contractsRoot, "out", "DIAReceiverFactory.sol", "DIAReceiverFactory.json");
  const artifact = JSON.parse(await fs.readFile(artifactPath, "utf-8"));
  const code = artifact.bytecode?.object;
  if (!code || !code.startsWith("0x")) {
    throw new Error("DIAReceiverFactory artifact missing creation bytecode — run forge build first.");
  }
  return code;
}

export const RECEIVER_FACTORY_SALT = ethers.id("DIAReceiverFactory-v1");

/**
 * Predict the DIAReceiverFactory address on any chain (deterministic:
 * Arachnid factory + fixed salt + no constructor args → identical everywhere).
 */
export async function predictReceiverFactoryAddress(): Promise<string> {
  const initCode = await buildReceiverFactoryInitCode();
  return ethers.getCreate2Address(CREATE2_FACTORY, RECEIVER_FACTORY_SALT, ethers.keccak256(initCode));
}

export interface EnsuredFactory {
  address: string;
  reused: boolean;
  txHash?: string;
}

/**
 * Ensure a DIAReceiverFactory exists on the target chain:
 * 1. registry hit + code on-chain  -> reuse
 * 2. predicted address has code    -> adopt into registry, reuse
 * 3. otherwise                    -> deploy via Arachnid proxy, record
 */
export async function ensureReceiverFactory(options: {
  rpcUrl: string;
  chainId: number;
  networkName: string;
  privateKey: string;
  legacy?: boolean;
  gasPrice?: string;
}): Promise<EnsuredFactory> {
  const registry = await loadFactoryRegistry();
  const key = String(options.chainId);
  const predicted = await predictReceiverFactoryAddress();
  const provider = new ethers.JsonRpcProvider(options.rpcUrl);

  const hasCode = async (addr: string): Promise<boolean> => {
    const code = await provider.getCode(addr);
    return Boolean(code && code !== "0x");
  };

  // 1. registry hit, verified live
  const entry = registry[key];
  if (entry?.address) {
    if (await hasCode(entry.address)) {
      return { address: entry.address, reused: true, txHash: entry.txHash };
    }
    // stale entry (wrong RPC, reorg, fork) — fall through to predicted check
  }

  // 2. anyone may have deployed it (deterministic address) — adopt
  if (await hasCode(predicted)) {
    registry[key] = { address: predicted, deployedAt: new Date().toISOString() };
    await saveFactoryRegistry(registry);
    return { address: predicted, reused: true };
  }

  // 3. deploy fresh via the Arachnid proxy
  const presence = await checkFactoryPresence(options.rpcUrl);
  if (presence === "missing") {
    throw new Error(
      `Cannot deploy DIAReceiverFactory: Arachnid CREATE2 factory ${CREATE2_FACTORY} not deployed on ${options.networkName}.`
    );
  }
  const initCode = await buildReceiverFactoryInitCode();
  const result = await executeCreate2DeployWithInitCode({
    rpcUrl: options.rpcUrl,
    chainId: options.chainId,
    privateKey: options.privateKey,
    salt: RECEIVER_FACTORY_SALT,
    initCode,
    legacy: options.legacy,
    gasPrice: options.gasPrice,
  });
  registry[key] = { address: result.address, txHash: result.txHash, deployedAt: new Date().toISOString() };
  await saveFactoryRegistry(registry);
  return { address: result.address, reused: false, txHash: result.txHash };
}

/**
 * Compute the PushOracleReceiverV2 address that DIAReceiverFactory.deployReceiver
 * will produce: CREATE2 from the factory, salt = keccak256(abi.encode(owner, userSalt)).
 */
export function predictReceiverAddress(
  receiverFactory: string,
  owner: string,
  userSalt: string,
  initCodeHash: string
): string {
  const salt = ethers.keccak256(
    ethers.AbiCoder.defaultAbiCoder().encode(["address", "bytes32"], [owner, userSalt])
  );
  return ethers.getCreate2Address(receiverFactory, salt, initCodeHash);
}

/**
 * Owner-salted CREATE2 salt used by DIAReceiverFactory.deployReceiver.
 */
export function receiverCreate2Salt(owner: string, userSalt: string): string {
  return ethers.keccak256(
    ethers.AbiCoder.defaultAbiCoder().encode(["address", "bytes32"], [owner, userSalt])
  );
}
