// SPDX-License-Identifier: GPL-3.0
pragma solidity 0.8.29;

import "@openzeppelin/contracts/security/ReentrancyGuard.sol";
import { IProtocolFeeHook } from "./interfaces/hooks/IProtocolFeeHook.sol";
import { Ownable } from "@openzeppelin/contracts/access/Ownable.sol";
import { Message } from "./libs/Message.sol";

/** @title ProtocolFeeHook
 * @notice This contract implements a post-dispatch hook that requires a fee
 * @author Diadata.org
 * to be paid after a message dispatch. The required fee is calculated based on
 * the current transaction gas price.
 */
contract ProtocolFeeHook is IProtocolFeeHook, Ownable, ReentrancyGuard {
    using Message for bytes;

    /// @notice Gas used per transaction, adjustable by the owner
    uint256 public gasUsedPerTx = 97440; 
    /// @notice Minimum base fee in wei, adjustable by the owner
    uint256 public minFeeWei = 1; 

    /// @notice only Message from this mailbox will be handled
    address public trustedMailBox;
    
    /// @notice Tracks if a message has been validated to prevent double processing
    mapping(bytes32 => bool) public messageValidated;

    /// @notice Enum representing different hook types
    /// @return The type of hook
    function hookType() external pure override returns (uint8) {
        return uint8(Types.PROTOCOL_FEE);
    }

   /// @notice Modifier to validate that an address is not the zero address
    modifier validateAddress(address _address) {
        if (_address == address(0)) revert InvalidAddress();
        _;
    }

    /// @notice Checks if the hook supports metadata
    /// @return True if the hook supports metadata
    function supportsMetadata(
        bytes calldata
    ) external pure override returns (bool) {
        return true;
    }


    /// @notice Ensures a message is only validated once
    modifier validateMessageOnce(bytes calldata _message) {
        bytes32 messageId = _message.id();
        if (messageValidated[messageId]) revert MessageAlreadyValidated();
        _;
        messageValidated[messageId] = true;
    }


   /**
    * @notice Handles post-dispatch logic for messages
    * @param metadata The metadata associated with the message
    * @param message The message payload
    */
    function postDispatch(
        bytes calldata metadata,
        bytes calldata message
    ) external payable override validateMessageOnce(message) {
        bytes32 messageId = message.id();
        if (msg.sender != trustedMailBox) revert UnauthorizedMailbox();

        uint256 requiredFee = quoteDispatch(metadata, message);

        if (msg.value < requiredFee) revert InsufficientFeePaid();

        emit DispatchFeePaid(requiredFee, msg.value, messageId);
    }

    /**
     * @notice Calculates the required fee for message dispatch
     * @dev Combines dynamic gas cost with fixed base fee
     * @return Total fee in wei required to process the dispatch
     */
    function quoteDispatch(
        bytes calldata,
        bytes calldata
    ) public view override returns (uint256) {
        uint256 gasPrice = tx.gasprice;
        uint256 cost = (gasUsedPerTx * gasPrice) + minFeeWei;
        return cost;
    }

 

    /** 
    * @notice Sets the gas used per tx
    * @param _gasUsedPerTx The new gas used per tx
    */
    function setGasUsedPerTx(uint256 _gasUsedPerTx) external onlyOwner {
        emit GasUsedPerTxUpdated(gasUsedPerTx, _gasUsedPerTx);
        gasUsedPerTx = _gasUsedPerTx;
    }

    /**
     * @notice Withdraws accumulated fees to a specified recipient
     * @param feeRecipient The address to receive the withdrawn fees
     * @param amount The amount of fees to withdraw
     */
    function withdrawFees(address feeRecipient, uint256 amount) external onlyOwner nonReentrant {
        if (feeRecipient == address(0)) revert InvalidFeeRecipient();
        uint256 balance = address(this).balance;
        if (balance == 0) revert NoBalanceToWithdraw();
        if (amount > balance) revert InsufficientBalance();  


        emit FeesWithdrawn(feeRecipient, amount);

        (bool success, ) = payable(feeRecipient).call{ value: amount }("");
        if (!success) revert FeeTransferFailed();
    }


    /** 
     * @notice Sets the trusted mailbox address
     * @param _mailbox The new trusted mailbox address
     */
    function setTrustedMailBox(
        address _mailbox
    ) external onlyOwner validateAddress(_mailbox) {
        emit TrustedMailBoxUpdated(trustedMailBox, _mailbox);
        trustedMailBox = _mailbox;
    }

    /** 
     * @notice Sets the minimum base fee in wei
     * @param _minFeeWei The new minimum fee in wei
     */
    function setMinFeeWei(uint256 _minFeeWei) external onlyOwner {
        emit MinFeeWeiUpdated(minFeeWei, _minFeeWei);
        minFeeWei = _minFeeWei;
    }

    /// @notice Allows the contract to receive ether
    receive() external payable {}

    /// @notice Fallback function to receive ether
    fallback() external payable {}
}
