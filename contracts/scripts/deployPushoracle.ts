   
import { ethers } from "hardhat";

async function main() {
  const OracleTrigger = await ethers.getContractFactory("PushOracleReceiver");
  const oracleTrigger = await OracleTrigger.deploy();
 
  console.log("PushOracleReceiver deployed to:",await  oracleTrigger.getAddress());
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});