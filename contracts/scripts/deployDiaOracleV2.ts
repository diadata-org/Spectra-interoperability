   
import { ethers } from "hardhat";

async function main() {
  const DIAOracleV2MetaFactory = await ethers.getContractFactory("DIAOracleV2");
  const oracleUpdateRecipient = await DIAOracleV2MetaFactory.deploy();
 
  console.log("DIAOracleV2 deployed to:",await  oracleUpdateRecipient.getAddress());



  

  // let tx = await DIAOracleV2Meta.setThreshold(2);
  // await tx.wait(); // Wait for the transaction to be mined

  //   tx = await DIAOracleV2Meta.setTimeoutSeconds(120);
  // await tx.wait(); // Wait for the transaction to be mined
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});

//0x492F0C388f6897f6F149Be8B53554bAae52d3682