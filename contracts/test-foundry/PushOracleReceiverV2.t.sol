// // SPDX-License-Identifier: GPL-3.0
// pragma solidity 0.8.29;

// import "forge-std/Test.sol";
// import "../contracts/PushOracleReceiverV2.sol";
// import "../contracts/interfaces/oracle/IPushOracleReceiverV2.sol";
// import "../contracts/interfaces/IInterchainSecurityModule.sol";
// import "../contracts/ProtocolFeeHook.sol";
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
    
//     receive() external payable {
//         // Mock successful fee receipt
//     }
    
//     fallback() external payable {
//         // Mock successful fee receipt
//     }
// }

// contract PushOracleReceiverV2Test is Test {
//     PushOracleReceiverV2 public oracle;
//     MockInterchainSecurityModule public ism;
//     MockProtocolFeeHook public feeHook;
    
//     address public owner;
//     address public trustedMailbox;
//     address public authorizedSigner;
//     address public unauthorizedSigner;
    
//     // Domain configuration
//     string constant DOMAIN_NAME = "OracleIntentRegistry";
//     string constant DOMAIN_VERSION = "1.0.0";
//     uint256 constant SOURCE_CHAIN_ID = 100640;
//     address constant VERIFYING_CONTRACT = address(0x1234567890123456789012345678901234567890);
    
//     // Test data
//     string constant TEST_SYMBOL = "BTC";
//     uint256 constant TEST_PRICE = 50000e18;
//     uint256 constant TEST_TIMESTAMP = 1710000000;
    
//     event IntentBasedUpdateReceived(
//         bytes32 indexed intentHash,
//         string indexed symbol,
//         uint256 price,
//         uint256 timestamp,
//         address indexed signer
//     );
    
//     event ReceivedMessage(string key, uint128 timestamp, uint128 value);
//     event SignerAuthorizationChanged(address indexed signer, bool isAuthorized);

//     function setUp() public {
//         owner = address(this);
//         trustedMailbox = address(0x123);
//         authorizedSigner = address(0x456);
//         unauthorizedSigner = address(0x789);
        
//         // Deploy mocks
//         ism = new MockInterchainSecurityModule();
//         feeHook = new MockProtocolFeeHook(1000);  // Reasonable gas amount for testing
        
//         // Deploy oracle with domain configuration
//         oracle = new PushOracleReceiverV2(
//             DOMAIN_NAME,
//             DOMAIN_VERSION,
//             SOURCE_CHAIN_ID,
//             VERIFYING_CONTRACT
//         );
        
//         // Setup oracle configuration
//         oracle.setInterchainSecurityModule(address(ism));
//         oracle.setPaymentHook(payable(address(feeHook)));
//         oracle.setTrustedMailBox(trustedMailbox);
//         oracle.setSignerAuthorization(authorizedSigner, true);
        
//         // Fund contracts
//         vm.deal(address(oracle), 10 ether);
//         vm.deal(address(feeHook), 1 ether);
//     }

//     // ===== CONSTRUCTOR TESTS =====
    
//     function testConstructorValidation() public {
//         // Test empty domain name
//         vm.expectRevert(abi.encodeWithSignature("InvalidAddress()"));
//         new PushOracleReceiverV2("", DOMAIN_VERSION, SOURCE_CHAIN_ID, VERIFYING_CONTRACT);
        
//         // Test empty domain version
//         vm.expectRevert(abi.encodeWithSignature("InvalidAddress()"));
//         new PushOracleReceiverV2(DOMAIN_NAME, "", SOURCE_CHAIN_ID, VERIFYING_CONTRACT);
        
//         // Test zero chain ID
//         vm.expectRevert(abi.encodeWithSignature("InvalidAddress()"));
//         new PushOracleReceiverV2(DOMAIN_NAME, DOMAIN_VERSION, 0, VERIFYING_CONTRACT);
        
//         // Test zero verifying contract
//         vm.expectRevert(abi.encodeWithSignature("InvalidAddress()"));
//         new PushOracleReceiverV2(DOMAIN_NAME, DOMAIN_VERSION, SOURCE_CHAIN_ID, address(0));
//     }
    
