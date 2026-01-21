import { runCast } from "../utils/forge";
import { OracleIntentInput } from "../utils/intents";

export interface StoredOracleIntent {
  intentType: string;
  version: string;
  chainId: bigint;
  nonce: bigint;
  expiry: bigint;
  symbol: string;
  price: bigint;
  timestamp: bigint;
  source: string;
  signature: string;
  signer: string;
}

export async function fetchIntentByHash(
  rpcUrl: string,
  registryAddress: string,
  intentHash: string
): Promise<StoredOracleIntent> {
  const result = await runCast([
    "call",
    registryAddress,
    "intents(bytes32)(string,string,uint256,uint256,uint256,string,uint256,uint256,string,bytes,address)",
    intentHash,
    "--rpc-url",
    rpcUrl,
    "--json",
  ]);
  const raw = result.stdout.trim();
  const parsed = JSON.parse(raw);
  let values: any[] | undefined;

  if (Array.isArray(parsed)) {
    values = parsed;
  } else if (Array.isArray(parsed?.value)) {
    values = parsed.value;
  }

  if (!values || values.length < 11) {
    throw new Error("Unexpected intent response format");
  }

  const normalizeValue = (entry: any) => (entry && typeof entry === "object" && "value" in entry ? entry.value : entry);
  const normalized = values.map(normalizeValue);

  const [
    intentType,
    version,
    chainId,
    nonce,
    expiry,
    symbol,
    price,
    timestamp,
    source,
    signature,
    signer,
  ] = normalized;

  const toBigInt = (value: any, label: string): bigint => {
    if (typeof value === "bigint") {
      return value;
    }
    if (typeof value === "number") {
      return BigInt(Math.trunc(value));
    }
    if (typeof value === "string") {
      if (/^0x[0-9a-fA-F]+$/.test(value)) {
        return BigInt(value);
      }
      if (/^[0-9]+$/.test(value)) {
        return BigInt(value);
      }
      const numeric = Number(value);
      if (Number.isFinite(numeric)) {
        return BigInt(Math.trunc(numeric));
      }
    }
    throw new Error(`Unable to parse ${label} value '${value}' as bigint`);
  };

  return {
    intentType: String(intentType),
    version: String(version),
    chainId: toBigInt(chainId, "chainId"),
    nonce: toBigInt(nonce, "nonce"),
    expiry: toBigInt(expiry, "expiry"),
    symbol: String(symbol),
    price: toBigInt(price, "price"),
    timestamp: toBigInt(timestamp, "timestamp"),
    source: String(source),
    signature: String(signature),
    signer: String(signer),
  };
}

export function toOracleIntentInput(record: StoredOracleIntent): OracleIntentInput {
  return {
    intentType: record.intentType,
    version: record.version,
    chainId: Number(record.chainId),
    nonce: record.nonce,
    expiry: record.expiry,
    symbol: record.symbol,
    price: record.price,
    timestamp: record.timestamp,
    source: record.source,
  };
}

export function intentToPrintable(record: StoredOracleIntent): Record<string, string> {
  return {
    intentType: record.intentType,
    version: record.version,
    chainId: record.chainId.toString(),
    nonce: record.nonce.toString(),
    expiry: record.expiry.toString(),
    symbol: record.symbol,
    price: record.price.toString(),
    timestamp: record.timestamp.toString(),
    source: record.source,
    signer: record.signer,
    signature: record.signature,
  };
}

export async function fetchDomainSeparator(rpcUrl: string, contractAddress: string): Promise<string> {
  const result = await runCast([
    "call",
    contractAddress,
    "getDomainSeparator()(bytes32)",
    "--rpc-url",
    rpcUrl,
  ]);
  const output = result.stdout.trim().split(/\s+/).pop();
  if (!output || !output.startsWith("0x")) {
    throw new Error(`Failed to read domain separator from ${contractAddress}`);
  }
  return output;
}
