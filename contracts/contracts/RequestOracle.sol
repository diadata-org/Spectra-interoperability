// SPDX-License-Identifier: MIT OR Apache-2.0
pragma solidity >=0.8.0;

import {Ownable} from "@openzeppelin/contracts/access/Ownable.sol";

import {IMessageRecipient} from "./interfaces/IMessageRecipient.sol";
import {IInterchainSecurityModule, ISpecifiesInterchainSecurityModule} from "./interfaces/IInterchainSecurityModule.sol";
import {IMailbox} from "./interfaces/IMailbox.sol";
import {IPostDispatchHook} from "./interfaces/hooks/IPostDispatchHook.sol";
import {TypeCasts} from "./libs/TypeCasts.sol";
import "forge-std/console.sol";

using TypeCasts for address;


/**
 * @title RequestOracle
 * @dev This contract receives and stores oracle data from an interchain messaging protocol.
 * It allows sending requests and handling received oracle responses.
 */
contract RequestOracle is
    Ownable,
    IMessageRecipient,
    ISpecifiesInterchainSecurityModule
{
    IInterchainSecurityModule public interchainSecurityModule;
    bytes32 public lastSender;
    bytes public lastData;

    address public lastCaller;
    string public lastCallMessage;
    address public paymentHook;

    struct Data {
        string key;
        uint128 timestamp;
        uint128 value;
    }
    Data public receivedData;

    mapping(string => Data) public updates;

    event ReceivedMessage(string key, uint128 timestamp, uint128 value);


  /**
     * @notice Sends a request to a receiver on another chain.
     * @param _mailbox The mailbox contract responsible for dispatching messages.
     * @param receiver The receiver address on the destination chain.
     * @param _destinationDomain The domain ID of the destination chain.
     * @param _messageBody The encoded message payload.
     * @return messageId The ID of the dispatched message.
     */
    function request(
        IMailbox _mailbox,
        address receiver,
        uint32 _destinationDomain,
        bytes calldata _messageBody
    ) external payable returns (bytes32 messageId) {
        // bytes memory messageBody = abi.encode("aa", 111111, 11);
 
        IPostDispatchHook merkleTree = IPostDispatchHook(paymentHook);
        


        return
            _mailbox.dispatch{value: msg.value}(
                _destinationDomain,
                receiver.addressToBytes32(),
                _messageBody,
                bytes(""),
                merkleTree
            );
    }

    event ReceivedCall(address indexed caller, uint256 amount, string message);

 /**
     * @notice Handles received messages from the interchain mailbox.
     * @param _origin The domain ID of the sender chain.
     * @param _sender The sender address in bytes32 format.
     * @param _data The encoded oracle data received.
     */
    function handle(
        uint32 _origin,
        bytes32 _sender,
        bytes calldata _data
    ) external payable virtual override {
        // check who is calling this

        (string memory key, uint128 timestamp, uint128 value) = abi.decode(
            _data,
            (string, uint128, uint128)
        );
        receivedData = Data({key: key, timestamp: timestamp, value: value});

        updates[key] = receivedData;
        emit ReceivedMessage(key, timestamp, value);
        lastSender = _sender;
        lastData = _data;
    }

 /**
     * @notice Sets the interchain security module contract.
     * @dev Can only be called by the owner.
     * @param _ism The address of the interchain security module.
     */
    function setInterchainSecurityModule(address _ism) external onlyOwner {
        interchainSecurityModule = IInterchainSecurityModule(_ism);
    }

    /**
     * @notice Sets the payment hook contract.
     * @dev Can only be called by the owner.
     * @param _paymentHook The address of the payment hook contract.
     */

    function setPaymentHook(address _paymentHook) external onlyOwner {
        paymentHook = _paymentHook;
    }

    receive() external payable {}

    fallback() external payable {}
}
