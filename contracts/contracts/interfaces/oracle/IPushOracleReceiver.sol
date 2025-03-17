// SPDX-License-Identifier: GPL-3.0
pragma solidity 0.8.29;

import {IMessageRecipient} from "../IMessageRecipient.sol";
import {ISpecifiesInterchainSecurityModule} from "../IInterchainSecurityModule.sol";

interface IPushOracleReceiver is  
    IMessageRecipient,
    ISpecifiesInterchainSecurityModule 
{
    // @notice Thrown when the address is invalid
    error InvalidAddress();

    // @notice Thrown when the mailbox address is unauthorized
    error UnauthorizedMailbox();

    // @notice Thrown when there is no balance in the contract to withdraw from
    error NoBalanceToWithdraw();

    // @notice Thrown when the transfer of any amount fails
    error AmountTransferFailed();

    // @notice Emitted when stuck funds are recovered
    // @param recipient The address that received the funds
    // @param amount The amount of funds recovered
    event TokensRecovered(address indexed recipient, uint256 amount);

    // @notice Emitted when a message is received for the new update value
    // @param key The key of the update
    // @param timestamp The timestamp of the update
    // @param value The value of the update
    event ReceivedMessage(string key, uint128 timestamp, uint128 value);
 
    // @notice Emitted when the trusted mailbox is updated
    // @param previousMailBox The previous mailbox address
    // @param newMailBox The new mailbox address
    event TrustedMailBoxUpdated(
        address indexed previousMailBox,
        address indexed newMailBox
    );

    // @notice Emitted when the interchain security module is updated
    // @param previousISM The previous interchain security module address
    // @param newISM The new interchain security module address
    event InterchainSecurityModuleUpdated(
        address indexed previousISM,
        address indexed newISM
    );

    // @notice Emitted when the payment hook is updated
    // @param previousPaymentHook The previous payment hook address
    // @param newPaymentHook The new payment hook address
    event PaymentHookUpdated(
        address indexed previousPaymentHook,
        address indexed newPaymentHook
    );


    struct Data {
        uint128 timestamp;
        uint128 value;
    }

    /**
     * @notice Handles incoming interchain messages by decoding the payload and updating state
     * @param _origin The origin domain identifier
     * @param _sender The sender's address (in bytes32 format)
     * @param _data The encoded payload containing the oracle data
     */
    function handle(
        uint32 _origin,
        bytes32 _sender,
        bytes calldata _data
    ) external payable;

    /**
     * @notice Sets the interchain security module.
     * @dev restricted to onlyOwner
     * @param _ism The address of the new interchain security module.
     */
    function setInterchainSecurityModule(address _ism) external;

    /**
     * @notice Sets the payment hook address
     * @dev restricted to onlyOwner
     * @param _paymentHook The address of the new payment hook.
     */
    function setPaymentHook( address payable _paymentHook) external;

    /**
     * @notice Sets the trusted mailbox address.
     * @dev restricted to onlyOwner
     * @param _mailbox The address of the new trusted mailbox.
     */
    function setTrustedMailBox(address _mailbox) external;

    /**
     * @notice Withdraws stuck funds to the specified address
     * @dev restricted to onlyOwner
     * @param receiver The address to receive the funds.
     */
    function retrieveLostTokens(address receiver) external;
}
