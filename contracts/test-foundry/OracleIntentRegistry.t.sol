// SPDX-License-Identifier: MIT
pragma solidity ^0.8.29;

import "forge-std/Test.sol";
import "../contracts/OracleIntentRegistry.sol";
import "../contracts/libs/OracleIntentUtils.sol";

/**
 * @title OracleIntentRegistryTest
 * @dev Comprehensive unit tests for OracleIntentRegistry contract
 */
contract OracleIntentRegistryTest is Test {
    OracleIntentRegistry public registry;
    
    address public owner;
    address public user1;
    address public user2;
    address public signer1;
    address public signer2;
    uint256 public signer1Pk;
    uint256 public signer2Pk;
    
    // Test constants
    string constant DOMAIN_NAME = "DIA Oracle Intent";
    string constant DOMAIN_VERSION = "1";
    string constant TEST_SYMBOL = "BTC";
    string constant TEST_SYMBOL_2 = "ETH";
    uint256 constant TEST_PRICE = 50000e18;
    uint256 constant TEST_PRICE_2 = 3000e18;
    uint256 constant TEST_TIMESTAMP = 1710000000;
    uint256 constant TEST_NONCE = 1;
    string constant TEST_SOURCE = "DIA";
    
    // Events
    event IntentRegistered(bytes32 indexed intentHash, string indexed symbol, uint256 indexed price, uint256 timestamp, address signer);
    event SignerAuthorized(address indexed signer, bool indexed status);
    event BatchIntentsRegistered(uint256 indexed count);
    
    function setUp() public {
        owner = address(this);
        user1 = address(0x1);
        user2 = address(0x2);
        signer1Pk = 1;
        signer2Pk = 2;
        signer1 = vm.addr(signer1Pk);
        signer2 = vm.addr(signer2Pk);
        
        // Deploy registry
        registry = new OracleIntentRegistry();
    }
    
     
    function testConstructorInitialization() public view {
         assertEq(registry.owner(), owner);
        
         assertTrue(registry.authorizedSigners(owner));
        
         bytes32 expectedDomain = OracleIntentUtils.createDomainSeparator(
            DOMAIN_NAME,
            DOMAIN_VERSION,
            block.chainid,
            address(registry)
        );
        assertEq(registry.getDomainSeparator(), expectedDomain);
    }
    
    // ===== ACCESS CONTROL TESTS =====
    
    function testOnlyOwnerModifier() public {
        // Test setSignerAuthorization with non-owner should fail
        vm.prank(user1);
        vm.expectRevert(OracleIntentRegistry.NotOwner.selector);
        registry.setSignerAuthorization(signer1, true);
        
        // Test transferOwnership with non-owner should fail
        vm.prank(user1);
        vm.expectRevert(OracleIntentRegistry.NotOwner.selector);
        registry.transferOwnership(user2);
    }
    
    function testOwnerCanCallRestrictedFunctions() public {
        // Owner should be able to authorize signers
        registry.setSignerAuthorization(signer1, true);
        assertTrue(registry.authorizedSigners(signer1));
        
        // Owner should be able to transfer ownership
        registry.transferOwnership(user1);
        assertEq(registry.owner(), user1);
    }
    
    // ===== SIGNER AUTHORIZATION TESTS =====
    
    function testSetSignerAuthorization() public {
        // Initially signer1 should not be authorized
        assertFalse(registry.authorizedSigners(signer1));
        
        // Authorize signer1
        vm.expectEmit(true, true, false, false);
        emit SignerAuthorized(signer1, true);
        registry.setSignerAuthorization(signer1, true);
        assertTrue(registry.authorizedSigners(signer1));
        
        // Deauthorize signer1
        vm.expectEmit(true, true, false, false);
        emit SignerAuthorized(signer1, false);
        registry.setSignerAuthorization(signer1, false);
        assertFalse(registry.authorizedSigners(signer1));
    }
    
    // ===== OWNERSHIP TRANSFER TESTS =====
    
    function testTransferOwnership() public {
        assertEq(registry.owner(), owner);
        
        // Expect the OwnershipTransferred event
        vm.expectEmit(true, true, false, false);
        emit OracleIntentRegistry.OwnershipTransferred(owner, user1);
        
        registry.transferOwnership(user1);
        assertEq(registry.owner(), user1);
    }
    
    function testTransferOwnershipToZeroAddress() public {
        vm.expectRevert(OracleIntentRegistry.ZeroAddress.selector);
        registry.transferOwnership(address(0));
    }
    
    // ===== DOMAIN SEPARATOR TESTS =====
    
    function testGetDomainSeparator() public view {
        bytes32 domainSeparator = registry.getDomainSeparator();
        bytes32 expectedDomain = OracleIntentUtils.createDomainSeparator(
            DOMAIN_NAME,
            DOMAIN_VERSION,
            block.chainid,
            address(registry)
        );
        assertEq(domainSeparator, expectedDomain);
    }
    
    // ===== SINGLE INTENT REGISTRATION TESTS =====
    
    function testRegisterIntentSuccess() public {
        // Authorize signer
        registry.setSignerAuthorization(signer1, true);
        
        // Create and register intent
        OracleIntentUtils.OracleIntent memory intent = createTestIntent(TEST_SYMBOL, TEST_NONCE);
        bytes32 intentHash = OracleIntentUtils.calculateIntentHash(intent, registry.getDomainSeparator());
        (uint8 v, bytes32 r, bytes32 s) = vm.sign(signer1Pk, intentHash);
        bytes memory signature = abi.encodePacked(r, s, v);
        
        vm.expectEmit(true, true, true, true);
        emit IntentRegistered(intentHash, TEST_SYMBOL, TEST_PRICE, TEST_TIMESTAMP, signer1);
        
        registry.registerIntent(
            intent.intentType,
            intent.version,
            intent.chainId,
            intent.nonce,
            intent.expiry,
            intent.symbol,
            intent.price,
            intent.timestamp,
            intent.source,
            signature,
            signer1
        );
        
        // Verify intent was stored
        assertTrue(registry.processedIntents(intentHash));
        assertEq(registry.latestIntentBySymbol(TEST_SYMBOL), intentHash);
        
        // Verify intent data
        OracleIntentUtils.OracleIntent memory storedIntent = registry.getIntent(intentHash);
        assertEq(storedIntent.symbol, TEST_SYMBOL);
        assertEq(storedIntent.price, TEST_PRICE);
        assertEq(storedIntent.timestamp, TEST_TIMESTAMP);
        assertEq(storedIntent.signer, signer1);
    }
    
    function testRegisterIntentWithExpiredIntent() public {
        registry.setSignerAuthorization(signer1, true);
        
        // Create intent with expiry in the past
        OracleIntentUtils.OracleIntent memory intent = createTestIntent(TEST_SYMBOL, TEST_NONCE);
        intent.expiry = block.timestamp - 1;
        
        bytes32 intentHash = OracleIntentUtils.calculateIntentHash(intent, registry.getDomainSeparator());
        (uint8 v, bytes32 r, bytes32 s) = vm.sign(signer1Pk, intentHash);
        bytes memory signature = abi.encodePacked(r, s, v);
        
        vm.expectRevert(OracleIntentRegistry.IntentExpired.selector);
        registry.registerIntent(
            intent.intentType,
            intent.version,
            intent.chainId,
            intent.nonce,
            intent.expiry,
            intent.symbol,
            intent.price,
            intent.timestamp,
            intent.source,
            signature,
            signer1
        );
    }
    
    function testRegisterIntentWithUnauthorizedSigner() public {
        // Don't authorize signer1
        OracleIntentUtils.OracleIntent memory intent = createTestIntent(TEST_SYMBOL, TEST_NONCE);
        bytes32 intentHash = OracleIntentUtils.calculateIntentHash(intent, registry.getDomainSeparator());
        (uint8 v, bytes32 r, bytes32 s) = vm.sign(signer1Pk, intentHash);
        bytes memory signature = abi.encodePacked(r, s, v);
        
        vm.expectRevert(OracleIntentRegistry.SignerNotAuthorized.selector);
        registry.registerIntent(
            intent.intentType,
            intent.version,
            intent.chainId,
            intent.nonce,
            intent.expiry,
            intent.symbol,
            intent.price,
            intent.timestamp,
            intent.source,
            signature,
            signer1
        );
    }
    
    function testRegisterIntentWithInvalidSignature() public {
        registry.setSignerAuthorization(signer1, true);
        
        OracleIntentUtils.OracleIntent memory intent = createTestIntent(TEST_SYMBOL, TEST_NONCE);
        bytes32 intentHash = OracleIntentUtils.calculateIntentHash(intent, registry.getDomainSeparator());
        
        // Sign with wrong private key
        (uint8 v, bytes32 r, bytes32 s) = vm.sign(signer2Pk, intentHash);
        bytes memory signature = abi.encodePacked(r, s, v);
        
        vm.expectRevert(OracleIntentRegistry.InvalidSignature.selector);
        registry.registerIntent(
            intent.intentType,
            intent.version,
            intent.chainId,
            intent.nonce,
            intent.expiry,
            intent.symbol,
            intent.price,
            intent.timestamp,
            intent.source,
            signature,
            signer1
        );
    }
    
    function testRegisterIntentAlreadyProcessed() public {
        registry.setSignerAuthorization(signer1, true);
        
        // Register intent first time
        OracleIntentUtils.OracleIntent memory intent = createTestIntent(TEST_SYMBOL, TEST_NONCE);
        bytes32 intentHash = OracleIntentUtils.calculateIntentHash(intent, registry.getDomainSeparator());
        (uint8 v, bytes32 r, bytes32 s) = vm.sign(signer1Pk, intentHash);
        bytes memory signature = abi.encodePacked(r, s, v);
        
        registry.registerIntent(
            intent.intentType,
            intent.version,
            intent.chainId,
            intent.nonce,
            intent.expiry,
            intent.symbol,
            intent.price,
            intent.timestamp,
            intent.source,
            signature,
            signer1
        );
        
        // Try to register same intent again
        vm.expectRevert(OracleIntentRegistry.IntentAlreadyProcessed.selector);
        registry.registerIntent(
            intent.intentType,
            intent.version,
            intent.chainId,
            intent.nonce,
            intent.expiry,
            intent.symbol,
            intent.price,
            intent.timestamp,
            intent.source,
            signature,
            signer1
        );
    }
    
    function testRegisterMultipleIntentsForSameSymbol() public {
        registry.setSignerAuthorization(signer1, true);
        
        // Register first intent with older timestamp
        OracleIntentUtils.OracleIntent memory intent1 = createTestIntent(TEST_SYMBOL, 1);
        intent1.timestamp = TEST_TIMESTAMP - 1000;
        registerValidIntent(intent1, signer1Pk, signer1);
        
        // Register second intent with newer timestamp
        OracleIntentUtils.OracleIntent memory intent2 = createTestIntent(TEST_SYMBOL, 2);
        intent2.timestamp = TEST_TIMESTAMP;
        bytes32 intentHash2 = registerValidIntent(intent2, signer1Pk, signer1);
        
        // Latest intent should be the newer one
        assertEq(registry.latestIntentBySymbol(TEST_SYMBOL), intentHash2);
        
        // Register third intent with even older timestamp
        OracleIntentUtils.OracleIntent memory intent3 = createTestIntent(TEST_SYMBOL, 3);
        intent3.timestamp = TEST_TIMESTAMP - 2000;
        registerValidIntent(intent3, signer1Pk, signer1);
        
        // Latest should still be the newest timestamp (intent2)
        assertEq(registry.latestIntentBySymbol(TEST_SYMBOL), intentHash2);
    }
    
    // ===== BATCH INTENT REGISTRATION TESTS =====
    
    function testRegisterMultipleIntentsSuccess() public {
        registry.setSignerAuthorization(signer1, true);
        
        uint256 batchSize = 3;
        OracleIntentUtils.OracleIntent[] memory intents = new OracleIntentUtils.OracleIntent[](batchSize);
        
        for (uint256 i = 0; i < batchSize; i++) {
            string memory symbol = string(abi.encodePacked("TOKEN", vm.toString(i)));
            OracleIntentUtils.OracleIntent memory intent = createTestIntent(symbol, i + 1);
            
            bytes32 intentHash = OracleIntentUtils.calculateIntentHash(intent, registry.getDomainSeparator());
            (uint8 v, bytes32 r, bytes32 s) = vm.sign(signer1Pk, intentHash);
            intent.signature = abi.encodePacked(r, s, v);
            intent.signer = signer1;
            
            intents[i] = intent;
        }
        
        vm.expectEmit(true, false, false, false);
        emit BatchIntentsRegistered(batchSize);
        
        registry.registerMultipleIntents(intents);
        
        // Verify all intents were processed
        for (uint256 i = 0; i < batchSize; i++) {
            string memory symbol = string(abi.encodePacked("TOKEN", vm.toString(i)));
            bytes32 latestHash = registry.latestIntentBySymbol(symbol);
            assertTrue(latestHash != bytes32(0));
            assertTrue(registry.processedIntents(latestHash));
        }
    }
    
    function testRegisterMultipleIntentsEmpty() public {
        OracleIntentUtils.OracleIntent[] memory emptyIntents = new OracleIntentUtils.OracleIntent[](0);
        
        vm.expectRevert(OracleIntentRegistry.IntentNotFound.selector);
        registry.registerMultipleIntents(emptyIntents);
    }
    
    function testRegisterMultipleIntentsPartialSuccess() public {
        registry.setSignerAuthorization(signer1, true);
        
        OracleIntentUtils.OracleIntent[] memory intents = new OracleIntentUtils.OracleIntent[](3);
        
        // Valid intent
        intents[0] = createSignedIntent(createTestIntent("TOKEN0", 1), signer1Pk, signer1);
        
        // Expired intent (should be skipped)
        intents[1] = createTestIntent("TOKEN1", 2);
        intents[1].expiry = block.timestamp - 1;
        intents[1] = createSignedIntent(intents[1], signer1Pk, signer1);
        
        // Valid intent
        intents[2] = createSignedIntent(createTestIntent("TOKEN2", 3), signer1Pk, signer1);
        
        vm.expectEmit(true, false, false, false);
        emit BatchIntentsRegistered(2); // Only 2 valid intents
        
        registry.registerMultipleIntents(intents);
        
        // Check which intents were processed
        assertTrue(registry.latestIntentBySymbol("TOKEN0") != bytes32(0));
        assertTrue(registry.latestIntentBySymbol("TOKEN1") == bytes32(0)); // Should be empty
        assertTrue(registry.latestIntentBySymbol("TOKEN2") != bytes32(0));
    }
    
    function testRegisterMultipleIntentsAllInvalid() public {
        // Don't authorize any signers
        OracleIntentUtils.OracleIntent[] memory intents = new OracleIntentUtils.OracleIntent[](2);
        intents[0] = createSignedIntent(createTestIntent("TOKEN0", 1), signer1Pk, signer1);
        intents[1] = createSignedIntent(createTestIntent("TOKEN1", 2), signer1Pk, signer1);
        
        vm.expectEmit(true, false, false, false);
        emit BatchIntentsRegistered(0); // No valid intents
        
        registry.registerMultipleIntents(intents);
    }
    
    function testRegisterMultipleIntentsWithAlreadyProcessed() public {
        registry.setSignerAuthorization(signer1, true);
        
        // Create and register first intent
        OracleIntentUtils.OracleIntent memory intent1 = createTestIntent("TOKEN0", 1);
        bytes32 intentHash1 = registerValidIntent(intent1, signer1Pk, signer1);
        
        // Create batch with same intent (already processed) and a new one
        OracleIntentUtils.OracleIntent[] memory intents = new OracleIntentUtils.OracleIntent[](2);
        intents[0] = createSignedIntent(intent1, signer1Pk, signer1); // Already processed
        intents[1] = createSignedIntent(createTestIntent("TOKEN1", 2), signer1Pk, signer1); // New intent
        
        vm.expectEmit(true, false, false, false);
        emit BatchIntentsRegistered(1); // Only 1 new intent processed
        
        registry.registerMultipleIntents(intents);
        
        // Verify the already processed intent is still there
        assertTrue(registry.processedIntents(intentHash1));
        // Verify the new intent was processed  
        assertTrue(registry.latestIntentBySymbol("TOKEN1") != bytes32(0));
    }
    
    function testRegisterMultipleIntentsWithInvalidSignatures() public {
        registry.setSignerAuthorization(signer1, true);
        
        // Create batch with invalid signatures and valid ones
        OracleIntentUtils.OracleIntent[] memory intents = new OracleIntentUtils.OracleIntent[](3);
        
        // Valid intent
        intents[0] = createSignedIntent(createTestIntent("TOKEN0", 1), signer1Pk, signer1);
        
        // Invalid signature (signed with wrong key but claiming signer1)
        OracleIntentUtils.OracleIntent memory invalidIntent = createTestIntent("TOKEN1", 2);
        bytes32 invalidHash = OracleIntentUtils.calculateIntentHash(invalidIntent, registry.getDomainSeparator());
        (uint8 v, bytes32 r, bytes32 s) = vm.sign(signer2Pk, invalidHash); // Wrong key
        invalidIntent.signature = abi.encodePacked(r, s, v);
        invalidIntent.signer = signer1; // But claiming to be signer1
        intents[1] = invalidIntent;
        
        // Another valid intent
        intents[2] = createSignedIntent(createTestIntent("TOKEN2", 3), signer1Pk, signer1);
        
        vm.expectEmit(true, false, false, false);
        emit BatchIntentsRegistered(2); // Only 2 valid intents
        
        registry.registerMultipleIntents(intents);
        
        // Verify only valid intents were processed
        assertTrue(registry.latestIntentBySymbol("TOKEN0") != bytes32(0));
        assertTrue(registry.latestIntentBySymbol("TOKEN1") == bytes32(0)); // Invalid signature skipped
        assertTrue(registry.latestIntentBySymbol("TOKEN2") != bytes32(0));
    }
    
    // ===== INTENT RETRIEVAL TESTS =====
    
    function testGetLatestPrice() public {
        registry.setSignerAuthorization(signer1, true);
        
        // Register intent
        OracleIntentUtils.OracleIntent memory intent = createTestIntent(TEST_SYMBOL, TEST_NONCE);
        registerValidIntent(intent, signer1Pk, signer1);
        
        // Get latest price
        (uint256 price, uint256 timestamp, string memory source) = registry.getLatestPrice(TEST_SYMBOL);
        assertEq(price, TEST_PRICE);
        assertEq(timestamp, TEST_TIMESTAMP);
        assertEq(source, TEST_SOURCE);
    }
    
    function testGetLatestPriceNoIntent() public {
        vm.expectRevert(OracleIntentRegistry.NoIntentForSymbol.selector);
        registry.getLatestPrice("NONEXISTENT");
    }
    
    function testGetIntent() public {
        registry.setSignerAuthorization(signer1, true);
        
        // Register intent
        OracleIntentUtils.OracleIntent memory intent = createTestIntent(TEST_SYMBOL, TEST_NONCE);
        bytes32 intentHash = registerValidIntent(intent, signer1Pk, signer1);
        
        // Get intent
        OracleIntentUtils.OracleIntent memory retrievedIntent = registry.getIntent(intentHash);
        assertEq(retrievedIntent.symbol, TEST_SYMBOL);
        assertEq(retrievedIntent.price, TEST_PRICE);
        assertEq(retrievedIntent.timestamp, TEST_TIMESTAMP);
        assertEq(retrievedIntent.signer, signer1);
    }
    
    function testGetIntentNotFound() public {
        bytes32 nonExistentHash = keccak256("nonexistent");
        vm.expectRevert(OracleIntentRegistry.IntentNotFound.selector);
        registry.getIntent(nonExistentHash);
    }
    
    // ===== EDGE CASES AND COMPLEX SCENARIOS =====
    
    function testMultipleSignersForDifferentSymbols() public {
        registry.setSignerAuthorization(signer1, true);
        registry.setSignerAuthorization(signer2, true);
        
        // Register intent with signer1 for BTC
        OracleIntentUtils.OracleIntent memory btcIntent = createTestIntent("BTC", 1);
        bytes32 btcHash = registerValidIntent(btcIntent, signer1Pk, signer1);
        
        // Register intent with signer2 for ETH
        OracleIntentUtils.OracleIntent memory ethIntent = createTestIntent("ETH", 2);
        ethIntent.price = TEST_PRICE_2;
        bytes32 ethHash = registerValidIntent(ethIntent, signer2Pk, signer2);
        
        // Verify both intents are stored correctly
        assertEq(registry.latestIntentBySymbol("BTC"), btcHash);
        assertEq(registry.latestIntentBySymbol("ETH"), ethHash);
        
        (uint256 btcPrice,,) = registry.getLatestPrice("BTC");
        (uint256 ethPrice,,) = registry.getLatestPrice("ETH");
        
        assertEq(btcPrice, TEST_PRICE);
        assertEq(ethPrice, TEST_PRICE_2);
    }
    
    function testTimestampBasedLatestIntentUpdate() public {
        registry.setSignerAuthorization(signer1, true);
        
        uint256 baseTimestamp = 1710000000;
        
        // Register intent with timestamp 3
        OracleIntentUtils.OracleIntent memory intent3 = createTestIntent(TEST_SYMBOL, 3);
        intent3.timestamp = baseTimestamp + 300;
        intent3.price = 300;
        bytes32 hash3 = registerValidIntent(intent3, signer1Pk, signer1);
        
        // Register intent with timestamp 1 (older)
        OracleIntentUtils.OracleIntent memory intent1 = createTestIntent(TEST_SYMBOL, 1);
        intent1.timestamp = baseTimestamp + 100;
        intent1.price = 100;
        registerValidIntent(intent1, signer1Pk, signer1);
        
        // Register intent with timestamp 2 (middle)
        OracleIntentUtils.OracleIntent memory intent2 = createTestIntent(TEST_SYMBOL, 2);
        intent2.timestamp = baseTimestamp + 200;
        intent2.price = 200;
        registerValidIntent(intent2, signer1Pk, signer1);
        
        // Latest should still be the one with highest timestamp (intent3)
        assertEq(registry.latestIntentBySymbol(TEST_SYMBOL), hash3);
        (uint256 latestPrice,,) = registry.getLatestPrice(TEST_SYMBOL);
        assertEq(latestPrice, 300);
    }
    
    function testRegisterIntentOlderThanExisting() public {
        registry.setSignerAuthorization(signer1, true);
        
        // Register newer intent first
        OracleIntentUtils.OracleIntent memory newerIntent = createTestIntent(TEST_SYMBOL, 1);
        newerIntent.timestamp = TEST_TIMESTAMP + 1000;
        newerIntent.price = 60000e18;
        bytes32 newerHash = registerValidIntent(newerIntent, signer1Pk, signer1);
        
        // Verify it's the latest
        assertEq(registry.latestIntentBySymbol(TEST_SYMBOL), newerHash);
        
        // Register older intent (should not become latest)
        OracleIntentUtils.OracleIntent memory olderIntent = createTestIntent(TEST_SYMBOL, 2);
        olderIntent.timestamp = TEST_TIMESTAMP; // Older timestamp
        olderIntent.price = 40000e18;
        bytes32 olderHash = registerValidIntent(olderIntent, signer1Pk, signer1);
        
        // Latest should still be the newer one (tests the else branch)
        assertEq(registry.latestIntentBySymbol(TEST_SYMBOL), newerHash);
        
        // But both intents should be stored
        assertTrue(registry.processedIntents(newerHash));
        assertTrue(registry.processedIntents(olderHash));
        
        // Latest price should be from newer intent
        (uint256 latestPrice,,) = registry.getLatestPrice(TEST_SYMBOL);
        assertEq(latestPrice, 60000e18);
    }
    
    // ===== HELPER FUNCTIONS =====
    
    function createTestIntent(string memory symbol, uint256 nonce) internal view returns (OracleIntentUtils.OracleIntent memory) {
        return OracleIntentUtils.OracleIntent({
            intentType: "OracleUpdate",
            version: "1.0.0",
            chainId: block.chainid,
            nonce: nonce,
            expiry: block.timestamp + 3600,
            symbol: symbol,
            price: TEST_PRICE,
            timestamp: TEST_TIMESTAMP,
            source: TEST_SOURCE,
            signature: new bytes(65),
            signer: address(0)
        });
    }
    
    function createSignedIntent(
        OracleIntentUtils.OracleIntent memory intent,
        uint256 signerPk,
        address signerAddr
    ) internal view returns (OracleIntentUtils.OracleIntent memory) {
        bytes32 intentHash = OracleIntentUtils.calculateIntentHash(intent, registry.getDomainSeparator());
        (uint8 v, bytes32 r, bytes32 s) = vm.sign(signerPk, intentHash);
        intent.signature = abi.encodePacked(r, s, v);
        intent.signer = signerAddr;
        return intent;
    }
    
    function registerValidIntent(
        OracleIntentUtils.OracleIntent memory intent,
        uint256 signerPk,
        address signerAddr
    ) internal returns (bytes32 intentHash) {
        intentHash = OracleIntentUtils.calculateIntentHash(intent, registry.getDomainSeparator());
        (uint8 v, bytes32 r, bytes32 s) = vm.sign(signerPk, intentHash);
        bytes memory signature = abi.encodePacked(r, s, v);
        
        registry.registerIntent(
            intent.intentType,
            intent.version,
            intent.chainId,
            intent.nonce,
            intent.expiry,
            intent.symbol,
            intent.price,
            intent.timestamp,
            intent.source,
            signature,
            signerAddr
        );
        
        return intentHash;
    }
}