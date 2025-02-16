   
import { ethers } from "hardhat";

async function main() {
  const OracleTrigger = await ethers.getContractFactory("ProtocolFeeHook");
  const oracleTrigger = await OracleTrigger.deploy();
 
  console.log("ProtocolFeeHook deployed to:",await  oracleTrigger.getAddress());
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});