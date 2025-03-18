// SPDX-License-Identifier: GPL-3.0
pragma solidity 0.8.29;

import "@openzeppelin/contracts/security/ReentrancyGuard.sol";
import { IProtocolFeeHook } from "./interfaces/hooks/IProtocolFeeHook.sol";
import { Ownable } from "@openzeppelin/contracts/access/Ownable.sol";
import { Message } from "./libs/Message.sol";

/* @title ProtocolFeeHook
 * @notice This contract implements a post-dispatch hook that requires a fee
 * to be paid after a message dispatch. The required fee is calculated based on
 * the current transaction gas price.
 */
contract ProtocolFeeHook is IProtocolFeeHook, Ownable {
    using Message for bytes;

    uint256 public gasUsedPerTx = 97440; // Default gas used

    mapping(bytes32 messageId => bool validated) public messageValidated;

    function hookType() external pure override returns (uint8) {
        return uint8(Types.PROTOCOL_FEE);
    }

    function supportsMetadata(
        bytes calldata
    ) external pure override returns (bool) {
        return true;
    }

    function postDispatch(
        bytes calldata metadata,
        bytes calldata message
    ) external payable override {
        bytes32 messageId = message.id();
        if (messageValidated[messageId]) revert MessageAlreadyValidated();

        uint256 requiredFee = quoteDispatch(metadata, message);

        if (msg.value < requiredFee) revert InsufficientFeePaid();

        emit DispatchFeePaid(requiredFee, msg.value, messageId);
        messageValidated[messageId] = true;
    }

    function quoteDispatch(
        bytes calldata,
        bytes calldata
    ) public view override returns (uint256) {
        uint256 gasPrice = tx.gasprice;
        uint256 cost = gasUsedPerTx * gasPrice;
        return cost;
    }

    function setGasUsedPerTx(uint256 _gasUsedPerTx) external onlyOwner {
        emit GasUsedPerTxUpdated(gasUsedPerTx, _gasUsedPerTx);
        gasUsedPerTx = _gasUsedPerTx;
    }

    function withdrawFees(address feeRecipient) external onlyOwner {
        if (feeRecipient == address(0)) revert InvalidFeeRecipient();
        uint256 balance = address(this).balance;
        if (balance == 0) revert NoBalanceToWithdraw();

        (bool success, ) = payable(feeRecipient).call{ value: balance }("");
        if (!success) revert FeeTransferFailed();
        emit FeesWithdrawn(feeRecipient, balance);
    }

    receive() external payable {}

    fallback() external payable {}
}
