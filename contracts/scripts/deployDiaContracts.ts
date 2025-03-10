import { ethers, run } from "hardhat";
import fs from "fs";
import path from "path";
import dotenv from "dotenv";

dotenv.config();

async function main() {
    const source = process.env.SOURCE === "true";
    const destination = process.env.DESTINATION === "true";

    const [deployer] = await ethers.getSigners();
    const chainId = (await deployer.provider.getNetwork()).chainId.toString();
    console.log("Deploying contracts on chain:", chainId);

    if (source) {
        await deploySourceContracts(chainId);
    } else if (destination) {
        await deployDestinationContracts(chainId);
    } else {
        console.error("Please set SOURCE or DESTINATION environment variable to true");
        process.exit(1);
    }
}

async function deploySourceContracts(chainId) {
    const oracleTrigger = await deployContract("OracleTrigger");
    const ism = await deployContract("Ism");
    const oracleRequestRecipient = await deployContract("OracleRequestRecipient");

    // Read metadata contract address and mailbox from oracle_metadata.json
    const metadataFilePath = path.resolve("./oracle_metadata.json");
    let metadata = {};
    if (fs.existsSync(metadataFilePath)) {
        metadata = JSON.parse(fs.readFileSync(metadataFilePath, "utf8"));
    }
    
    if (metadata[chainId]) {
        const mailboxAddress = metadata[chainId].MailBox || "";
        const metadataContractAddress = metadata[chainId].MetadataContract || "";
        
        if (mailboxAddress) {
            console.log(`Updating MailBox in OracleTrigger to: ${mailboxAddress}`);
            await (await oracleTrigger.setMailBox(mailboxAddress)).wait();
        }
        
        if (metadataContractAddress) {
            console.log(`Updating MetadataContract in OracleTrigger to: ${metadataContractAddress}`);
            await (await oracleTrigger.updateMetadataContract(metadataContractAddress)).wait();
        }
    }else{
        console.log(`Metadata Not Found`)
    }
    
    updateDeployedContracts(chainId, "source", {
        OracleTrigger: oracleTrigger.target,
        Ism: ism.target,
        OracleRequestRecipient: oracleRequestRecipient.target
    });

    await verifyContract("OracleTrigger", oracleTrigger.target);
    await verifyContract("Ism", ism.target);
    await verifyContract("OracleRequestRecipient", oracleRequestRecipient.target);
}

async function deployDestinationContracts(chainId) {
    const ism = await deployContract("Ism");
    const requestOracle = await deployContract("RequestOracle");
    const pushOracleReceiver = await deployContract("PushOracleReceiver");
    const protocolFeeHook = await  deployContract("ProtocolFeeHook");


    console.log(`Updating ISM in to allowall to: ${ism.target}`);
    await (await ism.setAllowAll(true)).wait();

    

    console.log(`Updating ISM in RequestOracle to: ${ism.target}`);
    await (await requestOracle.setInterchainSecurityModule(ism.target)).wait();

    console.log(`Updating ProtocolFeeHook in RequestOracle to: ${protocolFeeHook.target}`);
    await (await requestOracle.setPaymentHook(protocolFeeHook.target)).wait();


    console.log(`Updating ISM in pushOracleReceiver to: ${ism.target}`);
    await (await pushOracleReceiver.setInterchainSecurityModule(ism.target)).wait();

    console.log(`Updating ProtocolFeeHook in pushOracleReceiver to: ${protocolFeeHook.target}`);
    await (await pushOracleReceiver.setPaymentHook(protocolFeeHook.target)).wait();


    const metadataFilePath = path.resolve("./oracle_metadata.json");
    let metadata = {};
    if (fs.existsSync(metadataFilePath)) {
        metadata = JSON.parse(fs.readFileSync(metadataFilePath, "utf8"));
    }

    if (metadata[chainId]) {
        const mailboxAddress = metadata[chainId].MailBox || "";
         
        if (mailboxAddress) {
            console.log(`Updating MailBox in RequestOracle to: ${mailboxAddress}`);
            await (await requestOracle.setTrustedMailBox(mailboxAddress)).wait();

            console.log(`Updating MailBox in pushOracleReceiver to: ${mailboxAddress}`);
            await (await pushOracleReceiver.setTrustedMailBox(mailboxAddress)).wait();
        }
        
        
    }else{
        console.log(`Metadata Not Found for chain`, chainId)
    }


 
    
    updateDeployedContracts(chainId, "destination", {
        Ism: ism.target,
        RequestOracle: requestOracle.target,
        PushOracleReceiver: pushOracleReceiver.target,
        ProtocolFeeHook:protocolFeeHook.target,
    });
    
    await verifyContract("Ism", ism.target);
    await verifyContract("RequestOracle", requestOracle.target);
    await verifyContract("PushOracleReceiver", pushOracleReceiver.target);
}

async function deployContract(contractName) {
    console.log(`Deploying ${contractName}`);

    const ContractFactory = await ethers.getContractFactory(contractName);
    console.log(`Deploying ..  ${contractName}`);

    const contract = await ContractFactory.deploy();
    console.log(`Deploying .. ..  ${contractName}`);

    await contract.waitForDeployment();
    console.log(`${contractName} deployed at:`, contract.target);
    return contract;
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
        console.error(`${contractName} verification failed:`, error);
    }
}

main().catch(console.error);
