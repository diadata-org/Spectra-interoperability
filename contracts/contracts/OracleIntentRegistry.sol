// SPDX-License-Identifier: MIT
pragma solidity ^0.8.29;

/**
 * @title OracleIntentRegistry
 * @dev A contract for storing and managing oracle intents across chains
 */
contract OracleIntentRegistry {
    // Intent structure
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
    
    // Intent data structure for batch processing
    struct IntentData {
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
    
    // Mapping from intent hash to intent
    mapping(bytes32 => OracleIntent) public intents;
    
    // Mapping from symbol to latest intent hash
    mapping(string => bytes32) public latestIntentBySymbol;
    
    // Mapping of authorized signers
    mapping(address => bool) public authorizedSigners;
    
    // Mapping to track processed intents
    mapping(bytes32 => bool) public processedIntents;
    
    // EIP-712 domain separator
    bytes32 private immutable DOMAIN_SEPARATOR;
    
    // EIP-712 type hash
    bytes32 private constant ORACLE_INTENT_TYPEHASH = keccak256(
        "OracleIntent(string intentType,string version,uint256 chainId,uint256 nonce,uint256 expiry,string symbol,uint256 price,uint256 timestamp,string source)"
    );
    
    // Events
    event IntentRegistered(bytes32 indexed intentHash, string indexed symbol, uint256 price, uint256 timestamp, address signer);
    event SignerAuthorized(address indexed signer, bool status);
    event BatchIntentsRegistered(uint256 count);
    
    // Owner of the contract
    address public owner;
    
    // Modifiers
    modifier onlyOwner() {
        require(msg.sender == owner, "OracleIntentRegistry: caller is not the owner");
        _;
    }
    
    modifier onlyAuthorized() {
        require(authorizedSigners[msg.sender] || msg.sender == owner, "OracleIntentRegistry: caller is not authorized");
        _;
    }
    
    constructor() {
        owner = msg.sender;
        authorizedSigners[msg.sender] = true;
        
        // Create the EIP-712 domain separator
        DOMAIN_SEPARATOR = keccak256(
            abi.encode(
                keccak256("EIP712Domain(string name,string version,uint256 chainId,address verifyingContract,bytes32 salt)"),
                keccak256("DIA Oracle Intent"),
                keccak256("1"),
                block.chainid,
                address(this),
                bytes32(0)
            )
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
        require(block.timestamp <= expiry, "OracleIntentRegistry: intent has expired");
        
        // Verify the signer is authorized
        require(authorizedSigners[signer], "OracleIntentRegistry: signer is not authorized");
        
        // Create the intent hash for EIP-712
        bytes32 structHash = keccak256(
            abi.encode(
                ORACLE_INTENT_TYPEHASH,
                keccak256(bytes(intentType)),
                keccak256(bytes(version)),
                chainId,
                nonce,
                expiry,
                keccak256(bytes(symbol)),
                price,
                timestamp,
                keccak256(bytes(source))
            )
        );
        
        // Create the EIP-712 hash
        bytes32 hash = keccak256(
            abi.encodePacked(
                "\x19\x01",
                DOMAIN_SEPARATOR,
                structHash
            )
        );
        
        // Check if this intent has already been processed
        require(!processedIntents[hash], "OracleIntentRegistry: intent already processed");
        
        // Verify the signature and get the actual signer
        address recoveredSigner = recoverSigner(hash, signature);
        require(recoveredSigner == signer, "OracleIntentRegistry: invalid signature");
        
        // Mark the intent as processed
        processedIntents[hash] = true;
        
        // Store the intent with the recovered signer
        OracleIntent memory intent = OracleIntent({
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
            signer: recoveredSigner
        });
        
        // Use the EIP-712 hash as the intent hash
        bytes32 intentHash = hash;
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
    function registerMultipleIntents(IntentData[] memory intentsData) external {
        require(intentsData.length > 0, "OracleIntentRegistry: no intents provided");
        
        uint256 successCount = 0;
        
        for (uint256 i = 0; i < intentsData.length; i++) {
            IntentData memory data = intentsData[i];
            
            // Skip expired intents
            if (block.timestamp > data.expiry) {
                continue;
            }
            
            // Skip intents from unauthorized signers
            if (!authorizedSigners[data.signer]) {
                continue;
            }
            
            // Create the intent hash for EIP-712
            bytes32 structHash = keccak256(
                abi.encode(
                    ORACLE_INTENT_TYPEHASH,
                    keccak256(bytes(data.intentType)),
                    keccak256(bytes(data.version)),
                    data.chainId,
                    data.nonce,
                    data.expiry,
                    keccak256(bytes(data.symbol)),
                    data.price,
                    data.timestamp,
                    keccak256(bytes(data.source))
                )
            );
            
            // Create the EIP-712 hash
            bytes32 hash = keccak256(
                abi.encodePacked(
                    "\x19\x01",
                    DOMAIN_SEPARATOR,
                    structHash
                )
            );
            
            // Skip already processed intents
            if (processedIntents[hash]) {
                continue;
            }
            
            // Verify the signature
            if (recoverSigner(hash, data.signature) != data.signer) {
                continue;
            }
            
            // Mark the intent as processed
            processedIntents[hash] = true;
            
            // Store the intent
            OracleIntent memory intent = OracleIntent({
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
            
            // Use the EIP-712 hash as the intent hash
            bytes32 intentHash = hash;
            intents[intentHash] = intent;
            
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
        require(intentHash != bytes32(0), "OracleIntentRegistry: no intent found for symbol");
        
        OracleIntent memory intent = intents[intentHash];
        return (intent.price, intent.timestamp, intent.source);
    }
    
    /**
     * @dev Gets the intent details by hash
     * @param intentHash The hash of the intent
     * @return The intent details
     */
    function getIntent(bytes32 intentHash) external view returns (OracleIntent memory) {
        require(intents[intentHash].timestamp > 0, "OracleIntentRegistry: intent not found");
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
        require(newOwner != address(0), "OracleIntentRegistry: new owner is the zero address");
        owner = newOwner;
    }
    
    /**
     * @dev Recovers the signer address from a signature
     * @param hash The hash that was signed
     * @param signature The signature
     * @return The address of the signer
     */
    function recoverSigner(bytes32 hash, bytes memory signature) internal pure returns (address) {
        require(signature.length == 65, "OracleIntentRegistry: invalid signature length");
        
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
        
        require(v == 27 || v == 28, "OracleIntentRegistry: invalid signature 'v' value");
        
        return ecrecover(hash, v, r, s);
    }
    
    /**
     * @dev Returns the domain separator for EIP-712 signatures
     */
    function getDomainSeparator() external view returns (bytes32) {
        return DOMAIN_SEPARATOR;
    }
} 