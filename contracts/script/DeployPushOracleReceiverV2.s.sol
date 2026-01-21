// SPDX-License-Identifier: GPL-3.0
pragma solidity 0.8.29;

import "forge-std/Script.sol";
import "../contracts/PushOracleReceiverV2.sol";

/**
 * @title Deploy PushOracleReceiverV2 to Arbitrum Sepolia
 * @notice Deploys PushOracleReceiverV2 with proper bridge configuration
 * @dev This script configures the contract for direct bridge signing
 */
contract DeployPushOracleReceiverV2 is Script {
    
    // Configuration constants
    address constant BRIDGE_WALLET = 0x0Fa4D71382178ecB0DBA9961cB31153819043DfE;
    uint256 constant TARGET_CHAIN_ID = 421614; // Arbitrum Sepolia
    uint256 constant SOURCE_CHAIN_ID = 100640; // Lasernet
    
    // Existing contract addresses from deployed_contracts.json
    address constant ISM_ADDRESS = 0xb869617a3CFcdA07A4cC230d996120074e7c817e;
    address constant PROTOCOL_FEE_HOOK = 0x611C8b288c642336136a436d7125AC49FA71468B;
    
    // Domain configuration for bridge signing
    string constant DOMAIN_NAME = "SpectraBridge";
    string constant DOMAIN_VERSION = "1";
    
    function run() external {
        uint256 deployerPrivateKey = vm.envUint("PRIVATE_KEY");
        address deployer = vm.addr(deployerPrivateKey);
        
        console.log("=== PushOracleReceiverV2 Deployment on Arbitrum Sepolia ===");
        console.log("Deployer address:", deployer);
        console.log("Target Chain ID:", TARGET_CHAIN_ID);
        console.log("Bridge Wallet:", BRIDGE_WALLET);
        
        // Verify we're on the right network
        require(block.chainid == TARGET_CHAIN_ID, "Wrong network - expected Arbitrum Sepolia (421614)");
        
        // Check deployer balance
        uint256 balance = deployer.balance;
        console.log("Deployer balance:", balance / 1e18, "ETH");
        require(balance > 0.01 ether, "Insufficient balance for deployment");
        
        vm.startBroadcast(deployerPrivateKey);
        
        // Deploy PushOracleReceiverV2 with bridge-compatible domain
        console.log("\n=== Deploying Contract ===");
        console.log("Domain Name:", DOMAIN_NAME);
        console.log("Domain Version:", DOMAIN_VERSION);
        console.log("Source Chain ID:", SOURCE_CHAIN_ID);
        console.log("Verifying Contract (Bridge Address):", BRIDGE_WALLET);
        
        PushOracleReceiverV2 receiver = new PushOracleReceiverV2(
            DOMAIN_NAME,
            DOMAIN_VERSION,
            SOURCE_CHAIN_ID,
            BRIDGE_WALLET  // Use bridge wallet as verifying contract for EIP-712
        );
        
        address receiverAddress = address(receiver);
        console.log("PushOracleReceiverV2 deployed at:", receiverAddress);
        
        // Configure the contract
        console.log("\n=== Configuring Contract ===");
        
        // 1. Set ISM
        console.log("Setting ISM to:", ISM_ADDRESS);
        receiver.setInterchainSecurityModule(ISM_ADDRESS);
        
        // 2. Set Protocol Fee Hook
        console.log("Setting Protocol Fee Hook to:", PROTOCOL_FEE_HOOK);
        receiver.setPaymentHook(payable(PROTOCOL_FEE_HOOK));
        
        // 3. Authorize bridge wallet for signing
        console.log("Authorizing bridge wallet as signer:", BRIDGE_WALLET);
        receiver.setSignerAuthorization(BRIDGE_WALLET, true);
        
        // 4. Also authorize deployer as backup signer
        console.log("Authorizing deployer as backup signer:", deployer);
        receiver.setSignerAuthorization(deployer, true);
        
        // Verify configuration
        console.log("\n=== Verification ===");
        bytes32 domainSeparator = receiver.getDomainSeparator();
        console.log("Domain Separator:", vm.toString(domainSeparator));
        
        bool bridgeAuthorized = receiver.isAuthorizedSigner(BRIDGE_WALLET);
        bool deployerAuthorized = receiver.isAuthorizedSigner(deployer);
        console.log("Bridge wallet authorized:", bridgeAuthorized);
        console.log("Deployer authorized:", deployerAuthorized);
        
        require(bridgeAuthorized, "Bridge wallet not properly authorized");
        require(deployerAuthorized, "Deployer not properly authorized");
        
        vm.stopBroadcast();
        
        // Output deployment information
        console.log("\n=== Deployment Complete ===");
        console.log("Contract Address:", receiverAddress);
        console.log("Bridge Wallet:", BRIDGE_WALLET);
        console.log("Domain Separator:", vm.toString(domainSeparator));
        console.log("ISM:", ISM_ADDRESS);
        console.log("Protocol Fee Hook:", PROTOCOL_FEE_HOOK);
        
        console.log("\n=== Bridge Configuration Update Required ===");
        console.log("Update bridge configuration with new contract address:", receiverAddress);
        console.log("Domain configuration:");
        console.log("  Name:", DOMAIN_NAME);
        console.log("  Version:", DOMAIN_VERSION);
        console.log("  Chain ID:", SOURCE_CHAIN_ID);
        console.log("  Verifying Contract:", BRIDGE_WALLET);
    }
}