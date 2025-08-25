// // SPDX-License-Identifier: GPL-3.0
// pragma solidity 0.8.29;

// import "forge-std/Test.sol";
// import "../contracts/OracleIntentRegistry.sol";
// import "../contracts/PushOracleReceiverV2.sol";
// import "../contracts/interfaces/oracle/IPushOracleReceiverV2.sol";
// import "../contracts/interfaces/IInterchainSecurityModule.sol";
// import "../contracts/libs/OracleIntentUtils.sol";

// contract MockInterchainSecurityModule is IInterchainSecurityModule {
//     function moduleType() external pure override returns (uint8) {
//         return 1;
//     }

//     function verify(
//         bytes calldata, // _message
//         bytes calldata  // _metadata
//     ) external pure override returns (bool) {
//         return true;
//     }
// }

// contract MockProtocolFeeHook {
//     uint256 public gasUsedPerTx;
    
//     constructor(uint256 _gasUsedPerTx) {
//         gasUsedPerTx = _gasUsedPerTx;
//     }
    
//     receive() external payable {}
//     fallback() external payable {}
// }

// /**
//  * @title OracleIntentRegistryIntegrationTest
//  * @dev Test the complete flow: Registry → Intent Creation → PushOracleReceiver Submission
//  */
// contract OracleIntentRegistryIntegrationTest is Test {
//     OracleIntentRegistry public registry;
//     PushOracleReceiverV2 public receiver;
//     MockInterchainSecurityModule public ism;
//     MockProtocolFeeHook public feeHook;
    
//     address public owner;
//     address public trustedMailbox;
//     address public oracleSigner;
//     uint256 public oracleSignerPk;
    
//     // Registry Domain Configuration (separate from receiver)
//     string constant REGISTRY_DOMAIN_NAME = "DIA Oracle Intent";
//     string constant REGISTRY_DOMAIN_VERSION = "1";
    
//     // Receiver Domain Configuration
//     string constant RECEIVER_DOMAIN_NAME = "OracleIntentRegistry";
//     string constant RECEIVER_DOMAIN_VERSION = "1.0.0";
//     uint256 constant SOURCE_CHAIN_ID = 100640;
    
//     // Test data
//     string constant TEST_SYMBOL = "BTC";
//     uint256 constant TEST_PRICE = 50000e18;
//     uint256 constant TEST_TIMESTAMP = 1710000000;
    
//     event IntentRegistered(bytes32 indexed intentHash, string indexed symbol, uint256 price, uint256 timestamp, address signer);
//     event IntentBasedUpdateReceived(bytes32 indexed intentHash, string indexed symbol, uint256 price, uint256 timestamp, address indexed signer);

//     function setUp() public {
//         owner = address(this);
//         trustedMailbox = address(0x123);
//         oracleSignerPk = 1;
//         oracleSigner = vm.addr(oracleSignerPk);
        
//         // Deploy mocks
//         ism = new MockInterchainSecurityModule();
//         feeHook = new MockProtocolFeeHook(1000);
        
//         // Deploy registry
//         registry = new OracleIntentRegistry();
        
//         // Deploy receiver using the SAME domain configuration as registry for consistency
//         receiver = new PushOracleReceiverV2(
//             REGISTRY_DOMAIN_NAME,      // Same domain name as registry
//             REGISTRY_DOMAIN_VERSION,   // Same domain version as registry  
//             uint32(block.chainid),     // Same chain ID as registry
//             address(registry)          // Registry as verifying contract
//         );
        
//         // Setup receiver configuration
//         receiver.setInterchainSecurityModule(address(ism));
//         receiver.setPaymentHook(payable(address(feeHook)));
//         receiver.setTrustedMailBox(trustedMailbox);
        
//         // Authorize the oracle signer in both contracts
//         registry.setSignerAuthorization(oracleSigner, true);
//         receiver.setSignerAuthorization(oracleSigner, true);
        
//         // Fund contracts
//         vm.deal(address(receiver), 10 ether);
//         vm.deal(address(feeHook), 1 ether);
//     }

