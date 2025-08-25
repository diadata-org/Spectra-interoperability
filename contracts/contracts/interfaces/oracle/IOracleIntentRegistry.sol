 pragma solidity ^0.8.29;

 import "../../libs/OracleIntentUtils.sol";
 


interface IOracleIntentRegistry {
    function getLatestPrice(string memory symbol) external view returns (uint256 price, uint256 timestamp, string memory source);
    function getIntent(bytes32 intentHash) external view returns (OracleIntentUtils.OracleIntent memory);
    function latestIntentBySymbol(string memory) external view returns (bytes32);
}