//     function testConstructorSuccess() public {
//         PushOracleReceiverV2 newOracle = new PushOracleReceiverV2(
//             DOMAIN_NAME,
//             DOMAIN_VERSION,
//             SOURCE_CHAIN_ID,
//             VERIFYING_CONTRACT
//         );
        
//         bytes32 expectedDomainSeparator = OracleIntentUtils.createDomainSeparator(
//             DOMAIN_NAME,
//             DOMAIN_VERSION,
//             SOURCE_CHAIN_ID,
//             VERIFYING_CONTRACT
//         );
        
//         assertEq(newOracle.getDomainSeparator(), expectedDomainSeparator);
//     }

    
//     function testHandleIntentUpdateSuccess() public {
//         // Create valid intent
//         OracleIntentUtils.OracleIntent memory intent = createValidIntent();
        
//         // Sign the intent
//         bytes32 intentHash = oracle.calculateIntentHash(intent);
//         (uint8 v, bytes32 r, bytes32 s) = vm.sign(1, intentHash); // Using private key 1
//         intent.signature = abi.encodePacked(r, s, v);
//         intent.signer = vm.addr(1); // Address corresponding to private key 1
        
//         // Authorize the signer
//         oracle.setSignerAuthorization(intent.signer, true);
        
//         // Expect the event
//         vm.expectEmit(true, true, true, true);
//         emit IntentBasedUpdateReceived(intentHash, intent.symbol, intent.price, intent.timestamp, intent.signer);
        
//         // Handle the intent update
//         oracle.handleIntentUpdate(intent);
        
//         // Verify data was updated
//         (uint128 storedTimestamp, uint128 storedValue) = oracle.updates(intent.symbol);
//         assertEq(storedTimestamp, uint128(intent.timestamp));
//         assertEq(storedValue, uint128(intent.price));
        
//         // Verify intent was marked as processed
//         assertTrue(oracle.isProcessedIntent(intentHash));
//     }
    
//     function testHandleIntentUpdateExpired() public {
//         OracleIntentUtils.OracleIntent memory intent = createValidIntent();
//         intent.expiry = block.timestamp - 1; // Expired
        
//         // Sign the intent
//         bytes32 intentHash = oracle.calculateIntentHash(intent);
//         (uint8 v, bytes32 r, bytes32 s) = vm.sign(1, intentHash);
//         intent.signature = abi.encodePacked(r, s, v);
//         intent.signer = vm.addr(1);
        
//         oracle.setSignerAuthorization(intent.signer, true);
        
//         vm.expectRevert(abi.encodeWithSignature("IntentExpired()"));
//         oracle.handleIntentUpdate(intent);
//     }
    
//     function testHandleIntentUpdateUnauthorizedSigner() public {
//         OracleIntentUtils.OracleIntent memory intent = createValidIntent();
        
//         // Sign with unauthorized signer
//         bytes32 intentHash = oracle.calculateIntentHash(intent);
//         (uint8 v, bytes32 r, bytes32 s) = vm.sign(1, intentHash);
//         intent.signature = abi.encodePacked(r, s, v);
//         intent.signer = vm.addr(1); // Not authorized
        
//         vm.expectRevert(abi.encodeWithSignature("UnauthorizedSigner()"));
//         oracle.handleIntentUpdate(intent);
//     }
    
//     function testHandleIntentUpdateAlreadyProcessed() public {
//         OracleIntentUtils.OracleIntent memory intent = createValidIntent();
        
//         // Sign the intent
//         bytes32 intentHash = oracle.calculateIntentHash(intent);
//         (uint8 v, bytes32 r, bytes32 s) = vm.sign(1, intentHash);
//         intent.signature = abi.encodePacked(r, s, v);
//         intent.signer = vm.addr(1);
        
//         oracle.setSignerAuthorization(intent.signer, true);
        
//         // Process intent first time
//         oracle.handleIntentUpdate(intent);
        
