// SPDX-License-Identifier: MIT OR Apache-2.0
pragma solidity 0.8.29;

import {Ownable} from "@openzeppelin/contracts/access/Ownable.sol";

import {IMessageRecipient} from "./interfaces/IMessageRecipient.sol";
import {IInterchainSecurityModule, ISpecifiesInterchainSecurityModule} from "./interfaces/IInterchainSecurityModule.sol";
import {IMailbox} from "./interfaces/IMailbox.sol";
import {IPostDispatchHook} from "./interfaces/hooks/IPostDispatchHook.sol";
import {ProtocolFeeHook} from "./ProtocolFeeHook.sol";

import {TypeCasts} from "./libs/TypeCasts.sol";

using TypeCasts for address;

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
contract PushOracleReceiver is
    Ownable,
    IMessageRecipient,
    ISpecifiesInterchainSecurityModule
{
    /// @notice Reference to the interchain security module.
    IInterchainSecurityModule public interchainSecurityModule;

    /// @notice Address for the post-dispatch payment hook.
    address payable public paymentHook;

    /// @notice only Message from this mailbox will be handled
    address public trustedMailBox;

    /// @notice Error thrown when a zero address is provided where a valid address is required.
    error ZeroAddress();

    /**
     * @notice Structure representing an oracle data update.
     * @param timestamp The timestamp when the data was recorded.
     * @param value The numerical value associated with the key.
     */
    struct Data {
        uint128 timestamp;
        uint128 value;
    }

    uint256 public gasUsedPerTx = 97440; // Default gas used

    /// @notice Mapping of oracle data updates by key.
    mapping(string => Data) public updates;

    /// @notice Emitted when a new oracle data message is received.
    event ReceivedMessage(string key, uint128 timestamp, uint128 value);

    /// @notice Emitted when a call is received (currently unused).
    event ReceivedCall(address indexed caller, uint256 amount, string message);

    /// @notice Emitted when the trusted mailbox address is updated.
    event TrustedMailBoxUpdated(
        address indexed previousMailBox,
        address indexed newMailBox
    );

    /// @notice Emitted when the interchain security module address is updated.
    event InterchainSecurityModuleUpdated(
        address indexed previousISM,
        address indexed newISM
    );

    /// @notice Emitted when the payment hook address is updated.
    event PaymentHookUpdated(
        address indexed previousPaymentHook,
        address indexed newPaymentHook
    );

    /// @notice Ensures that the provided address is not a zero address.
    modifier validateAddress(address _address) {
        if (_address == address(0)) revert ZeroAddress();
        _;
    }

    /**
     * @notice Handles incoming interchain messages by decoding the payload and updating state.
     * @param _origin The origin domain identifier.
     * @param _sender The sender's address (in bytes32 format).
     * @param _data The encoded payload containing the oracle data.
     */
    function handle(
        uint32 _origin,
        bytes32 _sender,
        bytes calldata _data
    ) external payable override validateAddress(paymentHook) {
        require(msg.sender == trustedMailBox, "Unauthorized Mailbox");

        // Decode the incoming data into its respective components.
        (string memory key, uint128 timestamp, uint128 value) = abi.decode(
            _data,
            (string, uint128, uint128)
        );

        // Ensure the new timestamp is more recent
        if (updates[key].timestamp >= timestamp) {
            return; // Ignore outdated data
        }

        // Update the stored oracle data.
        Data memory newData = Data({timestamp: timestamp, value: value});
        updates[key] = newData;

        emit ReceivedMessage(key, timestamp, value);

        // Calculate the transaction fee based on gas used and gas price.
        uint256 gasPrice = tx.gasprice;
        uint256 fee = ProtocolFeeHook(payable(paymentHook)).gasUsedPerTx() *
            gasPrice;

        // Transfer the fee to the payment hook.
        bool success;
        {
            (success, ) = paymentHook.call{value: fee}("");
        }

        require(success, "Fee transfer failed");
    }

    /**
     * @notice Sets the interchain security module.
     * @dev Only the contract owner can call this function.
     * @param _ism The address of the new interchain security module.
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
     * @notice Sets the payment hook address.
     * @dev Only the contract owner can call this function.
     * @param _paymentHook The address of the new payment hook.
     */
    function setPaymentHook(
        address payable _paymentHook
    ) external onlyOwner validateAddress(_paymentHook) {
        emit PaymentHookUpdated(paymentHook, _paymentHook);
        paymentHook = _paymentHook;
    }

    /**
     * @notice Sets the trusted mailbox address.
     * @dev Only the contract owner can call this function.
     * @param _mailbox The address of the new trusted mailbox.
     */
    function setTrustedMailBox(
        address _mailbox
    ) external onlyOwner validateAddress(_mailbox) {
        emit TrustedMailBoxUpdated(trustedMailBox, _mailbox);
        trustedMailBox = _mailbox;
    }

    receive() external payable {}

    fallback() external payable {}
}
