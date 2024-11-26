   
import { ethers } from "hardhat";

async function main() {
  const PostDispatchHook = await ethers.getContractFactory("PostDispatchHook");
  const postDispatchHook = await PostDispatchHook.deploy();
 
  console.log("PostDispatchHook deployed to:",await  postDispatchHook.getAddress());
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});