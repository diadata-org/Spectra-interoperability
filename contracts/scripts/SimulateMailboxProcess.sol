// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "forge-std/Script.sol";
import "forge-std/console.sol";
import "../contracts/interfaces/oracle/IOracleTrigger.sol";
import "../contracts/hyperlane/Mailbox.sol";




interface IOracleRequestRecipient {
    function handle(uint32 _origin, bytes32 _sender, bytes calldata _data) external payable;
}

/**
 * @title OracleRequestRecipientSimulation
 * @dev A Foundry script to simulate calling handle() on the deployed contract.
 */
contract MailBoxSimulation is Script {
    address constant MAILBOX_CONTRACT = 0x598facE78a4302f11E3de0bee1894Da0b2Cb71F8; //mailbox
 
    address constant TX_SENDER = 0x4Db7E4E00401Db46c0Afb3f93E5E06A3E5966872;
 
    function run() external {

         runProcess();
    
    }

     function runProcess() public {
        vm.startBroadcast(TX_SENDER);

        Mailbox mb = Mailbox(MAILBOX_CONTRACT);

        console.log("Starting process() simulation...");

        // Encode the message
        bytes memory metadata = hex"00"; // Placeholder metadata
        bytes memory message = hex"03000b0bf20001892000000000000000000000000088d2dcbcc832a314b89818776c2b6286bd8579b800066eee000000000000000000000000031bd0523fd3aededc1b1eaddcb0fa164d49977600000000000000000000000000000000000000000000000000000000000000600000000000000000000000000000000000000000000000000000000067e2a47e0000000000000000000000000000000000000000000000000000002fe8b0f5bb00000000000000000000000000000000000000000000000000000000000000074554482f55534400000000000000000000000000000000000000000000000000"; // Example message

        bytes32 messageId = keccak256(message);

       

        // Call process()
        mb.process(metadata, message);

        console.log("Finished process() simulation.");

        vm.stopBroadcast();
    }
 
}