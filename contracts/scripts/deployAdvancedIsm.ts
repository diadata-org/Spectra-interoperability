import { ethers } from "hardhat";
import fs from "fs";
import path from "path";
import { readFileSync } from 'fs';
import { getAddress } from "ethers";

interface ValidatorConfig {
  network: string;
  validators: {
    name: string;
    address: string;
  }[];
}

interface ValidatorConfigs {
  [chainId: string]: ValidatorConfig;
}

// ABI for both factory contracts since they have the same interface
const factoryABI = [
  "function deploy(address[] memory _validators, uint8 _threshold) external returns (address)"
];

const staticAddressSetFactoryABI = [
    "function deploy(address[] memory _values) external returns (address)"
  ];

  enum Deploy {
    Multisig,
    Aggregation,
    Hook
  }

interface DeployParams {
  chainId: number;
  addresses: string[];
  threshold: number;
  type: Deploy;
}

async function deploy({chainId, addresses, threshold, type}: DeployParams) {
  // Read config file
  const configPath = path.resolve("../agent-config-private.json");
  const config = JSON.parse(fs.readFileSync(configPath, "utf8"));

  const chainConfig = Object.values(config.chains).find(
    (chain: any) => chain.chainId === chainId
  );

  if (!chainConfig) {
    throw new Error(`Chain ID ${chainId} not found in config`);
  }

  // Get factory address based on type
  let factoryAddress;
  let addressType;

  switch (type) {
    case Deploy.Multisig:
      factoryAddress = chainConfig.staticMessageIdMultisigIsmFactory;
      addressType = "validators";
      // Load validators from JSON if no addresses provided
      if (!addresses.length) {
        const validators = await getValidatorsForChain(chainId);
        addresses = validators[chainId];
      }
      break;
    case Deploy.Aggregation:
      factoryAddress = chainConfig.staticAggregationIsmFactory;
      addressType = "ISM contracts";
      break;
    case Deploy.Hook:
      factoryAddress = chainConfig.staticAggregationHookFactory;
      addressType = "Hook contracts";
      break;
    default:
      throw new Error(`Invalid type: ${type}`);
  }

  if (!factoryAddress) {
    throw new Error(`Factory address not found for chain ${chainId}`);
  }

  console.log(`=== Deploying ${type.toString()} ===`);
  console.log(`Factory address: ${factoryAddress}`);
  console.log(`${addressType}: ${addresses.join(", ")}`);
  if (type !== Deploy.Hook) {
    console.log(`Threshold: ${threshold}`);
  }

  const [deployer] = await ethers.getSigners();
  console.log("Deploying with account:", deployer.address);

  // Deploy based on type
  if (type === Deploy.Hook) {
    return deployHook(factoryAddress, addresses, deployer);
  } else {
    return deployWithThreshold(factoryAddress, addresses, threshold, deployer);
  }
}

// Helper functions
async function deployHook(factoryAddress: string, addresses: string[], deployer: any) {
  // Convert factory address and all input addresses to checksum format
  const checksumFactory = getAddress(factoryAddress);
  const checksumAddresses = addresses.map(addr => getAddress(addr));
  
  const factory = new ethers.Contract(
    checksumFactory,
    staticAddressSetFactoryABI,
    deployer
  );

  const tx = await factory.deploy(checksumAddresses);
  console.log(`Transaction hash: ${tx.hash}`);
  const receipt = await tx.wait();
  return receipt.logs[0]?.address;
}

async function deployWithThreshold(
  factoryAddress: string, 
  addresses: string[], 
  threshold: number, 
  deployer: any
) {
  // Convert factory address and all input addresses to checksum format
  const checksumFactory = getAddress(factoryAddress);
  const checksumAddresses = addresses.map(addr => getAddress(addr));
  
  const factory = new ethers.Contract(
    checksumFactory,
    factoryABI,
    deployer
  );

  const tx = await factory.deploy(checksumAddresses, threshold);
  console.log(`Transaction hash: ${tx.hash}`);
  const receipt = await tx.wait();
  return receipt.logs[0]?.address;
}

