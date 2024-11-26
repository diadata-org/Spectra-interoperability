import pkg from "hardhat";
import {
  addressToBytes32,
  bytes32ToAddress,
  timeout,
} from "@hyperlane-xyz/utils";

 
const { ethers } = pkg;

async function main() {
  let destinationDomain = 10640;
  let diaRecipient = "0x97C989740aE765518FA85E64ED61512D39765e43"
  let formattedRecipient = addressToBytes32(
    diaRecipient
  );

  const network = await ethers.provider.getNetwork();
  console.log("Chain ID:", network.chainId);


  const key =  "UNI/USD"; // Assuming key is an address or a bytes32 value
 
  const abiCoder = new ethers.AbiCoder();

   

  const body = abiCoder.encode(
    ["string"], // Types of the parameters
    [key] // Values to encode
  );
  let messageBody = ethers.hexlify(body);

  console.log("messageBody", messageBody);

 
  let mailBoxAddress;
  let oracleRequestorAddress;

  switch (network.chainId){
    case 84532n: {
       mailBoxAddress = "0x6966b0E55883d49BFB24539356a2f8A673E02039"
      oracleRequestorAddress = "0x52A2F754d876bF15aE61cfAC62c7d948699965D9"
    }
    break;

    case 11155111n: {
      mailBoxAddress = "0xfFAEF09B3cd11D9b20d1a19bECca54EEC2884766"
      oracleRequestorAddress = "0x3b64691c14bca163c8230e726c6f880b0e74ab0d"
    }
    break;


    case 421614n: {
       mailBoxAddress = "0x598facE78a4302f11E3de0bee1894Da0b2Cb71F8"
      oracleRequestorAddress = "0xebcdc9d3ef5d07B7B668146E41C73b003314a37f"
    }
    break;
    case 11155420n: {
      mailBoxAddress = "0x6966b0E55883d49BFB24539356a2f8A673E02039"
     oracleRequestorAddress = "0xbEc3e192175D1bEEdb7ACc4b87F7d158fF363841"
   }
   

    
    break;
    default:{
 
      mailBoxAddress = "0x6966b0E55883d49BFB24539356a2f8A673E02039"
      oracleRequestorAddress = "0x52A2F754d876bF15aE61cfAC62c7d948699965D9"
    }

  }


  // dia mailbox 0x9475dF7350BE17a0a2F6A285b57564631b149461
  // sepolia mailbox 0x6966b0E55883d49BFB24539356a2f8A673E02039
  const mailbox = await ethers.getContractAt(
    "IMailbox",
    mailBoxAddress
  );

  console.log("-----")

  const OracleUpdateRecipient = await ethers.getContractAt(
    "OracleRequestor",
    oracleRequestorAddress
  );

  console.log("--quoteDispatch---",mailBoxAddress)


  // const mb = mailbox.attach(mailBoxAddress)

  //   let  hook = await mailbox.defaultHook();
  // // console.log("hook",hook)
    "0x6966b0E55883d49BFB24539356a2f8A673E02039"
  );

  const OracleUpdateRecipient = await ethers.getContractAt(
    "OracleRequestor",
    "0x90a26776EC9B2C0b7234140a4A5Cc085eEFb63cc"
  );

  // const mb = mailbox.attach("0x68a528ccedc27ead40d7391ebb5182154b754fc9")

  //   let  hook = await mailbox.defaultHook();
  // console.log("hook",hook)
  // console.log("messageBody",messageBody)
  // console.log("formattedRecipient",formattedRecipient)

  const v = await mailbox['quoteDispatch(uint32,bytes32,bytes)'](destinationDomain, formattedRecipient, messageBody);
  console.log("value",v)

  // console.log("value",messageBody)


  // IMailbox _mailbox,
  // address reciever,
  // uint32 _destinationDomain,
  // bytes calldata _messageBody

  console.log("mailBoxAddress",  addressToBytes32(
    mailBoxAddress
  ))
  console.log("formattedRecipient",  formattedRecipient)
  console.log("sender",  addressToBytes32(
    oracleRequestorAddress
  ))


  let messageTx = await OracleUpdateRecipient.request(
    mailBoxAddress,
    diaRecipient,
  let messageTx = await OracleUpdateRecipient.request(
    "0x6966b0E55883d49BFB24539356a2f8A673E02039",
    "0x6ccDb47E3292630699fBCcC1753d78DE18ea5B7e",
    destinationDomain,
    messageBody,
    { value: 1 }
  );

  // console.log("t", t);

  // let messageTx = await mailbox['dispatch(uint32,bytes32,bytes,bytes,address)'](
  //   destinationDomain,
  //   formattedRecipient,
  //   messageBody,
  //   '0x',
  //    '0x0000000000000000000000000000000000000000',
  //   { value: v },
  // )
  //   const messageTx = await mailbox['dispatch(uint32,bytes32,bytes,bytes,address)'](destinationDomain, formattedRecipient, messageBody,'0x','0x0000000000000000000000000000000000000000', {value:v});
   console.log("messageTx",messageTx)
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
