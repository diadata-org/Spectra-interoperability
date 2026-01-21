import { ethers } from "ethers";

async function main() {
  // Bridge private key from config
  const BRIDGE_PRIVATE_KEY = "0xde9d753fb7c1f4e2a284e3c58d930560f1141840e77cbaccf875689483df76be";
  const RPC_URL = "https://sepolia.optimism.io";

  // Create provider and wallet
  const provider = new ethers.JsonRpcProvider(RPC_URL);
  const wallet = new ethers.Wallet(BRIDGE_PRIVATE_KEY, provider);
  
  console.log("Testing transaction from:", wallet.address);

  // Get network info
  const network = await provider.getNetwork();
  console.log("Network:", network.name, "Chain ID:", network.chainId);

  // Get balance
  const balance = await provider.getBalance(wallet.address);
  console.log("Balance:", ethers.formatEther(balance), "ETH");

  // Get current nonce
  const nonce = await wallet.getNonce();
  console.log("Current nonce:", nonce);

  // Get gas price
  const feeData = await provider.getFeeData();
  console.log("Gas price:", ethers.formatUnits(feeData.gasPrice!, "gwei"), "gwei");

  // Send a simple transaction to self
  console.log("\nSending test transaction...");
  const tx = await wallet.sendTransaction({
    to: wallet.address,
    value: ethers.parseEther("0.00001"), // Send 0.00001 ETH to self
    gasLimit: 21000,
  });

  console.log("Transaction hash:", tx.hash);
  console.log("Waiting for confirmation...");
  
  const receipt = await tx.wait();
  console.log("Transaction confirmed!");
  console.log("Block number:", receipt?.blockNumber);
  console.log("Gas used:", receipt?.gasUsed.toString());
  console.log("\nView on Etherscan: https://sepolia-optimism.etherscan.io/tx/" + tx.hash);
}

main()
  .then(() => process.exit(0))
  .catch((error) => {
    console.error("Error:", error);
    process.exit(1);
  });