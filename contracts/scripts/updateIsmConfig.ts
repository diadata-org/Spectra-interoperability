import { ethers } from "hardhat";

async function main() {
  const [deployer] = await ethers.getSigners();
  
  console.log("Updating contract with the account:", deployer.address);
 
  const ism = "0x3AF4d750a82d65c77E2Fa2CDE250647DE7CBddE2";
  const sender = "0x252Cd6aEe2E776f6B80d92DB360e8D9716eA25Bc";

  const OracleReciepint = await ethers.getContractFactory("Ism");
  const oracleReciepint = OracleReciepint.attach(ism);

  // 11155111
  
  const senderShouldBe = await oracleReciepint.senderShouldBe();
  console.log("senderShouldBe:", senderShouldBe);

  // const tx = await oracleReciepint.setSenderShouldBe(sender);
  
  // console.log("Transaction hash:", tx.hash);

  // await tx.wait();

  // console.log(`ISM updated: ism=${ism}, sender=${sender}`);
}

main()
  .then(() => process.exit(0))
  .catch((error) => {
    console.error(error);
    process.exit(1);
  });