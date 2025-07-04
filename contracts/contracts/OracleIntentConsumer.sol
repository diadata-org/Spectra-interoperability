// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

/**
 * @title OracleIntentConsumer
 * @dev A contract for consuming oracle intents from the L2 chain
 */
contract OracleIntentConsumer {
    // Intent structure (must match the structure in OracleIntentRegistry)
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
    }
    
    // Mapping from symbol to latest price
    mapping(string => uint256) public latestPrices;
    
    // Mapping from symbol to latest timestamp
    mapping(string => uint256) public latestTimestamps;
    
    // Mapping from symbol to latest source
    mapping(string => string) public latestSources;
    
    // Mapping of authorized relayers
    mapping(address => bool) public authorizedRelayers;
    
    // Events
    event PriceUpdated(string indexed symbol, uint256 price, uint256 timestamp, string source);
    event RelayerAuthorized(address indexed relayer, bool status);
    
    // Owner of the contract
    address public owner;
    uint64 public currentNonce; // Renamed from nonce to avoid shadowing

    // Modifiers
    modifier onlyOwner() {
        require(msg.sender == owner, "OracleIntentConsumer: caller is not the owner");
        _;
    }
    
    modifier onlyAuthorized() {
        require(authorizedRelayers[msg.sender] || msg.sender == owner, "OracleIntentConsumer: caller is not authorized");
        _;
    }
    
    constructor() {
        owner = msg.sender;
        authorizedRelayers[msg.sender] = true;
    }
    
    /**
     * @dev Updates the price from an oracle intent
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
    function updatePrice(
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
        require(block.timestamp <= expiry, "OracleIntentConsumer: intent has expired");
        
        // Check if the timestamp is newer than the latest timestamp
        require(timestamp > latestTimestamps[symbol], "OracleIntentConsumer: timestamp is not newer");
        
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
        
        // Verify the signature
        bytes32 ethSignedMessageHash = keccak256(abi.encodePacked("\x19Ethereum Signed Message:\n32", intentHash));
        require(recoverSigner(ethSignedMessageHash, signature) == signer, "OracleIntentConsumer: invalid signature");
        
        // Update the price
        latestPrices[symbol] = price;
        latestTimestamps[symbol] = timestamp;
        latestSources[symbol] = source;
        
        // Emit event
        emit PriceUpdated(symbol, price, timestamp, source);
    }
    
    /**
     * @dev Gets the latest price for a symbol
     * @param symbol The symbol to get the price for
     * @return price The latest price
     * @return timestamp The timestamp of the price
     * @return source The source of the price
     */
    function getLatestPrice(string memory symbol) external view returns (uint256 price, uint256 timestamp, string memory source) {
        require(latestTimestamps[symbol] > 0, "OracleIntentConsumer: no price found for symbol");
        return (latestPrices[symbol], latestTimestamps[symbol], latestSources[symbol]);
    }
    
    /**
     * @dev Authorizes or deauthorizes a relayer
     * @param relayer The address of the relayer
     * @param status The authorization status
     */
    function setRelayerAuthorization(address relayer, bool status) external onlyOwner {
        authorizedRelayers[relayer] = status;
        emit RelayerAuthorized(relayer, status);
    }
    
    /**
     * @dev Transfers ownership of the contract
     * @param newOwner The address of the new owner
     */
    function transferOwnership(address newOwner) external onlyOwner {
        require(newOwner != address(0), "OracleIntentConsumer: new owner is the zero address");
        owner = newOwner;
    }
    
    /**
     * @dev Recovers the signer address from a signature
     * @param hash The hash that was signed
     * @param signature The signature
     * @return The address of the signer
     */
    function recoverSigner(bytes32 hash, bytes memory signature) internal pure returns (address) {
        require(signature.length == 65, "OracleIntentConsumer: invalid signature length");
        
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
        
        require(v == 27 || v == 28, "OracleIntentConsumer: invalid signature 'v' value");
        
        return ecrecover(hash, v, r, s);
    }
    
    /**
     * @dev Attests a value by creating intent data
     * @param price The price value
     * @return The hash of the intent data
     */
    function attestValue(
        uint256 price,
        string memory symbol
    ) external onlyAuthorized returns (bytes32) {
        // Create the intent hash
        bytes32 intentHash = keccak256(abi.encode(
            "OracleUpdate",
            "1.0",
            block.chainid,
            currentNonce,
            block.timestamp + 3600,
            symbol,
            price,
            block.timestamp,
            "OracleIntentConsumer"
        ));
        
        // Increment nonce for next use
        currentNonce++;
        
        return intentHash;
    }
}