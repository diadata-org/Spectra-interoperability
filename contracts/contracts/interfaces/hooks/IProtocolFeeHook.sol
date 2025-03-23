// SPDX-License-Identifier: GPL-3.0
pragma solidity 0.8.29;

import { IPostDispatchHook } from "./IPostDispatchHook.sol";

interface IProtocolFeeHook is IPostDispatchHook {
    // @notice Thrown when a message is already validated
    error MessageAlreadyValidated();

    // @notice Thrown when the fee paid is insufficient
    error InsufficientFeePaid();

    // @notice Thrown when the fee recipient is invalid
    error InvalidFeeRecipient();

    // @notice Thrown when there is no balance to withdraw
    error NoBalanceToWithdraw();

    // @notice Thrown when the mailbox address is unauthorized
    error UnauthorizedMailbox();

    // @notice Thrown when the fee transfer fails
    error FeeTransferFailed();

    /// @notice Error thrown when an invalid address (zero address) is used.
    error InvalidAddress();

    // @notice Emitted when a dispatch fee is paid
    // @param requiredFee The required fee
    // @param actualFee The actual fee paid
    // @param messageId The id of the message
    event DispatchFeePaid(
        uint256 requiredFee,
        uint256 actualFee,
        bytes32 messageId
    );

    // @notice Emitted when the trusted mailbox is updated
    // @param previousMailBox The previous mailbox address
    // @param newMailBox The new mailbox address
    event TrustedMailBoxUpdated(
        address indexed previousMailBox,
        address indexed newMailBox
    );

    // @notice Emitted when the gas used per tx is updated
    // @param previousGasUsed The previous gas used per tx
    // @param newGasUsed The new gas used per tx
    event GasUsedPerTxUpdated(uint256 previousGasUsed, uint256 newGasUsed);

    // @notice Emitted when the fees are withdrawn
    // @param feeRecipient The address of the fee recipient
    // @param amount The amount of fees withdrawn
    event FeesWithdrawn(address indexed feeRecipient, uint256 amount);

    // @notice Sets the gas used per tx
    // @param _gasUsedPerTx The new gas used per tx
    function setGasUsedPerTx(uint256 _gasUsedPerTx) external;

    // @notice Withdraws the fees
    // @param feeRecipient The address of the fee recipient
    function withdrawFees(address feeRecipient) external;

    // @notice Returns the gas used per tx
    // @return The gas used per tx
    function gasUsedPerTx() external view returns (uint256);

    // @notice Returns the validation status of a message
    // @param messageId The id of the message
    // @return status of the message
    function messageValidated(bytes32 messageId) external view returns (bool);
}
