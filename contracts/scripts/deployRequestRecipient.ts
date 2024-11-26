   
import { ethers } from "hardhat";

async function main() {
  const OracleRequestRecipient = await ethers.getContractFactory("OracleRequestRecipient");
  const oracleRequestRecipient = await OracleRequestRecipient.deploy();
 
  console.log("OracleRequestRecipient deployed to:",await  oracleRequestRecipient.getAddress());
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});