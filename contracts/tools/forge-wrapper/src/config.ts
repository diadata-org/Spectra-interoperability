import path from "path";
import {
  AccountConfig,
  NetworkConfig,
  VerificationConfig,
  networkConfigSchema,
} from "./types";
import {
  getNetworkConfigPath,
  getProjectRoot,
  getKeysDir,
  ensureCustomerDirs,
} from "./utils/paths";
import { pathExists, readTextFile, readYamlFile } from "./utils/fs";
import { readStoredPrivateKey } from "./services/keys";

function expandEnvTemplates(value: string): string {
  return value.replace(/\$\{([^}]+)\}/g, (_, name: string) => {
    const resolved = process.env[name];
    if (!resolved) {
      throw new Error(`Environment variable ${name} is not set but required by configuration`);
    }
    return resolved;
  });
}

export async function loadNetworkConfig(networkName: string): Promise<NetworkConfig> {
  const configPath = getNetworkConfigPath(networkName);
  if (!(await pathExists(configPath))) {
    throw new Error(`Network configuration not found for ${networkName} at ${configPath}`);
  }

  const raw = await readYamlFile<Record<string, unknown> | null>(configPath, null);
  if (!raw) {
    throw new Error(`Configuration file ${configPath} is empty`);
  }

  const result = networkConfigSchema.safeParse(raw);
  if (!result.success) {
    throw new Error(`Invalid network configuration at ${configPath}: ${result.error.message}`);
  }

  const config = result.data;
  config.rpc_url = expandEnvTemplates(config.rpc_url);
  if (config.gas?.max_fee_per_gas) {
    config.gas.max_fee_per_gas = expandEnvTemplates(config.gas.max_fee_per_gas);
  }
  if (config.gas?.priority_fee) {
    config.gas.priority_fee = expandEnvTemplates(config.gas.priority_fee);
  }
  if (config.verification?.api_key_value) {
    config.verification.api_key_value = expandEnvTemplates(config.verification.api_key_value);
  }

  return config;
}

function resolveRelativePath(filePath: string): string {
  if (path.isAbsolute(filePath)) {
    return filePath;
  }
  return path.join(getProjectRoot(), filePath);
}

export async function resolveAccountPrivateKey(
  network: NetworkConfig,
  accountName: string,
  customer: string
): Promise<string | undefined> {
  const accounts = network.accounts ?? {};
  const account = accounts[accountName];
  if (!account) {
    try {
      const priv = await readStoredPrivateKey(customer, accountName);
      return normalizePrivateKey(priv);
    } catch (error) {
      try {
        const priv = await readStoredPrivateKey("master", accountName);
        return normalizePrivateKey(priv);
      } catch (masterError) {
        return undefined;
      }
    }
  }

  return resolveAccountSecret(account, customer);
}

async function resolveAccountSecret(account: AccountConfig, customer: string): Promise<string> {
  switch (account.type) {
    case "file": {
      const target = resolveRelativePath(account.path);
      const value = await readTextFile(target);
      if (!value) {
        throw new Error(`Private key file ${target} is empty`);
      }
      return normalizePrivateKey(value);
    }
    case "env": {
      const key = process.env[account.name];
      if (!key) {
        throw new Error(`Environment variable ${account.name} is not set`);
      }
      return normalizePrivateKey(key);
    }
    case "alias": {
      const customerDir = getKeysDir(customer);
      try {
        const priv = await readStoredPrivateKey(customer, account.name);
        return normalizePrivateKey(priv);
      } catch (error) {
        // fallback to legacy .key file if present
        const candidate = path.join(customerDir, `${account.name}.key`);
        if (await pathExists(candidate)) {
          const value = await readTextFile(candidate);
          if (!value) {
            throw new Error(`Key alias ${account.name} for customer ${customer} is empty`);
          }
          return normalizePrivateKey(value);
        }
      }

      const masterDir = getKeysDir("master");
      try {
        const priv = await readStoredPrivateKey("master", account.name);
        return normalizePrivateKey(priv);
      } catch (error) {
        const fallback = path.join(masterDir, `${account.name}.key`);
        if (await pathExists(fallback)) {
          const value = await readTextFile(fallback);
          if (!value) {
            throw new Error(`Key alias ${account.name} in master store is empty`);
          }
          return normalizePrivateKey(value);
        }
      }

      throw new Error(
        `Key alias '${account.name}' not found for customer ${customer} nor in master key store`
      );
    }
    default: {
      const neverAccount: never = account;
      throw new Error(`Unsupported account type ${(neverAccount as any).type}`);
    }
  }
}

export async function prepareCustomerEnvironment(customer: string): Promise<void> {
  await ensureCustomerDirs(customer);
}

export function resolveVerificationConfig(
  network: NetworkConfig,
  overrides: { apiKey?: string; chain?: string; watch?: boolean } = {}
): { config: VerificationConfig; apiKey?: string } | undefined {
  const base = network.verification;
  const apiKeyOverride = overrides.apiKey?.trim();
  const chainOverride = overrides.chain;
  const watchOverride = overrides.watch;

  const apiKeyFromEnv = base?.api_key_env ? process.env[base.api_key_env]?.trim() : undefined;
  const apiKeyFromValue = base?.api_key_value?.trim();
  const apiKey = apiKeyOverride ?? apiKeyFromEnv ?? apiKeyFromValue;

  if (!base && !apiKeyOverride && chainOverride === undefined && watchOverride === undefined) {
    return undefined;
  }

  const verifier = base?.verifier?.trim();
  const verifierUrl = base?.verifier_url?.trim();
  const apiKeyRequired =
    !verifier || verifier.toLowerCase() === "etherscan" || verifier.toLowerCase() === "polygonscan";

  if (apiKeyRequired && (!apiKey || apiKey.length === 0)) {
    throw new Error(
      "Verification requires an explorer API key. Provide --api-key or configure network.verification.api_key_env/api_key_value."
    );
  }

  const result: VerificationConfig = {
    chain: chainOverride ?? base?.chain,
    explorer_url: base?.explorer_url,
    watch: typeof watchOverride === "boolean" ? watchOverride : base?.watch,
    verifier,
    verifier_url: verifierUrl,
  };

  return { config: result, apiKey };
}

function normalizePrivateKey(value: string): string {
  const trimmed = value.trim();
  if (trimmed.startsWith("0x")) {
    return trimmed;
  }
  return `0x${trimmed}`;
}
