   
import { ethers } from "hardhat";

async function main() {
  const DIAOracleV2MetaFactory = await ethers.getContractFactory("DIAOracleV2Meta");
  // const oracleUpdateRecipient = await OracleUpdateRecipient.deploy();
 
  // console.log("DIAOracleV2Meta deployed to:",await  oracleUpdateRecipient.getAddress());



  // const DIAOracleV2Meta = await DIAOracleV2MetaFactory.deploy();

  const DIAOracleV2Meta = await DIAOracleV2MetaFactory.attach("0x90857994FA959d77728067A71EFA8dc154D89813")



  // Example array of oracle addresses to add
  const oracleAddresses = [
    "0xda0cb380cacaa4310dcbd32a3df99d73422402f6",
    "0xd80f387d3f218b00f5f9c4e3769c26f1d139945b",
    "0x40b0bb2688f4410391a783db0ca3689bf8b88d41",
    "0x6bcb18a819a952f80404c9c9c2ffb837a70cb668",
    "0x18c29ef26fa5069fe9da117dad6a921239ab101a"
   ];


 
 

  // Get the signer (admin) for calling addOracle
  const [deployer] = await ethers.getSigners();

  // Add each oracle address
  for (const address of oracleAddresses) {
    const tx = await DIAOracleV2Meta.addOracle(address);
    await tx.wait(); // Wait for the transaction to be mined
    console.log(`Added oracle address: ${address}`);
  }

  console.log("All oracle addresses added successfully.");



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