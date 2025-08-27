// SPDX-License-Identifier: MIT
pragma solidity 0.8.29;

import { Test } from "forge-std/Test.sol";
import { console2 } from "forge-std/console2.sol";
import "../contracts/libs/OracleIntentUtils.sol";

/**
 * @title OracleIntentUtilsTest
 * @notice Comprehensive unit tests for the OracleIntentUtils library
 * @dev Tests all public functions including edge cases and error conditions
 */
contract OracleIntentUtilsTest is Test {
    using OracleIntentUtils for OracleIntentUtils.OracleIntent;
    
    // Test constants
    string constant TEST_DOMAIN_NAME = "DIA Oracle Intent";
    string constant TEST_DOMAIN_VERSION = "1";
    uint256 constant TEST_CHAIN_ID = 1;
    address constant TEST_VERIFYING_CONTRACT = address(0x1234567890123456789012345678901234567890);
    
    // Test intent data
    string constant TEST_INTENT_TYPE = "OracleUpdate";
    string constant TEST_VERSION = "1";
    uint256 constant TEST_NONCE = 12345;
    uint256 constant TEST_EXPIRY = 1234567890;
    string constant TEST_SYMBOL = "BTC";
    uint256 constant TEST_PRICE = 50000e18;
    uint256 constant TEST_TIMESTAMP = 1234567880;
    string constant TEST_SOURCE = "dia.data";
    
    // Test wallets
    uint256 signerPk = 0x1234;
    address signer;
    
    bytes32 domainSeparator;
    OracleIntentUtils.OracleIntent validIntent;
    
    function setUp() public {
        signer = vm.addr(signerPk);
        
        // Create domain separator
        domainSeparator = OracleIntentUtils.createDomainSeparator(
            TEST_DOMAIN_NAME,
            TEST_DOMAIN_VERSION,
            TEST_CHAIN_ID,
            TEST_VERIFYING_CONTRACT
        );
        
        // Create valid intent
        validIntent = createTestIntent();
    }
    
    // ===== DOMAIN SEPARATOR TESTS =====
    
    function testCreateDomainSeparator() public {
        bytes32 separator = OracleIntentUtils.createDomainSeparator(
            TEST_DOMAIN_NAME,
            TEST_DOMAIN_VERSION,
            TEST_CHAIN_ID,
            TEST_VERIFYING_CONTRACT
        );
        
        // Should not be zero
        assertNotEq(separator, bytes32(0));
        
        // Should be deterministic
        bytes32 separator2 = OracleIntentUtils.createDomainSeparator(
            TEST_DOMAIN_NAME,
            TEST_DOMAIN_VERSION,
            TEST_CHAIN_ID,
            TEST_VERIFYING_CONTRACT
        );
        assertEq(separator, separator2);
    }
    
    function testCreateDomainSeparatorDifferentInputs() public {
        bytes32 baseline = OracleIntentUtils.createDomainSeparator(
            TEST_DOMAIN_NAME,
            TEST_DOMAIN_VERSION,
            TEST_CHAIN_ID,
            TEST_VERIFYING_CONTRACT
        );
        
        // Different name should produce different separator
        bytes32 diffName = OracleIntentUtils.createDomainSeparator(
            "Different Name",
            TEST_DOMAIN_VERSION,
            TEST_CHAIN_ID,
            TEST_VERIFYING_CONTRACT
        );
        assertNotEq(baseline, diffName);
        
        // Different version should produce different separator
        bytes32 diffVersion = OracleIntentUtils.createDomainSeparator(
            TEST_DOMAIN_NAME,
            "2",
            TEST_CHAIN_ID,
            TEST_VERIFYING_CONTRACT
        );
        assertNotEq(baseline, diffVersion);
        
        // Different chain ID should produce different separator
        bytes32 diffChainId = OracleIntentUtils.createDomainSeparator(
            TEST_DOMAIN_NAME,
            TEST_DOMAIN_VERSION,
            42,
            TEST_VERIFYING_CONTRACT
        );
        assertNotEq(baseline, diffChainId);
        
        // Different contract should produce different separator
        bytes32 diffContract = OracleIntentUtils.createDomainSeparator(
            TEST_DOMAIN_NAME,
            TEST_DOMAIN_VERSION,
            TEST_CHAIN_ID,
            address(0x9876543210987654321098765432109876543210)
        );
        assertNotEq(baseline, diffContract);
    }
    
    // ===== STRUCT HASH TESTS =====
    
    function testCalculateStructHash() public {
        bytes32 structHash = OracleIntentUtils.calculateStructHash(validIntent);
        
        // Should not be zero
        assertNotEq(structHash, bytes32(0));
        
        // Should be deterministic
        bytes32 structHash2 = OracleIntentUtils.calculateStructHash(validIntent);
        assertEq(structHash, structHash2);
    }
    
    function testCalculateStructHashDifferentData() public {
        bytes32 baseline = OracleIntentUtils.calculateStructHash(validIntent);
        
        // Different symbol should produce different hash
        OracleIntentUtils.OracleIntent memory diffSymbol = validIntent;
        diffSymbol.symbol = "ETH";
        assertNotEq(baseline, OracleIntentUtils.calculateStructHash(diffSymbol));
        
        // Different price should produce different hash
        OracleIntentUtils.OracleIntent memory diffPrice = validIntent;
        diffPrice.price = 1000e18;
        assertNotEq(baseline, OracleIntentUtils.calculateStructHash(diffPrice));
        
        // Different timestamp should produce different hash
        OracleIntentUtils.OracleIntent memory diffTimestamp = validIntent;
        diffTimestamp.timestamp = block.timestamp;
        assertNotEq(baseline, OracleIntentUtils.calculateStructHash(diffTimestamp));
        
        // Different nonce should produce different hash
        OracleIntentUtils.OracleIntent memory diffNonce = validIntent;
        diffNonce.nonce = 99999;
        assertNotEq(baseline, OracleIntentUtils.calculateStructHash(diffNonce));
    }
    
    function testCalculateStructHashEmptyStrings() public {
        OracleIntentUtils.OracleIntent memory emptyIntent = validIntent;
        emptyIntent.intentType = "";
        emptyIntent.version = "";
        emptyIntent.symbol = "";
        emptyIntent.source = "";
        
        bytes32 structHash = OracleIntentUtils.calculateStructHash(emptyIntent);
        assertNotEq(structHash, bytes32(0));
    }
    
    // ===== INTENT HASH TESTS =====
    
    function testCalculateIntentHash() public {
        bytes32 intentHash = OracleIntentUtils.calculateIntentHash(validIntent, domainSeparator);
        
        // Should not be zero
        assertNotEq(intentHash, bytes32(0));
        
        // Should be deterministic
        bytes32 intentHash2 = OracleIntentUtils.calculateIntentHash(validIntent, domainSeparator);
        assertEq(intentHash, intentHash2);
        
        // Different domain separator should produce different hash
        bytes32 diffDomain = OracleIntentUtils.createDomainSeparator(
            "Different Domain",
            TEST_DOMAIN_VERSION,
            TEST_CHAIN_ID,
            TEST_VERIFYING_CONTRACT
        );
        bytes32 diffIntentHash = OracleIntentUtils.calculateIntentHash(validIntent, diffDomain);
        assertNotEq(intentHash, diffIntentHash);
    }
    
    function testCalculateIntentHashEIP712Compliance() public {
        bytes32 intentHash = OracleIntentUtils.calculateIntentHash(validIntent, domainSeparator);
        
        // Manual calculation for verification
        bytes32 structHash = OracleIntentUtils.calculateStructHash(validIntent);
        bytes32 expectedHash = keccak256(
            abi.encodePacked(
                "\x19\x01",
                domainSeparator,
                structHash
            )
        );
        
        assertEq(intentHash, expectedHash, "Intent hash should match EIP-712 standard");
    }
    
    // ===== SIGNATURE RECOVERY TESTS =====
    
    function testRecoverSigner() public {
        bytes32 hash = keccak256("test message");
        (uint8 v, bytes32 r, bytes32 s) = vm.sign(signerPk, hash);
        bytes memory signature = abi.encodePacked(r, s, v);
        
        address recovered = OracleIntentUtils.recoverSigner(hash, signature);
        assertEq(recovered, signer);
    }
    
    function testRecoverSignerDifferentMessages() public {
        bytes32 hash1 = keccak256("message 1");
        bytes32 hash2 = keccak256("message 2");
        
        (uint8 v1, bytes32 r1, bytes32 s1) = vm.sign(signerPk, hash1);
        (uint8 v2, bytes32 r2, bytes32 s2) = vm.sign(signerPk, hash2);
        
        bytes memory sig1 = abi.encodePacked(r1, s1, v1);
        bytes memory sig2 = abi.encodePacked(r2, s2, v2);
        
        address recovered1 = OracleIntentUtils.recoverSigner(hash1, sig1);
        address recovered2 = OracleIntentUtils.recoverSigner(hash2, sig2);
        
        assertEq(recovered1, signer);
        assertEq(recovered2, signer);
        assertEq(recovered1, recovered2);
    }
    
    function testRecoverSignerInvalidSignatureLength() public {
        bytes32 hash = keccak256("test message");
        bytes memory invalidSignature = hex"1234"; // Too short
        
        vm.expectRevert(OracleIntentUtils.InvalidSignature.selector);
        this.callRecoverSigner(hash, invalidSignature);
    }
    
    function testRecoverSignerInvalidSignatureLengthTooLong() public {
        bytes32 hash = keccak256("test message");
        bytes memory invalidSignature = hex"123456789012345678901234567890123456789012345678901234567890123456789012345678901234567890123456789012345678901234567890123456789012345678"; // Too long
        
        vm.expectRevert(OracleIntentUtils.InvalidSignature.selector);
        this.callRecoverSigner(hash, invalidSignature);
    }
    
    function testRecoverSignerInvalidV() public {
        bytes32 hash = keccak256("test message");
        (uint8 v, bytes32 r, bytes32 s) = vm.sign(signerPk, hash);
        
        // Create signature with invalid v value
        bytes memory invalidSignature = abi.encodePacked(r, s, uint8(26)); // v should be 27 or 28
        
        vm.expectRevert(OracleIntentUtils.InvalidSignature.selector);
        this.callRecoverSigner(hash, invalidSignature);
    }
    
    function testRecoverSignerInvalidVHighValue() public {
        bytes32 hash = keccak256("test message");
        (uint8 v, bytes32 r, bytes32 s) = vm.sign(signerPk, hash);
        
        // Create signature with invalid v value
        bytes memory invalidSignature = abi.encodePacked(r, s, uint8(29)); // v should be 27 or 28
        
        vm.expectRevert(OracleIntentUtils.InvalidSignature.selector);
        this.callRecoverSigner(hash, invalidSignature);
    }
    
    function testRecoverSignerVNormalization() public {
        bytes32 hash = keccak256("test message");
        (uint8 v, bytes32 r, bytes32 s) = vm.sign(signerPk, hash);
        
        // Create signature with v - 27 (should be normalized)
        bytes memory signature = abi.encodePacked(r, s, uint8(v - 27));
        
        address recovered = OracleIntentUtils.recoverSigner(hash, signature);
        assertEq(recovered, signer);
    }
    
    // ===== SIGNATURE VALIDATION TESTS =====
    
    function testValidateSignature() public {
        // Create signed intent
        OracleIntentUtils.OracleIntent memory signedIntent = createSignedIntent();
        
        bool isValid = OracleIntentUtils.validateSignature(signedIntent, domainSeparator);
        assertTrue(isValid);
    }
    
    function testValidateSignatureInvalid() public {
        // Create intent with wrong signer
        OracleIntentUtils.OracleIntent memory invalidIntent = createSignedIntent();
        invalidIntent.signer = address(0x999);
        
        bool isValid = OracleIntentUtils.validateSignature(invalidIntent, domainSeparator);
        assertFalse(isValid);
    }
    
    function testValidateSignatureWrongDomain() public {
        OracleIntentUtils.OracleIntent memory signedIntent = createSignedIntent();
        
        bytes32 wrongDomain = OracleIntentUtils.createDomainSeparator(
            "Wrong Domain",
            TEST_DOMAIN_VERSION,
            TEST_CHAIN_ID,
            TEST_VERIFYING_CONTRACT
        );
        
        bool isValid = OracleIntentUtils.validateSignature(signedIntent, wrongDomain);
        assertFalse(isValid);
    }
    
    function testValidateSignatureModifiedIntent() public {
        OracleIntentUtils.OracleIntent memory signedIntent = createSignedIntent();
        
        // Modify intent after signing
        signedIntent.price = 99999e18;
        
        bool isValid = OracleIntentUtils.validateSignature(signedIntent, domainSeparator);
        assertFalse(isValid);
    }
    
    // ===== INTENT FORMAT DETECTION TESTS =====
    
    function testIsIntentFormatLargeData() public {
        // Create data larger than 200 bytes
        bytes memory largeData = new bytes(300);
        for (uint i = 0; i < 300; i++) {
            largeData[i] = bytes1(uint8(i % 256));
        }
        
        bool isIntent = this.checkIsIntentFormat(largeData);
        assertTrue(isIntent);
    }
    
    function testIsIntentFormatSmallData() public {
        // Create data smaller than 200 bytes
        bytes memory smallData = new bytes(100);
        
        bool isIntent = this.checkIsIntentFormat(smallData);
        assertFalse(isIntent);
    }
    
    function testIsIntentFormatExactBoundary() public {
        // Test exactly 200 bytes
        bytes memory exactData = new bytes(200);
        bool isIntent = this.checkIsIntentFormat(exactData);
        assertTrue(isIntent);
        
        // Test 199 bytes
        bytes memory almostData = new bytes(199);
        bool isNotIntent = this.checkIsIntentFormat(almostData);
        assertFalse(isNotIntent);
    }
    
    function testIsIntentFormatEmptyData() public {
        bytes memory emptyData = new bytes(0);
        bool isIntent = this.checkIsIntentFormat(emptyData);
        assertFalse(isIntent);
    }
    
    // Helper function to convert bytes memory to calldata
    function checkIsIntentFormat(bytes calldata data) external pure returns (bool) {
        return OracleIntentUtils.isIntentFormat(data);
    }
    
    // Helper function to call recoverSigner externally for revert testing
    function callRecoverSigner(bytes32 hash, bytes memory signature) external pure returns (address) {
        return OracleIntentUtils.recoverSigner(hash, signature);
    }
    
    // ===== EDGE CASES AND INTEGRATION TESTS =====
    
    function testFullWorkflowSignAndValidate() public {
        // Create intent
        OracleIntentUtils.OracleIntent memory intent = createTestIntent();
        
        // Calculate hash
        bytes32 intentHash = OracleIntentUtils.calculateIntentHash(intent, domainSeparator);
        
        // Sign hash
        (uint8 v, bytes32 r, bytes32 s) = vm.sign(signerPk, intentHash);
        intent.signature = abi.encodePacked(r, s, v);
        intent.signer = signer;
        
        // Validate signature
        bool isValid = OracleIntentUtils.validateSignature(intent, domainSeparator);
        assertTrue(isValid);
        
        // Verify signer recovery
        address recovered = OracleIntentUtils.recoverSigner(intentHash, intent.signature);
        assertEq(recovered, signer);
    }
    
    function testMultipleSignersWorkflow() public {
        uint256 signer2Pk = 0x5678;
        address signer2 = vm.addr(signer2Pk);
        
        // Create same intent, sign with different signers
        bytes32 intentHash = OracleIntentUtils.calculateIntentHash(validIntent, domainSeparator);
        
        (uint8 v1, bytes32 r1, bytes32 s1) = vm.sign(signerPk, intentHash);
        (uint8 v2, bytes32 r2, bytes32 s2) = vm.sign(signer2Pk, intentHash);
        
        bytes memory sig1 = abi.encodePacked(r1, s1, v1);
        bytes memory sig2 = abi.encodePacked(r2, s2, v2);
        
        address recovered1 = OracleIntentUtils.recoverSigner(intentHash, sig1);
        address recovered2 = OracleIntentUtils.recoverSigner(intentHash, sig2);
        
        assertEq(recovered1, signer);
        assertEq(recovered2, signer2);
        assertNotEq(recovered1, recovered2);
    }
    
    function testTypeHashConstant() public {
        // Verify the type hash constant is correct
        bytes32 expectedTypeHash = keccak256(
            "OracleIntent(string intentType,string version,uint256 chainId,uint256 nonce,uint256 expiry,string symbol,uint256 price,uint256 timestamp,string source)"
        );
        
        // Access the internal constant through a struct hash calculation
        OracleIntentUtils.OracleIntent memory testIntent = createTestIntent();
        bytes32 structHash = OracleIntentUtils.calculateStructHash(testIntent);
        
        // Manually calculate expected struct hash
        bytes32 expectedStructHash = keccak256(
            abi.encode(
                expectedTypeHash,
                keccak256(bytes(testIntent.intentType)),
                keccak256(bytes(testIntent.version)),
                testIntent.chainId,
                testIntent.nonce,
                testIntent.expiry,
                keccak256(bytes(testIntent.symbol)),
                testIntent.price,
                testIntent.timestamp,
                keccak256(bytes(testIntent.source))
            )
        );
        
        assertEq(structHash, expectedStructHash, "Type hash should match expected value");
    }
    
    // ===== HELPER FUNCTIONS =====
    
    function createTestIntent() internal pure returns (OracleIntentUtils.OracleIntent memory) {
        return OracleIntentUtils.OracleIntent({
            intentType: TEST_INTENT_TYPE,
            version: TEST_VERSION,
            chainId: TEST_CHAIN_ID,
            nonce: TEST_NONCE,
            expiry: TEST_EXPIRY,
            symbol: TEST_SYMBOL,
            price: TEST_PRICE,
            timestamp: TEST_TIMESTAMP,
            source: TEST_SOURCE,
            signature: "",
            signer: address(0)
        });
    }
    
    function createSignedIntent() internal view returns (OracleIntentUtils.OracleIntent memory) {
        OracleIntentUtils.OracleIntent memory intent = createTestIntent();
        bytes32 intentHash = OracleIntentUtils.calculateIntentHash(intent, domainSeparator);
        (uint8 v, bytes32 r, bytes32 s) = vm.sign(signerPk, intentHash);
        
        intent.signature = abi.encodePacked(r, s, v);
        intent.signer = signer;
        
        return intent;
    }
}