async function getValidatorsForChain(...chainIds: number[]): Promise<{ [chainId: number]: string[] }> {
  const validatorsPath = path.resolve("./scripts/validators.json");
  const validatorConfig: ValidatorConfigs = JSON.parse(readFileSync(validatorsPath, "utf8"));
  
  const result: { [chainId: number]: string[] } = {};
  
  for (const chainId of chainIds) {
    const chainValidators = validatorConfig[chainId.toString()];
    if (!chainValidators) {
      console.warn(`Warning: No validators found for chain ID ${chainId}`);
      continue;
    }
    
    // Convert addresses to checksum format
    result[chainId] = chainValidators.validators.map(v => getAddress(v.address));
    console.log(`Found ${result[chainId].length} validators for chain ${chainId} (${chainValidators.network})`);
  }

  if (Object.keys(result).length === 0) {
    throw new Error(`No validators found for any of the specified chain IDs: ${chainIds.join(", ")}`);
  }
  
  return result;
}

// Add interface for deployment configuration
interface DeploymentConfig {
  validatorChainIds: number[];  // Chains to pull validators from
  type: Deploy;
  threshold?: number;
  addresses?: string[];
}

async function main() {
  const deployments: Record<number, string> = {};
  
  // Get the network we're deploying to
  const network = await ethers.provider.getNetwork();
  const deploymentChainId = Number(network.chainId);
  
  // Example deployment variables
  const deployConfig: DeploymentConfig = {
    validatorChainIds: [10, 8453,42161], // Get validators from Optimism and Base
    type: Deploy.Hook,
    threshold: 2,
    addresses: [
    "0x048050547eb6e68cB37Fb21EEafEad40CF2CbdbB",
    "0x19dc38aeae620380430C200a6E990D5Af5480117"
  ]
  };

  try {
    switch (deployConfig.type) {
      case Deploy.Multisig:
        // Get validators from specified chains
        const validators = await getValidatorsForChain(...deployConfig.validatorChainIds);
        // Combine all validators into a single array
        const allValidators = Object.values(validators).flat();
        
        // Deploy only to the current network
        deployments[deploymentChainId] = await deploy({
          chainId: deploymentChainId,
          addresses: allValidators,
          threshold: deployConfig.threshold || 1,
          type: Deploy.Multisig
        });
        break;

      case Deploy.Aggregation:
        if (!deployConfig.addresses) {
          throw new Error('ISM addresses required for Aggregation deployment');
        }
        deployments[deploymentChainId] = await deploy({
          chainId: deploymentChainId,
          addresses: deployConfig.addresses,
          threshold: deployConfig.threshold || 1,
          type: Deploy.Aggregation
        });
        break;

      case Deploy.Hook:
        if (!deployConfig.addresses) {
          throw new Error('Hook addresses required for Hook deployment');
        }
        deployments[deploymentChainId] = await deploy({
          chainId: deploymentChainId,
          addresses: deployConfig.addresses,
          threshold: 0,
          type: Deploy.Hook
        });
        break;
    }

    // Log results
    console.log('\n=== Deployment Results ===');
    console.log(`Deployed to chain ${deploymentChainId}: ${deployments[deploymentChainId]}`);

  } catch (error) {
    console.error('Deployment failed:', error);
    process.exitCode = 1;
  }
}

// Example configurations
// const multisigConfig: DeploymentConfig = {
//   validatorChainIds: [10], // Get validators from Optimism
//   type: Deploy.Multisig,
//   threshold: 2
// };

// const aggregationConfig: DeploymentConfig = {
//   validatorChainIds: [], // Not used for aggregation
//   type: Deploy.Aggregation,
//   threshold: 2,
//   addresses: [
//     "0xE74B7D236A97eED9026926073cBb436638266888",
//     "0x6C187cDC9DAaF10DFce81DE5Ff9687d1b7ebBE4C"
//   ]
// };

// const hookConfig: DeploymentConfig = {
//   chainIds: [8453], // Base
//   type: Deploy.Hook,
//   addresses: [
//     "0x0785a05b6f9ACD9e35E3b7B8fC5A2aD0cF76193b",
//     "0x92F0f4C9F769ed83609CD2ccD1ACcE224bBC8cBF"
//   ]
// };

if (require.main === module) {
  main();
}

//  DIA mainnet validator  0x6f9ea6CccD00e974374170012f5064a7a3665c0E

/* optimism 0xde92FBAd083b7b45fac04e4d84C4404049F021e7 multisig ism
            0xE74B7D236A97eED9026926073cBb436638266888 ISM




            
    base 0x0785a05b6f9ACD9e35E3b7B8fC5A2aD0cF76193b  multisig ism
            0x92F0f4C9F769ed83609CD2ccD1ACcE224bBC8cBF ISM

    DIA ISM 0xE74B7D236A97eED9026926073cBb436638266888
     Multisig  0x6C187cDC9DAaF10DFce81DE5Ff9687d1b7ebBE4C with new validators

            */