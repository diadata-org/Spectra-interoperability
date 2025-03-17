// SPDX-License-Identifier: MIT OR Apache-2.0
pragma solidity 0.8.29;

import {Ownable} from "@openzeppelin/contracts/access/Ownable.sol";

import {IMessageRecipient} from "../interfaces/IMessageRecipient.sol";
import {IInterchainSecurityModule, ISpecifiesInterchainSecurityModule} from "../interfaces/IInterchainSecurityModule.sol";
import {IMailbox} from "../interfaces/IMailbox.sol";
import {IPostDispatchHook} from "../interfaces/hooks/IPostDispatchHook.sol";
import {TypeCasts} from "../libs/TypeCasts.sol";


using TypeCasts for address;

contract RequestBasedOracleExample is
    Ownable,
    IMessageRecipient,
    ISpecifiesInterchainSecurityModule
{
    IInterchainSecurityModule public interchainSecurityModule;
    bytes32 public lastSender;
    bytes public lastData;

    address public lastCaller;
    string public lastCallMessage;

    struct Data {
        string key;
        uint128 timestamp;
        uint128 value;
    }
    Data public receivedData;

    mapping(string => Data) public updates;


    event ReceivedMessage(
        string key,
        uint128 timestamp,
        uint128 value
    );


        function request(
        IMailbox _mailbox,
        address reciever,
        uint32 _destinationDomain,
        bytes calldata _messageBody

    ) external payable returns (bytes32 messageId) {
        // bytes memory messageBody = abi.encode("aa", 111111, 11);

        return _mailbox.dispatch{value: msg.value}(
            _destinationDomain,
            reciever.addressToBytes32(),
            _messageBody,
            bytes(""),
            IPostDispatchHook(0x0000000000000000000000000000000000000000)
        );
    }


    event ReceivedCall(address indexed caller, uint256 amount, string message);

    function handle(
        uint32 _origin,
        bytes32 _sender,
        bytes calldata _data
    ) external payable virtual override {
        (string memory  key, uint128 timestamp, uint128 value) = abi.decode(
            _data,
            (string, uint128, uint128)
        );
        receivedData = Data({key: key, timestamp: timestamp, value: value});

        updates[key] = receivedData;
        emit ReceivedMessage(key,timestamp,value);
        lastSender = _sender;
        lastData = _data;
    }
 

    function setInterchainSecurityModule(address _ism) external onlyOwner {
        interchainSecurityModule = IInterchainSecurityModule(_ism);
    }
}
