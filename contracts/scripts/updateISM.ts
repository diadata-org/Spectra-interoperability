import { ethers } from "hardhat";

async function main() {
  const [deployer] = await ethers.getSigners();
  
  console.log("Updating contract with the account:", deployer.address);
 
  const reciepient = "0xEf9dA4422b4C4E949CE1fBeC86Bb58528E328DE5";
  const OracleReciepint = await ethers.getContractFactory("OracleRequestRecipient");
  const oracleReciepint = OracleReciepint.attach(reciepient);

    const ism = "0x223057FDEef80fb8087DB0406DD67D073DD7c597";
 
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