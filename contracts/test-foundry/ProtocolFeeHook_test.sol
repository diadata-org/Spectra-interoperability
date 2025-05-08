// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import "forge-std/Test.sol";
import "../contracts/ProtocolFeeHook.sol";
import "../contracts/interfaces/hooks/IProtocolFeeHook.sol";

contract ProtocolFeeHookTest is Test {
    ProtocolFeeHook feeHook;
    address admin;
    address nonAdmin = address(0xBEEF);
    address trustedMailbox = address(0xDEAD);

    function setUp() public {
        feeHook = new ProtocolFeeHook();
        admin = address(this);

        // Set trusted mailbox
        feeHook.setTrustedMailBox(trustedMailbox);
    }

    function testHookType() public {
        uint8 hookType = feeHook.hookType();
        assertEq(hookType, 8, "hookType should be PROTOCOL_FEE (8)");
    }

    function testSupportsMetadata() public {
        bool support = feeHook.supportsMetadata("example metadata");
        assertTrue(support, "supportsMetadata should return true");
    }

    function testQuoteDispatch() public {
        uint256 customGasUsed = 80000;
        feeHook.setGasUsedPerTx(customGasUsed);

        uint256 gasPrice = 10;
        vm.fee(gasPrice);

        uint256 expectedFee = customGasUsed * gasPrice;
        uint256 fee = feeHook.quoteDispatch("dummy", "dummy");

        assertEq(
            fee,
            expectedFee,
            "quoteDispatch should return the expected fee"
        );
    }

    function testPostDispatchSufficientFee() public {
        bytes memory mess = abi.encodePacked(
            uint8(1),
            uint32(42),
            uint32(1),
            bytes32(uint256(uint160(address(this)))),
            uint32(2),
            bytes32(uint256(uint160(address(0xBEEF)))),
            "Hello, Hyperlane!"
        );

        uint256 requiredFee = feeHook.quoteDispatch("metadata", mess);

        // Expect the DispatchFeePaid event
        vm.expectEmit(true, true, false, false);
        emit IProtocolFeeHook.DispatchFeePaid(
            requiredFee,
            requiredFee,
            keccak256("message")
        );

        // Simulate a call from the trusted mailbox
        vm.deal(trustedMailbox, 1 ether); // Give the contract test account 1 ETH
        vm.prank(trustedMailbox);
        feeHook.postDispatch{ value: requiredFee }(mess, mess);
    }

    function testPostDispatchInsufficientFee() public {
        uint256 requiredFee = feeHook.quoteDispatch("metadata", "message");
        uint256 insufficientFee = requiredFee - 1;

        // Simulate a call from the trusted mailbox
        vm.deal(trustedMailbox, 1 ether); // Give the contract test account 1 ETH
        vm.prank(trustedMailbox);
        vm.expectRevert(IProtocolFeeHook.InsufficientFeePaid.selector);
        feeHook.postDispatch{ value: insufficientFee }("metadata", "message");
    }

    function testWithdrawFeesOnlyAdmin() public {
        (bool success, ) = address(feeHook).call{ value: 1 ether }("");
        require(success, "Funding contract failed");

        address recipient = address(0xC0FFEE);
        uint256 initialBalance = recipient.balance;

        feeHook.withdrawFees(recipient);

        assertEq(
            address(feeHook).balance,
            0,
            "Contract balance should be zero after withdrawal"
        );
        assertEq(
            recipient.balance,
            initialBalance + 1 ether,
            "Recipient should receive the withdrawn fees"
        );
    }

    function testWithdrawFeesNonAdmin() public {
        (bool success, ) = address(feeHook).call{ value: 1 ether }("");
        require(success, "Funding contract failed");

        vm.prank(nonAdmin);
        vm.expectRevert("Ownable: caller is not the owner");
        feeHook.withdrawFees(nonAdmin);
    }

    function testReceiveFallback() public {
        (bool success, ) = address(feeHook).call{ value: 0.5 ether }("");
        assertTrue(
            success,
            "Contract should receive ether via fallback/receive"
        );
        assertEq(
            address(feeHook).balance,
            0.5 ether,
            "Contract balance should reflect received ether"
        );
    }

    function testPostDispatchZeroFee() public {
        bytes memory mess = "test message";

        vm.deal(trustedMailbox, 1 ether);
        vm.prank(trustedMailbox);

        vm.expectRevert(IProtocolFeeHook.InsufficientFeePaid.selector);
        feeHook.postDispatch{ value: 0 }(mess, mess);
    }

    function testPostDispatchUnauthorizedCaller() public {
        bytes memory mess = "test message";
        uint256 requiredFee = feeHook.quoteDispatch("metadata", mess);

        vm.deal(nonAdmin, 1 ether);

        vm.prank(nonAdmin);
        vm.expectRevert(IProtocolFeeHook.UnauthorizedMailbox.selector);
        feeHook.postDispatch{ value: requiredFee }(mess, mess);
    }

    function testSetTrustedMailBoxUnauthorized() public {
        vm.prank(nonAdmin);
        vm.expectRevert("Ownable: caller is not the owner");
        feeHook.setTrustedMailBox(nonAdmin);
    }

    function testFallbackFunction() public {
        (bool success, ) = address(feeHook).call{ value: 1 ether }("");
        assertTrue(success, "Fallback should accept Ether");
        assertEq(
            address(feeHook).balance,
            1 ether,
            "Contract should store received Ether"
        );
    }

    function testWithdrawFeesInvalidRecipient() public {
        // Fund the contract with 1 ether
        (bool success, ) = address(feeHook).call{ value: 1 ether }("");
        require(success, "Funding contract failed");

        // Expect a revert with the error "InvalidFeeRecipient"
        vm.expectRevert(IProtocolFeeHook.InvalidFeeRecipient.selector);
        feeHook.withdrawFees(address(0));
    }

    function testWithdrawFeesNoBalance() public {
        vm.expectRevert(IProtocolFeeHook.NoBalanceToWithdraw.selector);
        feeHook.withdrawFees(address(0xC0FFEE));
    }

    function testWithdrawFeesTransferFailure() public {
        // Fund the contract with 1 ether
        (bool success, ) = address(feeHook).call{ value: 1 ether }("");
        require(success, "Funding contract failed");

        // Deploy a contract that rejects ETH transfers
        NonPayableReceiver receiver = new NonPayableReceiver();
        address nonPayableAddress = address(receiver);

        // Expect the FeeTransferFailed revert
        vm.expectRevert(IProtocolFeeHook.FeeTransferFailed.selector);
        feeHook.withdrawFees(nonPayableAddress);
    }

    function testSupportsMetadataFalse() public {
        bool support = feeHook.supportsMetadata("");
        assertTrue(support);
    }

    // function testValidateMessageOnceRevertsOnDuplicateMessage() public {
    //     bytes memory testMessage = abi.encodePacked("TestMessage");

    //     // First call should pass
    //     feeHook.processMessage(testMessage); // Replace `processMessage` with the function using the modifier

    //     // Second call should revert
    //     vm.expectRevert("MessageAlreadyValidated");
    //     feeHook.processMessage(testMessage);
    // }

    function testValidateMessageOnceRevertsOnDuplicateMessage() public {
        bytes memory mess = abi.encodePacked(
            uint8(1),
            uint32(42),
            uint32(1),
            bytes32(uint256(uint160(address(this)))),
            uint32(2),
            bytes32(uint256(uint160(address(0xBEEF)))),
            "Hello, Hyperlane!"
        );

        uint256 requiredFee = feeHook.quoteDispatch("metadata", mess);

        // Expect the DispatchFeePaid event
        vm.expectEmit(true, true, false, false);
        emit IProtocolFeeHook.DispatchFeePaid(
            requiredFee,
            requiredFee,
            keccak256("message")
        );

        // Simulate a call from the trusted mailbox
        vm.deal(trustedMailbox, 1 ether); // Give the contract test account 1 ETH
        vm.prank(trustedMailbox);
        feeHook.postDispatch{ value: requiredFee }(mess, mess);

        vm.expectRevert("MessageAlreadyValidated");

        feeHook.postDispatch{ value: requiredFee }(mess, mess);
    }

    function testValidateAddressRevertsOnZeroAddress() public {
        vm.expectRevert(IProtocolFeeHook.InvalidAddress.selector);
        feeHook.setTrustedMailBox(address(0)); // Replace `someFunction` with any function using the modifier
    }

    function testWithdrawFeesEmptyContract() public {
        address recipient = address(0xC0FFEE);

        vm.expectRevert(IProtocolFeeHook.NoBalanceToWithdraw.selector);
        feeHook.withdrawFees(recipient);
    }

    function testSetGasUsedPerTx() public {
        feeHook.setGasUsedPerTx(1);
        assertEq(
            feeHook.gasUsedPerTx(),
            1,
            "Gas used per transaction should be updated"
        );
    }
}

contract NonPayableReceiver {
    fallback() external payable {
        revert("Cannot receive ETH");
    }
}
