// SPDX-License-Identifier: MIT
pragma solidity ^0.8.19;

import "forge-std/Script.sol";
import "../contracts/RandomRequestManager.sol";

contract DeploySomniaRandomManager is Script {
    function run() external {
        uint256 deployerPrivateKey = vm.envUint("PRIVATE_KEY");
        
        vm.startBroadcast(deployerPrivateKey);
        
        RandomRequestManager randomManager = new RandomRequestManager();
        
        console.log("RandomRequestManager deployed at:", address(randomManager));
        
        vm.stopBroadcast();
    }
}