//     // ===== COMBINED FLOW TESTS =====
    
//     /**
//      * @dev Test complete flow: Create intent in registry → Submit to receiver
//      */
//     function testCompleteOracleIntentFlow() public {
//         // Step 1: Register intent in registry using contract function
//         bytes32 registryIntentHash = registerIntentInRegistry(TEST_SYMBOL, 1);
        
//         // Step 2: Retrieve the registered intent from the contract
//         OracleIntentUtils.OracleIntent memory registeredIntent = registry.getIntent(registryIntentHash);
        
//         // Step 3: Create receiver intent using exact same data from registered intent (including signature)
//         OracleIntentUtils.OracleIntent memory receiverIntent = createReceiverIntentFromRegistry(registeredIntent);
        
//         // Step 4: Submit to receiver (intent hash should be same as registry since same domain)
//         bytes32 receiverIntentHash = receiver.calculateIntentHash(receiverIntent);
//         assertEq(receiverIntentHash, registryIntentHash); // Should be same hash
        
//         vm.expectEmit(true, true, true, true);
//         emit IntentBasedUpdateReceived(receiverIntentHash, TEST_SYMBOL, TEST_PRICE, TEST_TIMESTAMP, oracleSigner);
        
//         receiver.handleIntentUpdate(receiverIntent);
        
//         // Step 5: Verify data was updated in receiver
//         (uint128 storedTimestamp, uint128 storedValue) = receiver.updates(TEST_SYMBOL);
//         assertEq(storedTimestamp, uint128(TEST_TIMESTAMP));
//         assertEq(storedValue, uint128(TEST_PRICE));
//         assertTrue(receiver.isProcessedIntent(receiverIntentHash));
//     }
    
//     /**
//      * @dev Test batch flow: Register multiple intents → Submit batch to receiver
//      */
//     function testBatchOracleIntentFlow() public {
//         uint256 batchSize = 3;
        
//         // Step 1: Register multiple intents in registry
//         bytes32[] memory registryIntentHashes = new bytes32[](batchSize);
//         OracleIntentUtils.OracleIntent[] memory receiverIntents = new OracleIntentUtils.OracleIntent[](batchSize);
        
//         for (uint i = 0; i < batchSize; i++) {
//             string memory symbol = string(abi.encodePacked("TOKEN", vm.toString(i)));
//             uint256 nonce = i + 1;
            
//             // Register intent in registry
//             registryIntentHashes[i] = registerIntentInRegistry(symbol, nonce);
            
//             // Retrieve registered intent and create receiver intent with exact same data
//             OracleIntentUtils.OracleIntent memory registeredIntent = registry.getIntent(registryIntentHashes[i]);
//             OracleIntentUtils.OracleIntent memory receiverIntent = createReceiverIntentFromRegistry(registeredIntent);
            
//             receiverIntents[i] = receiverIntent;
//         }
        
//         // Step 2: Verify all intents were registered
//         for (uint i = 0; i < batchSize; i++) {
//             string memory symbol = string(abi.encodePacked("TOKEN", vm.toString(i)));
//             bytes32 latestHash = registry.latestIntentBySymbol(symbol);
//             assertEq(latestHash, registryIntentHashes[i]);
//         }
        
//         // Step 3: Submit batch to receiver
//         receiver.handleBatchIntentUpdates(receiverIntents);
        
//         // Step 4: Verify all data was updated in receiver
//         for (uint i = 0; i < batchSize; i++) {
//             string memory symbol = string(abi.encodePacked("TOKEN", vm.toString(i)));
//             (uint128 timestamp, uint128 value) = receiver.updates(symbol);
//             assertEq(timestamp, uint128(TEST_TIMESTAMP));
//             assertEq(value, uint128(TEST_PRICE));
            
//             bytes32 receiverHash = receiver.calculateIntentHash(receiverIntents[i]);
//             assertTrue(receiver.isProcessedIntent(receiverHash));
//         }
//     }
    
