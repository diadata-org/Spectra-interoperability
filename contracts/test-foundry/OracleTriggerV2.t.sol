// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import "forge-std/Test.sol";
import "forge-std/console.sol";
import "../contracts/OracleTriggerV2.sol";
import "../contracts/OracleIntentRegistry.sol";
import "../contracts/interfaces/oracle/IOracleTriggerV2.sol";
import "../contracts/libs/OracleIntentUtils.sol";
/**
 * @title OracleTriggerV2Test  
 * @dev Test contract for OracleTriggerV2 using composition pattern to reuse existing tests
 * @notice This pattern allows reusing all existing test logic while adding V2-specific functionality
 */
contract OracleTriggerV2Test is Test {
    // V2-specific contracts
    OracleTriggerV2 public oracleTriggerV2;
    OracleIntentRegistry public intentRegistry;
    
    // Test addresses (matching original test)
    address public owner = address(0x1);
    address public newOwner = address(0x2);
    address public recipient = address(0x3);
    address public mailbox = address(0x4);
    uint32 public chainId = 1;
    
    // V2-specific test addresses
    address public oracleSigner;
    uint256 public oracleSignerPk;
    
    // V2-specific test data
    string constant DOMAIN_NAME = "DIA Oracle Inten";
    string constant DOMAIN_VERSION = "1";
    uint256 constant SOURCE_CHAIN_ID = 100640;
    string constant TEST_SYMBOL = "BTC";
    uint256 constant TEST_PRICE = 50000e18;
    uint256 constant TEST_TIMESTAMP = 1710000000;

    function setUp() public {
        // Setup V2-specific addresses
        oracleSignerPk = 1;
        oracleSigner = vm.addr(oracleSignerPk);

        // Deploy V2 contract
        vm.prank(owner);
        oracleTriggerV2 = new OracleTriggerV2();
        
        // Deploy intent registry
        intentRegistry = new OracleIntentRegistry();
        
        // Setup V2-specific configuration
        vm.prank(owner);
        oracleTriggerV2.updateIntentRegistryContract(address(intentRegistry));
        
        vm.prank(owner);
        oracleTriggerV2.setDomainSeparator(DOMAIN_NAME, DOMAIN_VERSION, SOURCE_CHAIN_ID);
        
        // Authorize oracle signer
        intentRegistry.setSignerAuthorization(oracleSigner, true);
    }

    // ===== REUSED TESTS FROM V1  =====
    
    function testOwnerInitialization() public view {
        assertTrue(oracleTriggerV2.hasRole(oracleTriggerV2.OWNER_ROLE(), owner));
        assertTrue(oracleTriggerV2.hasRole(oracleTriggerV2.DEFAULT_ADMIN_ROLE(), owner));
    }

    function testAddChain() public {
        vm.prank(owner);
        oracleTriggerV2.addChain(chainId, recipient);

        address storedRecipient = oracleTriggerV2.viewChain(chainId);
        assertEq(storedRecipient, recipient);
    }

    function testUpdateChain() public {
        vm.prank(owner);
        oracleTriggerV2.addChain(chainId, recipient);
        address newRecipient = address(0x6);

        vm.prank(owner);
        oracleTriggerV2.updateChain(chainId, newRecipient);
        assertEq(oracleTriggerV2.viewChain(chainId), newRecipient);
    }

    function testCannotAddChainWithoutOwner() public {
        vm.expectRevert();
        oracleTriggerV2.addChain(chainId, recipient);
    }

    function testSetMailBox() public {
        vm.prank(owner);
        oracleTriggerV2.setMailBox(mailbox);
        assertEq(oracleTriggerV2.getMailBox(), mailbox);
    }

    function testSetMailBoxToZeroAddress() public {
        vm.prank(owner);
        vm.expectRevert(abi.encodeWithSelector(IOracleTriggerV2.InvalidAddress.selector));
        oracleTriggerV2.setMailBox(address(0x0));
    }

    function testAddOwner() public {
        vm.prank(owner);
        oracleTriggerV2.grantRole(keccak256("OWNER_ROLE"), newOwner);
        assertTrue(oracleTriggerV2.hasRole(oracleTriggerV2.OWNER_ROLE(), newOwner));
    }

    function testRemoveOwner() public {
        vm.prank(owner);
        oracleTriggerV2.grantRole(keccak256("OWNER_ROLE"), newOwner);

        vm.prank(owner);
        oracleTriggerV2.revokeRole(keccak256("OWNER_ROLE"), newOwner);

        assertFalse(oracleTriggerV2.hasRole(keccak256("OWNER_ROLE"), newOwner));
    }

    function testRetrieveLostTokens() public {
        vm.deal(address(oracleTriggerV2), 0.5 ether);
        assertEq(address(oracleTriggerV2).balance, 0.5 ether);

        uint256 recipientBalanceBefore = recipient.balance;
        uint256 contractBalanceBefore = address(oracleTriggerV2).balance;

        vm.prank(owner);
        oracleTriggerV2.retrieveLostTokens(recipient);

        assertEq(recipient.balance, recipientBalanceBefore + contractBalanceBefore);
        assertEq(address(oracleTriggerV2).balance, 0);
    }

    function testRetrieveLostTokensUnauthorized() public {
        vm.prank(newOwner);
        vm.expectRevert();
        oracleTriggerV2.retrieveLostTokens(recipient);
    }

    function testCannotAddDuplicateChain() public {
        vm.prank(owner);
        oracleTriggerV2.addChain(chainId, recipient);

        vm.prank(owner);
        vm.expectRevert(abi.encodeWithSignature("ChainAlreadyExists(uint32)", chainId));
        oracleTriggerV2.addChain(chainId, address(0x8));
    }

    function testDeleteChain() public {
        vm.prank(owner);
        oracleTriggerV2.addChain(chainId, recipient);
        assertEq(oracleTriggerV2.viewChain(chainId), recipient);

        vm.prank(owner);
        oracleTriggerV2.deleteChain(chainId);
        
        vm.expectRevert();
        oracleTriggerV2.viewChain(chainId);
    }

    function testDeleteChainFailsIfNotConfigured() public {
        vm.prank(owner);
        vm.expectRevert();
        oracleTriggerV2.deleteChain(chainId);
    }
    
    // ===== V2-SPECIFIC TESTS =====
    
    function testIntentRegistryConfiguration() public view {
        assertEq(oracleTriggerV2.getIntentRegistry(), address(intentRegistry));
    }
    
    function testUpdateIntentRegistryContract() public {
        OracleIntentRegistry newRegistry = new OracleIntentRegistry();
        
        vm.prank(owner);
        oracleTriggerV2.updateIntentRegistryContract(address(newRegistry));
        
        assertEq(oracleTriggerV2.getIntentRegistry(), address(newRegistry));
    }
    
    function testCannotUpdateIntentRegistryWithoutOwner() public {
        OracleIntentRegistry newRegistry = new OracleIntentRegistry();
        
        vm.prank(newOwner);
        vm.expectRevert();
        oracleTriggerV2.updateIntentRegistryContract(address(newRegistry));
    }
    
    function testCannotSetZeroIntentRegistry() public {
        vm.prank(owner);
        vm.expectRevert(IOracleTriggerV2.InvalidAddress.selector);
        oracleTriggerV2.updateIntentRegistryContract(address(0));
    }
    
    function testDomainSeparatorConfiguration() public view {
        bytes32 expectedDomain = OracleIntentUtils.createDomainSeparator(
            DOMAIN_NAME,
            DOMAIN_VERSION,
            SOURCE_CHAIN_ID,
            address(oracleTriggerV2)
        );
        
        assertEq(oracleTriggerV2.domainSeparator(), expectedDomain);
    }
    
    function testSetDomainSeparator() public {
        string memory newDomainName = "New Domain";
        string memory newDomainVersion = "2.0";
        uint256 newChainId = 42;
        
        vm.expectEmit(true, false, false, true, address(oracleTriggerV2));
        emit IOracleTriggerV2.DomainSeparatorUpdated(
            OracleIntentUtils.createDomainSeparator(newDomainName, newDomainVersion, newChainId, address(oracleTriggerV2)),
            newDomainName,
            newDomainVersion,
            newChainId,
            address(oracleTriggerV2)
        );
        
        vm.prank(owner);
        oracleTriggerV2.setDomainSeparator(newDomainName, newDomainVersion, newChainId);
    }
    
    function testCannotSetDomainSeparatorWithoutOwner() public {
        vm.prank(newOwner);
        vm.expectRevert();
        oracleTriggerV2.setDomainSeparator("Test", "1.0", 1);
    }
    

    
    function testDispatchToChainWithIntent() public {
        // First register an intent in the registry
        registerTestIntent(TEST_SYMBOL, 1);
        
        // Setup chain and mailbox
        vm.prank(owner);
        oracleTriggerV2.addChain(chainId, recipient);
        
        vm.prank(owner);
        oracleTriggerV2.setMailBox(mailbox);
        
        // Grant dispatcher role (using owner who has admin role)
        vm.startPrank(owner);
        oracleTriggerV2.grantRole(oracleTriggerV2.DISPATCHER_ROLE(), owner);
        vm.stopPrank();
        
        // Fund the contract
        vm.deal(owner, 1 ether);
        
        vm.stopPrank();
        
        // Dispatch should work with registered intent
        vm.prank(owner);
        oracleTriggerV2.dispatchToChain{value: 0.1 ether}(chainId,"OracleUpdate", TEST_SYMBOL);
        
        // Test passes if no revert occurs
    }
    
    function testDispatchWithIntent() public {
        // Register an intent
        registerTestIntent(TEST_SYMBOL, 1);
        
        // Setup mailbox
        vm.prank(owner);
        oracleTriggerV2.setMailBox(mailbox);
        
        // Grant dispatcher role
        vm.startPrank(owner);
        oracleTriggerV2.grantRole(oracleTriggerV2.DISPATCHER_ROLE(), owner);
        vm.stopPrank();
        
        // Fund the contract
        vm.deal(owner, 1 ether);
        
        vm.stopPrank();
        
        // Dispatch should work with registered intent
        vm.prank(owner);
        oracleTriggerV2.dispatch{value: 0.1 ether}(chainId, recipient, "OracleUpdate", TEST_SYMBOL);
        
        // Test passes if no revert occurs
    }
    
    function testCannotDispatchWithoutRegistry() public {
        // Create a fresh contract without registry setup
        vm.prank(owner);
        OracleTriggerV2 freshContract = new OracleTriggerV2();
        
        vm.prank(owner);
        freshContract.addChain(chainId, recipient);
        
        vm.prank(owner);
        freshContract.setMailBox(mailbox);
        
        vm.startPrank(owner);
        freshContract.grantRole(freshContract.DISPATCHER_ROLE(), owner);
        
        vm.deal(owner, 1 ether);

        vm.expectRevert(abi.encodeWithSelector(IOracleTriggerV2.RegistryUnavailable.selector, "OracleUpdate", TEST_SYMBOL));
        freshContract.dispatchToChain{value: 0.1 ether}(chainId,"OracleUpdate", TEST_SYMBOL);
        vm.stopPrank();
    }
    
    function testCannotDispatchWithoutIntent() public {
        // Setup with empty registry (no intent registered)
        vm.prank(owner);
        oracleTriggerV2.addChain(chainId, recipient);
        
        vm.prank(owner);
        oracleTriggerV2.setMailBox(mailbox);
        
        vm.startPrank(owner);
        oracleTriggerV2.grantRole(oracleTriggerV2.DISPATCHER_ROLE(), owner);
        
        vm.deal(owner, 1 ether);
        
        // Should fail because no intent is registered for TEST_SYMBOL
        vm.expectRevert(abi.encodeWithSelector(IOracleTriggerV2.RegistryUnavailable.selector, "OracleUpdate", TEST_SYMBOL));
        oracleTriggerV2.dispatchToChain{value: 0.1 ether}(chainId, "OracleUpdate", TEST_SYMBOL);
        vm.stopPrank();
    }
    
    function testDispatchWithInvalidIntentDataEmptySymbol() public {
        // Test empty symbol validation
        MockInvalidIntentRegistry mockRegistry = new MockInvalidIntentRegistry();
        mockRegistry.setReturnType(0); // Empty symbol
        
        vm.prank(owner);
        oracleTriggerV2.updateIntentRegistryContract(address(mockRegistry));
        setupBasicDispatchTest();
        
        vm.expectRevert(abi.encodeWithSelector(IOracleTriggerV2.IntentDataInvalid.selector, TEST_SYMBOL, "Empty symbol"));
        oracleTriggerV2.dispatchToChain{value: 0.1 ether}(chainId,"OracleUpdate", TEST_SYMBOL);
        vm.stopPrank();
    }
    
    function testDispatchWithInvalidIntentDataZeroPrice() public {
        // Test zero price validation
        MockInvalidIntentRegistry mockRegistry = new MockInvalidIntentRegistry();
        mockRegistry.setReturnType(1); // Zero price
        
        vm.prank(owner);
        oracleTriggerV2.updateIntentRegistryContract(address(mockRegistry));
        setupBasicDispatchTest();
        
        vm.expectRevert(abi.encodeWithSelector(IOracleTriggerV2.IntentDataInvalid.selector, TEST_SYMBOL, "Zero price"));
        oracleTriggerV2.dispatchToChain{value: 0.1 ether}(chainId,"OracleUpdate", TEST_SYMBOL);
        vm.stopPrank();
    }
    
    function testDispatchWithInvalidIntentDataZeroTimestamp() public {
        // Test zero timestamp validation
        MockInvalidIntentRegistry mockRegistry = new MockInvalidIntentRegistry();
        mockRegistry.setReturnType(2); // Zero timestamp
        
        vm.prank(owner);
        oracleTriggerV2.updateIntentRegistryContract(address(mockRegistry));
        setupBasicDispatchTest();
        
        vm.expectRevert(abi.encodeWithSelector(IOracleTriggerV2.IntentDataInvalid.selector, TEST_SYMBOL, "Zero timestamp"));
        oracleTriggerV2.dispatchToChain{value: 0.1 ether}(chainId,"OracleUpdate", TEST_SYMBOL);
        vm.stopPrank();
    }
    
    function testDispatchWithInvalidIntentDataInvalidSigner() public {
        // Test invalid signer (address(0)) validation
        MockInvalidIntentRegistry mockRegistry = new MockInvalidIntentRegistry();
        mockRegistry.setReturnType(3); // Invalid signer
        
        vm.prank(owner);
        oracleTriggerV2.updateIntentRegistryContract(address(mockRegistry));
        setupBasicDispatchTest();
        
        vm.expectRevert(abi.encodeWithSelector(IOracleTriggerV2.IntentDataInvalid.selector, TEST_SYMBOL, "Invalid signer"));
        oracleTriggerV2.dispatchToChain{value: 0.1 ether}(chainId,"OracleUpdate", TEST_SYMBOL);
        vm.stopPrank();
    }
    
    function testDispatchWithInvalidIntentDataEmptySignature() public {
        // Test empty signature validation
        MockInvalidIntentRegistry mockRegistry = new MockInvalidIntentRegistry();
        mockRegistry.setReturnType(4); // Empty signature
        
        vm.prank(owner);
        oracleTriggerV2.updateIntentRegistryContract(address(mockRegistry));
        setupBasicDispatchTest();
        
        vm.expectRevert(abi.encodeWithSelector(IOracleTriggerV2.IntentDataInvalid.selector, TEST_SYMBOL, "Empty signature"));
        oracleTriggerV2.dispatchToChain{value: 0.1 ether}(chainId, "OracleUpdate",TEST_SYMBOL);
        vm.stopPrank();
    }
    
    function testRetrieveLostTokensTransferFailed() public {
        // Deploy a contract that rejects ETH transfers
        RejectingReceiver rejector = new RejectingReceiver();
        
        vm.deal(address(oracleTriggerV2), 1 ether);
        
        vm.prank(owner);
        vm.expectRevert(abi.encodeWithSelector(IOracleTriggerV2.AmountTransferFailed.selector));
        oracleTriggerV2.retrieveLostTokens(address(rejector));
    }
    
    function testRetrieveLostTokensNoBalance() public {
        // Test the NoBalanceToWithdraw branch when contract has 0 balance
        // Ensure contract has 0 balance
        assertEq(address(oracleTriggerV2).balance, 0);
        
        vm.prank(owner);
        vm.expectRevert(abi.encodeWithSelector(IOracleTriggerV2.NoBalanceToWithdraw.selector));
        oracleTriggerV2.retrieveLostTokens(recipient);
    }
    
    function testValidateAddressZeroAddressChecks() public {
        // Test validateAddress modifier with address(0) for various functions
        
        // Test addChain with address(0)
        vm.prank(owner);
        vm.expectRevert(abi.encodeWithSelector(IOracleTriggerV2.InvalidAddress.selector));
        oracleTriggerV2.addChain(999, address(0));
        
        // Test updateChain with address(0) 
        vm.prank(owner);
        oracleTriggerV2.addChain(888, recipient); // First add a valid chain
        
        vm.prank(owner);
        vm.expectRevert(abi.encodeWithSelector(IOracleTriggerV2.InvalidAddress.selector));
        oracleTriggerV2.updateChain(888, address(0));
        
        // Test retrieveLostTokens with address(0) - already tested in testRetrieveLostTokensRecipient
        
        // Test dispatch with address(0) recipient 
        registerTestIntent(TEST_SYMBOL, 1);
        
        vm.prank(owner);
        oracleTriggerV2.setMailBox(mailbox);
        
        vm.startPrank(owner);
        oracleTriggerV2.grantRole(oracleTriggerV2.DISPATCHER_ROLE(), owner);
        
        vm.deal(owner, 1 ether);
        
        vm.expectRevert(abi.encodeWithSelector(IOracleTriggerV2.InvalidAddress.selector));
        oracleTriggerV2.dispatch{value: 0.1 ether}(chainId, address(0),"OracleUpdate", TEST_SYMBOL);
        vm.stopPrank();
    }
    
    function testValidateChainModifierEdgeCases() public {
        // Test various edge cases for validateChain modifier
        
        // Test viewChain with non-existent chain
        vm.expectRevert(abi.encodeWithSelector(IOracleTriggerV2.ChainNotConfigured.selector, 99999));
        oracleTriggerV2.viewChain(99999);
        
        // Test updateChain with non-existent chain 
        vm.prank(owner);
        vm.expectRevert(abi.encodeWithSelector(IOracleTriggerV2.ChainNotConfigured.selector, 77777));
        oracleTriggerV2.updateChain(77777, recipient);
        
        // Test deleteChain with non-existent chain (already covered in testDeleteChainFailsIfNotConfigured)
        
        // Test dispatchToChain with non-existent chain
        registerTestIntent(TEST_SYMBOL, 2);
        
        vm.prank(owner);
        oracleTriggerV2.setMailBox(mailbox);
        
        vm.startPrank(owner);
        oracleTriggerV2.grantRole(oracleTriggerV2.DISPATCHER_ROLE(), owner);
        
        vm.deal(owner, 1 ether);
        
        vm.expectRevert(abi.encodeWithSelector(IOracleTriggerV2.ChainNotConfigured.selector, 55555));
        oracleTriggerV2.dispatchToChain{value: 0.1 ether}(55555, "OracleUpdate",TEST_SYMBOL);
        vm.stopPrank();
    }
    
    function testChainAlreadyExistsError() public {
        // Test the specific branch for ChainAlreadyExists
        vm.prank(owner);
        oracleTriggerV2.addChain(chainId, recipient);
        
        // Try to add the same chain again
        vm.prank(owner);
        vm.expectRevert(abi.encodeWithSelector(IOracleTriggerV2.ChainAlreadyExists.selector, chainId));
        oracleTriggerV2.addChain(chainId, address(0x999));
    }
    
    function testCurrentLatestIntentTimestampCheck() public {
        // Test the branch where current latest intent has newer timestamp
        bytes32 firstIntentHash = registerTestIntent("BTC", 1);
        
        // Create an intent with older timestamp
        OracleIntentUtils.OracleIntent memory olderIntent = OracleIntentUtils.OracleIntent({
            intentType: "OracleUpdate",
            version: "1.0.0",
            chainId: SOURCE_CHAIN_ID,
            nonce: 2,
            expiry: block.timestamp + 3600,
            symbol: "BTC",
            price: TEST_PRICE + 1000e18,
            timestamp: TEST_TIMESTAMP - 1000, // Older timestamp
            source: "DIA",
            signature: new bytes(65),
            signer: address(0)
        });
        
        bytes32 olderIntentHash = OracleIntentUtils.calculateIntentHash(olderIntent, intentRegistry.getDomainSeparator());
        
        (uint8 v, bytes32 r, bytes32 s) = vm.sign(oracleSignerPk, olderIntentHash);
        bytes memory signature = abi.encodePacked(r, s, v);
        
        // Register older intent - should not update latestIntentBySymbol
        intentRegistry.registerIntent(
            olderIntent.intentType,
            olderIntent.version,
            olderIntent.chainId,
            olderIntent.nonce,
            olderIntent.expiry,
            olderIntent.symbol,
            olderIntent.price,
            olderIntent.timestamp,
            olderIntent.source,
            signature,
            oracleSigner
        );
        
        // Latest intent should still be the first one (newer timestamp)
        bytes32 latestHash = intentRegistry.getLatestIntentHashByType("OracleUpdate","BTC");
        assertEq(latestHash, firstIntentHash);
    }
    
    function testDispatchWithSpecificRecipientSuccess() public {
        // Test the dispatch function with specific recipient (not using chains mapping)
        registerTestIntent(TEST_SYMBOL, 1);
        
        vm.prank(owner);
        oracleTriggerV2.setMailBox(mailbox);
        
        vm.startPrank(owner);
        oracleTriggerV2.grantRole(oracleTriggerV2.DISPATCHER_ROLE(), owner);
        
        vm.deal(owner, 1 ether);
        
        // Dispatch to specific recipient (not using configured chain)
        address specificRecipient = address(0x777);
        oracleTriggerV2.dispatch{value: 0.1 ether}(chainId, specificRecipient,"OracleUpdate", TEST_SYMBOL);
        vm.stopPrank();
        
        // Test passes if no revert occurs
    }

    // ===== HELPER FUNCTIONS =====
    
    /**
     * @dev Helper to setup basic dispatch test configuration
     */
    function setupBasicDispatchTest() internal {
        vm.prank(owner);
        oracleTriggerV2.addChain(chainId, recipient);
        
        vm.prank(owner);
        oracleTriggerV2.setMailBox(mailbox);
        
        vm.startPrank(owner);
        oracleTriggerV2.grantRole(oracleTriggerV2.DISPATCHER_ROLE(), owner);
        
        vm.deal(owner, 1 ether);
    }
    
    /**
     * @dev Helper to register a test intent in the registry
     */
    function registerTestIntent(string memory symbol, uint256 nonce) internal returns (bytes32 intentHash) {
        OracleIntentUtils.OracleIntent memory intent = OracleIntentUtils.OracleIntent({
            intentType: "OracleUpdate",
            version: "1.0.0",
            chainId: SOURCE_CHAIN_ID,
            nonce: nonce,
            expiry: block.timestamp + 3600,
            symbol: symbol,
            price: TEST_PRICE,
            timestamp: TEST_TIMESTAMP,
            source: "DIA",
            signature: new bytes(65),
            signer: address(0)
        });
        
        intentHash = OracleIntentUtils.calculateIntentHash(intent, intentRegistry.getDomainSeparator());
        
        (uint8 v, bytes32 r, bytes32 s) = vm.sign(oracleSignerPk, intentHash);
        bytes memory signature = abi.encodePacked(r, s, v);
        
        intentRegistry.registerIntent(
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
            oracleSigner
        );
        
        return intentHash;
    }
}

