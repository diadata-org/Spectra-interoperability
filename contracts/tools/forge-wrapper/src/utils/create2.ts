import path from "path";
import { promises as fs } from "fs";
import { ethers } from "ethers";
import chalk from "chalk";
import { runForge } from "./forge";

/**
 * Arachnid deterministic-deployment-proxy.
 * Same address + same runtime code on every EVM chain where the canonical
 * pre-signed tx (sender 0x914d7Fec6aaC8cd542e72Bca78B30650d45643d7, nonce 0)
 * has been broadcast. Probed present on: dia-mainnet(1050), lumina-new(10050),
 * base(8453), arbitrum(42161/421614), base-sepolia, polygon(137), telos(40),
 * sonic(146), optimism(10), op-sepolia, astar(592), sei(1329), linea(59144),
 * boba(288), hemi(43111), unichain-sepolia(1301), somnia(50312),
 * plume-testnet, litvm(4441), swell(1924), bnb-testnet(97), goat-testnet,
 * hyperliquid-testnet, ethereum(1). NOT present on: wanchain(888),
 * unit0-testnet(88817), lumina(100640).
 */
export const CREATE2_FACTORY = "0x4e59b44847b379578588920cA78FbF26c0B4956C";

/** Canonical factory runtime code, used for reference/documentation. */
const CANONICAL_FACTORY_RUNTIME =
  "0x7fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffe03601600081602082378035828234f58015156039578182fd5b8082525050506014600cf3";
void CANONICAL_FACTORY_RUNTIME;

export function getContractsRoot(): string {
  // If running from tools/forge-wrapper, go up to contracts root
  if (process.cwd().includes("/tools/forge-wrapper")) {
    return process.cwd().replace(/\/tools\/forge-wrapper.*$/, "");
  }
  return process.cwd();
}

export function normalizeSalt(salt: string): string {
  const trimmed = salt.trim();
  if (/^0x[0-9a-fA-F]{64}$/.test(trimmed)) {
    return trimmed.toLowerCase();
  }
  // Arbitrary string -> keccak256 (versioned-salt convention, e.g.
  // "DIA-PushOracleReceiverV2-v1" so future revisions get fresh addresses)
  return ethers.id(trimmed);
}

/** Vanity suffix must be hex chars only (addresses can't contain i/o/etc). */
export function isValidVanitySuffix(suffix: string): boolean {
  return /^[0-9a-fA-F]{1,6}$/.test(suffix);
}

/**
 * Grind salts until the CREATE2 address ends with `suffix`.
 * Deterministic: tries keccak256(`${base}-${i}`) for i = startAfter+1.. .
 * Cost per hex char of suffix: ~16x more tries (1 char ≈ 16, 4 chars ≈ 65K ≈ 2s, 6 ≈ 16M).
 * Returns the absolute `index` so a rerun with startAfter=index yields the NEXT hit.
 */
export function grindVanitySalt(
  initCodeHash: string,
  suffix: string,
  options: {
    base?: string;
    maxTries?: number;
    startAfter?: number;
    factory?: string;
    /** Optional salt transform (e.g. owner-namespacing when deploying via DIAReceiverFactory). */
    transformSalt?: (salt: string) => string;
  } = {}
): { salt: string; saltStr: string; address: string; tries: number; index: number } {
  const target = suffix.toLowerCase();
  if (!isValidVanitySuffix(target)) {
    throw new Error(`Vanity suffix must be 1-6 hex characters (got "${suffix}")`);
  }
  const factory = options.factory ?? CREATE2_FACTORY;
  const base = options.base ?? "dia";
  const startAfter = options.startAfter ?? 0;
  const maxTries = options.maxTries ?? 50_000_000;
  for (let i = startAfter + 1; i <= startAfter + maxTries; i++) {
    const saltStr = `${base}-${i}`;
    const salt = options.transformSalt ? options.transformSalt(ethers.id(saltStr)) : ethers.id(saltStr);
    const address = ethers.getCreate2Address(factory, salt, initCodeHash);
    if (address.toLowerCase().endsWith(target)) {
      return { salt, saltStr, address, tries: i - startAfter, index: i };
    }
  }
  throw new Error(`No salt found ending in "${suffix}" after ${maxTries} tries — use a shorter suffix.`);
}

/**
 * On-chain check: return deployed runtime code at `address` ("" if empty/EOA).
 * RPC failures return "" — callers that must be sure re-check before broadcast.
 */
export async function getDeployedCode(rpcUrl: string, address: string): Promise<string> {
  try {
    const provider = new ethers.JsonRpcProvider(rpcUrl);
    const code = await provider.getCode(address);
    return code && code !== "0x" ? code : "";
  } catch (error) {
    return "";
  }
}

export interface Create2Plan {
  address: string;
  initCodeHash: string;
  salt: string;
  factory: string;
  initCodeByteLength: number;
}

/**
 * Compute the deterministic CREATE2 address for an artifact + constructor args.
 * Reads the compiled creation code from the forge artifact (must be built
 * first — same file `forge create` would deploy), appends ABI-encoded
 * constructor args, and hashes it against (factory, salt).
 */
