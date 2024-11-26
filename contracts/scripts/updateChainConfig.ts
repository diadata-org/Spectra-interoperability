import { ethers } from "hardhat";

async function main() {
  const [deployer] = await ethers.getSigners();
  
  console.log("Updating contract with the account:", deployer.address);
 
  const contractAddress = "0x6ccDb47E3292630699fBCcC1753d78DE18ea5B7e";
  const OracleTrigger = await ethers.getContractFactory("OracleTrigger");
  const oracleTrigger = OracleTrigger.attach(contractAddress);

  // 11155111
  // const chainId = 23104;
  // const mailBox = "0x6966b0E55883d49BFB24539356a2f8A673E02039";
  // const recipientAddress = "0xC0BE1265B5429E802e3C3b2b3bfB2e481E907061";


  const chainId = 84532;
  const mailBox = "0x9475dF7350BE17a0a2F6A285b57564631b149461";
  const recipientAddress = "0x001ED91E7b2EBe579E3D051fd32130087324b13D";

  const tx = await oracleTrigger.addChain(chainId, mailBox, recipientAddress);
  
  console.log("Transaction hash:", tx.hash);

  await tx.wait();

  console.log(`Chain added: chainId=${chainId}, mailBox=${mailBox}, recipientAddress=${recipientAddress}`);
}

main()
  .then(() => process.exit(0))
  .catch((error) => {
    console.error(error);
    process.exit(1);
  });