import {
  Wallet,
  SigningKey,
  AbiCoder,
  keccak256,
  toUtf8Bytes,
  getBytes,
  concat,
} from "ethers";
import { normalizePrivateKey } from "../services/keys";

export interface OracleIntentInput {
  intentType: string;
  version: string;
  chainId: number;
  nonce: bigint;
  expiry: bigint;
  symbol: string;
  price: bigint;
  timestamp: bigint;
  source: string;
}

export interface SignedOracleIntent {
  intent: OracleIntentInput;
  signer: string;
  signature: string;
}

const abiCoder = new AbiCoder();
const ORACLE_INTENT_TYPEHASH = keccak256(
  toUtf8Bytes(
    "OracleIntent(string intentType,string version,uint256 chainId,uint256 nonce,uint256 expiry,string symbol,uint256 price,uint256 timestamp,string source)"
  )
);

export function calculateIntentStructHash(intent: OracleIntentInput): string {
  return keccak256(
    abiCoder.encode(
      [
        "bytes32",
        "bytes32",
        "bytes32",
        "uint256",
        "uint256",
        "uint256",
        "bytes32",
        "uint256",
        "uint256",
        "bytes32",
      ],
      [
        ORACLE_INTENT_TYPEHASH,
        keccak256(toUtf8Bytes(intent.intentType)),
        keccak256(toUtf8Bytes(intent.version)),
        intent.chainId,
        intent.nonce,
        intent.expiry,
        keccak256(toUtf8Bytes(intent.symbol)),
        intent.price,
        intent.timestamp,
        keccak256(toUtf8Bytes(intent.source)),
      ]
    )
  );
}

export async function signOracleIntent(
  privateKey: string,
  domainSeparator: string,
  intent: OracleIntentInput
): Promise<SignedOracleIntent> {
  const normalizedKey = normalizePrivateKey(privateKey);
  const wallet = new Wallet(normalizedKey);
  const structHash = calculateIntentStructHash(intent);
  const digest = keccak256(
    concat([getBytes("0x1901"), getBytes(domainSeparator), getBytes(structHash)])
  );

  const signingKey = new SigningKey(normalizedKey);
  const signature = signingKey.sign(digest).serialized;

  return {
    intent,
    signer: wallet.address,
    signature,
  };
}

export function defaultOracleIntentInput(symbol: string): OracleIntentInput {
  const now = BigInt(Math.floor(Date.now() / 1000));
  return {
    intentType: "OracleUpdate",
    version: "1.0",
    chainId: 0,
    nonce: now,
    expiry: now + 3600n,
    symbol,
    price: 0n,
    timestamp: now,
    source: "cli",
  };
}
