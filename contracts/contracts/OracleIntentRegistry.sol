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
        uint64 chainId;
        uint64 nonce;
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
    
    // Mapping from intent hash to intent
    mapping(bytes32 => OracleIntent) public intents;
    
    // Mapping from symbol to latest intent hash
    mapping(string => bytes32) public latestIntentBySymbol;
    
    // Mapping of authorized signers
    mapping(address => bool) public authorizedSigners;
    
    // Events
    event IntentRegistered(bytes32 indexed intentHash, string indexed symbol, uint256 price, uint256 timestamp, address signer);
    event SignerAuthorized(address indexed signer, bool status);
    
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
    }
    
    /**
     * @dev Registers a new oracle intent
     * @param intentType The type of intent (e.g., "OracleUpdate")
     * @param version The version of the intent format
     * @param chainId The chain ID where the intent originates
     * @param nonce A unique identifier for this intent
     * @param expiry When this intent expires (unix timestamp)
     * @param symbol The symbol of the oracle data
     * @param price The price value
     * @param timestamp The timestamp of the oracle data
     * @param source The source of the oracle data
     * @param signature The signature of the intent
     * @param signer The address of the signer
     */
    function registerIntent(
        string memory intentType,
        string memory version,
        uint64 chainId,
        uint64 nonce,
        uint256 expiry,
        string memory symbol,
        uint256 price,
        uint256 timestamp,
        string memory source,
        bytes memory signature,
        address signer
    ) external onlyAuthorized {
        // Check if the intent has expired
        require(block.timestamp <= expiry, "OracleIntentRegistry: intent has expired");
        
        // Verify the signer is authorized
        require(authorizedSigners[signer], "OracleIntentRegistry: signer is not authorized");
        
        // Create the intent hash
        bytes32 intentHash = keccak256(abi.encode(
            intentType,
            version,
            chainId,
            nonce,
            expiry,
            symbol,
            price,
            timestamp,
            source
        ));
        
        // Verify the signature using personal_sign format
        // Hash the message directly with Ethereum signed message prefix
        bytes32 ethSignedMessageHash = keccak256(abi.encodePacked("\x19Ethereum Signed Message:\n32", intentHash));
        
        // Verify the signature
        require(recoverSigner(ethSignedMessageHash, signature) == signer, "OracleIntentRegistry: invalid signature");
        
        // Store the intent
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
            signer: signer
        });
        
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
     * @dev Registers a new oracle intent from a JSON string
     * This function is provided for convenience when integrating with external systems
     * Implementation is not provided in this version
     */
    function registerIntentFromJSON() external view onlyAuthorized {
        // In a real implementation, you would parse the JSON and call registerIntent
        // This is a placeholder for demonstration purposes
        revert("OracleIntentRegistry: JSON parsing not implemented");
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
     * @dev Converts a bytes32 to a hex string
     * @param data The bytes32 to convert
     * @return The hex string
     */
    function toHexString(bytes32 data) internal pure returns (string memory) {
        bytes memory hexChars = "0123456789abcdef";
        bytes memory result = new bytes(64); // 32 bytes * 2 hex chars
        
        for (uint i = 0; i < 32; i++) {
            result[i*2] = hexChars[uint8(data[i] >> 4)];
            result[i*2+1] = hexChars[uint8(data[i] & 0x0f)];
        }
        
        return string(abi.encodePacked("0x", result));
    }
    
    /**
     * @dev Converts a uint to a string
     * @param value The uint to convert
     * @return The string representation
     */
    function uintToString(uint256 value) internal pure returns (string memory) {
        if (value == 0) {
            return "0";
        }
        
        uint256 temp = value;
        uint256 digits;
        
        while (temp != 0) {
            digits++;
            temp /= 10;
        }
        
        bytes memory buffer = new bytes(digits);
        
        while (value != 0) {
            digits -= 1;
            buffer[digits] = bytes1(uint8(48 + uint256(value % 10)));
            value /= 10;
        }
        
        return string(buffer);
    }
}