//         // Try to process again
//         vm.expectRevert(abi.encodeWithSignature("IntentAlreadyProcessed()"));
//         oracle.handleIntentUpdate(intent);
//     }
    
//     function testHandleIntentUpdateInvalidSignature() public {
//         OracleIntentUtils.OracleIntent memory intent = createValidIntent();
        
//         // Create invalid signature
//         intent.signature = abi.encodePacked(bytes32(0), bytes32(0), uint8(27));
//         intent.signer = vm.addr(1);
        
//         oracle.setSignerAuthorization(intent.signer, true);
        
//         vm.expectRevert(abi.encodeWithSignature("InvalidSignature()"));
//         oracle.handleIntentUpdate(intent);
//     }

//     // ===== BATCH INTENT HANDLING TESTS =====
    
//     function testHandleBatchIntentUpdatesSuccess() public {
//         // Create multiple valid intents
//         OracleIntentUtils.OracleIntent[] memory intents = new OracleIntentUtils.OracleIntent[](3);
        
//         for (uint i = 0; i < 3; i++) {
//             intents[i] = createValidIntent();
//             intents[i].symbol = string(abi.encodePacked("TOKEN", vm.toString(i)));
//             intents[i].nonce = i + 1;
            
//             // Sign each intent
//             bytes32 intentHash = oracle.calculateIntentHash(intents[i]);
//             (uint8 v, bytes32 r, bytes32 s) = vm.sign(1, intentHash);
//             intents[i].signature = abi.encodePacked(r, s, v);
//             intents[i].signer = vm.addr(1);
//         }
        
//         oracle.setSignerAuthorization(vm.addr(1), true);
        
//         // Handle batch update
//         oracle.handleBatchIntentUpdates(intents);
        
//         // Verify all intents were processed
//         for (uint i = 0; i < 3; i++) {
//             bytes32 intentHash = oracle.calculateIntentHash(intents[i]);
//             assertTrue(oracle.isProcessedIntent(intentHash));
            
//             (uint128 storedTimestamp, uint128 storedValue) = oracle.updates(intents[i].symbol);
//             assertEq(storedTimestamp, uint128(intents[i].timestamp));
//             assertEq(storedValue, uint128(intents[i].price));
//         }
//     }
    
//     function testHandleBatchIntentUpdatesPartialFailure() public {
//         OracleIntentUtils.OracleIntent[] memory intents = new OracleIntentUtils.OracleIntent[](3);
        
//         // First intent: valid
//         intents[0] = createValidIntent();
//         intents[0].symbol = "TOKEN0";
//         bytes32 hash0 = oracle.calculateIntentHash(intents[0]);
//         (uint8 v0, bytes32 r0, bytes32 s0) = vm.sign(1, hash0);
//         intents[0].signature = abi.encodePacked(r0, s0, v0);
//         intents[0].signer = vm.addr(1);
        
//         // Second intent: expired
//         intents[1] = createValidIntent();
//         intents[1].symbol = "TOKEN1";
//         intents[1].expiry = block.timestamp - 1;
//         intents[1].nonce = 2;
//         bytes32 hash1 = oracle.calculateIntentHash(intents[1]);
//         (uint8 v1, bytes32 r1, bytes32 s1) = vm.sign(1, hash1);
//         intents[1].signature = abi.encodePacked(r1, s1, v1);
//         intents[1].signer = vm.addr(1);
        
//         // Third intent: valid
//         intents[2] = createValidIntent();
//         intents[2].symbol = "TOKEN2";
//         intents[2].nonce = 3;
//         bytes32 hash2 = oracle.calculateIntentHash(intents[2]);
//         (uint8 v2, bytes32 r2, bytes32 s2) = vm.sign(1, hash2);
//         intents[2].signature = abi.encodePacked(r2, s2, v2);
//         intents[2].signer = vm.addr(1);
        
//         oracle.setSignerAuthorization(vm.addr(1), true);
        
//         // Should not revert, but only process valid intents
//         oracle.handleBatchIntentUpdates(intents);
        
