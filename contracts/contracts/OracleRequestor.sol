// SPDX-License-Identifier: MIT OR Apache-2.0
pragma solidity >=0.8.0;

import { Ownable } from "@openzeppelin/contracts/access/Ownable.sol";

import { IMessageRecipient } from "./interfaces/IMessageRecipient.sol";
import {
    IInterchainSecurityModule,
    ISpecifiesInterchainSecurityModule
} from "./interfaces/IInterchainSecurityModule.sol";
import { IMailbox } from "./interfaces/IMailbox.sol";
import { IPostDispatchHook } from "./interfaces/hooks/IPostDispatchHook.sol";
import { TypeCasts } from "./libs/TypeCasts.sol";

using TypeCasts for address;

/**
 * @title OracleRequestor
 * @notice Dispatches interchain messages and handles incoming oracle data updates.
 */
contract OracleRequestor is Ownable, IMessageRecipient, ISpecifiesInterchainSecurityModule {
    /// @notice Reference to the interchain security module.
    IInterchainSecurityModule public interchainSecurityModule;

    /// @notice Stores the sender of the last received message.
    bytes32 public lastSender;

    /// @notice Stores the raw data of the last received message.
    bytes public lastData;



    /// @notice Address for the post-dispatch payment hook.
    address public paymentHook;

    /**
     * @notice Structure representing an oracle data update.
     * @param key The identifier for the data.
     * @param timestamp The timestamp when the data was recorded.
     * @param value The numerical value associated with the key.
     */
    struct Data {
        string key;
        uint128 timestamp;
        uint128 value;
    }
    
    /// @notice The most recent oracle data received.
    Data public receivedData;

    /// @notice Mapping of oracle data updates by key.
    mapping(string => Data) public updates;

    /// @notice Emitted when a new oracle data message is received.
    event ReceivedMessage(string key, uint128 timestamp, uint128 value);

    /// @notice Emitted when a call is received (currently unused).
    event ReceivedCall(address indexed caller, uint256 amount, string message);

    /**
     * @notice Dispatches an interchain message via the provided mailbox.
     * @param _mailbox The mailbox contract used to dispatch the message.
     * @param receiver The address of the message recipient.
     * @param _destinationDomain The destination domain identifier.
     * @param _messageBody The body of the message.
     * @return messageId The unique identifier of the dispatched message.
     */
    function request(
        IMailbox _mailbox,
        address receiver,
        uint32 _destinationDomain,
        bytes calldata _messageBody
    ) external payable returns (bytes32 messageId) {
        IPostDispatchHook hook = IPostDispatchHook(paymentHook);
        return _mailbox.dispatch{value: msg.value}(
            _destinationDomain,
            receiver.addressToBytes32(),
            _messageBody,
            "", // No additional data
            hook
        );
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
    ) external payable override {
        // Decode the incoming data into its respective components.
        (string memory key, uint128 timestamp, uint128 value) = abi.decode(
            _data,
            (string, uint128, uint128)
        );

        // Update the stored oracle data.
        Data memory newData = Data({ key: key, timestamp: timestamp, value: value });
        receivedData = newData;
        updates[key] = newData;

        emit ReceivedMessage(key, timestamp, value);

        lastSender = _sender;
        lastData = _data;
    }

    /**
     * @notice Sets the interchain security module.
     * @param _ism The address of the interchain security module.
     */
    function setInterchainSecurityModule(address _ism) external onlyOwner {
        interchainSecurityModule = IInterchainSecurityModule(_ism);
    }
}