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
    event IntentRejected(bytes32 indexed intentHash, string indexed symbol, address indexed signer, OracleIntentRegistry.RejectionReason reason);
    
    function setUp() public {
        owner = address(this);
        user1 = address(0x1);
        user2 = address(0x2);
        signer1Pk = 1;
        signer2Pk = 2;
        signer1 = vm.addr(signer1Pk);
        signer2 = vm.addr(signer2Pk);
        
        // Deploy registry
        registry = new OracleIntentRegistry("DIA Oracle Intent","1");
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
    
    function testSetSignerAuthorizationZeroAddress() public {
         vm.expectRevert(OracleIntentRegistry.ZeroAddress.selector);
        registry.setSignerAuthorization(address(0), true);
        
        vm.expectRevert(OracleIntentRegistry.ZeroAddress.selector);
        registry.setSignerAuthorization(address(0), false);
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
        assertEq(registry.getLatestIntentHashByType("OracleUpdate",TEST_SYMBOL), intentHash);
        
        // Verify intent data
        OracleIntentUtils.OracleIntent memory storedIntent = registry.getIntent(intentHash);
        assertEq(storedIntent.symbol, TEST_SYMBOL);
        assertEq(storedIntent.price, TEST_PRICE);
        assertEq(storedIntent.timestamp, TEST_TIMESTAMP);
        assertEq(storedIntent.signer, signer1);
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

    function TestRegisterExpiredIntent() public {
        registry.setSignerAuthorization(signer1, true);
        
        // Create intent with past expiry
        OracleIntentUtils.OracleIntent memory intent = createTestIntent(TEST_SYMBOL, TEST_NONCE);
        intent.expiry = block.timestamp - 1; // Already expired
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
        assertEq(registry.getLatestIntentHashByType("OracleUpdate",TEST_SYMBOL), intentHash2);

        // Register third intent with even older timestamp
        OracleIntentUtils.OracleIntent memory intent3 = createTestIntent(TEST_SYMBOL, 3);
        intent3.timestamp = TEST_TIMESTAMP - 2000;
        registerValidIntent(intent3, signer1Pk, signer1);
        
        // Latest should still be the newest timestamp (intent2)
        assertEq(registry.getLatestIntentHashByType("OracleUpdate",TEST_SYMBOL), intentHash2);
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
            bytes32 latestHash = registry.getLatestIntentHashByType("OracleUpdate",symbol);
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
        assertTrue(registry.getLatestIntentHashByType("OracleUpdate","TOKEN0") != bytes32(0));
        assertTrue(registry.getLatestIntentHashByType("OracleUpdate","TOKEN1") == bytes32(0)); // Should be empty
        assertTrue(registry.getLatestIntentHashByType("OracleUpdate","TOKEN2") != bytes32(0));
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
        assertTrue(registry.getLatestIntentHashByType("OracleUpdate","TOKEN1") != bytes32(0));
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
        assertTrue(registry.getLatestIntentHashByType("OracleUpdate","TOKEN0") != bytes32(0));
        assertTrue(registry.getLatestIntentHashByType("OracleUpdate","TOKEN1") == bytes32(0)); // Invalid signature skipped
        assertTrue(registry.getLatestIntentHashByType("OracleUpdate","TOKEN2") != bytes32(0));
    }
    
    // ===== INTENT RETRIEVAL TESTS =====
    
    
   
    
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
        assertEq(registry.getLatestIntentHashByType("OracleUpdate","BTC"), btcHash);
        assertEq(registry.getLatestIntentHashByType("OracleUpdate","ETH"), ethHash);
        
        (OracleIntentUtils.OracleIntent memory btcPrice) = registry.getLatestIntentByType("OracleUpdate","BTC");
        (OracleIntentUtils.OracleIntent memory ethPrice) = registry.getLatestIntentByType("OracleUpdate","ETH");
        
        assertEq(btcPrice.price, TEST_PRICE);
        assertEq(ethPrice.price, TEST_PRICE_2);
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
        assertEq(registry.getLatestIntentHashByType("OracleUpdate",TEST_SYMBOL), hash3);
        (OracleIntentUtils.OracleIntent memory latestIntent) = registry.getLatestIntentByType("OracleUpdate", TEST_SYMBOL);
        assertEq(latestIntent.price, 300);
    }
    
    function testRegisterIntentOlderThanExisting() public {
        registry.setSignerAuthorization(signer1, true);
        
        // Register newer intent first
        OracleIntentUtils.OracleIntent memory newerIntent = createTestIntent(TEST_SYMBOL, 1);
        newerIntent.timestamp = TEST_TIMESTAMP + 1000;
        newerIntent.price = 60000e18;
        bytes32 newerHash = registerValidIntent(newerIntent, signer1Pk, signer1);
        
        // Verify it's the latest
        assertEq(registry.getLatestIntentHashByType("OracleUpdate",TEST_SYMBOL), newerHash);
        
        // Register older intent (should not become latest)
        OracleIntentUtils.OracleIntent memory olderIntent = createTestIntent(TEST_SYMBOL, 2);
        olderIntent.timestamp = TEST_TIMESTAMP; // Older timestamp
        olderIntent.price = 40000e18;
        bytes32 olderHash = registerValidIntent(olderIntent, signer1Pk, signer1);
        
        // Latest should still be the newer one (tests the else branch)
        assertEq(registry.getLatestIntentHashByType("OracleUpdate",TEST_SYMBOL), newerHash);
        
        // But both intents should be stored
        assertTrue(registry.processedIntents(newerHash));
        assertTrue(registry.processedIntents(olderHash));
        
        // Latest price should be from newer intent
        (OracleIntentUtils.OracleIntent memory latestPrice) = registry.getLatestIntentByType("OracleUpdate",TEST_SYMBOL);
        assertEq(latestPrice.price, 60000e18);
    }
    
    // ===== INTENT TYPE COLLISION TESTS =====
    
    function testDifferentIntentTypesWithSameSymbol() public {
        registry.setSignerAuthorization(signer1, true);
        
        // Register "OracleUpdate" intent for BTC
        OracleIntentUtils.OracleIntent memory oracleUpdateIntent = createTestIntent("BTC", 1);
        oracleUpdateIntent.intentType = "OracleUpdate";
        oracleUpdateIntent.timestamp = block.timestamp;
        bytes32 oracleUpdateHash = OracleIntentUtils.calculateIntentHash(oracleUpdateIntent, registry.getDomainSeparator());
        (uint8 v1, bytes32 r1, bytes32 s1) = vm.sign(signer1Pk, oracleUpdateHash);
        bytes memory signature1 = abi.encodePacked(r1, s1, v1);
        registerValidIntent(oracleUpdateIntent, signer1Pk, signer1);
        
        // Register "PriceUpdate" intent for same symbol BTC with newer timestamp
        OracleIntentUtils.OracleIntent memory priceUpdateIntent = createTestIntent("BTC", 2);
        priceUpdateIntent.intentType = "PriceUpdate";  // Different intent type!
        priceUpdateIntent.timestamp = block.timestamp + 1000; // Newer timestamp
        priceUpdateIntent.price = 60000e18; // Different price
        bytes32 priceUpdateHash = registerValidIntent(priceUpdateIntent, signer1Pk, signer1);
        
        // CRITICAL: latestIntentBySymbol should now point to PriceUpdate intent
        // because it has a newer timestamp, even though it's a different intent type
        bytes32 latestHash = registry.getLatestIntentHashByType("PriceUpdate","BTC");
        assertEq(latestHash, priceUpdateHash, "Latest should be PriceUpdate due to newer timestamp");
        
        // // Verify getLatestPrice returns data from PriceUpdate intent
        // (uint256 price, uint256 timestamp, string memory source) = registry.getLatestPrice("BTC");
        // assertEq(price, 60000e18, "Price should be from PriceUpdate intent");
        // assertEq(timestamp, block.timestamp + 1000, "Timestamp should be from PriceUpdate intent");
        
        // Verify both intents are stored separately
        OracleIntentUtils.OracleIntent memory retrievedOracleUpdate = registry.getIntent(oracleUpdateHash);
        OracleIntentUtils.OracleIntent memory retrievedPriceUpdate = registry.getIntent(priceUpdateHash);
        
        assertEq(retrievedOracleUpdate.intentType, "OracleUpdate");
        assertEq(retrievedPriceUpdate.intentType, "PriceUpdate");
        assertNotEq(oracleUpdateHash, priceUpdateHash, "Different intent types should have different hashes");
    }
    
    function testDifferentIntentTypesOlderOverridesNewer() public {
        registry.setSignerAuthorization(signer1, true);
        
        // Register "PriceUpdate" intent for BTC with newer timestamp
        OracleIntentUtils.OracleIntent memory priceUpdateIntent = createTestIntent("BTC", 1);
        priceUpdateIntent.intentType = "PriceUpdate";
        priceUpdateIntent.timestamp = block.timestamp + 1000; // Newer
        priceUpdateIntent.price = 60000e18;
        bytes32 priceUpdateHash = registerValidIntent(priceUpdateIntent, signer1Pk, signer1);
        
        // Register "OracleUpdate" intent for same symbol with EVEN NEWER timestamp
        OracleIntentUtils.OracleIntent memory oracleUpdateIntent = createTestIntent("BTC", 2);
        oracleUpdateIntent.intentType = "OracleUpdate";
        oracleUpdateIntent.timestamp = block.timestamp + 2000; // Even newer
        oracleUpdateIntent.price = 70000e18; // Different price
        bytes32 oracleUpdateHash = registerValidIntent(oracleUpdateIntent, signer1Pk, signer1);
        
        // Latest should now be OracleUpdate because it has newer timestamp
        bytes32 latestHash = registry.getLatestIntentHashByType("OracleUpdate","BTC");
        assertEq(latestHash, oracleUpdateHash, "Latest should be OracleUpdate due to newest timestamp");
        
        // getLatestPrice now returns data from OracleUpdate intent
        // (uint256 price, uint256 timestamp, string memory source) = registry.getLatestPrice("BTC");
        // assertEq(price, 70000e18, "Price should be from OracleUpdate intent");
        // assertEq(timestamp, block.timestamp + 2000, "Timestamp should be from OracleUpdate intent");
    }
    
    // ===== INTENT TYPE QUERY TESTS =====
    
    function testGetLatestIntentByType() public {
        registry.setSignerAuthorization(signer1, true);
        
        // Register different intent types for same symbol
        OracleIntentUtils.OracleIntent memory priceIntent = createTestIntent("BTC", 1);
        priceIntent.intentType = "PriceUpdate";
        priceIntent.price = 50000e18;
        priceIntent.timestamp = block.timestamp;
        registerValidIntent(priceIntent, signer1Pk, signer1);
        
        OracleIntentUtils.OracleIntent memory volumeIntent = createTestIntent("BTC", 2);
        volumeIntent.intentType = "VolumeUpdate";
        volumeIntent.price = 1000000e18; // This represents volume, not price
        volumeIntent.timestamp = block.timestamp + 100;
        registerValidIntent(volumeIntent, signer1Pk, signer1);
        
        // Test getLatestIntentByType for each type
        OracleIntentUtils.OracleIntent memory retrievedPriceIntent = registry.getLatestIntentByType("PriceUpdate", "BTC");
        assertEq(retrievedPriceIntent.intentType, "PriceUpdate");
        assertEq(retrievedPriceIntent.price, 50000e18);
        
        OracleIntentUtils.OracleIntent memory retrievedVolumeIntent = registry.getLatestIntentByType("VolumeUpdate", "BTC");
        assertEq(retrievedVolumeIntent.intentType, "VolumeUpdate");
        assertEq(retrievedVolumeIntent.price, 1000000e18);
        
        // Verify they are different intents
        assertNotEq(retrievedPriceIntent.nonce, retrievedVolumeIntent.nonce);
    }
    
    function testGetLatestIntentByTypeNoIntentForSymbol() public {
        // Try to get intent for non-existent symbol+type combination
        vm.expectRevert(OracleIntentRegistry.NoIntentForSymbol.selector);
        registry.getLatestIntentByType("PriceUpdate", "NONEXISTENT");
    }
    
    function testGetLatestIntentByTypeIntentNotFound() public {
        vm.expectRevert(OracleIntentRegistry.NoIntentForSymbol.selector);
        registry.getLatestIntentByType("NonExistentType", "NonExistentSymbol");
    }
    
    function testGetLatestIntentHashByType() public {
        registry.setSignerAuthorization(signer1, true);
        
        // Register intent
        OracleIntentUtils.OracleIntent memory intent = createTestIntent("ETH", 1);
        intent.intentType = "MetadataUpdate";
        bytes32 expectedHash = registerValidIntent(intent, signer1Pk, signer1);
        
        // Query by type
        bytes32 retrievedHash = registry.getLatestIntentHashByType("MetadataUpdate", "ETH");
        assertEq(retrievedHash, expectedHash);
        
        // Query non-existent type should return zero
        bytes32 nonExistentHash = registry.getLatestIntentHashByType("NonExistent", "ETH");
        assertEq(nonExistentHash, bytes32(0));
    }
    
    
    
    function testCompositeKey() public view {
        // Test composite key generation
        bytes32 key1 = registry.getCompositeKey("PriceUpdate", "BTC");
        bytes32 key2 = registry.getCompositeKey("VolumeUpdate", "BTC");
        bytes32 key3 = registry.getCompositeKey("PriceUpdate", "ETH");
        
        // All keys should be different
        assertNotEq(key1, key2, "Different intent types should produce different keys");
        assertNotEq(key1, key3, "Different symbols should produce different keys");
        assertNotEq(key2, key3, "Different type+symbol combinations should produce different keys");
        
        // Same type+symbol should produce same key
        bytes32 key4 = registry.getCompositeKey("PriceUpdate", "BTC");
        assertEq(key1, key4, "Same intent type and symbol should produce same key");
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
    
    
    function testRegisterMultipleIntentsWithUnauthorizedSignersOnly() public {
        // Test batch with ALL unauthorized signers (different path than mixed batch)
        OracleIntentUtils.OracleIntent[] memory intents = new OracleIntentUtils.OracleIntent[](2);
        intents[0] = createSignedIntent(createTestIntent("TOKEN0", 1), signer1Pk, signer1);
        intents[1] = createSignedIntent(createTestIntent("TOKEN1", 2), signer2Pk, signer2);
        
        // Neither signer is authorized
        vm.expectEmit(true, false, false, false);
        emit BatchIntentsRegistered(0);
        
        registry.registerMultipleIntents(intents);
    }
    
    function testRegisterMultipleIntentsWithDuplicateIntents() public {
        registry.setSignerAuthorization(signer1, true);
        
        // Create same intent twice in same batch
        OracleIntentUtils.OracleIntent memory intent1 = createTestIntent("TOKEN0", 1);
        OracleIntentUtils.OracleIntent[] memory intents = new OracleIntentUtils.OracleIntent[](2);
        intents[0] = createSignedIntent(intent1, signer1Pk, signer1);
        intents[1] = createSignedIntent(intent1, signer1Pk, signer1); // Same intent
        
        vm.expectEmit(true, false, false, false);
        emit BatchIntentsRegistered(1); // Only first one should be processed
        
        registry.registerMultipleIntents(intents);
        
        // Verify only one was processed
        bytes32 latestHash = registry.getLatestIntentHashByType("OracleUpdate", "TOKEN0");
        assertTrue(latestHash != bytes32(0));
    }
    
    function testBatchRegistrationTimestampOrdering() public {
        registry.setSignerAuthorization(signer1, true);
        
        // Create intents with out-of-order timestamps but process in batch
        OracleIntentUtils.OracleIntent[] memory intents = new OracleIntentUtils.OracleIntent[](3);
        
        // Intent with newest timestamp (should become latest)
        OracleIntentUtils.OracleIntent memory intent1 = createTestIntent("TOKEN0", 1);
        intent1.timestamp = block.timestamp + 300;
        intent1.price = 300e18;
        intents[0] = createSignedIntent(intent1, signer1Pk, signer1);
        
        // Intent with oldest timestamp
        OracleIntentUtils.OracleIntent memory intent2 = createTestIntent("TOKEN0", 2);
        intent2.timestamp = block.timestamp + 100;
        intent2.price = 100e18;
        intents[1] = createSignedIntent(intent2, signer1Pk, signer1);
        
        // Intent with middle timestamp
        OracleIntentUtils.OracleIntent memory intent3 = createTestIntent("TOKEN0", 3);
        intent3.timestamp = block.timestamp + 200;
        intent3.price = 200e18;
        intents[2] = createSignedIntent(intent3, signer1Pk, signer1);
        
        registry.registerMultipleIntents(intents);
        
        // Latest should be the one with highest timestamp (300)
        OracleIntentUtils.OracleIntent memory latestIntent = registry.getLatestIntentByType("OracleUpdate", "TOKEN0");
        assertEq(latestIntent.price, 300e18);
        assertEq(latestIntent.timestamp, block.timestamp + 300);
    }
    
    function testRegisterMultipleIntentsWithMalformedSignatures() public {
        registry.setSignerAuthorization(signer1, true);
        
        // Create intent with invalid signature format that ecrecover will reject
        OracleIntentUtils.OracleIntent[] memory intents = new OracleIntentUtils.OracleIntent[](1);
        
        OracleIntentUtils.OracleIntent memory badSigIntent = createTestIntent("TOKEN1", 2);
        // Create a 65-byte signature that's properly formatted but invalid (all zeros except last byte)
        badSigIntent.signature = new bytes(65);
        badSigIntent.signature[64] = 0x1c; // Valid v value
        badSigIntent.signer = signer1;
        intents[0] = badSigIntent;
        
        // The signature won't recover to signer1, so it should be skipped
        registry.registerMultipleIntents(intents);
        
        // Verify no intents were processed (no event check needed as it's not deterministic)
        assertTrue(registry.getLatestIntentHashByType("OracleUpdate", "TOKEN1") == bytes32(0));
    }
    
    function testRegisterIntentLatestIntentCases() public {
        registry.setSignerAuthorization(signer1, true);
        
        // First, register an intent when no latest intent exists
        OracleIntentUtils.OracleIntent memory firstIntent = createTestIntent("NEWTOKEN", 1);
        firstIntent.timestamp = block.timestamp;
        bytes32 firstHash = registerValidIntent(firstIntent, signer1Pk, signer1);
        
        // Verify it became the latest
        assertEq(registry.getLatestIntentHashByType("OracleUpdate", "NEWTOKEN"), firstHash);
        
        // Register newer intent (covers first condition of OR)
        OracleIntentUtils.OracleIntent memory newerIntent = createTestIntent("NEWTOKEN", 2);
        newerIntent.timestamp = block.timestamp + 1000;
        bytes32 newerHash = registerValidIntent(newerIntent, signer1Pk, signer1);
        
        // Verify newer intent became latest
        assertEq(registry.getLatestIntentHashByType("OracleUpdate", "NEWTOKEN"), newerHash);
        
        // Register older intent 
        OracleIntentUtils.OracleIntent memory olderIntent = createTestIntent("NEWTOKEN", 3);
        olderIntent.timestamp = block.timestamp + 500; // Older than current latest
        registerValidIntent(olderIntent, signer1Pk, signer1);
        
        // Verify latest didn't change
        assertEq(registry.getLatestIntentHashByType("OracleUpdate", "NEWTOKEN"), newerHash);
    }
    
    
    function testExpiredIntentEmitsEnumEvent() public {
        registry.setSignerAuthorization(signer1, true);
        
        OracleIntentUtils.OracleIntent memory intent = createTestIntent("BTC", 1);
        intent.expiry = block.timestamp - 1; 
        intent = createSignedIntent(intent, signer1Pk, signer1);
        
        OracleIntentUtils.OracleIntent[] memory intents = new OracleIntentUtils.OracleIntent[](1);
        intents[0] = intent;
        
        vm.expectEmit(true, true, true, true);
        emit IntentRejected(
            OracleIntentUtils.calculateIntentHash(intent, registry.getDomainSeparator()),
            "BTC", 
            signer1, 
            OracleIntentRegistry.RejectionReason.Expired
        );
        
        registry.registerMultipleIntents(intents);
    }
    
    function testUnauthorizedSignerEmitsEnumEvent() public {
        address unauthorizedSigner = address(0x99);
        uint256 unauthorizedPk = 0x99;
        
        OracleIntentUtils.OracleIntent memory intent = createTestIntent("ETH", 1);
        intent.expiry = block.timestamp + 3600;
        intent = createSignedIntent(intent, unauthorizedPk, unauthorizedSigner);
        
        OracleIntentUtils.OracleIntent[] memory intents = new OracleIntentUtils.OracleIntent[](1);
        intents[0] = intent;
        
        vm.expectEmit(true, true, true, true);
        emit IntentRejected(
            OracleIntentUtils.calculateIntentHash(intent, registry.getDomainSeparator()),
            "ETH", 
            unauthorizedSigner, 
            OracleIntentRegistry.RejectionReason.UnauthorizedSigner
        );
        
        registry.registerMultipleIntents(intents);
    }
    
    function testAlreadyProcessedIntentEmitsEnumEvent() public {
        registry.setSignerAuthorization(signer1, true);
        
        OracleIntentUtils.OracleIntent memory intent = createTestIntent("ADA", 1);
        intent = createSignedIntent(intent, signer1Pk, signer1);
        bytes32 intentHash = OracleIntentUtils.calculateIntentHash(intent, registry.getDomainSeparator());
        
        OracleIntentUtils.OracleIntent[] memory firstBatch = new OracleIntentUtils.OracleIntent[](1);
        firstBatch[0] = intent;
        registry.registerMultipleIntents(firstBatch);
        
        OracleIntentUtils.OracleIntent[] memory secondBatch = new OracleIntentUtils.OracleIntent[](1);
        secondBatch[0] = intent;
        
        vm.expectEmit(true, true, true, true);
        emit IntentRejected(
            intentHash,
            "ADA", 
            signer1, 
            OracleIntentRegistry.RejectionReason.AlreadyProcessed
        );
        
        registry.registerMultipleIntents(secondBatch);
    }
    
    function testInvalidSignatureEmitsEnumEvent() public {
        registry.setSignerAuthorization(signer1, true);
        
        OracleIntentUtils.OracleIntent memory intent = createTestIntent("DOT", 1);
        intent = createSignedIntent(intent, signer2Pk, signer1);  
        
        OracleIntentUtils.OracleIntent[] memory intents = new OracleIntentUtils.OracleIntent[](1);
        intents[0] = intent;
        
        vm.expectEmit(true, true, true, true);
        emit IntentRejected(
            OracleIntentUtils.calculateIntentHash(intent, registry.getDomainSeparator()),
            "DOT", 
            signer1, 
            OracleIntentRegistry.RejectionReason.InvalidSignature
        );
        
        registry.registerMultipleIntents(intents);
    }
    
    function testEnumValuesAreCorrect() public pure {
        assert(uint(OracleIntentRegistry.RejectionReason.Expired) == 0);
        assert(uint(OracleIntentRegistry.RejectionReason.UnauthorizedSigner) == 1);
        assert(uint(OracleIntentRegistry.RejectionReason.AlreadyProcessed) == 2);
        assert(uint(OracleIntentRegistry.RejectionReason.InvalidSignature) == 3);
    }
    
    function testAllRejectionReasonsInBatch() public {
        registry.setSignerAuthorization(signer1, true);
        address unauthorizedSigner = address(0x99);
        uint256 unauthorizedPk = 0x99;
        
        OracleIntentUtils.OracleIntent[] memory intents = new OracleIntentUtils.OracleIntent[](4);
        
         intents[0] = createTestIntent("BTC", 1);
        intents[0].expiry = block.timestamp - 1;
        intents[0] = createSignedIntent(intents[0], signer1Pk, signer1);
        
         intents[1] = createTestIntent("ETH", 2);
        intents[1] = createSignedIntent(intents[1], unauthorizedPk, unauthorizedSigner);
        
         intents[2] = createTestIntent("ADA", 3);
        intents[2] = createSignedIntent(intents[2], signer1Pk, signer1);
        
         intents[3] = intents[2]; 
        
         vm.recordLogs();
        registry.registerMultipleIntents(intents);
        
        Vm.Log[] memory logs = vm.getRecordedLogs();
        
        uint rejectionCount = 0;
        uint successCount = 0;
        
        for (uint i = 0; i < logs.length; i++) {
            if (logs[i].topics[0] == keccak256("IntentRejected(bytes32,string,address,uint8)")) {
                rejectionCount++;
                
                 uint8 reason = abi.decode(logs[i].data, (uint8));
                
                if (rejectionCount == 1) {
                     assertEq(reason, uint8(OracleIntentRegistry.RejectionReason.Expired));
                } else if (rejectionCount == 2) {
                     assertEq(reason, uint8(OracleIntentRegistry.RejectionReason.UnauthorizedSigner));
                } else if (rejectionCount == 3) {
                     assertEq(reason, uint8(OracleIntentRegistry.RejectionReason.AlreadyProcessed));
                }
            } else if (logs[i].topics[0] == keccak256("IntentRegistered(bytes32,string,uint256,uint256,address)")) {
                successCount++;
            }
        }
        
        assertEq(rejectionCount, 3, "Should have 3 rejection events");
        assertEq(successCount, 1, "Should have 1 success event");
    }
}