export interface ConstructorTemplate {
  artifact: string;
  args?: string[];
  constructorSignature?: string;
}

import path from "path";
import { readFileSync } from "fs";
import { parse } from "yaml";
import { getProjectRoot } from "../utils/paths";

const DEFAULT_TEMPLATES: Record<string, ConstructorTemplate> = {
  OracleIntentRegistry: {
    artifact: "contracts/OracleIntentRegistry.sol:OracleIntentRegistry",
    args: ["DIA Oracle", "1.0"],
    constructorSignature: "constructor(string,string)",
  },
  PushOracleReceiverV2: {
    artifact: "contracts/PushOracleReceiverV2.sol:PushOracleReceiverV2",
    args: [],
    constructorSignature: "constructor(string,string,uint256,address)",
  },
};

let TEMPLATE_CACHE: Record<string, ConstructorTemplate> | null = null;

function loadTemplates(): Record<string, ConstructorTemplate> {
  if (TEMPLATE_CACHE) {
    return TEMPLATE_CACHE;
  }

  const templates: Record<string, ConstructorTemplate> = { ...DEFAULT_TEMPLATES };
  const filePath = path.join(getProjectRoot(), "templates", "contracts.yaml");

  try {
    const raw = readFileSync(filePath, "utf8");
    const parsed = parse(raw) as {
      templates?: Record<
        string,
        {
          artifact?: string;
          constructorArgs?: unknown[];
          constructorSignature?: unknown;
        }
      >;
    };

    if (parsed?.templates) {
      for (const [alias, value] of Object.entries(parsed.templates)) {
        if (!value) continue;
        const templateValue = value as Record<string, unknown>;
        const artifact = typeof templateValue.artifact === "string"
          ? templateValue.artifact
          : templates[alias]?.artifact;
        const args = Array.isArray(templateValue.constructorArgs)
          ? templateValue.constructorArgs.map((arg) => String(arg))
          : templates[alias]?.args;
        const signature =
          typeof templateValue.constructorSignature === "string"
            ? templateValue.constructorSignature
            : templates[alias]?.constructorSignature;

        if (artifact) {
          templates[alias] = {
            artifact,
            args,
            constructorSignature: signature,
          };
        }
      }
    }
  } catch (error) {
    // ignore missing file or parse errors; defaults remain
  }

  TEMPLATE_CACHE = templates;
  return TEMPLATE_CACHE;
}

export function getTemplate(alias: string): ConstructorTemplate | undefined {
  const templates = loadTemplates();
  return templates[alias];
}
