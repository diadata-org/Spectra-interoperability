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
    error SignerNotAuthorized(address signer);
    error IntentAlreadyProcessed();
    error InvalidSignature();
    error NoIntentForSymbol();
    error IntentNotFound();
    error IntentExpired();
    error InvalidTimestamp(uint256 timestamp, uint256 blockTimestamp);
    error ZeroAddress();
    
    // Note: Batch uses OracleIntentUtils.OracleIntent directly to avoid duplication
    
    /// @notice Mapping from intent hash to OracleIntent details
    mapping(bytes32 => OracleIntentUtils.OracleIntent) public intents;
    
    /// @notice Mapping from composite key (intentType + symbol) to latest intent hash
    /// @dev Key format: keccak256(abi.encodePacked(intentType, "|", symbol))
    mapping(bytes32 => bytes32) public latestIntentByTypeAndSymbol;
    
    /// @notice Mapping of authorized signers
    mapping(address => bool) public authorizedSigners;
    
    /// @notice Mapping to track processed intents to prevent replay
    mapping(bytes32 => bool) public processedIntents;
    
    /// @notice EIP-712 domain separator
    bytes32 private immutable CACHED_DOMAIN_SEPARATOR;

    /// @notice Cached chain ID in case of fork
    uint256 private immutable CACHED_CHAIN_ID;

    ///@notice Cached contract address in case of fork
    address private immutable CACHED_SELF_ADDRESS;
 
    /// @notice EIP-712 domain name
    string private  _name;

    /// @notice EIP-712 domain version
    string private  _version;
    
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

    /**
     * @notice Enumeration of possible intent rejection reasons
     * @dev Used in IntentRejected event for gas efficiency and type safety
     */
    enum RejectionReason {
        Expired,              
        InvalidTimestamp,     
        UnauthorizedSigner,  
        AlreadyProcessed,     
        InvalidSignature 
    }

    /**
     * @notice Event when an intent is rejected during processing
     * @param intentHash The hash of the rejected intent
     * @param symbol The symbol of the intent
     * @param signer The signer of the intent
     * @param reason The reason for rejection (enum value for gas efficiency)
     */
    event IntentRejected(
        bytes32 indexed intentHash, 
        string indexed symbol, 
        address indexed signer, 
        RejectionReason reason
    );

    /// @notice Contract owner
    address public owner;
    
    /// @notice Modifier to restrict functions to only the owner
    modifier onlyOwner() {
        if (msg.sender != owner) revert NotOwner();
        _;
    }
 
    /// @notice Contract  constructor
    /// @param domainName The EIP-712 domain name
    /// @param domainVersion The EIP-712 domain version
      constructor(string memory domainName, string memory domainVersion) {
        owner = msg.sender;
        authorizedSigners[msg.sender] = true;
        _name = domainName;
        _version = domainVersion;
        CACHED_CHAIN_ID = block.chainid;
        CACHED_SELF_ADDRESS = address(this);

        // Create the EIP-712 domain separator using shared library
        CACHED_DOMAIN_SEPARATOR = OracleIntentUtils.createDomainSeparator(
            domainName,
            domainVersion,
            block.chainid,
            address(this)
        );
    }

    /**
     @notice Gets the EIP-712 domain separator, optimized for gas
     @return domain separator for the current chain.
     */
    function domainSeparator() internal view returns (bytes32) {
        if (address(this) == CACHED_SELF_ADDRESS && block.chainid == CACHED_CHAIN_ID) {
            return CACHED_DOMAIN_SEPARATOR;
        } else {
            return _buildDomainSeparator();
        }
    }

    /**
    @notice Builds the domain separator if chain ID has changed
    @return calculate domain separator
     */
    function _buildDomainSeparator() private view returns (bytes32) {
       return  OracleIntentUtils.createDomainSeparator(
            _name,
            _version,
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
        if (block.timestamp > expiry) {
            revert IntentExpired();
        }

        // Validate timestamp is not in the future to prevent DOS attacks
        if (timestamp > block.timestamp) {
            revert InvalidTimestamp(timestamp, block.timestamp);
        }

        // Verify the signer is authorized
        if (!authorizedSigners[signer]) revert SignerNotAuthorized(signer);
        
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
        
        bytes32 intentHash = OracleIntentUtils.calculateIntentHash(intent, domainSeparator());
        
        // Check if this intent has already been processed
        if (processedIntents[intentHash]) revert IntentAlreadyProcessed();
        
        // Verify the signature using shared library
        address recoveredSigner = OracleIntentUtils.recoverSigner(intentHash, signature);
        if (recoveredSigner != signer) revert InvalidSignature();
        
        // Mark the intent as processed
        processedIntents[intentHash] = true;
        intents[intentHash] = intent;
        
       
        
        // Update latest intent by type and symbol (new functionality)
        bytes32 compositeKey = getCompositeKey(intentType, symbol);
        bytes32 currentLatestByTypeHash = latestIntentByTypeAndSymbol[compositeKey];
        if (currentLatestByTypeHash == bytes32(0) || intents[currentLatestByTypeHash].timestamp < timestamp) {
            latestIntentByTypeAndSymbol[compositeKey] = intentHash;
        }
        
        emit IntentRegistered(intentHash, symbol, price, timestamp, signer);
    }

    /**
     * @dev Registers multiple oracle intents with EIP-712 signatures in a single transaction
     * @notice Anyone can call this function with valid signed intents
     * @param intentsData Array of intent data to register, timestamp order is required for updates else old timestamp will be ignored
     */
    function registerMultipleIntents(OracleIntentUtils.OracleIntent[] calldata intentsData) external {
        if (intentsData.length == 0) revert IntentNotFound();
        
        uint256 successCount = 0;
        bytes32 domainSep = domainSeparator();
        
        for (uint256 i = 0; i < intentsData.length; i++) {
            if (_processIntent(intentsData[i], domainSep)) {
                ++successCount;
            }
        }
        
        emit BatchIntentsRegistered(successCount);
    }

    /**
     * @notice Internal function to process a single intent
     * @dev Processes a single intent, returns true if successful
     * @param data The intent data to process
     * @param domainSep The cached domain separator
     * @return success Whether the intent was successfully processed
     */
    function _processIntent(
        OracleIntentUtils.OracleIntent calldata data,
        bytes32 domainSep
    ) private returns (bool success) {
        bytes32 intentHash = OracleIntentUtils.calculateIntentHash(data, domainSep);

        if (block.timestamp > data.expiry) {
            emit IntentRejected(intentHash, data.symbol, data.signer, RejectionReason.Expired);
            return false;
        }
        
        if (data.timestamp > block.timestamp) {
            emit IntentRejected(intentHash, data.symbol, data.signer, RejectionReason.InvalidTimestamp);
            return false;
        }
        
        if (!authorizedSigners[data.signer]) {
            emit IntentRejected(intentHash, data.symbol, data.signer, RejectionReason.UnauthorizedSigner);
            return false;
        }
        
        if (processedIntents[intentHash]) {
            emit IntentRejected(intentHash, data.symbol, data.signer, RejectionReason.AlreadyProcessed);
            return false;
        }
        
        address recoveredSigner = OracleIntentUtils.recoverSigner(intentHash, data.signature);
        if (recoveredSigner != data.signer) {
            emit IntentRejected(intentHash, data.symbol, data.signer, RejectionReason.InvalidSignature);
            return false;
        }
         
        _storeAndUpdateIntentCalldata(intentHash, data);
        return true;
    }

    /**
     * @notice Internal function to store intent and update latest mapping
     * @dev Stores the intent and updates the latest intent by type and symbol mapping
     * @param intentHash The hash of the intent
     * @param data The intent data to store
     */
    function _storeAndUpdateIntentCalldata(
        bytes32 intentHash,
        OracleIntentUtils.OracleIntent calldata data
    ) private {
        processedIntents[intentHash] = true;
        intents[intentHash] = data;
        
        bytes32 compositeKey = getCompositeKey(data.intentType, data.symbol);
        bytes32 currentLatestByTypeHash = latestIntentByTypeAndSymbol[compositeKey];
        if (currentLatestByTypeHash == bytes32(0) || intents[currentLatestByTypeHash].timestamp < data.timestamp) {
            latestIntentByTypeAndSymbol[compositeKey] = intentHash;
        }
        
        emit IntentRegistered(intentHash, data.symbol, data.price, data.timestamp, data.signer);
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
        if (signer == address(0)) revert ZeroAddress();
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
        return domainSeparator();
    }
    
    /**
     * @notice Creates a composite key for intentType and symbol lookups
     * @param intentType The type of intent (e.g., "PriceUpdate", "RWAUpdate")
     * @param symbol The symbol (e.g., "BTC", "ETH")
     * @return The composite key for mapping lookups
     */
    function getCompositeKey(string memory intentType, string memory symbol) 
        public pure returns (bytes32) {
        return keccak256(abi.encodePacked(intentType, "|", symbol));
    }
    
    /**
     * @notice Gets the latest intent hash for a specific intent type and symbol
     * @param intentType The type of intent to query
     * @param symbol The symbol to query
     * @return intentHash The hash of the latest intent, or bytes32(0) if none exists
     * @dev WARNING: This function does not validate signer authorization. Use getLatestAuthorizedIntentHashByType for security-critical applications.
     */
    function getLatestIntentHashByType(string calldata intentType, string calldata symbol) 
        external view returns (bytes32 intentHash) {
        bytes32 compositeKey = getCompositeKey(intentType, symbol);
        return latestIntentByTypeAndSymbol[compositeKey];
    }
    
    /**
     * @notice Gets the latest intent for a specific intent type and symbol
     * @param intentType The type of intent to query
     * @param symbol The symbol to query
     * @return intent The latest intent details
     * @dev Reverts if the latest intent is from an unauthorized signer
     */
    function getLatestIntentByType(string calldata intentType, string calldata symbol) 
        external view returns (OracleIntentUtils.OracleIntent memory intent) {
        bytes32 compositeKey = getCompositeKey(intentType, symbol);
        bytes32 intentHash = latestIntentByTypeAndSymbol[compositeKey];
        
        if (intentHash == bytes32(0)) revert NoIntentForSymbol();
        if (intents[intentHash].timestamp == 0) revert IntentNotFound();
        
        // Validate that the signer is still authorized
        address intentSigner = intents[intentHash].signer;
        if (!authorizedSigners[intentSigner]) revert SignerNotAuthorized(intentSigner);
        
        return intents[intentHash];
    }
    
} 