import fs from "fs";
import path from "path";
import dotenv from "dotenv";
import {
  addressToBytes32,
  bytes32ToAddress,
  timeout,
} from "@hyperlane-xyz/utils";

import pkg from "hardhat";

const { ethers } = pkg;

dotenv.config();

let gasSummary = [];

async function main() {
  let sourceChainID = 100640 //1050
   //11155420, 84532, 421614, 11155111, 1301
  let destinationChainID = 1301;
  let chainId ;

  const destination = process.env.DESTINATION === "true";


  if (  !chainId) {
    const network = await ethers.provider.getNetwork();

    console.log("Network name=", network.name);
    console.log("Network chain id=", network.chainId);
    chainId = network.chainId.toString();
    // console.error("Please set TASK (source/destination) and CHAIN_ID environment variables");
    // process.exit(1);
  }

  const [deployer] = await ethers.getSigners();
  console.log(`Executing setup on chain:`, chainId);

  // Read deployed contract addresses
  const filePath = path.resolve("./deployed_contracts.json");
  if (!fs.existsSync(filePath)) {
    console.error("deployed_contracts.json not found");
    process.exit(1);
  }

  const deployedContracts = JSON.parse(fs.readFileSync(filePath, "utf8"));
  if (!deployedContracts[chainId] || !deployedContracts[chainId].Contracts) {
    console.error(`No contracts found for chainId: ${chainId}`);
    process.exit(1);
  }

  if (!destination) {
    console.log(`Executing setup on DIA:`, chainId);

    await setupSource(chainId, deployedContracts, destinationChainID);
  } else  {
    console.log(`Executing setup on Destination:`, chainId);

    await setupDestination(chainId, deployedContracts,sourceChainID);
  }  

  printGasSummary();
}

async function setupSource(chainId, deployedContracts, destinationChainID) {

  const metadataFilePath = path.resolve("./oracle_metadata.json");
  let metadata = {};
  if (fs.existsSync(metadataFilePath)) {
      metadata = JSON.parse(fs.readFileSync(metadataFilePath, "utf8"));
  }
  let mailboxAddress;

  console.log("chainId",chainId)
  console.log("metadata",metadata)


  if (metadata[chainId]) {
        mailboxAddress = metadata[chainId].MailBox || "";
  }
  const requestRecipientAddress =
    deployedContracts[chainId].Contracts.OracleRequestRecipient;
  const requestOracleAddress =
    deployedContracts[destinationChainID].Contracts.RequestOracle;
  const oracleTrigger = deployedContracts[chainId].Contracts.OracleTrigger;

  const ism = deployedContracts[chainId].Contracts.Ism;

 
  

  console.log("ism----------",ism)

  if (!requestRecipientAddress || !requestOracleAddress) {
    console.error("Missing contract addresses in deployed_contracts.json");
    process.exit(1);
  }

  const requestRecipient = await ethers.getContractAt(
    "OracleRequestRecipient",
    requestRecipientAddress
  );
  const oracleTriggerFactory = await ethers.getContractAt(
    "OracleTrigger",
    oracleTrigger
  );


  const ismFactory = await ethers.getContractAt(
    "Ism",
    ism
  );

  


  
  await executeTransaction(ismFactory.addSenderShouldBe, [destinationChainID,requestOracleAddress], "add requestoracle to ism", "");
  await executeTransaction(ismFactory.setTrustedMailBox, [mailboxAddress], "setTrustedMailBox in ISM  ", "");
  // await executeTransaction(ismFactory.addTrustedRelayer, ["0x4Db7E4E00401Db46c0Afb3f93E5E06A3E5966872"], "addTrustedRelayer in ISM  ", "");


 

  await executeTransaction(requestRecipient.addToWhitelist, [destinationChainID,addressToBytes32(requestOracleAddress)], "add whitelist to OrcleRequestReceipent", "");


 

  await executeTransaction(requestRecipient.setOracleTriggerAddress, [oracleTrigger], "setOracleTriggerAddress for OrcleRequestReceipent ", "");


  

  await executeTransaction(requestRecipient.setInterchainSecurityModule, [ism], "setInterchainSecurityModule for OrcleRequestReceipent ", "");

   const signers = await ethers.getSigners();

  console.log("Current address:", signers[0].address);
 

  const role = ethers.keccak256(ethers.toUtf8Bytes("DISPATCHER_ROLE")); // Replace with the role name



  await executeTransaction(oracleTriggerFactory.grantRole, [role,"0x9bb71344ed950f9cfd85ee1c7258553b01d95fa0"], "add Dispatcher role to OrcleRequestReceipent in OracleTrigger   ", "");




 

 

 


 console.log("ism",ism)

// await (await ismFactory.setSenderShouldBe(421614,"0x9Fc4cC815c0599AC9757f13d10251056D372d8aB")).wait();

 
}


