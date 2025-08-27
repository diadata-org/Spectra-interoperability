// SPDX-License-Identifier: MIT
pragma solidity 0.8.29;

import { OracleIntentUtils } from "./libs/OracleIntentUtils.sol";

/**
 * @title OracleIntentRegistry
 * @dev A contract for storing and managing oracle intents across chains
 * @author Diadata.org
 * @notice This contract allows authorized signers to register oracle intents with EIP-712 signatures
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
    
    /// @notice Mapping from intent hash to OracleIntent details
    mapping(bytes32 => OracleIntentUtils.OracleIntent) public intents;
    
    /// @notice Mapping from symbol to latest intent hash
    mapping(string => bytes32) public latestIntentBySymbol;
    
    /// @notice Mapping of authorized signers
    mapping(address => bool) public authorizedSigners;
    
    /// @notice Mapping to track processed intents to prevent replay
    mapping(bytes32 => bool) public processedIntents;
    
    ///@notice EIP-712 domain separator
    bytes32 private immutable domainSeparator;
    
    /** 
        * @notice Event when a new intent is registered
        * @param intentHash The hash of the registered intent
        * @param symbol The symbol of the oracle data
        * @param price The price value
        * @param timestamp The timestamp of the oracle data
        * @param signer The address of the signer
    */
    event IntentRegistered(bytes32 indexed intentHash, string indexed symbol, uint256 indexed price, uint256  timestamp, address  signer);
    
    /**
     * @notice Event when a signer is authorized or deauthorized
     * @param signer The address of the signer
     * @param status The authorization status (true = authorized, false = deauthorized)
     */
    event SignerAuthorized(address indexed signer, bool indexed status);

    /**
     * @notice Event when multiple intents are registered in a batch
     * @param count The number of intents registered
     */
    event BatchIntentsRegistered(uint256 indexed count);

    /**
     * @notice Event when ownership is transferred
     * @param previousOwner The address of the previous owner
     * @param newOwner The address of the new owner
     */
    event OwnershipTransferred(address indexed previousOwner, address indexed newOwner);

    /// @notice Contract owner
    address public owner;
    
    /// @notice Modifier to restrict functions to only the owner
    modifier onlyOwner() {
        if (msg.sender != owner) revert NotOwner();
        _;
    }
 
    /// @notice Contract  constructor
    constructor() {
        owner = msg.sender;
        authorizedSigners[msg.sender] = true;
        
        // Create the EIP-712 domain separator using shared library
        domainSeparator = OracleIntentUtils.createDomainSeparator(
            "DIA Oracle Intent",
            "1",
            block.chainid,
            address(this)
        );
    }
    
    /**
     * @dev Registers a new oracle intent with EIP-712 signature
     * @notice Anyone can call this function with a valid signed intent
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
        string calldata intentType,
        string calldata version,
        uint256 chainId,
        uint256 nonce,
        uint256 expiry,
        string calldata symbol,
        uint256 price,
        uint256 timestamp,
        string calldata source,
        bytes calldata signature,
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
        
        bytes32 intentHash = OracleIntentUtils.calculateIntentHash(intent, domainSeparator);
        
        // Check if this intent has already been processed
        if (processedIntents[intentHash]) revert IntentAlreadyProcessed();
        
        // Verify the signature using shared library
        address recoveredSigner = OracleIntentUtils.recoverSigner(intentHash, signature);
        if (recoveredSigner != signer) revert InvalidSignature();
        
        // Mark the intent as processed
        processedIntents[intentHash] = true;
        intents[intentHash] = intent;
        bytes32 currentLatestIntentHash = latestIntentBySymbol[symbol];
        if (currentLatestIntentHash == bytes32(0) || intents[currentLatestIntentHash].timestamp < timestamp) {
            latestIntentBySymbol[symbol] = intentHash;
        }
        emit IntentRegistered(intentHash, symbol, price, timestamp, signer);
    }

    /**
     * @dev Registers multiple oracle intents with EIP-712 signatures in a single transaction
     * @notice Anyone can call this function with valid signed intents
     * @param intentsData Array of intent data to register
     */
    function registerMultipleIntents(OracleIntentUtils.OracleIntent[] calldata intentsData) external {
        if (intentsData.length == 0) revert IntentNotFound();
        
        uint256 successCount = 0;
        
        for (uint256 i = 0; i < intentsData.length; i++) {
            OracleIntentUtils.OracleIntent calldata data = intentsData[i];
            bytes32 intentHash = OracleIntentUtils.calculateIntentHash(data, domainSeparator);
            
            if (block.timestamp > data.expiry) {
                continue;
            }
            if (!authorizedSigners[data.signer]) {
                continue;
            }
            if (processedIntents[intentHash]) {
                continue;
            }
            if (OracleIntentUtils.recoverSigner(intentHash, data.signature) != data.signer) {
                continue;
            }
            
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
            
            bytes32 currentLatestIntentHash = latestIntentBySymbol[data.symbol];
            if (currentLatestIntentHash == bytes32(0) || intents[currentLatestIntentHash].timestamp < data.timestamp) {
                latestIntentBySymbol[data.symbol] = intentHash;
            }
            
            emit IntentRegistered(intentHash, data.symbol, data.price, data.timestamp, data.signer);
            ++successCount;
        }
        
        // Emit batch event
        emit BatchIntentsRegistered(successCount);
    }
    
    /**
     * @dev Gets the latest price for a symbol
     * @notice Returns the latest registered intent for the given symbol
     * @param symbol The symbol to get the price for
     * @return price The latest price
     * @return timestamp The timestamp of the price
     * @return source The source of the price
     */
    function getLatestPrice(string calldata symbol) external view returns (uint256 price, uint256 timestamp, string memory source) {
        bytes32 intentHash = latestIntentBySymbol[symbol];
        if (intentHash == bytes32(0)) revert NoIntentForSymbol();
        
        OracleIntentUtils.OracleIntent memory intent = intents[intentHash];
        return (intent.price, intent.timestamp, intent.source);
    }
    
    /**
     * @dev Gets the intent details by hash
     * @notice Returns the details of a registered intent by its hash
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
     * @notice Only the contract owner can authorize or deauthorize signers
     */
    function setSignerAuthorization(address signer, bool status) external onlyOwner {
        authorizedSigners[signer] = status;
        emit SignerAuthorized(signer, status);
    }
    
    /**
     * @dev Transfers ownership of the contract
     * @param newOwner The address of the new owner
     * @notice Only the current owner can transfer ownership
     */
    function transferOwnership(address newOwner) external onlyOwner {
        if (newOwner == address(0)) revert ZeroAddress();
        address previousOwner = owner;
        owner = newOwner;
        emit OwnershipTransferred(previousOwner, newOwner);
    }
    
    
    /**
     * @notice Gets the EIP-712 domain separator for signature validation
     * @dev Returns the domain separator for EIP-712 signatures
     * @return The domain separator used for EIP-712 signatures
     */
    function getDomainSeparator() external view returns (bytes32) {
        return domainSeparator;
    }
} 