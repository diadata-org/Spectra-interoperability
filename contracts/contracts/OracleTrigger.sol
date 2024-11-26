// SPDX-License-Identifier: MIT OR Apache-2.0
pragma solidity >=0.8.0;

import {IInterchainGasPaymaster} from "./interfaces/IInterchainGasPaymaster.sol";
import {IMessageRecipient} from "./interfaces/IMessageRecipient.sol";
import {IMailbox} from "./interfaces/IMailbox.sol";
import {IPostDispatchHook} from "./interfaces/hooks/IPostDispatchHook.sol";
import {IInterchainSecurityModule, ISpecifiesInterchainSecurityModule} from "./interfaces/IInterchainSecurityModule.sol";
import {TypeCasts} from "./libs/TypeCasts.sol";
import {Ownable} from "@openzeppelin/contracts/access/Ownable.sol";


interface IDIAOracleV2 {
    function getValue(
        string memory key
    ) external view returns (uint128, uint128);
}

using TypeCasts for address;

contract OracleTrigger is IMessageRecipient,Ownable,ISpecifiesInterchainSecurityModule {
    struct ChainConfig {
        address MailBox;
        address RecipientAddress;
    }
        IInterchainSecurityModule public interchainSecurityModule;


        bytes public lastData;


    mapping(uint32 => ChainConfig) public chains;

    address public metadataContract;
 

    event ChainAdded(
        uint32 indexed chainId,
        address MailBox,
        address RecipientAddress
    );
    event ChainUpdated(
        uint32 indexed chainId,
        address MailBox,
        address RecipientAddress
    );

    function addChain(
        uint32 chainId,
        address MailBox,
        address RecipientAddress
    ) public {
        require(
            chains[chainId].MailBox == address(0),
            "Chain ID already exists"
        );
        chains[chainId] = ChainConfig(MailBox, RecipientAddress);
        emit ChainAdded(chainId, MailBox, RecipientAddress);
    }
    function updateMetadataContract(address newMetadata) external  onlyOwner{
        metadataContract = newMetadata;
    }

    function updateChain(
        uint32 chainId,
        address MailBox,
        address RecipientAddress
    ) public {
        require(
            chains[chainId].MailBox != address(0),
            "Chain ID does not exist"
        );
        chains[chainId] = ChainConfig(MailBox, RecipientAddress);
        emit ChainUpdated(chainId, MailBox, RecipientAddress);
    }

    function viewChain(uint32 chainId) public view returns (address, address) {
        require(
            chains[chainId].MailBox != address(0),
            "Chain ID does not exist"
        );
        ChainConfig memory config = chains[chainId];
        return (config.MailBox, config.RecipientAddress);
    }

    uint256 public constant HANDLE_GAS_AMOUNT = 50_000;

    event Handled(bytes32 blockHash);

    function dispatchToSelf(
        IMailbox _mailbox,
        uint32 _destinationDomain,
        bytes calldata _messageBody
    ) external payable {
        // TODO: handle topping up?
        _mailbox.dispatch{value: msg.value}(
            _destinationDomain,
            address(this).addressToBytes32(),
            _messageBody
        );
    }

    function dispatchToChain(
        uint32 _destinationDomain,
        string memory key
    ) external payable {
        ChainConfig memory config = chains[_destinationDomain];
        require(config.MailBox != address(0), "Chain configuration not found");

        uint128 currValue;
        uint128 currTimestamp;
        IDIAOracleV2 currOracle = IDIAOracleV2(
            metadataContract
        );

        try currOracle.getValue(key) returns (
            uint128 value,
            uint128 timestamp
        ) {
            currValue = value;
            currTimestamp = timestamp;
        } catch {
            currValue = 14;
            currTimestamp = 14;
        }

        bytes memory messageBody = abi.encode(key, currTimestamp, currValue);
        IMailbox(config.MailBox).dispatch{value: msg.value}(
            _destinationDomain,
            config.RecipientAddress.addressToBytes32(),
            messageBody
        );
    }



    function dispatch(
        IMailbox _mailbox,
        uint32 _destinationDomain,
        address recipientAddress,
        string memory key
    ) external payable {
        uint128 currValue;
        uint128 currTimestamp;
        IDIAOracleV2 currOracle = IDIAOracleV2(
            metadataContract
        );

        try currOracle.getValue(key) returns (
            uint128 value,
            uint128 timestamp
        ) {
            currValue = value;
            currTimestamp = timestamp;
        } catch {
            currValue = 14;
            currTimestamp = 14;
        }

        bytes memory messageBody = abi.encode(key, currTimestamp, currValue);

        _mailbox.dispatch{value: msg.value}(
            _destinationDomain,
            recipientAddress.addressToBytes32(),
            messageBody
        );
    }

        function dispatchTest(
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

 

 
    function dispatchToSelf(
        IMailbox _mailbox,
        uint32 _destinationDomain,
        bytes calldata _messageBody,
        IPostDispatchHook hook
    ) external payable {
        // TODO: handle topping up?
        _mailbox.dispatch{value: msg.value}(
            _destinationDomain,
            address(this).addressToBytes32(),
            _messageBody,
            bytes(""),
            hook
        );
    }


 

   function handle(
        uint32 _origin,
        bytes32 _sender,
        bytes calldata _data
    ) external payable virtual override {


        // (string memory  key) = abi.decode(
        //     _data,
        //     (string)
        // );

        // this.dispatchToChain(_origin,key);
 
        // // updates[key] = receivedData;
        // // emit ReceivedMessage(key,timestamp,value);
        // // lastSender = _sender;
        // lastData = _data;
    }
 

    function previousBlockHash() internal view returns (bytes32) {
        return blockhash(block.number - 1);
    }

    function setInterchainSecurityModule(address _ism) external onlyOwner {
        interchainSecurityModule = IInterchainSecurityModule(_ism);
    }
}

