import chalk from "chalk";
import { DeploymentRecord, NetworkConfig } from "../types";
import { runForge } from "./forge";
import { getTemplate } from "./templates";

interface EtherscanVerifyResponse {
  status: string;
  message: string;
  result: string;
}

function getContractsRoot(): string {
  // If running from tools/forge-wrapper, go up to contracts root
  if (process.cwd().includes('/tools/forge-wrapper')) {
    return process.cwd().replace(/\/tools\/forge-wrapper.*$/, '');
  }
  return process.cwd();
}

/**
 * Verify a contract using the Etherscan API directly
 * This bypasses forge's buggy --verifier etherscan implementation
 */
export async function verifyViaEtherscanAPI(
  record: DeploymentRecord,
  networkConfig: NetworkConfig,
  apiKey: string,
  watch: boolean
): Promise<void> {
  // Get contracts root directory
  const contractsRoot = getContractsRoot();

  // Get flattened source code
  // eslint-disable-next-line no-console
  console.log(chalk.gray("Flattening contract source..."));
  const sourceFilePath = record.artifact.split(":")[0];
  let flattenResult;
  try {
    flattenResult = await runForge(["flatten", sourceFilePath], { cwd: contractsRoot });
  } catch (error: any) {
    throw new Error(`Failed to flatten contract: ${error.message}\nStderr: ${error.stderr || 'none'}`);
  }
  const sourceCode = flattenResult.stdout.trim();

  // Get compiler version from build artifacts
  const compilerVersion = await getCompilerVersion(record, contractsRoot);

  // Get constructor args
  const constructorArgs = await getConstructorArgs(record, contractsRoot);

  // Get contract name
  const contractName = record.artifact.split(":")[1];

  // Get forge config for optimizer settings
  const forgeConfigResult = await runForge(["config", "--json"], { cwd: contractsRoot });
  const forgeConfig = JSON.parse(forgeConfigResult.stdout);

  // Prepare API request
  const chainId = networkConfig.chain_id;
  const verifierUrl = networkConfig.verification?.verifier_url || `https://api.etherscan.io/v2/api?chainid=${chainId}`;

  // eslint-disable-next-line no-console
  console.log(chalk.gray(`Submitting verification to ${verifierUrl}...`));
  // eslint-disable-next-line no-console
  console.log(chalk.gray(`Compiler: v${compilerVersion}, Optimizer: ${forgeConfig.optimizer}, Runs: ${forgeConfig.optimizer_runs}, Via IR: ${forgeConfig.via_ir}`));

  // If via_ir is enabled, we must use Standard JSON Input format
  let formData: URLSearchParams;

  if (forgeConfig.via_ir) {
    // Use Standard JSON Input format for via_ir support
    const standardJsonInput = {
      language: "Solidity",
      sources: {
        [sourceFilePath]: {
          content: sourceCode,
        },
      },
      settings: {
        optimizer: {
          enabled: forgeConfig.optimizer || false,
          runs: forgeConfig.optimizer_runs || 200,
        },
        viaIR: true,
        evmVersion: forgeConfig.evm_version || "default",
        outputSelection: {
          "*": {
            "*": ["abi", "evm.bytecode", "evm.deployedBytecode", "evm.methodIdentifiers"],
            "": ["ast"],
          },
        },
      },
    };

    formData = new URLSearchParams({
      module: "contract",
      action: "verifysourcecode",
      apikey: apiKey,
      contractaddress: record.address,
      sourceCode: JSON.stringify(standardJsonInput),
      codeformat: "solidity-standard-json-input",
      contractname: `${sourceFilePath}:${contractName}`,
      compilerversion: `v${compilerVersion}`,
      constructorArguements: constructorArgs || "",
      licenseType: "1",
    });
  } else {
    // Use simpler single-file format
    formData = new URLSearchParams({
      module: "contract",
      action: "verifysourcecode",
      apikey: apiKey,
      contractaddress: record.address,
      sourceCode: sourceCode,
      codeformat: "solidity-single-file",
      contractname: contractName,
      compilerversion: `v${compilerVersion}`,
      optimizationUsed: forgeConfig.optimizer ? "1" : "0",
      runs: String(forgeConfig.optimizer_runs || 200),
      constructorArguements: constructorArgs || "",
      evmversion: forgeConfig.evm_version || "default",
      licenseType: "1",
    });
  }

  const response = await fetch(verifierUrl, {
    method: "POST",
    headers: {
      "Content-Type": "application/x-www-form-urlencoded",
    },
    body: formData.toString(),
  });

  const result: EtherscanVerifyResponse = await response.json();

  if (result.status !== "1") {
    throw new Error(`Verification failed: ${result.message} - ${result.result}`);
  }

  const guid = result.result;
  // eslint-disable-next-line no-console
  console.log(chalk.gray(`Verification submitted. GUID: ${guid}`));

  if (watch) {
    // eslint-disable-next-line no-console
    console.log(chalk.gray("Waiting for verification to complete..."));
    await waitForVerification(verifierUrl, guid, apiKey);
  } else {
    // eslint-disable-next-line no-console
    console.log(chalk.yellow(`Check verification status: ${verifierUrl.split('?')[0]}?chainid=${chainId}&module=contract&action=checkverifystatus&guid=${guid}`));
  }
}

