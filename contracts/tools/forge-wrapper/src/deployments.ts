import { DeploymentFile, DeploymentRecord } from "./types";
import { getDeploymentFilePath } from "./utils/paths";
import { readYamlFile, writeYamlFile } from "./utils/fs";
import { timestampNow } from "./utils/dates";

const EMPTY_DEPLOYMENTS: DeploymentFile = {
  current: {},
  history: [],
};

export async function loadDeployments(customer: string, network: string): Promise<DeploymentFile> {
  const path = getDeploymentFilePath(customer, network);
  const data = await readYamlFile<DeploymentFile | null>(path, null);
  if (!data) {
    return { ...EMPTY_DEPLOYMENTS };
  }
  const current = Object.fromEntries(
    Object.entries(data.current ?? {}).map(([alias, record]) => [alias, normalizeRecord(record)])
  );
  const history = (data.history ?? []).map((record) => normalizeRecord(record));
  return { current, history };
}

export async function saveDeployments(
  customer: string,
  network: string,
  payload: DeploymentFile
): Promise<void> {
  const path = getDeploymentFilePath(customer, network);
  await writeYamlFile(path, payload);
}

export async function recordDeployment(
  customer: string,
  network: string,
  record: DeploymentRecord
): Promise<void> {
  const file = await loadDeployments(customer, network);
  const normalizedRecord = normalizeRecord(record);
  const existing = file.current[normalizedRecord.alias];
  if (existing) {
    file.history.unshift(existing);
  }
  file.current[normalizedRecord.alias] = normalizedRecord;
  await saveDeployments(customer, network, file);
}

export async function getDeployment(
  customer: string,
  network: string,
  alias: string
): Promise<DeploymentRecord | undefined> {
  const file = await loadDeployments(customer, network);
  let deployment = file.current[alias];

  // If not found and customer is not master, try master as fallback
  if (!deployment && customer !== "master") {
    try {
      const masterFile = await loadDeployments("master", network);
      deployment = masterFile.current[alias];
    } catch (error) {
      // Ignore master deployment lookup errors
    }
  }

  return deployment;
}

export function formatDeploymentRecord(record: DeploymentRecord): string {
  const lines = [
    `alias: ${record.alias}`,
    `address: ${record.address}`,
    `deployer: ${record.deployer.alias}${
      record.deployer.address ? ` (${record.deployer.address})` : ""
    }`,
    record.txHash ? `tx: ${record.txHash}` : undefined,
    `artifact: ${record.artifact}`,
    `deployedAt: ${record.deployedAt}`,
    record.deployMethod === "create2" ? `deployMethod: create2 (factory ${record.create2?.factory})` : undefined,
    record.create2 ? `salt: ${record.create2.salt}` : undefined,
    record.create2 ? `initCodeHash: ${record.create2.initCodeHash}` : undefined,
    record.constructorArgs.length
      ? `constructorArgs: [${record.constructorArgs.join(", ")}]`
      : undefined,
  ].filter(Boolean) as string[];
  return lines.join("\n");
}

function normalizeRecord(raw: any): DeploymentRecord {
  const alias = typeof raw?.alias === "string" ? raw.alias : "unknown";
  const address = typeof raw?.address === "string" ? raw.address : "";
  const txHash = typeof raw?.txHash === "string" ? raw.txHash : undefined;
  const deployedAt = typeof raw?.deployedAt === "string" ? raw.deployedAt : timestampNow();
  const artifact = typeof raw?.artifact === "string" ? raw.artifact : "";
  const constructorArgs = Array.isArray(raw?.constructorArgs)
    ? raw.constructorArgs
        .map((arg: unknown) => String(arg).trim())
        .filter((arg: string) => arg.length > 0)
    : [];

  let deployerAlias = "unknown";
  let deployerAddress: string | undefined;
  const deployer = raw?.deployer;
  if (typeof deployer === "string") {
    deployerAlias = deployer;
  } else if (deployer && typeof deployer === "object") {
    if (typeof deployer.alias === "string" && deployer.alias.trim().length > 0) {
      deployerAlias = deployer.alias;
    }
    if (typeof deployer.address === "string" && deployer.address.trim().length > 0) {
      deployerAddress = deployer.address;
    }
  }

  let verification: DeploymentRecord["verification"];
  const rawVerification = raw?.verification;
  if (rawVerification && typeof rawVerification === "object") {
    const status = rawVerification.status === "success" ? "success" : rawVerification.status === "failed" ? "failed" : undefined;
    const timestamp = typeof rawVerification.timestamp === "string" ? rawVerification.timestamp : undefined;
    const explorerUrl = typeof rawVerification.explorerUrl === "string" ? rawVerification.explorerUrl : undefined;
    if (status && timestamp) {
      verification = { status, timestamp, explorerUrl };
    }
  }

  let deployMethod: DeploymentRecord["deployMethod"];
  if (raw?.deployMethod === "create" || raw?.deployMethod === "create2") {
    deployMethod = raw.deployMethod;
  }

  let create2: DeploymentRecord["create2"];
  const rawCreate2 = raw?.create2;
  if (rawCreate2 && typeof rawCreate2 === "object") {
    const factory = typeof rawCreate2.factory === "string" ? rawCreate2.factory : undefined;
    const salt = typeof rawCreate2.salt === "string" ? rawCreate2.salt : undefined;
    const initCodeHash = typeof rawCreate2.initCodeHash === "string" ? rawCreate2.initCodeHash : undefined;
    if (factory && salt && initCodeHash) {
      create2 = { factory, salt, initCodeHash };
    }
  }

  return {
    alias,
    address,
    txHash,
    deployedAt,
    artifact,
    constructorArgs,
    deployer: {
      alias: deployerAlias,
      address: deployerAddress,
    },
    deployMethod,
    create2,
    verification,
  };
}
