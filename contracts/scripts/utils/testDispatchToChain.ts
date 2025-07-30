import { ethers } from "hardhat";

async function main() {
  console.log("=== Testing OracleTrigger.dispatchToChain ===");
  
  // Configuration
  const ORACLE_TRIGGER_ADDRESS = "0xFf0753b1E026c38ef397340dFEd742B6f943a0Bd";
  const DESTINATION_CHAIN_ID = 11155420; // OP Sepolia
  const SYMBOL = "BTC/USD";
  
  // Use the DIA deployment account
  const privateKey = "549951abf933331608c7971414b4442982b8e3c455637ba65f0d4d2610cf3624";
  const signer = new ethers.Wallet(privateKey, ethers.provider);
  console.log("Using signer:", signer.address);
  
  // Get the OracleTrigger contract
  const OracleTrigger = await ethers.getContractFactory("OracleTrigger");
  const trigger = OracleTrigger.attach(ORACLE_TRIGGER_ADDRESS);
  
  console.log("OracleTrigger address:", ORACLE_TRIGGER_ADDRESS);
  
  // Check current configuration
  try {
    const receiverAddress = await trigger.chains(DESTINATION_CHAIN_ID);
    console.log(`Receiver for chain ${DESTINATION_CHAIN_ID}:`, receiverAddress);
  } catch (error) {
    console.log("Error reading receiver:", error);
  }
  
  // Check if signer has DISPATCHER_ROLE
  const DISPATCHER_ROLE = ethers.id("DISPATCHER_ROLE");
  const hasRole = await trigger.hasRole(DISPATCHER_ROLE, signer.address);
  console.log("Signer has DISPATCHER_ROLE:", hasRole);
  
  if (!hasRole) {
    console.log("\n⚠️  Warning: Signer does not have DISPATCHER_ROLE");
    console.log("Transaction will likely fail. An admin needs to grant the role.");
    
    // Try to get admin role info
    const DEFAULT_ADMIN_ROLE = "0x0000000000000000000000000000000000000000000000000000000000000000";
    const adminCount = await trigger.getRoleMemberCount(DEFAULT_ADMIN_ROLE);
    console.log("Number of admins:", adminCount.toString());
    
    if (adminCount > 0) {
      const admin = await trigger.getRoleMember(DEFAULT_ADMIN_ROLE, 0);
      console.log("First admin:", admin);
    }
  }
  
  // Try to dispatch
  console.log(`\nAttempting to dispatch ${SYMBOL} to chain ${DESTINATION_CHAIN_ID}...`);
  
  try {
    // Send the transaction with a fixed gas limit
    const tx = await trigger.dispatchToChain(DESTINATION_CHAIN_ID, SYMBOL, {
      gasLimit: 500000
    });
    
    console.log("Transaction hash:", tx.hash);
    console.log("Waiting for confirmation...");
    
    const receipt = await tx.wait();
    console.log("Transaction confirmed!");
    console.log("Block number:", receipt.blockNumber);
    console.log("Gas used:", receipt.gasUsed.toString());
    
    // Log events
    if (receipt.events && receipt.events.length > 0) {
      console.log("\nEvents:");
      receipt.events.forEach((event: any) => {
        if (event.event) {
          console.log(`- ${event.event}`);
        }
      });
    }
    
  } catch (error: any) {
    console.error("\n❌ Error:", error.message);
    
    if (error.data) {
      try {
        // Try to decode the error
        const errorData = error.data;
        console.log("Error data:", errorData);
      } catch (e) {
        console.log("Could not decode error data");
      }
    }
    
    if (error.reason) {
      console.log("Reason:", error.reason);
    }
  }
}

main()
  .then(() => process.exit(0))
  .catch((error) => {
    console.error(error);
    process.exit(1);
  });