// Mock contracts for testing edge cases
contract MockInvalidIntentRegistry {
    uint256 public returnType = 0; // 0=empty symbol, 1=zero price, 2=zero timestamp, 3=invalid signer, 4=empty signature
    
    function setReturnType(uint256 _type) external {
        returnType = _type;
    }
    
    function latestIntentBySymbol(string memory) external pure returns (bytes32) {
        return bytes32(uint256(1)); // Non-zero hash
    }
    
    function getLatestIntentHashByType(string calldata, string calldata) external pure returns (bytes32) {
        return bytes32(uint256(1)); // Non-zero hash
    }
    
    function getIntent(bytes32) external view returns (OracleIntentUtils.OracleIntent memory) {
        if (returnType == 0) {
            // Empty symbol
            return OracleIntentUtils.OracleIntent({
                intentType: "OracleUpdate",
                version: "1.0.0", 
                chainId: 100640,
                nonce: 1,
                expiry: block.timestamp + 3600,
                symbol: "", // Empty symbol to trigger error
                price: 50000e18,
                timestamp: 1710000000,
                source: "DIA",
                signature: hex"1234",
                signer: address(1)
            });
        } else if (returnType == 1) {
            // Zero price
            return OracleIntentUtils.OracleIntent({
                intentType: "OracleUpdate",
                version: "1.0.0", 
                chainId: 100640,
                nonce: 1,
                expiry: block.timestamp + 3600,
                symbol: "BTC",
                price: 0, // Zero price to trigger error
                timestamp: 1710000000,
                source: "DIA",
                signature: hex"1234",
                signer: address(1)
            });
        } else if (returnType == 2) {
            // Zero timestamp
            return OracleIntentUtils.OracleIntent({
                intentType: "OracleUpdate",
                version: "1.0.0", 
                chainId: 100640,
                nonce: 1,
                expiry: block.timestamp + 3600,
                symbol: "BTC",
                price: 50000e18,
                timestamp: 0, // Zero timestamp to trigger error
                source: "DIA",
                signature: hex"1234",
                signer: address(1)
            });
        } else if (returnType == 3) {
            // Invalid signer
            return OracleIntentUtils.OracleIntent({
                intentType: "OracleUpdate",
                version: "1.0.0", 
                chainId: 100640,
                nonce: 1,
                expiry: block.timestamp + 3600,
                symbol: "BTC",
                price: 50000e18,
                timestamp: 1710000000,
                source: "DIA",
                signature: hex"1234",
                signer: address(0) // Invalid signer to trigger error
            });
        } else if (returnType == 4) {
            // Empty signature
            return OracleIntentUtils.OracleIntent({
                intentType: "OracleUpdate",
                version: "1.0.0", 
                chainId: 100640,
                nonce: 1,
                expiry: block.timestamp + 3600,
                symbol: "BTC",
                price: 50000e18,
                timestamp: 1710000000,
                source: "DIA",
                signature: "", // Empty signature to trigger error
                signer: address(1)
            });
        } else {
            // Default valid intent
            return OracleIntentUtils.OracleIntent({
                intentType: "OracleUpdate",
                version: "1.0.0", 
                chainId: 100640,
                nonce: 1,
                expiry: block.timestamp + 3600,
                symbol: "BTC",
                price: 50000e18,
                timestamp: 1710000000,
                source: "DIA",
                signature: hex"1234",
                signer: address(1)
            });
        }
    }
}

contract RejectingReceiver {
    receive() external payable {
        revert("Cannot receive ETH");
    }
}

// Test contract that extends OracleTriggerV2 to allow forcing zero domain separator
contract TestOracleTriggerV2WithMockDomain is OracleTriggerV2 {
    
    function initializeAsOwner(address _owner) external {
        _grantRole(DEFAULT_ADMIN_ROLE, _owner);
        _grantRole(OWNER_ROLE, _owner);
    }
    
    // Function that forces the domain separator check to trigger with bytes32(0)
    function setDomainSeparatorForceZero() external onlyRole(OWNER_ROLE) {
        bytes32 newDomainSeparator = bytes32(0); // Force zero value
        
        // This is the exact same check as in the original contract
        if (newDomainSeparator == bytes32(0)) {
            revert DomainSeparatorZero();
        }

        domainSeparator = newDomainSeparator;
        emit DomainSeparatorUpdated(
            domainSeparator,
            "forced",
            "zero",
            0,
            address(this)
        );
    }
}