//     /**
//      * @dev Test cross-chain intent forwarding via Hyperlane handle function
//      */
//     function testCrossChainIntentForwarding() public {
//         // Step 1: Register intent in registry first
//         bytes32 registryIntentHash = registerIntentInRegistry(TEST_SYMBOL, 1);
        
//         // Step 2: Get registered intent and create receiver version
//         OracleIntentUtils.OracleIntent memory registeredIntent = registry.getIntent(registryIntentHash);
//         OracleIntentUtils.OracleIntent memory intent = createReceiverIntentFromRegistry(registeredIntent);
        
//         // Step 3: Intent already has exact same data including signature
//         bytes32 intentHash = receiver.calculateIntentHash(intent);
//         assertEq(intentHash, registryIntentHash); // Should be same hash since exact same data
        
//         // Step 4: Encode intent as calldata for Hyperlane message
//         bytes memory intentData = abi.encode(
//             intent.intentType,
//             intent.version,
//             intent.chainId,
//             intent.nonce,
//             intent.expiry,
//             intent.symbol,
//             intent.price,
//             intent.timestamp,
//             intent.source,
//             intent.signature,
//             intent.signer
//         );
        
//         // Step 5: Simulate cross-chain message via Hyperlane
//         vm.expectEmit(true, true, true, true);
//         emit IntentBasedUpdateReceived(intentHash, TEST_SYMBOL, TEST_PRICE, TEST_TIMESTAMP, oracleSigner);
        
//         vm.prank(trustedMailbox);
//         receiver.handle(
//             uint32(SOURCE_CHAIN_ID),
//             bytes32(uint256(uint160(address(registry)))),
//             intentData
//         );
        
//         // Step 6: Verify data was updated
//         (uint128 timestamp, uint128 value) = receiver.updates(TEST_SYMBOL);
//         assertEq(timestamp, uint128(TEST_TIMESTAMP));
//         assertEq(value, uint128(TEST_PRICE));
//         assertTrue(receiver.isProcessedIntent(intentHash));
//     }
    
//     /**
//      * @dev Test format detection between intent and legacy formats
//      */
//     function testFormatDetectionInCombinedFlow() public {
//         // Create intent data (should be detected as intent format)
//         OracleIntentUtils.OracleIntent memory intent = createReceiverIntent();
//         bytes32 intentHash = receiver.calculateIntentHash(intent);
//         (uint8 v, bytes32 r, bytes32 s) = vm.sign(oracleSignerPk, intentHash);
//         intent.signature = abi.encodePacked(r, s, v);
//         intent.signer = oracleSigner;
        
//         bytes memory intentData = abi.encode(
//             intent.intentType,
//             intent.version,
//             intent.chainId,
//             intent.nonce,
//             intent.expiry,
//             intent.symbol,
//             intent.price,
//             intent.timestamp,
//             intent.source,
//             intent.signature,
//             intent.signer
//         );
        
//         // Create legacy data (should be detected as legacy format)
//         bytes memory legacyData = abi.encode(TEST_SYMBOL, uint128(TEST_TIMESTAMP), uint128(TEST_PRICE));
        
//         // Test intent format detection and processing
//         vm.prank(trustedMailbox);
//         receiver.handle(
//             uint32(SOURCE_CHAIN_ID),
//             bytes32(uint256(uint160(address(registry)))),
//             intentData
//         );
        
//         // Test legacy format detection and processing
//         vm.prank(trustedMailbox);
//         receiver.handle(
//             uint32(SOURCE_CHAIN_ID),
//             bytes32(uint256(uint160(address(registry)))),
//             legacyData
//         );
        
//         // Both should result in the same final data (intent timestamp is newer, so it should overwrite)
//         (uint128 timestamp, uint128 value) = receiver.updates(TEST_SYMBOL);
//         assertEq(timestamp, uint128(TEST_TIMESTAMP));
//         assertEq(value, uint128(TEST_PRICE));
//     }
    
//     /**
//      * @dev Test domain separator compatibility between registry and receiver
//      */
//     function testDomainSeparatorCompatibility() public view {
//         bytes32 registryDomainSeparator = registry.getDomainSeparator();
//         bytes32 receiverDomainSeparator = receiver.getDomainSeparator();
        
