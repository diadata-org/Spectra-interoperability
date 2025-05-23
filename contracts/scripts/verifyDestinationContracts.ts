import { run } from "hardhat";
import fs from "fs";
import path from "path";

async function verifyDestinationContracts() {
    const [deployer] = await ethers.getSigners();
    const chainId = (await deployer.provider.getNetwork()).chainId.toString();
    
    // Read deployed contracts
    const deployedContractsPath = path.resolve("./deployed_contracts.json");
    if (!fs.existsSync(deployedContractsPath)) {
        console.error("deployed_contracts.json not found");
        return;
    }

    const deployedContracts = JSON.parse(fs.readFileSync(deployedContractsPath, "utf8"));
    const chainData = deployedContracts[chainId];

    if (!chainData || chainData.Type !== "destination" || !chainData.Contracts) {
        console.error(`No destination contracts found for chain ID ${chainId}`);
        return;
    }

    const destinationContracts = {
        Ism: chainData.Contracts.Ism,
        RequestOracle: chainData.Contracts.RequestOracle,
        PushOracleReceiver: chainData.Contracts.PushOracleReceiver,
        ProtocolFeeHook: chainData.Contracts.ProtocolFeeHook
    };

    console.log(`Verifying destination contracts on chain ${chainId}...`);

    // Verify each destination contract
    for (const [contractName, address] of Object.entries(destinationContracts)) {
        if (!address) continue;
        
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
        await verifyDestinationContracts();
    } catch (error) {
        console.error("Verification failed:", error);
        process.exit(1);
    }
}

main();