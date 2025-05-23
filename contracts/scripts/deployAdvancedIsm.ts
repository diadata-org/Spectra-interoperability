import { ethers } from "hardhat";
import fs from "fs";
import path from "path";

// ABI for both factory contracts since they have the same interface
const factoryABI = [
  "function deploy(address[] memory _validators, uint8 _threshold) external returns (address)"
];

async function deployIsm(
  chainId: number,
  validatorAddresses: string[],
  threshold: number,
  isAggregate: boolean
) {
  // Read config file
  const configPath = path.resolve("../agent-config-private.json");
  const config = JSON.parse(fs.readFileSync(configPath, "utf8"));

  // Find chain config
  const chainConfig = Object.values(config.chains).find(
    (chain: any) => chain.chainId === chainId
  );

  if (!chainConfig) {
    throw new Error(`Chain ID ${chainId} not found in config`);
  }

  // Get factory address based on flag
  const factoryAddress = isAggregate 
    ? chainConfig.staticAggregationIsmFactory
    : chainConfig.staticMessageIdMultisigIsmFactory;

  if (!factoryAddress) {
    throw new Error(`Factory address not found for chain ${chainId}`);
  }

  console.log(`Using ${isAggregate ? 'Aggregation' : 'Multisig'} ISM Factory`);
  console.log(`Factory address: ${factoryAddress}`);
  console.log(`Validators: ${validatorAddresses.join(", ")}`);
  console.log(`Threshold: ${threshold}`);

  // Get signers from Hardhat
  const [deployer] = await ethers.getSigners();
  console.log("Deploying with account:", deployer.address);

  // Create contract instance using Hardhat
  const factory = new ethers.Contract(
    factoryAddress,
    factoryABI,
    deployer
  );

  try {
    const tx = await factory.deploy(validatorAddresses, threshold);
    console.log(`Transaction hash: ${tx.hash}`);
    
    const receipt = await tx.wait();
    console.log(`Transaction confirmed in block ${receipt.blockNumber}`);
    
    // Get deployed ISM address from logs
    const deployedAddress = receipt.logs[0]?.address; // Adjust based on your contract's events
    console.log("Deployed ISM address:", deployedAddress);
    
    return deployedAddress;
  } catch (error) {
    console.error("Deployment failed:", error);
    throw error;
  }
}

// Example usage with async/await
async function main() {
  const chainId = 1050; // Arbitrum
  const validators = [
    "0xec68258a7c882ac2fc46b81ce80380054ffb4ef2","0x5450447aee7b544c462c9352bef7cad049b0c2dc"  ];
  const threshold = 1;
//   const isAggregate = process.argv[2] === "aggregate";

  try {
    const deployedAddress = await deployIsm(chainId, validators, threshold, false);
    console.log("Deployment successful. ISM address:", deployedAddress);
  } catch (error) {
    console.error("Deployment failed:", error);
    process.exitCode = 1;
  }
}

// Execute the script
main();

//  DIA mainnet validator  0x6f9ea6CccD00e974374170012f5064a7a3665c0E

/* optimism 0xde92FBAd083b7b45fac04e4d84C4404049F021e7 multisig ism
            0xE74B7D236A97eED9026926073cBb436638266888 ISM

    base 0x0785a05b6f9ACD9e35E3b7B8fC5A2aD0cF76193b  multisig ism
            0x92F0f4C9F769ed83609CD2ccD1ACcE224bBC8cBF ISM

    DIA ISM 0xE74B7D236A97eED9026926073cBb436638266888
     Multisig  0x1aA381fB4808486605f87dC60989df49BFb6874e with new validators

            */