async function setupDestination(chainId, deployedContracts,sourceChainID) {
  const OracleTriggerAddress =
    deployedContracts[sourceChainID].Contracts.OracleTrigger;


    const OracleRequestReceipentAddress =
    deployedContracts[sourceChainID].Contracts.OracleRequestRecipient;

    const pushOracleReceiverAddress =
    deployedContracts[chainId].Contracts.PushOracleReceiver;

    const requestOracleAddress =
    deployedContracts[chainId].Contracts.RequestOracle;

    const protocolFeeHookAddress =
    deployedContracts[chainId].Contracts.ProtocolFeeHook;

    const ismAddress =
    deployedContracts[chainId].Contracts.Ism;

  if (!pushOracleReceiverAddress) {
    console.error(
      "Missing PushOracleReceiver contract address in deployed_contracts.json"
    );
    process.exit(1);
  }

  console.log("pushOracleReceiverAddress", pushOracleReceiverAddress);

  const protocolFeeHook = await ethers.getContractAt(
    "ProtocolFeeHook",
    protocolFeeHookAddress
  );

  
  const pushOracleReceiver = await ethers.getContractAt(
    "PushOracleReceiver",
    pushOracleReceiverAddress
  );


  const requestOracle = await ethers.getContractAt(
    "RequestOracle",
    requestOracleAddress
  );

  const ismfactory = await ethers.getContractAt(
    "Ism",
    ismAddress
  );


  console.log("requestOracleAddress", requestOracleAddress);


  const metadataFilePath = path.resolve("./oracle_metadata.json");
    let metadata = {};
    if (fs.existsSync(metadataFilePath)) {
        metadata = JSON.parse(fs.readFileSync(metadataFilePath, "utf8"));
    }

    console.log("chainId",chainId)
    console.log("metadata",metadata)


    if (metadata[chainId]) {
        const mailboxAddress = metadata[chainId].MailBox || "";
         
        if (mailboxAddress) {
            console.log(`Updating MailBox in RequestOracle to: ${mailboxAddress}`);
 
            await executeTransaction(requestOracle.setTrustedMailBox, [mailboxAddress], "Set MailBox in RequestOracle", "");


            console.log(`Updating MailBox in pushOracleReceiver to: ${mailboxAddress}`);
             await executeTransaction(pushOracleReceiver.setTrustedMailBox, [mailboxAddress], "Set MailBox in pushOracleReceiver", "");

             console.log(`Updating MailBox in ProtocolFeeHook to: ${mailboxAddress}`);
             await executeTransaction(protocolFeeHook.setTrustedMailBox, [mailboxAddress], "Set MailBox in protocolFeeHook", "");

 
             await executeTransaction(ismfactory.addSenderShouldBe, [sourceChainID,OracleTriggerAddress], "add oraclerequestreceipent in ISM", "");

             console.log("OracleRequestReceipentAddress",OracleRequestReceipentAddress);
             await executeTransaction(requestOracle.addToWhitelist, [sourceChainID,OracleRequestReceipentAddress], "add OracleRequestReceipentAddress to whitelist of RequestOracle", "");

             await executeTransaction(ismfactory.setTrustedMailBox, [mailboxAddress], "setTrustedMailBox in ISM  ", "");
            //  await executeTransaction(ismfactory.addTrustedRelayer, ["0x4Db7E4E00401Db46c0Afb3f93E5E06A3E5966872"], "setTrustedMailBox in ISM  ", "");
           
           
        }
        
        
    }else{
        console.log(`Metadata Not Found for chain`, chainId)
    }


    // await executeTransaction(ism.setSenderShouldBe, [100640, "0x4aAd43d11eE6858FE8ffb4fbd81b4746560AE6dF"], "Set Sender Should Be", deployer);




  const [signer] = await ethers.getSigners(); // Ensure you destructure correctly

  // const tx = await signer.sendTransaction({
  //   to: pushOracleReceiverAddress,
  //   value: ethers.parseEther("0.1"),
  // });
  // await tx.wait();

  // console.log(
  //   `Funded PushOracleReceiver (${pushOracleReceiverAddress}) with 0.1 ETH on chain ${chainId}`
  // );
}

async function executeTransaction(method, args, action, deployer) {
  try{const tx = await method(...args);
    const receipt = await tx.wait();
    
    const gasUsed = BigInt(receipt.gasUsed);
    const gasPrice = BigInt(receipt.gasPrice);
    const totalCost = gasUsed * gasPrice;
    
    gasSummary.push({ action, gasUsed, gasPrice, totalCost });
    
    console.log(`${action} completed. Gas Used: ${gasUsed}, Cost: ${ethers.formatEther(totalCost)} ETH`);}
    catch(error){
      console.log("err execution",error)
    }
  
    
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

main().catch(console.error);