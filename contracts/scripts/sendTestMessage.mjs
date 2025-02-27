import pkg from "hardhat";
import fs from "fs";

import path from "path";

import {
  addressToBytes32,
  bytes32ToAddress,
  timeout,
} from "@hyperlane-xyz/utils";

const { ethers } = pkg;

async function main() {
  let destinationDomain = 100640;

  let chainId = process.env.CHAIN_ID;
  const network = await ethers.provider.getNetwork();
  console.log("Network chain id=", network.chainId);
  chainId = network.chainId.toString()

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

  let diaRecipient = deployedContracts[destinationDomain].Contracts.OracleRequestRecipient
  console.log(`diaRecipient: ${diaRecipient}`);

  let allMailbox = {
    // "10640": {
    //   "MetadataContract": "0x7Dd70B4B76130Bc29E33635d2d1F88e088dF84A6",
    //   "MailBox": "0xB1869f5e26C7e673ECFF555F5AbAbF83c145044a"
    // },
    "11155420": {
      "MailBox": "0x6966b0E55883d49BFB24539356a2f8A673E02039"
    },
    "84532": {
      "MailBox": "0x6966b0E55883d49BFB24539356a2f8A673E02039"
    },
    "421614": {
      "MailBox": "0x598facE78a4302f11E3de0bee1894Da0b2Cb71F8"
    },
    "11155111":{
      "MailBox":"0xfFAEF09B3cd11D9b20d1a19bECca54EEC2884766"
    },
    "1301":{
      "MailBox":"0xDDcFEcF17586D08A5740B7D91735fcCE3dfe3eeD"
    }
  }

  console.log("mailbox-----",allMailbox[chainId+""].MailBox)

  let mailbox = allMailbox[chainId+""].MailBox
  console.log(`mailbox: ${mailbox}`);

  let oracleRequestorAddress = deployedContracts[chainId].Contracts.RequestOracle
  console.log(`oracleRequestorAddress: ${oracleRequestorAddress}`);




 
  const OracleUpdateRecipient = await ethers.getContractAt(
    "RequestOracle",
    oracleRequestorAddress
  );

    // create Message 
    const key =  "ETH/USD"; // Assuming key is an address or a bytes32 value
 
    const abiCoder = new ethers.AbiCoder();
  
    const body = abiCoder.encode(
      ["string"], // Types of the parameters
      [key] // Values to encode
    );
    let messageBody = ethers.hexlify(body);

    console.log("ethers.provider",ethers.provider)
  
    const gasPrice = (await ethers.provider.getFeeData()).gasPrice;
        const gasUsed = ethers.toBigInt(97440 *2 );
    const txCost = gasPrice * gasUsed;


    console.log("sender",addressToBytes32(oracleRequestorAddress))
  try{
    let messageTx = await OracleUpdateRecipient.request(
      mailbox,
      diaRecipient,
      destinationDomain,
      messageBody,
      { value: txCost }
    );

    console.log("messageTx",messageTx);


  }catch(e){
    console.log("err",e);

  }








  console.log("deployedContracts",deployedContracts)
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
