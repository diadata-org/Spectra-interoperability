// SPDX-License-Identifier: MIT OR Apache-2.0
pragma solidity 0.8.29;

import {Ownable} from "@openzeppelin/contracts/access/Ownable.sol";

import {Pausable} from "@openzeppelin/contracts/security/Pausable.sol";

import {IMessageRecipient} from "./interfaces/IMessageRecipient.sol";
import {IInterchainSecurityModule, ISpecifiesInterchainSecurityModule} from "./interfaces/IInterchainSecurityModule.sol";
import {IMailbox} from "./interfaces/IMailbox.sol";
import {IPostDispatchHook} from "./interfaces/hooks/IPostDispatchHook.sol";
import {TypeCasts} from "./libs/TypeCasts.sol";
// import "forge-std/console.sol";

using TypeCasts for address;

/**
 * @title RequestOracle
 * @dev This contract receives and stores oracle data from an interchain messaging protocol.Whitelisted in Hyperlane
 * It allows sending requests and handling received oracle responses.
 */
contract RequestOracle is
    Ownable,
    IMessageRecipient,
    Pausable,
    ISpecifiesInterchainSecurityModule
{
    IInterchainSecurityModule public interchainSecurityModule;
 
    address public paymentHook;

    address public trustedMailBox;

    struct Data {
         uint128 timestamp;
        uint128 value;
    }
    Data public receivedData;

    mapping(string => Data) public updates;

    event EmergencyPaused(address indexed admin);
    event EmergencyUnpaused(address indexed admin);



    event TrustedMailBoxUpdated(address indexed previousMailBox, address indexed newMailBox);

    event InterchainSecurityModuleUpdated(address indexed previousISM, address indexed newISM);
    
    event PaymentHookUpdated(address indexed previousPaymentHook, address indexed newPaymentHook);



    event ReceivedMessage(string key, uint128 timestamp, uint128 value);

    error ZeroAddress();


       /// @notice Ensures that the provided address is not a zero address.
    modifier validateAddress(address _address) {
        if (_address == address(0)) revert ZeroAddress();
        _;
    }


    event RequestSent(
    address indexed sender,
    address indexed receiver,
    uint32 destinationDomain,
    bytes32 messageId,
    uint256 value
);

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
    ) external payable whenNotPaused validateAddress(paymentHook) returns (bytes32 messageId) {
 
        IPostDispatchHook hook = IPostDispatchHook(paymentHook);

         
          messageId =   _mailbox.dispatch{value: msg.value}(
                _destinationDomain,
                receiver.addressToBytes32(),
                _messageBody,
                bytes(""),
                hook
            );
        emit RequestSent(msg.sender, receiver, _destinationDomain, messageId, msg.value);

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
        require(msg.sender == trustedMailBox, "Unauthorized Mailbox");

        (string memory key, uint128 timestamp, uint128 value) = abi.decode(
            _data,
            (string, uint128, uint128)
        );
        receivedData = Data({ timestamp: timestamp, value: value});


          // Ensure the new timestamp is more recent
        if (updates[key].timestamp >= timestamp) {
            return; // Ignore outdated data
        }

        updates[key] = receivedData;
        emit ReceivedMessage(key, timestamp, value);
     }

    /**
     * @notice Sets the interchain security module contract.
     * @dev Can only be called by the owner.
     * @param _ism The address of the interchain security module.
     */
    function setInterchainSecurityModule(address _ism) external onlyOwner validateAddress(_ism) {
        emit InterchainSecurityModuleUpdated(address(interchainSecurityModule), _ism);  
        interchainSecurityModule = IInterchainSecurityModule(_ism);
    }

    function setTrustedMailBox(address _mailbox) external onlyOwner validateAddress(_mailbox) {
         emit TrustedMailBoxUpdated(trustedMailBox, _mailbox);  
         trustedMailBox = _mailbox;
    }

    /**
     * @notice Sets the payment hook contract.
     * @dev Can only be called by the owner.
     * @param _paymentHook The address of the payment hook contract.
     */

    function setPaymentHook(address _paymentHook) external onlyOwner validateAddress(_paymentHook) {
        emit PaymentHookUpdated(paymentHook, _paymentHook);  
        paymentHook = _paymentHook;
    }

    /// @notice Allows the owner to pause the contract in case of an emergency.
    function pauseContract() external onlyOwner {
        _pause();
        emit EmergencyPaused(msg.sender);
    }

    /// @notice Allows the owner to unpause the contract when it's safe.
    function unpauseContract() external onlyOwner {
        _unpause();
        emit EmergencyUnpaused(msg.sender);
    }

    receive() external payable {}

    fallback() external payable {}

      /**
    * @notice Withdraw ETH to reover stuck funds 
     */
    function withdrawETH(address payable recipient) external onlyOwner {
    require(recipient != address(0), "Invalid recipient");
     recipient.transfer(address(this).balance);
    }
}
