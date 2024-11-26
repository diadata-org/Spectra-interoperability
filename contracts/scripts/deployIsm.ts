   
import { ethers } from "hardhat";

async function main() {
  const OracleTrigger = await ethers.getContractFactory("Ism");
  const oracleTrigger = await OracleTrigger.deploy();
 
  console.log("Ism deployed to:",await  oracleTrigger.getAddress());
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});