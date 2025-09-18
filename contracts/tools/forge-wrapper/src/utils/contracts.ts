interface ContractPreset {
  alias: string;
  artifact: string;
  description?: string;
}

export const CONTRACT_PRESETS: ContractPreset[] = [
  {
    alias: "PushOracleReceiverV2",
    artifact: "contracts/PushOracleReceiverV2.sol:PushOracleReceiverV2",
    description: "Intent-aware push oracle receiver",
  },
  {
    alias: "OracleTriggerV2",
    artifact: "contracts/OracleTriggerV2.sol:OracleTriggerV2",
    description: "Intent-based trigger for Hyperlane",
  },
  {
    alias: "OracleIntentRegistry",
    artifact: "contracts/OracleIntentRegistry.sol:OracleIntentRegistry",
    description: "Registry for oracle intents",
  },
  {
    alias: "ProtocolFeeHook",
    artifact: "contracts/ProtocolFeeHook.sol:ProtocolFeeHook",
    description: "Post-dispatch protocol fee hook",
  },
];

export function getPreset(alias: string): ContractPreset | undefined {
  return CONTRACT_PRESETS.find((preset) => preset.alias === alias);
}

export function buildPresetChoices(defaults: Record<string, string> | undefined) {
  const combined = new Map<string, { artifact: string; description?: string }>();

  for (const preset of CONTRACT_PRESETS) {
    combined.set(preset.alias, { artifact: preset.artifact, description: preset.description });
  }

  if (defaults) {
    for (const [alias, artifact] of Object.entries(defaults)) {
      combined.set(alias, { artifact, description: undefined });
    }
  }

  const choices = Array.from(combined.entries()).map(([alias, info]) => ({
    title: alias,
    value: alias,
    description: info.description || info.artifact,
  }));

  choices.push({
    title: "Custom alias",
    value: "__custom__",
    description: "Enter alias and artifact manually",
  });

  return choices;
}
