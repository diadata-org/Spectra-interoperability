// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import "forge-std/Test.sol";
import "../contracts/ProtocolFeeHook.sol";

contract ProtocolFeeHookTest is Test {
    ProtocolFeeHook feeHook;
    address admin;
    address nonAdmin = address(0xBEEF);

    function setUp() public {
        feeHook = new ProtocolFeeHook();
        admin = address(this);
    }

    function testHookType() public {
        // The enum in IPostDispatchHook should have PROTOCOL_FEE as its 9th item (index 8).
        uint8 hookType = feeHook.hookType();
        assertEq(hookType, 8, "hookType should be PROTOCOL_FEE (8)");
    }

    function testSupportsMetadata() public {
        // supportsMetadata should always return true.
        bool support = feeHook.supportsMetadata("example metadata");
        assertTrue(support, "supportsMetadata should return true");
    }

    function testQuoteDispatch() public {
        // Calculate expected fee using the current tx.gasprice.
        uint256 gasPrice = tx.gasprice;
        uint256 expectedFee = 2 * 97440 * gasPrice;
        uint256 fee = feeHook.quoteDispatch("dummy", "dummy");
        assertEq(fee, expectedFee, "quoteDispatch should return the expected fee");
    }

    function testPostDispatchSufficientFee() public {
        // Get the fee required by the hook.
        uint256 requiredFee = feeHook.quoteDispatch("metadata", "message");
        
        // Expect the event DispatchFeePaid to be emitted with requiredFee and actual fee.
        vm.expectEmit(true, true, false, true);
        emit ProtocolFeeHook.DispatchFeePaid(requiredFee, requiredFee);
        
        // Call postDispatch with sufficient fee.
        feeHook.postDispatch{value: requiredFee}("metadata", "message");
    }

    // function testPostDispatchInsufficientFee() public {
    //     uint256 requiredFee = feeHook.quoteDispatch("metadata", "message");
    //     uint256 insufficientFee = requiredFee - 1;
    //     vm.expectRevert("Insufficient fee paid");
    //     feeHook.postDispatch{value: insufficientFee}("metadata", "message");
    // }

    function testWithdrawFeesOnlyAdmin() public {
        // Fund the contract with 1 ether.
        (bool success, ) = address(feeHook).call{value: 1 ether}("");
        require(success, "Funding contract failed");

        // Record initial balance for recipient.
        address recipient = address(0xC0FFEE);
        uint256 initialRecipientBalance = recipient.balance;

        // Withdraw fees as admin.
        feeHook.withdrawFees(recipient);

        // Verify that the contract balance is now zero.
        assertEq(address(feeHook).balance, 0, "Contract balance should be zero after withdrawal");

        // Verify that the recipient's balance increased by 1 ether.
        uint256 finalRecipientBalance = recipient.balance;
        assertEq(finalRecipientBalance, initialRecipientBalance + 1 ether, "Recipient should receive the withdrawn fees");
    }

    function testWithdrawFeesNonAdmin() public {
        // Fund the contract with 1 ether.
        (bool success, ) = address(feeHook).call{value: 1 ether}("");
        require(success, "Funding contract failed");

        vm.prank(nonAdmin);
        vm.expectRevert("Not admin");
        feeHook.withdrawFees(nonAdmin);
    }

    function testReceiveFallback() public {
        // Send ether using an empty calldata call to trigger receive() or fallback.
        (bool success, ) = address(feeHook).call{value: 0.5 ether}("");
        assertTrue(success, "Contract should receive ether via fallback/receive");
        assertEq(address(feeHook).balance, 0.5 ether, "Contract balance should reflect received ether");
    }
}