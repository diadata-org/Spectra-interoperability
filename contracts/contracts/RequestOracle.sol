// SPDX-License-Identifier: MIT OR Apache-2.0
pragma solidity >=0.8.0;

import {IInterchainGasPaymaster} from "./interfaces/IInterchainGasPaymaster.sol";
import {IMessageRecipient} from "./interfaces/IMessageRecipient.sol";
import {IMailbox} from "./interfaces/IMailbox.sol";
import {IPostDispatchHook} from "./interfaces/hooks/IPostDispatchHook.sol";
import {IInterchainSecurityModule, ISpecifiesInterchainSecurityModule} from "./interfaces/IInterchainSecurityModule.sol";
import {TypeCasts} from "./libs/TypeCasts.sol";

interface IDIAOracleV2 {
    function getValue(
        string memory key
    ) external view returns (uint128, uint128);
}

using TypeCasts for address;

contract RequestOracle is IMessageRecipient {
    struct ChainConfig {
        address MailBox;
        address RecipientAddress;
    }

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

    function dispatchToSelf1(
        IMailbox _mailbox,
        uint32 _destinationDomain
    ) external payable {
        // TODO: handle topping up?
                bytes memory messageBody = abi.encode("a");

        _mailbox.dispatch{value: msg.value}(
            _destinationDomain,
            address(this).addressToBytes32(),
            messageBody
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
            0xb77690Eb2E97E235Bbc198588166a6F7Cb69e008
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
            0xb77690Eb2E97E235Bbc198588166a6F7Cb69e008
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

    function uint128ToHexBytes(
        uint128 _input
    ) public pure returns (bytes memory) {
        bytes memory hexString = new bytes(32);
        for (uint256 i = 0; i < 16; i++) {
            uint8 nibble = uint8(_input >> (4 * (31 - 2 * i)));
            hexString[2 * i] = _toHexChar(nibble >> 4);
            hexString[2 * i + 1] = _toHexChar(nibble & 0x0f);
        }
        return hexString;
    }

    function _toHexChar(uint8 _value) internal pure returns (bytes1) {
        if (_value < 10) {
            return bytes1(uint8(_value + 48)); // ASCII '0' is 48
        } else {
            return bytes1(uint8(_value + 87)); // ASCII 'a' is 97
        }
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

    function handle(uint32, bytes32, bytes calldata) external payable override {
        bytes32 blockHash = previousBlockHash();
        bool isBlockHashEndIn0 = uint256(blockHash) % 16 == 0;
        require(!isBlockHashEndIn0, "block hash ends in 0");
        emit Handled(blockHash);
    }

    function previousBlockHash() internal view returns (bytes32) {
        return blockhash(block.number - 1);
    }
}
