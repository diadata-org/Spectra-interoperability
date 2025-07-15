// SPDX-License-Identifier: MIT
pragma solidity ^0.8.19;

/**
 * @title OracleIntentSignature
 * @dev Common library for EIP-712 signature handling for Oracle Intents
 * @notice This library provides consistent signature creation and verification
 * across OracleTrigger and PushOracleReceiver contracts
 */
library OracleIntentSignature {
    /// @notice The EIP-712 type hash for OracleIntent
    bytes32 public constant ORACLE_INTENT_TYPEHASH = keccak256(
        "OracleIntent(string intentType,string version,uint256 chainId,uint256 nonce,uint256 expiry,string symbol,uint256 price,uint256 timestamp,string source)"
    );

    /// @notice OracleIntent structure for signature operations
    struct OracleIntent {
        string intentType;
        string version;
        uint256 chainId;
        uint256 nonce;
        uint256 expiry;
        string symbol;
        uint256 price;
        uint256 timestamp;
        string source;
        bytes signature;
        address signer;
    }

    /**
     * @notice Creates the EIP-712 domain separator to match OracleIntentRegistry
     * @param chainId The chain ID
     * @param verifyingContract The address of the verifying contract
     * @return The domain separator hash
     */
    function createDomainSeparator(
        uint256 chainId,
        address verifyingContract
    ) internal pure returns (bytes32) {
        return keccak256(
            abi.encode(
                keccak256("EIP712Domain(string name,string version,uint256 chainId,address verifyingContract,bytes32 salt)"),
                keccak256("DIA Oracle Intent"),
                keccak256("1"),
                chainId,
                verifyingContract,
                bytes32(0)
            )
        );
    }

    /**
     * @notice Calculates the struct hash for an OracleIntent
     * @param intent The OracleIntent to hash
     * @return The struct hash
     */
    function getStructHash(OracleIntent memory intent) internal pure returns (bytes32) {
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
     * @param intent The OracleIntent to hash
     * @param domainSeparator The domain separator
     * @return The EIP-712 hash
     */
    function getIntentHash(
        OracleIntent memory intent,
        bytes32 domainSeparator
    ) internal pure returns (bytes32) {
        bytes32 structHash = getStructHash(intent);
        return keccak256(
            abi.encodePacked(
                "\x19\x01",
                domainSeparator,
                structHash
            )
        );
    }

    /**
     * @notice Recovers the signer address from a signature
     * @param hash The hash that was signed
     * @param signature The signature bytes
     * @return The recovered signer address
     */
    function recoverSigner(bytes32 hash, bytes memory signature) internal pure returns (address) {
        require(signature.length == 65, "OracleIntentSignature: invalid signature length");
        
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
        
        require(v == 27 || v == 28, "OracleIntentSignature: invalid signature v value");
        
        address signer = ecrecover(hash, v, r, s);
        require(signer != address(0), "OracleIntentSignature: invalid signature");
        
        return signer;
    }

    /**
     * @notice Verifies an OracleIntent signature
     * @param intent The OracleIntent to verify
     * @param domainSeparator The domain separator
     * @return True if the signature is valid, false otherwise
     */
    function verifyIntentSignature(
        OracleIntent memory intent,
        bytes32 domainSeparator
    ) internal pure returns (bool) {
        bytes32 hash = getIntentHash(intent, domainSeparator);
        address recoveredSigner = recoverSigner(hash, intent.signature);
        return recoveredSigner == intent.signer;
    }

    /**
     * @notice Gets the hash that needs to be signed for an OracleIntent
     * @dev This function is for off-chain signing reference
     * @param intent The OracleIntent to get the hash for
     * @param domainSeparator The domain separator
     * @return The hash that should be signed off-chain
     */
    function getSigningHash(
        OracleIntent memory intent,
        bytes32 domainSeparator
    ) internal pure returns (bytes32) {
        return getIntentHash(intent, domainSeparator);
    }

    /**
     * @notice Validates the basic structure of an OracleIntent
     * @param intent The OracleIntent to validate
     * @return True if the intent structure is valid
     */
    function validateIntentStructure(OracleIntent memory intent) internal pure returns (bool) {
        return (
            bytes(intent.intentType).length > 0 &&
            bytes(intent.version).length > 0 &&
            intent.chainId > 0 &&
            intent.nonce > 0 &&
            intent.expiry > 0 &&
            bytes(intent.symbol).length > 0 &&
            intent.price > 0 &&
            intent.timestamp > 0 &&
            bytes(intent.source).length > 0 &&
            intent.signature.length == 65 &&
            intent.signer != address(0)
        );
    }
}