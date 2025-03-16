// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "forge-std/Test.sol";
import "../contracts/PushOracleReceiver.sol";
import "../contracts/ProtocolFeeHook.sol";

import "../contracts/interfaces/IMailbox.sol";
import "../contracts/interfaces/IInterchainSecurityModule.sol";
import "../contracts/interfaces/hooks/IPostDispatchHook.sol";
 import "forge-std/console.sol";

contract MockISM is IInterchainSecurityModule {
    function verify(
        bytes calldata,
        bytes calldata
    ) external pure override returns (bool) {
        return true;
    }

    function moduleType() external pure override returns (uint8) {
        return 1;
    }
}

 

contract MockMailbox1 is IMailbox {
    function dispatch(
        uint32,
        bytes32,
        bytes calldata,
        bytes calldata,
        IPostDispatchHook
    ) external payable returns (bytes32) {
        return bytes32(uint256(1));
    }

    function process(bytes calldata, bytes calldata) external payable {}

    function dispatch(
        uint32,
        bytes32,
        bytes calldata
    ) external payable returns (bytes32) {
        return bytes32(uint256(1));
    }

    function dispatch(
        uint32,
        bytes32,
        bytes calldata,
        bytes calldata
    ) external payable returns (bytes32) {
        return bytes32(uint256(1));
    }

    function quoteDispatch(
        uint32,
        bytes32,
        bytes calldata
    ) external pure returns (uint256) {
        return 0;
    }

    function quoteDispatch(
        uint32,
        bytes32,
        bytes calldata,
        bytes calldata
    ) external pure returns (uint256) {
        return 0;
    }

    function quoteDispatch(
        uint32,
        bytes32,
        bytes calldata,
        bytes calldata,
        IPostDispatchHook
    ) external pure returns (uint256) {
        return 0;
    }

    function delivered(bytes32) external pure returns (bool) {
        return false;
    }

    function recipientIsm(
        address
    ) external pure returns (IInterchainSecurityModule) {
        return IInterchainSecurityModule(address(0));
    }

    function defaultIsm() external pure returns (IInterchainSecurityModule) {
        return IInterchainSecurityModule(address(0));
    }

    function defaultHook() external pure returns (IPostDispatchHook) {
        return IPostDispatchHook(address(0));
    }

    function requiredHook() external pure returns (IPostDispatchHook) {
        return IPostDispatchHook(address(0));
    }

    function localDomain() external pure returns (uint32) {
        return 1;
    }

    function latestDispatchedId() external pure returns (bytes32) {
        return bytes32(0);
    }
}


 

contract PushOracleReceiverTest is Test {
    PushOracleReceiver receiver;
    MockMailbox1 mailbox;
    MockISM ism;
    ProtocolFeeHook hook;
 
    address owner = address(0x1);
    address user = address(0x2);
    uint32 destinationDomain = 1;

    event ReceivedMessage(string key, uint128 timestamp, uint128 value);

    function setUp() public {
        vm.startPrank(owner);

        receiver = new PushOracleReceiver();
        mailbox = new MockMailbox1();
        ism = new MockISM();
        hook = new ProtocolFeeHook();
 
        receiver.setInterchainSecurityModule(address(ism));
        receiver.setPaymentHook(payable(hook));
         receiver.setTrustedMailBox(address(mailbox));

        vm.stopPrank();
    }

    function testInitialState() public {
        assertEq(receiver.owner(), owner);
        assertEq(address(receiver.interchainSecurityModule()), address(ism));
        assertEq(receiver.paymentHook(), address(hook));
      }

  

    function testHandleMessage() public {
        string memory key = "BTC/USD";
        uint128 timestamp = uint128(block.timestamp);
        uint128 value = 50000;
        console.log("-----");

        bytes memory data = abi.encode(key, timestamp, value);
        bytes32 sender = bytes32(uint256(uint160(user)));

        vm.deal(address(mailbox), 1 ether);
        vm.prank(address(mailbox));

         receiver.handle{value: 0.1 ether}(destinationDomain, sender, data);

  

        (
            uint128 storedTimestamp,
            uint128 storedValue
        ) = receiver.updates("BTC/USD");
        // assertEq(storedKey, key);
        assertEq(storedTimestamp, timestamp);
        assertEq(storedValue, value);
    }

       function testHandleMessageIncorrectTimeStamp() public {
        string memory key = "BTC/USD";
        uint128 timestamp = uint128(2);
        uint128 value = 50000;
        console.log("-----");

        bytes memory data = abi.encode(key, timestamp, value);
        bytes memory data_old = abi.encode(key, timestamp-1, value-10);

        bytes32 sender = bytes32(uint256(uint160(user)));

        vm.deal(address(mailbox), 1 ether);
        vm.prank(address(mailbox));

         receiver.handle{value: 0.1 ether}(destinationDomain, sender, data);

        vm.prank(address(mailbox));

        receiver.handle{value: 0.1 ether}(destinationDomain, sender, data_old);


  
  // It should have old data with higher timestamp

        (
             uint128 storedTimestamp,
            uint128 storedValue
        ) = receiver.updates("BTC/USD");

         console.log("----");
        assertEq(storedTimestamp, timestamp);
        assertEq(storedValue, value);
    }



 

    function testSetPaymentHook() public {
        address newHook = address(0x4);
        vm.prank(owner);
        receiver.setPaymentHook(payable(newHook));
        assertEq(receiver.paymentHook(), newHook);
    }

   

    function testReceiveFunction() public {
        vm.deal(address(this), 1 ether);
        (bool success, ) = address(receiver).call{value: 0.1 ether}("");
        assertTrue(success);
    }

    function testHandleMessageInsufficientBalance() public {
  
        string memory key = "BTC/USD";
        uint128 timestamp = uint128(block.timestamp);
        uint128 value = 50000;

        bytes memory data = abi.encode(key, timestamp, value);
        bytes32 sender = bytes32(uint256(uint160(user)));

 
        console.log("Gas Price before handle():", tx.gasprice); // gas price is set in foundry.toml

        vm.prank(address(mailbox));
        vm.expectRevert("Fee transfer failed");

        receiver.handle(destinationDomain, sender, data);
    }

}
