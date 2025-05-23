import { run } from "hardhat";
import fs from "fs";
import path from "path";

async function verifyDeployedContracts() {
    const [deployer] = await ethers.getSigners();
    const chainId = (await deployer.provider.getNetwork()).chainId.toString();
    
    // Read deployed contracts from JSON
    const deployedContractsPath = path.resolve("./deployed_contracts.json");
    if (!fs.existsSync(deployedContractsPath)) {
        console.error("deployed_contracts.json not found");
        return;
    }

    const deployedContracts = JSON.parse(fs.readFileSync(deployedContractsPath, "utf8"));
    const chainData = deployedContracts[chainId];

    if (!chainData || !chainData.Contracts) {
        console.error(`No contracts found for chain ID ${chainId}`);
        return;
    }

    console.log(`Verifying contracts on chain ${chainId}...`);
    console.log("Type:", chainData.Type);

    // Verify each contract
    for (const [contractName, address] of Object.entries(chainData.Contracts)) {
        console.log(`\nVerifying ${contractName} at ${address}...`);
        try {
            await run("verify:verify", {
                address: address,
                constructorArguments: []
            });
            console.log(`✅ ${contractName} verified successfully`);
        } catch (error) {
            console.error(`❌ Failed to verify ${contractName}:`, error.message);
        }
    }
}

async function main() {
    try {
        await verifyDeployedContracts();
    } catch (error) {
        console.error("Verification failed:", error);
        process.exit(1);
    }
}

main();