//         // Check that valid intents were processed
//         assertTrue(oracle.isProcessedIntent(hash0));
//         assertFalse(oracle.isProcessedIntent(hash1)); // Expired
//         assertTrue(oracle.isProcessedIntent(hash2));
//     }

     
//     function testHandleISMValidatedMessage() public {
//         bytes memory legacyData = abi.encode(TEST_SYMBOL, uint128(TEST_TIMESTAMP), uint128(TEST_PRICE));
        
//         vm.expectEmit(true, true, true, true);
//         emit ReceivedMessage(TEST_SYMBOL, uint128(TEST_TIMESTAMP), uint128(TEST_PRICE));
        
//         vm.prank(trustedMailbox);
//         oracle.handle(1, bytes32(uint256(uint160(address(0x123)))), legacyData);
        
//         // Verify data was updated
//         (uint128 storedTimestamp, uint128 storedValue) = oracle.updates(TEST_SYMBOL);
//         assertEq(storedTimestamp, uint128(TEST_TIMESTAMP));
//         assertEq(storedValue, uint128(TEST_PRICE));
//     }
    
//     function testHandleUnauthorizedMailbox() public {
//         bytes memory legacyData = abi.encode(TEST_SYMBOL, uint128(TEST_TIMESTAMP), uint128(TEST_PRICE));
        
//         vm.expectRevert(abi.encodeWithSignature("UnauthorizedMailbox()"));
//         oracle.handle(1, bytes32(uint256(uint160(address(0x123)))), legacyData);
//     }
    
//     function testHandleInvalidISM() public {
//         // Deploy oracle without ISM
//         PushOracleReceiverV2 oracleNoISM = new PushOracleReceiverV2(
//             DOMAIN_NAME,
//             DOMAIN_VERSION,
//             SOURCE_CHAIN_ID,
//             VERIFYING_CONTRACT
//         );
//         oracleNoISM.setPaymentHook(payable(address(feeHook)));
//         oracleNoISM.setTrustedMailBox(trustedMailbox);
//         vm.deal(address(oracleNoISM), 1 ether);
        
//         bytes memory legacyData = abi.encode(TEST_SYMBOL, uint128(TEST_TIMESTAMP), uint128(TEST_PRICE));
        
//         vm.prank(trustedMailbox);
//         vm.expectRevert(abi.encodeWithSignature("InvalidISMAddress()"));
//         oracleNoISM.handle(1, bytes32(uint256(uint160(address(0x123)))), legacyData);
//     }

     
//     function testSetSignerAuthorization() public {
//         address newSigner = address(0xABC);
        
//         vm.expectEmit(true, true, false, true);
//         emit SignerAuthorizationChanged(newSigner, true);
        
//         oracle.setSignerAuthorization(newSigner, true);
//         assertTrue(oracle.isAuthorizedSigner(newSigner));
        
//         // Test deauthorization
//         vm.expectEmit(true, true, false, true);
//         emit SignerAuthorizationChanged(newSigner, false);
        
//         oracle.setSignerAuthorization(newSigner, false);
//         assertFalse(oracle.isAuthorizedSigner(newSigner));
//     }
    
//     function testSetSignerAuthorizationZeroAddress() public {
//         vm.expectRevert(abi.encodeWithSignature("InvalidAddress()"));
//         oracle.setSignerAuthorization(address(0), true);
//     }
    
//     function testOnlyOwnerModifiers() public {
//         address nonOwner = address(0x999);
        
//         vm.prank(nonOwner);
//         vm.expectRevert("Ownable: caller is not the owner");
//         oracle.setSignerAuthorization(address(0x123), true);
        
//         vm.prank(nonOwner);
//         vm.expectRevert("Ownable: caller is not the owner");
//         oracle.setInterchainSecurityModule(address(0x123));
        
//         vm.prank(nonOwner);
//         vm.expectRevert("Ownable: caller is not the owner");
//         oracle.setPaymentHook(payable(address(0x123)));
        
//         vm.prank(nonOwner);
//         vm.expectRevert("Ownable: caller is not the owner");
//         oracle.setTrustedMailBox(address(0x123));
//     }
    