//         // Domain separators should be the SAME since both use identical domain configuration
//         assertEq(registryDomainSeparator, receiverDomainSeparator);
        
//         // Verify both domain separators match expected value
//         bytes32 expectedDomain = OracleIntentUtils.createDomainSeparator(
//             REGISTRY_DOMAIN_NAME,
//             REGISTRY_DOMAIN_VERSION,
//             block.chainid,
//             address(registry)
//         );
//         assertEq(registryDomainSeparator, expectedDomain);
//         assertEq(receiverDomainSeparator, expectedDomain);
//     }

//     // ===== HELPER FUNCTIONS =====
    
//     /**
//      * @dev Registers an intent in the registry contract and returns the intent hash
//      * @param symbol The symbol for the intent
//      * @param nonce The nonce for the intent
//      * @return intentHash The hash of the registered intent
//      */
//     function registerIntentInRegistry(string memory symbol, uint256 nonce) internal returns (bytes32 intentHash) {
//         // Create intent data for registry domain
//         OracleIntentUtils.OracleIntent memory registryIntent = OracleIntentUtils.OracleIntent({
//             intentType: "OracleUpdate",
//             version: "1.0.0",
//             chainId: block.chainid,
//             nonce: nonce,
//             expiry: block.timestamp + 3600,
//             symbol: symbol,
//             price: TEST_PRICE,
//             timestamp: TEST_TIMESTAMP,
//             source: "DIA",
//             signature: new bytes(65),
//             signer: address(0)
//         });
        
//         // Calculate intent hash for registry domain
//         intentHash = OracleIntentUtils.calculateIntentHash(registryIntent, registry.getDomainSeparator());
        
//         // Sign the intent
//         (uint8 v, bytes32 r, bytes32 s) = vm.sign(oracleSignerPk, intentHash);
//         bytes memory signature = abi.encodePacked(r, s, v);
        
//         // Register the intent in the registry
//         vm.expectEmit(true, true, false, true);
//         emit IntentRegistered(intentHash, symbol, TEST_PRICE, TEST_TIMESTAMP, oracleSigner);
        
//         registry.registerIntent(
//             registryIntent.intentType,
//             registryIntent.version,
//             registryIntent.chainId,
//             registryIntent.nonce,
//             registryIntent.expiry,
//             registryIntent.symbol,
//             registryIntent.price,
//             registryIntent.timestamp,
//             registryIntent.source,
//             signature,
//             oracleSigner
//         );
        
//         // Verify intent was registered
//         assertTrue(registry.processedIntents(intentHash));
//         return intentHash;
//     }
    
//     /**
//      * @dev Returns the exact same intent from registry without any modification
//      * @param registeredIntent The intent retrieved from registry
//      * @return The exact same intent (no modifications needed)
//      */
//     function createReceiverIntentFromRegistry(OracleIntentUtils.OracleIntent memory registeredIntent) 
//         internal 
//         pure 
//         returns (OracleIntentUtils.OracleIntent memory) 
//     {
//         // Return exact same intent - no modifications whatsoever
//         return registeredIntent;
//     }
    
//     function createReceiverIntent() internal view returns (OracleIntentUtils.OracleIntent memory) {
//         return createReceiverIntentWithParams(TEST_SYMBOL, 1);
//     }
    
//     function createReceiverIntentWithParams(string memory symbol, uint256 nonce) internal view returns (OracleIntentUtils.OracleIntent memory) {
//         return OracleIntentUtils.OracleIntent({
//             intentType: "PriceUpdate",
//             version: "1.0.0",
//             chainId: SOURCE_CHAIN_ID,
//             nonce: nonce,
//             expiry: block.timestamp + 3600,
//             symbol: symbol,
//             price: TEST_PRICE,
//             timestamp: TEST_TIMESTAMP,
//             source: "DIA",
//             signature: new bytes(65),
//             signer: address(0)
//         });
//     }
    
//     receive() external payable {}
// }