import { run } from 'hardhat';

async function main() {
  const PUSH_ORACLE_RECEIVER = "0xf359f17fc18f7d7c3ed6b2faadbe66ec0c7894de";
  
  console.log("Verifying PushOracleReceiver on Optimism Sepolia...");
  console.log("Contract address:", PUSH_ORACLE_RECEIVER);
  
  try {
    // PushOracleReceiver has no constructor arguments
    await run("verify:verify", {
      address: PUSH_ORACLE_RECEIVER,
      constructorArguments: [],
      contract: "contracts/PushOracleReceiver.sol:PushOracleReceiver"
    });
    
    console.log("Contract verified successfully!");
  } catch (error: any) {
    if (error.message.includes("Already Verified")) {
      console.log("Contract is already verified!");
    } else {
      console.error("Verification failed:", error);
    }
  }
}

main()
  .then(() => process.exit(0))
  .catch((error) => {
    console.error(error);
    process.exit(1);
  });