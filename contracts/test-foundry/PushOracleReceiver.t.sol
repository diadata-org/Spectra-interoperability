// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

import "forge-std/Test.sol";
import "../contracts/PushOracleReceiver.sol";
import "../contracts/interfaces/IMailbox.sol";
import "../contracts/interfaces/IInterchainSecurityModule.sol";
import "../contracts/interfaces/hooks/IPostDispatchHook.sol";
import "../contracts/UserWallet.sol";
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

contract MockHook is IPostDispatchHook {
    function quoteDispatch(
        bytes calldata,
        bytes calldata
    ) external pure override returns (uint256) {
        return 0;
    }

    function postDispatch(
        bytes calldata,
        bytes calldata
    ) external payable override {}

    function hookType() external pure override returns (uint8) {
        return 1;
    }

    function supportsMetadata(
        bytes calldata
    ) external pure override returns (bool) {
        return true;
    }

    receive() external payable {}
}

contract MockMailbox is IMailbox {
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

contract MockUserWallet {
    uint256 public balance = 10 ether;

     function deductFee(uint256 amount) external   {
        require(address(this).balance >= amount, "Insufficient balance");
        (bool sent, ) = msg.sender.call{value: amount}("");
        require(sent, "Transfer failed");
     }

    function setBalance(uint256 amount) external {
        balance = amount;
    }
}

contract MockWalletFactory {
    address public mockWallet;

    constructor(address _mockWallet) {
        mockWallet = _mockWallet;
    }

    function getAddress(address) external view returns (address) {
        return mockWallet;
    }
}

contract PushOracleReceiverTest is Test {
    PushOracleReceiver receiver;
    MockMailbox mailbox;
    MockISM ism;
    MockHook hook;
    MockUserWallet userWallet;
    MockWalletFactory walletFactory;

    address owner = address(0x1);
    address user = address(0x2);
    uint32 destinationDomain = 1;

    event ReceivedMessage(string key, uint128 timestamp, uint128 value);

    function setUp() public {
        vm.startPrank(owner);

        receiver = new PushOracleReceiver();
        mailbox = new MockMailbox();
        ism = new MockISM();
        hook = new MockHook();
        userWallet = new MockUserWallet();
        walletFactory = new MockWalletFactory(address(userWallet));

        receiver.setInterchainSecurityModule(address(ism));
        receiver.setPaymentHook(payable(hook));
        receiver.setWalletFactory(address(walletFactory));
        receiver.setTrustedMailBox(address(mailbox));

        vm.stopPrank();
    }

    function testInitialState() public {
        assertEq(receiver.owner(), owner);
        assertEq(address(receiver.interchainSecurityModule()), address(ism));
        assertEq(receiver.paymentHook(), address(hook));
        assertEq(receiver.walletFactory(), address(walletFactory));
        assertEq(receiver.feeFromUserWallet(), false);
    }

    function testSetFeeSource() public {
        vm.prank(owner);
        receiver.setFeeSource(true);
        assertTrue(receiver.feeFromUserWallet());
    }

    function testSetFeeSourceUnauthorized() public {
        vm.prank(user);
        vm.expectRevert("Ownable: caller is not the owner");
        receiver.setFeeSource(true);
    }

    function testHandleMessage() public {
        string memory key = "BTC/USD";
        uint128 timestamp = uint128(block.timestamp);
        uint128 value = 50000;

        bytes memory data = abi.encode(key, timestamp, value);
        bytes32 sender = bytes32(uint256(uint160(user)));

        vm.deal(address(mailbox), 1 ether);
        vm.prank(address(mailbox));
        receiver.handle{value: 0.1 ether}(destinationDomain, sender, data);

  

        // (
        //     string memory storedKey,
        //     uint128 storedTimestamp,
        //     uint128 storedValue
        // ) = receiver.receivedData();
        // assertEq(storedKey, key);
        // assertEq(storedTimestamp, timestamp);
        // assertEq(storedValue, value);
    }

    function testHandleMessageWithUserWallet() public {
        vm.prank(owner);
        receiver.setFeeSource(true);

        string memory key = "ETH/USD";
        uint128 timestamp = uint128(block.timestamp);
        uint128 value = 3000;

        bytes memory data = abi.encode(key, timestamp, value);
        bytes32 sender = bytes32(uint256(uint160(user)));
        vm.deal(address(userWallet), 10 ether);

        uint256 initialBalance = userWallet.balance();
        uint256 expectedFee = 97440 * tx.gasprice;

        vm.prank(address(mailbox));
        receiver.handle(destinationDomain, sender, data);

     }

 

    function testSetPaymentHook() public {
        address newHook = address(0x4);
        vm.prank(owner);
        receiver.setPaymentHook(payable(newHook));
        assertEq(receiver.paymentHook(), newHook);
    }

    function testSetWalletFactory() public {
        address newFactory = address(0x5);
        vm.prank(owner);
        receiver.setWalletFactory(newFactory);
        assertEq(receiver.walletFactory(), newFactory);
    }

    function testReceiveFunction() public {
        vm.deal(address(this), 1 ether);
        (bool success, ) = address(receiver).call{value: 0.1 ether}("");
        assertTrue(success);
    }

    function testHandleMessageInsufficientBalance() public {
        vm.prank(owner);
        receiver.setFeeSource(false);

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

    function testHandleMessageInsufficientWalletBalance() public {
        vm.prank(owner);
        receiver.setFeeSource(true);

        userWallet.setBalance(0);

        string memory key = "BTC/USD";
        uint128 timestamp = uint128(block.timestamp);
        uint128 value = 50000;

        bytes memory data = abi.encode(key, timestamp, value);
        bytes32 sender = bytes32(uint256(uint160(user)));

        vm.prank(address(mailbox));
        vm.expectRevert("Fee deduction failed");
        receiver.handle(destinationDomain, sender, data);
    }
}
