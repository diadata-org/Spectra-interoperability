import { ethers, run } from "hardhat";
import fs from "fs";
import path from "path";
import dotenv from "dotenv";

dotenv.config();

let gasSummary = [];

async function main() {
    const destination = process.env.DESTINATION === "true";
    const [deployer] = await ethers.getSigners();
    const chainId = (await deployer.provider.getNetwork()).chainId.toString();
    
    if (!destination) {
        console.log("Deploying contracts on DIA Oracle chain:", chainId);
        await deploySourceContracts(chainId, deployer);
    } else {
        console.log("Deploying contracts on Destination chain:", chainId);
        await deployDestinationContracts(chainId, deployer);
    }
    
    printGasSummary();
}

async function deploySourceContracts(chainId, deployer) {
    const oracleTrigger = await deployContract("OracleTrigger", deployer);
    const ism = await deployContract("Ism", deployer);
    const oracleRequestRecipient = await deployContract("OracleRequestRecipient", deployer);

    const metadataFilePath = path.resolve("./oracle_metadata.json");
    let metadata = {};
    if (fs.existsSync(metadataFilePath)) {
        metadata = JSON.parse(fs.readFileSync(metadataFilePath, "utf8"));
        
    }
    
    if (metadata[chainId]) {
        const mailboxAddress = metadata[chainId].MailBox || "";
        const metadataContractAddress = metadata[chainId].MetadataContract || "";
        
        if (mailboxAddress) {
            await executeTransaction(oracleTrigger.setMailBox, [mailboxAddress], "Set MailBox in OracleTrigger", deployer);
        }
        
        if (metadataContractAddress) {
            await executeTransaction(oracleTrigger.updateMetadataContract, [metadataContractAddress], "Update MetadataContract in OracleTrigger", deployer);
        }
    } else {
        console.log(`Metadata Not Found`);
    }

    await verifyContract("OracleTrigger", oracleTrigger.target);
    await verifyContract("Ism", ism.target);
    await verifyContract("OracleRequestRecipient", oracleRequestRecipient.target);


    updateDeployedContracts(chainId, "source", {
        OracleTrigger: oracleTrigger.target,
        Ism: ism.target,
        OracleRequestRecipient: oracleRequestRecipient.target
    });
}

async function deployDestinationContracts(chainId, deployer) {
    const ism = await deployContract("Ism", deployer);
    const requestOracle = await deployContract("RequestOracle", deployer);
    const pushOracleReceiver = await deployContract("PushOracleReceiver", deployer);
    const protocolFeeHook = await deployContract("ProtocolFeeHook", deployer);


    await executeTransaction(requestOracle.setInterchainSecurityModule, [ism.target], "Set ISM in RequestOracle", deployer);
    await executeTransaction(requestOracle.setPaymentHook, [protocolFeeHook.target], "Set ProtocolFeeHook in RequestOracle", deployer);
    await executeTransaction(pushOracleReceiver.setInterchainSecurityModule, [ism.target], "Set ISM in PushOracleReceiver", deployer);
    await executeTransaction(pushOracleReceiver.setPaymentHook, [protocolFeeHook.target], "Set ProtocolFeeHook in PushOracleReceiver", deployer);


    await verifyContract("Ism", ism.target);
    await verifyContract("RequestOracle", requestOracle.target);
    await verifyContract("PushOracleReceiver", pushOracleReceiver.target);
    await verifyContract("ProtocolFeeHook", protocolFeeHook.target);


    updateDeployedContracts(chainId, "destination", {
        Ism: ism.target,
        RequestOracle: requestOracle.target,
        PushOracleReceiver: pushOracleReceiver.target,
        ProtocolFeeHook:protocolFeeHook.target,
    });
}

async function deployContract(contractName, deployer) {
    console.log(`Deploying ${contractName}...`);
    const ContractFactory = await ethers.getContractFactory(contractName);
    const contract = await ContractFactory.deploy();
    const receipt = await contract.deploymentTransaction().wait();
    
    const gasUsed = BigInt(receipt.gasUsed);
    const gasPrice = BigInt(receipt.gasPrice);
    const totalCost = gasUsed * gasPrice;


    
    
    gasSummary.push({ action: `Deploy ${contractName}`, gasUsed, gasPrice, totalCost });
    
    console.log(`${contractName} deployed at: ${contract.target} (Gas Used: ${gasUsed}, Cost: ${ethers.formatEther(totalCost)} ETH)`);
    return contract;
}

async function executeTransaction(method, args, action, deployer) {
    const tx = await method(...args);
    const receipt = await tx.wait();
    
    const gasUsed = BigInt(receipt.gasUsed);
    const gasPrice = BigInt(receipt.gasPrice);
    const totalCost = gasUsed * gasPrice;
    
    gasSummary.push({ action, gasUsed, gasPrice, totalCost });
    
    console.log(`${action} completed. Gas Used: ${gasUsed}, Cost: ${ethers.formatEther(totalCost)} ETH`);
}

function printGasSummary() {
    console.log("\nGas Usage Summary:");
    console.log("-------------------------------------------------------------------------------");
    console.log("| Action                             | Gas Used | Gas Price (Gwei) | Cost (ETH) | Simulated Cost (ETH @ 0.0013) |");
    console.log("-------------------------------------------------------------------------------");
    
    gasSummary.forEach(({ action, gasUsed, gasPrice, totalCost }) => {
        const simulatedCost = gasUsed * BigInt(1300000000); // 0.0013 ETH = 1.3 Gwei = 1.3 * 10^9 wei
        console.log(`| ${action.padEnd(35)} | ${gasUsed.toString().padEnd(9)} | ${ethers.formatUnits(gasPrice, "gwei").padEnd(16)} | ${ethers.formatEther(totalCost).padEnd(10)} | ${ethers.formatEther(simulatedCost).padEnd(10)} |`);
    });

    console.log("-------------------------------------------------------------------------------");
}

function updateDeployedContracts(chainId, type, contractAddresses) {
    const filePath = path.resolve("./deployed_contracts.json");
    let deployedContracts = {};
    if (fs.existsSync(filePath)) {
        deployedContracts = JSON.parse(fs.readFileSync(filePath, "utf8"));
    }
    
    if (!deployedContracts[chainId]) {
        deployedContracts[chainId] = { ChainId: chainId, Type: type, Contracts: {} };
    }
    
    Object.assign(deployedContracts[chainId].Contracts, contractAddresses);
    fs.writeFileSync(filePath, JSON.stringify(deployedContracts, null, 2));
    console.log("Updated contract addresses in deployed_contracts.json");
}


async function verifyContract(contractName, contractAddress) {
    console.log(`Verifying ${contractName}...`);
    try {
        await run("verify:verify", { address: contractAddress, constructorArguments: [] });
        console.log(`${contractName} verified`);
    } catch (error) {
        console.error(`${contractName} verification failed:`, error.message);
    }
}


main().catch(console.error);
