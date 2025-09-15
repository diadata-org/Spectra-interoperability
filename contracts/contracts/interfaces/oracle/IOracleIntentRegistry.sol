 pragma solidity 0.8.29;

 import "../../libs/OracleIntentUtils.sol";
 


interface IOracleIntentRegistry {
    function getLatestPrice(string memory symbol) external view returns (uint256 price, uint256 timestamp, string memory source);
    function getIntent(bytes32 intentHash) external view returns (OracleIntentUtils.OracleIntent memory);
    
    function getCompositeKey(string memory intentType, string memory symbol) external pure returns (bytes32);
    function getLatestIntentHashByType(string calldata intentType, string calldata symbol) external view returns (bytes32);
    function getLatestIntentByType(string calldata intentType, string calldata symbol) external view returns (OracleIntentUtils.OracleIntent memory);
    function getDomainSeparator() external view returns (bytes32);
}