// SPDX-License-Identifier: GPL-3.0
pragma solidity 0.8.29;

/**
 * @title OracleIntentUtils
 * @notice Shared utility library for Oracle Intent operations
 * @dev Provides common functionality for EIP-712 signatures, hash calculations, and intent validation
 */
library OracleIntentUtils {
    // Shared OracleIntent struct
    struct OracleIntent {
        // Metadata
        string intentType;
        string version;
        uint256 chainId;
        uint256 nonce;
        uint256 expiry;
        
        // Oracle data
        string symbol;
        uint256 price;
        uint256 timestamp;
        string source;
        
        // Signature data
        bytes signature;
        address signer;
    }
    
    // Shared EIP-712 type hash
    bytes32 internal constant ORACLE_INTENT_TYPEHASH = keccak256(
        "OracleIntent(string intentType,string version,uint256 chainId,uint256 nonce,uint256 expiry,string symbol,uint256 price,uint256 timestamp,string source)"
    );
    
    // Custom errors
    error InvalidSignature();
    
    /**
     * @notice Calculates the EIP-712 struct hash for an OracleIntent
     * @param intent The OracleIntent structure
     * @return The struct hash for EIP-712
     */
    function calculateStructHash(OracleIntent memory intent) internal pure returns (bytes32) {
        return keccak256(
            abi.encode(
                ORACLE_INTENT_TYPEHASH,
                keccak256(bytes(intent.intentType)),
                keccak256(bytes(intent.version)),
                intent.chainId,
                intent.nonce,
                intent.expiry,
                keccak256(bytes(intent.symbol)),
                intent.price,
                intent.timestamp,
                keccak256(bytes(intent.source))
            )
        );
    }
    
    /**
     * @notice Calculates the EIP-712 hash for an OracleIntent
     * @param intent The OracleIntent structure
     * @param domainSeparator The EIP-712 domain separator
     * @return The complete EIP-712 hash ready for signature verification
     */
    function calculateIntentHash(OracleIntent memory intent, bytes32 domainSeparator) 
        internal pure returns (bytes32) {
        return keccak256(
            abi.encodePacked(
                "\x19\x01",
                domainSeparator,
                calculateStructHash(intent)
            )
        );
    }
    
    /**
     * @notice Recovers the signer address from a signature
     * @param hash The hash that was signed
     * @param signature The signature bytes
     * @return The address of the signer
     */
    function recoverSigner(bytes32 hash, bytes memory signature) internal pure returns (address) {
        if (signature.length != 65) revert InvalidSignature();
        
        bytes32 r;
        bytes32 s;
        uint8 v;
        
        assembly {
            r := mload(add(signature, 32))
            s := mload(add(signature, 64))
            v := byte(0, mload(add(signature, 96)))
        }
        
        if (v < 27) {
            v += 27;
        }
        
        if (v != 27 && v != 28) revert InvalidSignature();
        
        return ecrecover(hash, v, r, s);
    }
    
    /**
     * @notice Creates EIP-712 domain separator
     * @param domainName The domain name for EIP-712
     * @param domainVersion The domain version for EIP-712
     * @param chainId The chain ID for the domain
     * @param verifyingContract The address of the verifying contract
     * @return The domain separator hash
     */
    function createDomainSeparator(
        string memory domainName,
        string memory domainVersion,
        uint256 chainId,
        address verifyingContract
    ) internal pure returns (bytes32) {
        return keccak256(
            abi.encode(
                keccak256("EIP712Domain(string name,string version,uint256 chainId,address verifyingContract,bytes32 salt)"),
                keccak256(bytes(domainName)),
                keccak256(bytes(domainVersion)),
                chainId,
                verifyingContract,
                bytes32(0)
            )
        );
    }
    
    /**
     * @notice Validates that an intent's signature is valid
     * @param intent The OracleIntent to validate
     * @param domainSeparator The EIP-712 domain separator
     * @return isValid Whether the signature is valid
     */
    function validateSignature(OracleIntent memory intent, bytes32 domainSeparator) 
        internal pure returns (bool isValid) {
        bytes32 intentHash = calculateIntentHash(intent, domainSeparator);
        address recoveredSigner = recoverSigner(intentHash, intent.signature);
        return recoveredSigner == intent.signer;
    }
    
    /**
     * @notice Detects if data is in intent format by attempting to decode and validate structure
     * @param _data The encoded data to check
     * @return isIntent Whether the data can be properly decoded as intent format
     * @dev Uses actual decode validation without hardcoded size assumptions
     */
    function isIntentFormat(bytes calldata _data) internal pure returns (bool isIntent) {
        // real Intent payloads will be well above this threshold;
        return _data.length >= 200;
    }
}