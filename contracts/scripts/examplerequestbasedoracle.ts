   
import { ethers } from "hardhat";

async function main() {
  const RequestBasedOracleExample = await ethers.getContractFactory("RequestBasedOracleExample");
  const requestBasedOracleExample = await RequestBasedOracleExample.deploy();

  let rboexample = await  requestBasedOracleExample.getAddress()
 
  console.log("RequestBasedOracleExample deployed to:",await  requestBasedOracleExample.getAddress());

  // set ISM


  let rbo = await RequestBasedOracleExample.attach(rboexample)

  const tx = await rbo.setInterchainSecurityModule("0x1581C3BBBC81aEA5a21b8A7EB4712d5734767d84");
  console.log("RequestBasedOracleExample ism update");

}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});