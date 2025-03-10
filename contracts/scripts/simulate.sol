// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "forge-std/Script.sol";
import "forge-std/console.sol";
import "../contracts/interfaces/IOracleTrigger.sol";

interface IOracleRequestRecipient {
    function handle(uint32 _origin, bytes32 _sender, bytes calldata _data) external payable;
}

/**
 * @title OracleRequestRecipientSimulation
 * @dev A Foundry script to simulate calling handle() on the deployed contract.
 */
contract OracleRequestSimulation is Script {
    address constant DEPLOYED_CONTRACT = 0x371B09B87AB6c8f431281F7559C03B753b1328D2;
    address constant ORACTE_TRIGGER = 0x0e005CCB1A04a91EA12593F9338f0D060F533d6D;

    address constant TX_SENDER = 0xb7e28B14D76AAE43728AB15703521Fb3F7B599ff;
    uint32 constant ORIGIN = 1301;
    bytes32 constant SENDER = 0x000000000000000000000000b7e28b14d76aae43728ab15703521fb3f7b599ff;
    string constant KEY = "DIA/USD";

    function run() external {

        runRequestReceipentHandle();
        // runDispatch();
    
    }

     function runDispatch()  public{
        vm.startBroadcast(0x16cD72271498bcaD5aeB9f2D785bA82dC5AfA5E2);

        IOracleTrigger ot = IOracleTrigger(ORACTE_TRIGGER);

        // Encode the message body
        bytes memory messageBody = abi.encode(KEY);

        console.log("Simulating dispatch() call on contract:", ORACTE_TRIGGER);
        console.log("Sender:", TX_SENDER);
        console.log("Origin:", ORIGIN);
        console.logBytes32(SENDER);
        console.logBytes(messageBody);

        // Simulate calling the function
        ot.dispatch(ORIGIN, 0xc4666A676d92A3D774d95ddE2207634FEA546b50, KEY);

        vm.stopBroadcast();
    }

     function runRequestReceipentHandle() public {
        vm.startBroadcast(0x16cD72271498bcaD5aeB9f2D785bA82dC5AfA5E2);

        IOracleRequestRecipient recipient = IOracleRequestRecipient(DEPLOYED_CONTRACT);

        // Encode the message body
        bytes memory messageBody = abi.encode(KEY);

        console.log("Simulating handle() call on contract:", DEPLOYED_CONTRACT);
        console.log("Sender:", TX_SENDER);
        console.log("Origin:", ORIGIN);
        console.logBytes32(SENDER);
        console.logBytes(messageBody);

        // Simulate calling the function
        recipient.handle(ORIGIN, SENDER, messageBody);

        vm.stopBroadcast();
    }
}