// SPDX-License-Identifier: GPL-3.0
pragma solidity 0.8.29;

import { Ownable } from "@openzeppelin/contracts/access/Ownable.sol";
import { IPushOracleReceiver } from "./interfaces/oracle/IPushOracleReceiver.sol";
import { IInterchainSecurityModule } from "./interfaces/IInterchainSecurityModule.sol";
import { ProtocolFeeHook } from "./ProtocolFeeHook.sol";
import { TypeCasts } from "./libs/TypeCasts.sol";

/**
 * @title PushOracleReceiver
 * @notice Handles incoming oracle data updates and ensures security via Hyperlane.
 * @dev Implements IMessageRecipient and ISpecifiesInterchainSecurityModule.
 *
 * ## Data Flow:
 * - Go Feeder Service → OracleTrigger (reads price from metadata) → Hyperlane → PushOracleReceiver
 *
 * This contract receives and processes oracle updates from the DIA chain.
 *
 * ## Funding Mechanism:
 * - The contract should hold enough balance to cover transaction fees for updates.
 * - Each update requires two transactions: one on the DIA chain and another on the chain where PushOracleReceiver is deployed (Destination).
 * - The contract deducts the fee for each  Destination transaction and transfers it to the ProtocolFeeHook.
 *
 * ## Security Constraints:
 * - PushOracleReceiver processes messages only from the trusted mailbox.
 * - The oracle trigger address must be whitelisted in the ISM (Interchain Security Module) of PushOracleReceiver.
 */
abstract contract PushOracleReceiver is IPushOracleReceiver, Ownable {
    using TypeCasts for address;

    /// @notice Reference to the interchain security module
    IInterchainSecurityModule public interchainSecurityModule;

    /// @notice Address for the post-dispatch payment hook
    address payable public paymentHook;

    /// @notice only Message from this mailbox will be handled
    address public trustedMailBox;

    /// @notice Mapping of oracle data updates by key
    mapping(string => Data) public updates;

    /// @notice Error thrown when an ISM is not set (zero address) is used.
    error InvalidISMAddress();

    /// @notice Ensures that the provided address is not a zero address
    modifier validateAddress(address _address) {
        if (_address == address(0)) revert InvalidAddress();
        _;
    }

    /**
     * @dev See {IPushOracleReceiver-handle}.
     */
    /* solhint-disable no-unused-vars */
    function handle(
        uint32 _origin,
        bytes32 _sender,
        bytes calldata _data
    ) external payable override validateAddress(paymentHook) {
        if (msg.sender != trustedMailBox) revert UnauthorizedMailbox();
        if (address(interchainSecurityModule) == address(0))
            revert InvalidISMAddress();

        // Decode the incoming data into its respective components.
        (string memory key, uint128 timestamp, uint128 value) = abi.decode(
            _data,
            (string, uint128, uint128)
        );

        // Ensure the new timestamp is more recent
        if (updates[key].timestamp >= timestamp) {
            return; // Ignore outdated data
        }

        // Update the stored oracle data
        Data memory newData = Data({ timestamp: timestamp, value: value });
        updates[key] = newData;

        emit ReceivedMessage(key, timestamp, value);

        // Calculate the transaction fee based on gas used and gas price.
        uint256 gasPrice = tx.gasprice;
        uint256 fee = ProtocolFeeHook(payable(paymentHook)).gasUsedPerTx() *
            gasPrice;

        // Transfer the fee to the payment hook.
        bool success;
        {
            (success, ) = paymentHook.call{ value: fee }("");
        }

        if (!success) revert AmountTransferFailed();
    }
    /* solhint-disable no-unused-vars */

    /**
     * @dev See {IPushOracleReceiver-setInterchainSecurityModule}.
     */
    function setInterchainSecurityModule(
        address _ism
    ) external onlyOwner validateAddress(_ism) {
        emit InterchainSecurityModuleUpdated(
            address(interchainSecurityModule),
            _ism
        );
        interchainSecurityModule = IInterchainSecurityModule(_ism);
    }

    /**
     * @dev See {IPushOracleReceiver-setPaymentHook}.
     */
    function setPaymentHook(
        address payable _paymentHook
    ) external onlyOwner validateAddress(_paymentHook) {
        emit PaymentHookUpdated(paymentHook, _paymentHook);
        paymentHook = _paymentHook;
    }

    /**
     * @dev See {IPushOracleReceiver-setTrustedMailBox}.
     */
    function setTrustedMailBox(
        address _mailbox
    ) external onlyOwner validateAddress(_mailbox) {
        emit TrustedMailBoxUpdated(trustedMailBox, _mailbox);
        trustedMailBox = _mailbox;
    }

    /**
     * @dev See {IPushOracleReceiver-retrieveLostTokens}.
     */
    function retrieveLostTokens(
        address receiver
    ) external onlyOwner validateAddress(receiver) {
        uint256 balance = address(this).balance;
        if (balance == 0) revert NoBalanceToWithdraw();

        (bool success, ) = payable(receiver).call{ value: balance }("");
        if (!success) revert AmountTransferFailed();
        emit TokensRecovered(receiver, balance);
    }
    receive() external payable {}

    fallback() external payable {}
}
