import { ethers } from "hardhat";

async function main() {
  const [deployer] = await ethers.getSigners();
  
  console.log("Updating contract with the account:", deployer.address);
 
  const reciepient = "0x90a26776EC9B2C0b7234140a4A5Cc085eEFb63cc";
  const OracleReciepint = await ethers.getContractFactory("OracleUpdateRecipient");
  const oracleReciepint = OracleReciepint.attach(reciepient);

    const ism = "0x1581C3BBBC81aEA5a21b8A7EB4712d5734767d84";
 
  const tx = await oracleReciepint.setInterchainSecurityModule(ism);
  
  console.log("Transaction hash:", tx.hash);

  await tx.wait();

  console.log(`OracleReciepint updated: ism=${ism}, reciepient=${reciepient}`);
}

main()
  .then(() => process.exit(0))
  .catch((error) => {
    console.error(error);
    process.exit(1);
  });