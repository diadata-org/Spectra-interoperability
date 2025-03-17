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

async function main() {
  const task = process.env.TASK;
  //11155420, 84532, 421614, 11155111
  let destinationChainID = 421614;
  let chainId = 100640;

  if (!task || !chainId) {
    const network = await ethers.provider.getNetwork();

    console.log("Network name=", network.name);
    console.log("Network chain id=", network.chainId);
    chainId = network.chainId.toString();
    // console.error("Please set TASK (source/destination) and CHAIN_ID environment variables");
    // process.exit(1);
  }

  const [deployer] = await ethers.getSigners();
  console.log(`Executing ${task} setup on chain:`, chainId);

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

  if (task === "source") {
    await setupSource(chainId, deployedContracts, destinationChainID);
  } else if (task === "destination") {
    await setupDestination(chainId, deployedContracts);
  } else {
    console.error("Invalid TASK. Use 'source' or 'destination'");
    process.exit(1);
  }
}

async function setupSource(chainId, deployedContracts, destinationChainID) {
  const requestRecipientAddress =
    deployedContracts[chainId].Contracts.OracleRequestRecipient;
  const requestOracleAddress =
    deployedContracts[destinationChainID].Contracts.RequestOracle;
  const oracleTrigger = deployedContracts[chainId].Contracts.OracleTrigger;
  const ism = deployedContracts[chainId].Contracts.Ism;

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
//   let tx = await requestRecipient.addToWhitelist(
//     destinationChainID,
//     addressToBytes32(requestOracleAddress)
//   );
//   await tx.wait();

//   console.log(
//     `Whitelisted RequestOracle (${requestOracleAddress}) in OracleRequestRecipient (${requestRecipientAddress}) on chain ${destinationChainID}`
//   );

//   tx = await requestRecipient.setOracleTriggerAddress(oracleTrigger);
//   await tx.wait();
//   console.log(
//     ` setOracleTriggerAddress  oracleTrigger (${oracleTrigger}) in OracleRequestRecipient (${requestRecipientAddress}) on chain ${chainId}`
//   );

  // addism

  // add requestreceipet as owner in oracletrigger
//   const signers = await ethers.getSigners();

//   console.log("Current address:", signers[0].address);

//   const owners =  await oracleTriggerFactory.getOwners()
//   console.log("owners",owners);
//   console.log("to add as dispatcher",requestRecipientAddress);
 
//     tx = await oracleTriggerFactory.addDispatcher(requestRecipientAddress)
//   await tx.wait();
//   console.log(
//     ` addOwner in  oracleTrigger (${oracleTrigger}) add owner (${requestRecipientAddress}) on chain ${chainId}`
//   );


 console.log("ism",ism)

await (await ismFactory.setSenderShouldBe(421614,"0x9Fc4cC815c0599AC9757f13d10251056D372d8aB")).wait();

 
}


async function setupDestination(chainId, deployedContracts) {
  const pushOracleReceiverAddress =
    deployedContracts[chainId].Contracts.PushOracleReceiver;

    const requestOracleAddress =
    deployedContracts[chainId].Contracts.RequestOracle;

  if (!pushOracleReceiverAddress) {
    console.error(
      "Missing PushOracleReceiver contract address in deployed_contracts.json"
    );
    process.exit(1);
  }

  console.log("pushOracleReceiverAddress", pushOracleReceiverAddress);


  
  const pushOracleReceiver = await ethers.getContractAt(
    "PushOracleReceiver",
    pushOracleReceiverAddress
  );


  const requestOracle = await ethers.getContractAt(
    "RequestOracle",
    requestOracleAddress
  );


  console.log("requestOracleAddress", requestOracleAddress);


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



  const [signer] = await ethers.getSigners(); // Ensure you destructure correctly

  const tx = await signer.sendTransaction({
    to: pushOracleReceiverAddress,
    value: ethers.parseEther("0.1"),
  });
  await tx.wait();

  console.log(
    `Funded PushOracleReceiver (${pushOracleReceiverAddress}) with 0.1 ETH on chain ${chainId}`
  );
}

main().catch(console.error);