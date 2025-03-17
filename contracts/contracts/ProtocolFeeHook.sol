// SPDX-License-Identifier: MIT OR Apache-2.0
pragma solidity 0.8.29;

import {IPostDispatchHook} from "./interfaces/hooks/IPostDispatchHook.sol";
import {Ownable} from "@openzeppelin/contracts/access/Ownable.sol";
import "@openzeppelin/contracts/security/ReentrancyGuard.sol";
import {Message} from "./libs/Message.sol";

/// @title ProtocolFeeHook
/// @notice This contract implements a post-dispatch hook that requires a fee
/// to be paid after a message dispatch. The required fee is calculated based on
/// the current transaction gas price.
contract ProtocolFeeHook is IPostDispatchHook, Ownable {
    using Message for bytes;
 
    uint256 public gasUsedPerTx = 97440; // Default gas used

    mapping(bytes32 messageId => bool validated) public messageValidated;

    event DispatchFeePaid(
        uint256 requiredFee,
        uint256 actualFee,
        bytes32 messageId
    );

    event GasUsedPerTxUpdated(uint256 previousGasUsed, uint256 newGasUsed);

    event FeesWithdrawn(address indexed feeRecipient, uint256 amount);

    function hookType() external pure override returns (uint8) {
        return uint8(Types.PROTOCOL_FEE);
    }

    function supportsMetadata(
        bytes calldata
    ) external pure override returns (bool) {
        return true;
    }

    /// @notice Executes the post-dispatch logic.
    /// @dev Calculates the required fee and reverts if insufficient ETH is provided.
    /// @param metadata The metadata (unused).
    /// @param message The message (unused).
    function postDispatch(
        bytes calldata metadata,
        bytes calldata message
    ) external payable override {
        bytes32 messageId = message.id();
        require(!messageValidated[messageId], "MessageAlreadyValidated");

        uint256 requiredFee = quoteDispatch(metadata, message);

        require(msg.value >= requiredFee, "Insufficient fee paid");

        emit DispatchFeePaid(requiredFee, msg.value, messageId);
        messageValidated[messageId] = true;
    }

    /**
     * @notice Get quote for gas usage, not works in view txs.
     */

    function quoteDispatch(
        bytes calldata,
        bytes calldata
    ) public view override returns (uint256) {
        uint256 gasPrice = tx.gasprice;
        uint256 cost = gasUsedPerTx * gasPrice;
        return cost;
    }

    /**
     * @notice Sets Gas used by update tx.
     * @param _gasUsedPerTx Gas Used.
     */
    function setGasUsedPerTx(uint256 _gasUsedPerTx) external onlyOwner {
        emit GasUsedPerTxUpdated(gasUsedPerTx, _gasUsedPerTx);
        gasUsedPerTx = _gasUsedPerTx;
    }

    function withdrawFees(address feeRecipient) external onlyOwner {
        require(feeRecipient != address(0), "Invalid feeRecipient");
        uint256 balance = address(this).balance;
        require(balance > 0, "No balance to withdraw");

        (bool success, ) = payable(feeRecipient).call{value: balance}("");
        require(success, "Fee transfer failed");
        emit FeesWithdrawn(feeRecipient, balance);
    }

    receive() external payable {}

    fallback() external payable {}
}