async function getCompilerVersion(record: DeploymentRecord, contractsRoot: string): Promise<string> {
  const artifactPath = `${contractsRoot}/out/${record.artifact.split(":")[0].replace("contracts/", "")}/${record.artifact.split(":")[1]}.json`;

  try {
    const fs = await import("fs/promises");
    const artifact = JSON.parse(await fs.readFile(artifactPath, "utf-8"));
    const version = artifact.metadata?.compiler?.version;
    if (version) {
      return version;
    }
  } catch (error) {
    // Fall back to parsing from forge output
  }

  // Fallback: get from foundry.toml or forge config
  const configResult = await runForge(["config", "--json"], { cwd: contractsRoot });
  const config = JSON.parse(configResult.stdout);
  return config.solc_version || config.solc || "0.8.29";
}

async function getConstructorArgs(record: DeploymentRecord, contractsRoot: string): Promise<string> {
  if (!record.constructorArgs.length) {
    return "";
  }

  const template = getTemplate(record.alias);
  const signature = template?.constructorSignature;
  if (!signature) {
    throw new Error(
      `Constructor signature not found for ${record.alias}. Update templates/contracts.yaml (constructorSignature).`
    );
  }

  const { runCast } = await import("./forge");
  const encodeArgs = [signature, ...record.constructorArgs];
  const result = await runCast(["abi-encode", ...encodeArgs], { cwd: contractsRoot });
  return result.stdout.trim().replace("0x", "");
}

async function waitForVerification(
  baseUrl: string,
  guid: string,
  apiKey: string,
  maxAttempts: number = 30
): Promise<void> {
  const chainId = new URL(baseUrl).searchParams.get("chainid");
  const checkUrl = `${baseUrl.split('?')[0]}?chainid=${chainId}&module=contract&action=checkverifystatus&guid=${guid}&apikey=${apiKey}`;

  for (let i = 0; i < maxAttempts; i++) {
    await new Promise((resolve) => setTimeout(resolve, 5000)); // Wait 5 seconds

    const response = await fetch(checkUrl);
    const result: EtherscanVerifyResponse = await response.json();

    if (result.status === "1") {
      // eslint-disable-next-line no-console
      console.log(chalk.green(`✓ Verification successful: ${result.result}`));
      return;
    }

    if (result.result.includes("Fail")) {
      throw new Error(`Verification failed: ${result.result}`);
    }

    // Still pending
    // eslint-disable-next-line no-console
    console.log(chalk.gray(`Checking... (${i + 1}/${maxAttempts})`));
  }

  throw new Error("Verification timed out. Check status manually.");
}