//     function testCalculateIntentHash() public {
//         OracleIntentUtils.OracleIntent memory intent = createValidIntent();
        
//         bytes32 hash1 = oracle.calculateIntentHash(intent);
//         bytes32 hash2 = OracleIntentUtils.calculateIntentHash(
//             OracleIntentUtils.OracleIntent({
//                 intentType: intent.intentType,
//                 version: intent.version,
//                 chainId: intent.chainId,
//                 nonce: intent.nonce,
//                 expiry: intent.expiry,
//                 symbol: intent.symbol,
//                 price: intent.price,
//                 timestamp: intent.timestamp,
//                 source: intent.source,
//                 signature: intent.signature,
//                 signer: intent.signer
//             }),
//             oracle.getDomainSeparator()
//         );
        
//         assertEq(hash1, hash2);
//     }
    
//     function testFormatDetection() public view {
//         // Test with intent format data
//         OracleIntentUtils.OracleIntent memory intent = createValidIntent();
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
        
//         // Test with legacy format data
//         bytes memory legacyData = abi.encode(TEST_SYMBOL, uint128(TEST_TIMESTAMP), uint128(TEST_PRICE));
        
//         // Verify format detection logic using length checks (since library function requires calldata)
//         assert(intentData.length >= 200);  // Intent format should be large
//         assert(legacyData.length < 200);   // Legacy format should be smaller
//     }
    
//     function testStaleDataIgnored() public {
//         // First, add recent data
//         OracleIntentUtils.OracleIntent memory recentIntent = createValidIntent();
//         recentIntent.timestamp = TEST_TIMESTAMP;
        
//         bytes32 intentHash = oracle.calculateIntentHash(recentIntent);
//         (uint8 v, bytes32 r, bytes32 s) = vm.sign(1, intentHash);
//         recentIntent.signature = abi.encodePacked(r, s, v);
//         recentIntent.signer = vm.addr(1);
        
//         oracle.setSignerAuthorization(recentIntent.signer, true);
//         oracle.handleIntentUpdate{value: 0.01 ether}(recentIntent);
        
//         // Now try to add older data
//         OracleIntentUtils.OracleIntent memory staleIntent = createValidIntent();
//         staleIntent.timestamp = TEST_TIMESTAMP - 1000; // Older timestamp
//         staleIntent.nonce = 999;
        
//         bytes32 staleHash = oracle.calculateIntentHash(staleIntent);
//         (uint8 v2, bytes32 r2, bytes32 s2) = vm.sign(1, staleHash);
//         staleIntent.signature = abi.encodePacked(r2, s2, v2);
//         staleIntent.signer = vm.addr(1);
        
//         // Should not revert, but data shouldn't be updated
//         oracle.handleIntentUpdate{value: 0.01 ether}(staleIntent);
        
//         // Verify the recent data is still there
//         (uint128 storedTimestamp, uint128 storedValue) = oracle.updates(TEST_SYMBOL);
//         assertEq(storedTimestamp, uint128(TEST_TIMESTAMP));
//         assertEq(storedValue, uint128(TEST_PRICE));
//     }
    
//     function testRetrieveLostTokens() public {
//         address recipient = address(0x999);
//         uint256 initialBalance = address(oracle).balance;
        
//         oracle.retrieveLostTokens(recipient);
        
//         assertEq(address(oracle).balance, 0);
//         assertEq(recipient.balance, initialBalance);
//     }

    
//     function createValidIntent() internal view returns (OracleIntentUtils.OracleIntent memory) {
//         return OracleIntentUtils.OracleIntent({
//             intentType: "PriceUpdate",
//             version: "1.0.0",
//             chainId: SOURCE_CHAIN_ID,
//             nonce: 1,
//             expiry: block.timestamp + 3600, // 1 hour from now
//             symbol: TEST_SYMBOL,
//             price: TEST_PRICE,
//             timestamp: TEST_TIMESTAMP,
//             source: "DIA",
//             signature: new bytes(65), // Will be filled by signing
//             signer: address(0) // Will be set after signing
//         });
//     }
    
//     receive() external payable {}
// }