export async function buildCreate2Plan(options: {
  artifact: string;
  constructorArgs: string[];
  salt: string;
  forgeProfile?: string;
  evmVersion?: string;
}): Promise<Create2Plan> {
  const contractsRoot = getContractsRoot();

  const buildArgs = ["build", options.artifact.split(":")[0]];
  const buildEnv = options.forgeProfile ? { FOUNDRY_PROFILE: options.forgeProfile } : undefined;
  await runForge(buildArgs, { cwd: contractsRoot, env: buildEnv });

  const [sourcePath, contractName] = options.artifact.split(":");
  // forge mirrors the source tree under out/ (strip the leading "contracts/")
  const artifactRel = sourcePath.replace(/^contracts\//, "");
  const artifactPath = path.join(contractsRoot, "out", artifactRel, `${contractName}.json`);

  let artifact: any;
  try {
    artifact = JSON.parse(await fs.readFile(artifactPath, "utf-8"));
  } catch (error) {
    throw new Error(
      `Artifact not found at ${artifactPath}. Run forge build first (looked in contracts root ${contractsRoot}).`
    );
  }

  const creationCode = artifact.bytecode?.object;
  if (!creationCode || !creationCode.startsWith("0x")) {
    throw new Error(`Artifact ${options.artifact} has no creation bytecode.`);
  }

  const args = options.constructorArgs;
  let encodedArgs = "0x";
  if (args.length > 0) {
    if (!artifact.abi?.some((m: any) => m.type === "constructor")) {
      throw new Error(`Constructor arguments provided but ${options.artifact} has no constructor.`);
    }
    const iface = new ethers.Interface(artifact.abi);
    // Cast numeric strings to BigInt to avoid precision loss on large values
    const typed = args.map((arg) => {
      if (/^-?\d+$/.test(arg)) {
        try {
          return BigInt(arg);
        } catch {
          return arg;
        }
      }
      return arg;
    });
    encodedArgs = iface.encodeDeploy(typed as any[]);
  }

  const initCode = creationCode + encodedArgs.slice(2);
  const initCodeHash = ethers.keccak256(initCode);
  const salt = normalizeSalt(options.salt);

  return {
    address: ethers.getCreate2Address(CREATE2_FACTORY, salt, initCodeHash),
    initCodeHash,
    salt,
    factory: CREATE2_FACTORY,
    initCodeByteLength: (initCode.length - 2) / 2,
  };
}

/**
 * Check whether the canonical CREATE2 factory is deployed on a network.
 * Returns "present" | "missing" | "unknown" (RPC failure).
 */
export async function checkFactoryPresence(rpcUrl: string): Promise<"present" | "missing" | "unknown"> {
  try {
    const provider = new ethers.JsonRpcProvider(rpcUrl);
    const code = await provider.getCode(CREATE2_FACTORY);
    if (code && code.length > 4) {
      // Present (optionally identical to canonical runtime code — either way
      // the proxy exists and accepts salt++initCode deployments)
      return "present";
    }
    return "missing";
  } catch (error) {
    return "unknown";
  }
}

/**
 * Execute a CREATE2 deployment through the Arachnid proxy:
 *   to    = factory
 *   data  = salt (32 bytes) ++ initCode
 * Precomputes the address so it can be printed before broadcasting.
 */
export async function executeCreate2DeployWithInitCode(options: {
  rpcUrl: string;
  chainId: number;
  privateKey: string;
  salt: string;
  initCode: string;
  legacy?: boolean;
  maxFeePerGas?: string;
  maxPriorityFeePerGas?: string;
  gasPrice?: string;
}): Promise<{ address: string; txHash: string }> {
  const provider = new ethers.JsonRpcProvider(options.rpcUrl);
  const wallet = new ethers.Wallet(options.privateKey, provider);
  const salt = normalizeSalt(options.salt);
  const initCodeHash = ethers.keccak256(options.initCode);
  const expected = ethers.getCreate2Address(CREATE2_FACTORY, salt, initCodeHash);

  const data = "0x" + salt.slice(2) + options.initCode.slice(2);
  const tx: any = { to: CREATE2_FACTORY, data, chainId: options.chainId };

  if (options.legacy || options.gasPrice) {
    tx.type = 0;
    if (options.gasPrice) tx.gasPrice = ethers.parseUnits(options.gasPrice, "wei");
  } else {
    const fee = await provider.getFeeData();
    if (fee.maxFeePerGas) tx.maxFeePerGas = fee.maxFeePerGas + (fee.maxFeePerGas / 4n);
    if (fee.maxPriorityFeePerGas) tx.maxPriorityFeePerGas = fee.maxPriorityFeePerGas;
  }

  const deployer = await wallet.getAddress();
  const balance = await provider.getBalance(deployer);
  const gas = await provider.estimateGas({ ...tx, from: deployer });
  const txFee = gas * (tx.gasPrice ?? tx.maxFeePerGas ?? 0n);
  if (balance < txFee) {
    throw new Error(
      `Deployer ${deployer} balance ${ethers.formatEther(balance)} ETH is below estimated fee ${ethers.formatEther(txFee)} ETH.`
    );
  }

  const sent = await wallet.sendTransaction(tx);
  // eslint-disable-next-line no-console
  console.log(chalk`{gray tx: ${sent.hash}}`);
  const receipt = await sent.wait();
  if (!receipt || receipt.status !== 1) {
    throw new Error(`CREATE2 deployment tx reverted: ${sent.hash}`);
  }

  const deployedCode = await provider.getCode(expected);
  if (!deployedCode || deployedCode === "0x") {
    throw new Error(
      `Tx succeeded but no code at predicted address ${expected}. Factory may not be the canonical proxy.`
    );
  }
  return { address: expected, txHash: sent.hash };
}
