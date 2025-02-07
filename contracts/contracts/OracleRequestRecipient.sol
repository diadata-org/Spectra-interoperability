// SPDX-License-Identifier: MIT OR Apache-2.0
pragma solidity >=0.8.0;

import {Ownable} from "@openzeppelin/contracts/access/Ownable.sol";

import {IMessageRecipient} from "./interfaces/IMessageRecipient.sol";
import {IOracleTrigger} from "./interfaces/IOracleTrigger.sol";
import {TypeCasts} from "./libs/TypeCasts.sol";

import {IInterchainSecurityModule, ISpecifiesInterchainSecurityModule} from "./interfaces/IInterchainSecurityModule.sol";


/// @title OracleRequestRecipient
/// @notice Handles incoming cross-chain messages and triggers an oracle action via IOracleTrigger.
contract OracleRequestRecipient is
    Ownable,
    IMessageRecipient,
    ISpecifiesInterchainSecurityModule
{
    struct ChainConfig {
        address mailBox;
        address recipientAddress;
    }


    event ChainAdded(
        uint32 indexed chainId,
        address mailBox,
        address recipientAddress
    );
    event ChainUpdated(
        uint32 indexed chainId,
        address mailBox,
        address recipientAddress
    );

    mapping(uint32 => ChainConfig) public chains;

    IInterchainSecurityModule public interchainSecurityModule;
    bytes32 public lastSender;
    bytes public lastData;
    address public oracleTriggerAddress;
 

    address public lastCaller;
    string public lastCallMessage;

    struct Data {
        string key;
        uint128 timestamp;
        uint128 value;
    }
    Data public receivedData;

    mapping(string => Data) public updates;

    event ReceivedMessage(string key, uint128 timestamp, uint128 value);

    event ReceivedCall(address indexed caller, uint256 amount, string message);


   function addChain(
        uint32 chainId,
        address mailBox,
        address recipientAddress
    ) public onlyOwner {
        require(
            chains[chainId].mailBox == address(0),
            "Chain ID already exists"
        );
        chains[chainId] = ChainConfig(mailBox, recipientAddress);
        emit ChainAdded(chainId, mailBox, recipientAddress);
    }

     function updateChain(
        uint32 chainId,
        address mailBox,
        address recipientAddress
    ) public onlyOwner {
        require(
            chains[chainId].mailBox != address(0),
            "Chain ID does not exist"
        );
        chains[chainId] = ChainConfig(mailBox, recipientAddress);
        emit ChainUpdated(chainId, mailBox, recipientAddress);
    }

    function viewChain(uint32 chainId) public view returns (address, address) {
        require(
            chains[chainId].mailBox != address(0),
            "Chain ID does not exist"
        );
        ChainConfig memory config = chains[chainId];
        return (config.mailBox, config.recipientAddress);
    }


    /**
     * @notice Handles an incoming message.
     * @param _origin The originating chain ID.
     * @param _sender The sender's address encoded as bytes32.
     * @param _data The message data, expected to be ABI-encoded as a string.
     */
    function handle(
        uint32 _origin,
        bytes32 _sender,
        bytes calldata _data
    ) external payable virtual override {
        string memory key = abi.decode(_data, (string));

        ChainConfig memory config = chains[_origin];
        require(config.mailBox != address(0), "Chain configuration not found");
        require(oracleTriggerAddress != address(0), "Oracle trigger not set");

 
        emit  ReceivedCall(address(uint160(uint256(_sender))), 0, "");


        IOracleTrigger(oracleTriggerAddress)
            .dispatch(config.mailBox,_origin,address(uint160(uint256(_sender))), key);
       
        lastData = _data;
    }

    function setInterchainSecurityModule(address _ism) external onlyOwner {
        interchainSecurityModule = IInterchainSecurityModule(_ism);
    }

     function setOracleTriggerAddress(address _oracleTrigger) external onlyOwner {
        oracleTriggerAddress = _oracleTrigger;
    }

}
