   
import { ethers } from "hardhat";

async function main() {
  const OracleUpdateRecipient = await ethers.getContractFactory("OracleUpdateRecipient");
  const oracleUpdateRecipient = await OracleUpdateRecipient.deploy();
 
  console.log("OracleUpdateRecipient deployed to:",await  oracleUpdateRecipient.getAddress());
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});

//0x492F0C388f6897f6F149Be8B53554bAae52d3682