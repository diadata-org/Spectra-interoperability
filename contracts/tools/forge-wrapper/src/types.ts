import { z } from "zod";

export const accountConfigSchema = z.discriminatedUnion("type", [
  z.object({
    type: z.literal("file"),
    path: z.string().min(1),
    description: z.string().optional(),
  }),
  z.object({
    type: z.literal("env"),
    name: z.string().min(1),
    description: z.string().optional(),
  }),
  z.object({
    type: z.literal("alias"),
    name: z.string().min(1),
    description: z.string().optional(),
  }),
]);

export type AccountConfig = z.infer<typeof accountConfigSchema>;

export const gasConfigSchema = z.object({
  max_fee_per_gas: z.string().optional(),
  priority_fee: z.string().optional(),
});

export type GasConfig = z.infer<typeof gasConfigSchema>;

export const verificationConfigSchema = z.object({
  chain: z.string().optional(),
  api_key_env: z.string().optional(),
  api_key_value: z.string().optional(),
  explorer_url: z.string().optional(),
  watch: z.boolean().optional(),
  verifier: z.string().optional(),
  verifier_url: z.string().optional(),
});

export type VerificationConfig = z.infer<typeof verificationConfigSchema>;

export const networkConfigSchema = z.object({
  name: z.string().min(1),
  chain_id: z.number().int().nonnegative(),
  rpc_url: z.string().min(1),
  forge_profile: z.string().optional(),
  accounts: z
    .record(accountConfigSchema)
    .optional()
    .transform((value) => value ?? {}),
  default_contracts: z
    .record(z.string())
    .optional()
    .transform((value) => value ?? {}),
  gas: gasConfigSchema.optional(),
  verification: verificationConfigSchema.optional(),
});

export type NetworkConfig = z.infer<typeof networkConfigSchema>;

export interface DeploymentRecord {
  alias: string;
  address: string;
  txHash?: string;
  deployedAt: string;
  artifact: string;
  constructorArgs: string[];
  deployer: {
    alias: string;
    address?: string;
  };
  verification?: {
    status: "success" | "failed";
    timestamp: string;
    explorerUrl?: string;
  };
}

export interface DeploymentFile {
  current: Record<string, DeploymentRecord>;
  history: DeploymentRecord[];
}

export interface ForgeExecution {
  stdout: string;
  stderr: string;
}

export interface DeployOptions {
  alias: string;
  artifact?: string;
  constructorArgs: string[];
  customer: string;
  network: string;
  account: string;
  rpcUrl?: string;
  dryRun?: boolean;
  salt?: string;
  privateKeyOverride?: string;
  deployerAddress?: string;
}

export interface CallOptions {
  alias: string;
  customer: string;
  network: string;
  signature: string;
  args: string[];
  write: boolean;
  account?: string;
  dryRun?: boolean;
}

export interface DebugOptions {
  alias: string;
  customer: string;
  network: string;
}

export interface KeyImportOptions {
  customer: string;
  name: string;
  value: string;
  overwrite?: boolean;
}
