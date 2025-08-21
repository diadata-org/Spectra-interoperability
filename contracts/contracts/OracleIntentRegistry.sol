// SPDX-License-Identifier: MIT
pragma solidity ^0.8.29;

import { OracleIntentUtils } from "./libs/OracleIntentUtils.sol";

/**
 * @title OracleIntentRegistry
 * @dev A contract for storing and managing oracle intents across chains
 */
contract OracleIntentRegistry {
    // Use shared library struct
    using OracleIntentUtils for OracleIntentUtils.OracleIntent;
    
    // Custom errors for gas-efficient reverts
    error NotOwner();
    error NotAuthorized();
    error IntentExpired();
    error SignerNotAuthorized();
    error IntentAlreadyProcessed();
    error InvalidSignature();
    error NoIntentForSymbol();
    error IntentNotFound();
    error ZeroAddress();
    
    // Note: Batch uses OracleIntentUtils.OracleIntent directly to avoid duplication
    
    // Mapping from intent hash to intent
    mapping(bytes32 => OracleIntentUtils.OracleIntent) public intents;
    
    // Mapping from symbol to latest intent hash
    mapping(string => bytes32) public latestIntentBySymbol;
    
    // Mapping of authorized signers
    mapping(address => bool) public authorizedSigners;
    
    // Mapping to track processed intents
    mapping(bytes32 => bool) public processedIntents;
    
    // EIP-712 domain separator
    bytes32 private immutable DOMAIN_SEPARATOR;
    
    // Events
    event IntentRegistered(bytes32 indexed intentHash, string indexed symbol, uint256 price, uint256 timestamp, address signer);
    event SignerAuthorized(address indexed signer, bool status);
    event BatchIntentsRegistered(uint256 count);
    
    // Owner of the contract
    address public owner;
    
    // Modifiers
    modifier onlyOwner() {
        if (msg.sender != owner) revert NotOwner();
        _;
    }
    
    modifier onlyAuthorized() {
        if (!(authorizedSigners[msg.sender] || msg.sender == owner)) revert NotAuthorized();
        _;
    }
    
    constructor() {
        owner = msg.sender;
        authorizedSigners[msg.sender] = true;
        
        // Create the EIP-712 domain separator using shared library
        DOMAIN_SEPARATOR = OracleIntentUtils.createDomainSeparator(
            "DIA Oracle Intent",
            "1",
            block.chainid,
            address(this)
        );
    }
    
    /**
     * @dev Registers a new oracle intent with EIP-712 signature
     * @param intentType The type of intent (e.g., "OracleUpdate")
     * @param version The version of the intent format
     * @param chainId The chain ID where the intent originates
     * @param nonce A unique identifier for this intent
     * @param expiry When this intent expires (unix timestamp)
     * @param symbol The symbol of the oracle data
     * @param price The price value
     * @param timestamp The timestamp of the oracle data
     * @param source The source of the oracle data
     * @param signature The EIP-712 signature
     * @param signer The address of the signer
     */
    function registerIntent(
        string memory intentType,
        string memory version,
        uint256 chainId,
        uint256 nonce,
        uint256 expiry,
        string memory symbol,
        uint256 price,
        uint256 timestamp,
        string memory source,
        bytes memory signature,
        address signer
    ) external {
        // Check if the intent has expired
        if (block.timestamp > expiry) revert IntentExpired();
        
        // Verify the signer is authorized
        if (!authorizedSigners[signer]) revert SignerNotAuthorized();
        
        // Create intent struct using shared library
        OracleIntentUtils.OracleIntent memory intent = OracleIntentUtils.OracleIntent({
            intentType: intentType,
            version: version,
            chainId: chainId,
            nonce: nonce,
            expiry: expiry,
            symbol: symbol,
            price: price,
            timestamp: timestamp,
            source: source,
            signature: signature,
            signer: signer
        });
        
        // Calculate intent hash using shared library
        bytes32 intentHash = OracleIntentUtils.calculateIntentHash(intent, DOMAIN_SEPARATOR);
        
        // Check if this intent has already been processed
        if (processedIntents[intentHash]) revert IntentAlreadyProcessed();
        
        // Verify the signature using shared library
        address recoveredSigner = OracleIntentUtils.recoverSigner(intentHash, signature);
        if (recoveredSigner != signer) revert InvalidSignature();
        
        // Mark the intent as processed
        processedIntents[intentHash] = true;
        intents[intentHash] = intent;
        
        // Update latest intent for symbol if this timestamp is newer
        bytes32 currentLatestIntentHash = latestIntentBySymbol[symbol];
        if (currentLatestIntentHash == bytes32(0) || intents[currentLatestIntentHash].timestamp < timestamp) {
            latestIntentBySymbol[symbol] = intentHash;
        }
        
        // Emit event
        emit IntentRegistered(intentHash, symbol, price, timestamp, signer);
    }

    /**
     * @dev Registers multiple oracle intents with EIP-712 signatures in a single transaction
     * @param intentsData Array of intent data to register
     */
    function registerMultipleIntents(OracleIntentUtils.OracleIntent[] calldata intentsData) external {
        if (intentsData.length == 0) revert IntentNotFound();
        
        uint256 successCount = 0;
        
        for (uint256 i = 0; i < intentsData.length; i++) {
            OracleIntentUtils.OracleIntent calldata data = intentsData[i];
            
            // Skip expired intents
            if (block.timestamp > data.expiry) {
                continue;
            }
            
            // Skip intents from unauthorized signers
            if (!authorizedSigners[data.signer]) {
                continue;
            }
            
            // Calculate intent hash using shared library
            bytes32 intentHash = OracleIntentUtils.calculateIntentHash(data, DOMAIN_SEPARATOR);
            
            // Skip already processed intents
            if (processedIntents[intentHash]) {
                continue;
            }
            
            // Verify the signature using shared library
            if (OracleIntentUtils.recoverSigner(intentHash, data.signature) != data.signer) {
                continue;
            }
            
            // Mark the intent as processed
            processedIntents[intentHash] = true;
            intents[intentHash] = OracleIntentUtils.OracleIntent({
                intentType: data.intentType,
                version: data.version,
                chainId: data.chainId,
                nonce: data.nonce,
                expiry: data.expiry,
                symbol: data.symbol,
                price: data.price,
                timestamp: data.timestamp,
                source: data.source,
                signature: data.signature,
                signer: data.signer
            });
            
            // Update latest intent for symbol if this timestamp is newer
            bytes32 currentLatestIntentHash = latestIntentBySymbol[data.symbol];
            if (currentLatestIntentHash == bytes32(0) || intents[currentLatestIntentHash].timestamp < data.timestamp) {
                latestIntentBySymbol[data.symbol] = intentHash;
            }
            
            // Emit event for each successfully registered intent
            emit IntentRegistered(intentHash, data.symbol, data.price, data.timestamp, data.signer);
            
            // Increment success count
            successCount++;
        }
        
        // Emit batch event
        emit BatchIntentsRegistered(successCount);
    }
    
    /**
     * @dev Gets the latest price for a symbol
     * @param symbol The symbol to get the price for
     * @return price The latest price
     * @return timestamp The timestamp of the price
     * @return source The source of the price
     */
    function getLatestPrice(string memory symbol) external view returns (uint256 price, uint256 timestamp, string memory source) {
        bytes32 intentHash = latestIntentBySymbol[symbol];
        if (intentHash == bytes32(0)) revert NoIntentForSymbol();
        
        OracleIntentUtils.OracleIntent memory intent = intents[intentHash];
        return (intent.price, intent.timestamp, intent.source);
    }
    
    /**
     * @dev Gets the intent details by hash
     * @param intentHash The hash of the intent
     * @return The intent details
     */
    function getIntent(bytes32 intentHash) external view returns (OracleIntentUtils.OracleIntent memory) {
        if (intents[intentHash].timestamp == 0) revert IntentNotFound();
        return intents[intentHash];
    }
    
    /**
     * @dev Authorizes or deauthorizes a signer
     * @param signer The address of the signer
     * @param status The authorization status
     */
    function setSignerAuthorization(address signer, bool status) external onlyOwner {
        authorizedSigners[signer] = status;
        emit SignerAuthorized(signer, status);
    }
    
    /**
     * @dev Transfers ownership of the contract
     * @param newOwner The address of the new owner
     */
    function transferOwnership(address newOwner) external onlyOwner {
        if (newOwner == address(0)) revert ZeroAddress();
        owner = newOwner;
    }
    
    
    /**
     * @dev Returns the domain separator for EIP-712 signatures
     */
    function getDomainSeparator() external view returns (bytes32) {
        return DOMAIN_SEPARATOR;
    }
} 