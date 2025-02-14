pragma solidity ^0.8.26;

import {IMailbox} from "../interfaces/IMailbox.sol";
import {IInterchainSecurityModule} from "../interfaces/IInterchainSecurityModule.sol";
import {IPostDispatchHook} from "../interfaces/hooks/IPostDispatchHook.sol";


contract MockMailbox is IMailbox {
    uint32 public override localDomain;
    mapping(bytes32 => bool) public  deliveredMessages;
    IInterchainSecurityModule public override defaultIsm;
    IPostDispatchHook public override defaultHook;
    IPostDispatchHook public override requiredHook;

    bytes32 public lastMessageId;
    bytes32 public override latestDispatchedId;

    constructor(uint32 _localDomain) {
        localDomain = _localDomain;
    }

    function delivered(bytes32 messageId) external view override returns (bool) {
        return deliveredMessages[messageId];
    }

    function dispatch(
        uint32 destinationDomain,
        bytes32 recipientAddress,
        bytes calldata messageBody
    ) external payable override returns (bytes32 messageId) {
        messageId = keccak256(abi.encodePacked(destinationDomain, recipientAddress, messageBody, block.timestamp));
        latestDispatchedId = messageId;
        emit Dispatch(msg.sender, destinationDomain, recipientAddress, messageBody);
        emit DispatchId(messageId);
    }

    function quoteDispatch(
        uint32 destinationDomain,
        bytes32 recipientAddress,
        bytes calldata messageBody
    ) external view override returns (uint256 fee) {
        return 0; // Mock fee calculation
    }

    function dispatch(
        uint32 destinationDomain,
        bytes32 recipientAddress,
        bytes calldata body,
        bytes calldata defaultHookMetadata
    ) external payable override returns (bytes32 messageId) {
        messageId = keccak256(abi.encodePacked(destinationDomain, recipientAddress, body, defaultHookMetadata, block.timestamp));
        latestDispatchedId = messageId;
        emit Dispatch(msg.sender, destinationDomain, recipientAddress, body);
        emit DispatchId(messageId);
    }

    function quoteDispatch(
        uint32 destinationDomain,
        bytes32 recipientAddress,
        bytes calldata messageBody,
        bytes calldata defaultHookMetadata
    ) external view override returns (uint256 fee) {
        return 0; // Mock fee calculation
    }

    function dispatch(
        uint32 destinationDomain,
        bytes32 recipientAddress,
        bytes calldata body,
        bytes calldata customHookMetadata,
        IPostDispatchHook customHook
    ) external payable override returns (bytes32 messageId) {
        messageId = keccak256(abi.encodePacked(destinationDomain, recipientAddress, body, customHookMetadata, block.timestamp));
        latestDispatchedId = messageId;
        emit Dispatch(msg.sender, destinationDomain, recipientAddress, body);
        emit DispatchId(messageId);
    }

    function quoteDispatch(
        uint32 destinationDomain,
        bytes32 recipientAddress,
        bytes calldata messageBody,
        bytes calldata customHookMetadata,
        IPostDispatchHook customHook
    ) external view override returns (uint256 fee) {
        return 0; // Mock fee calculation
    }

    function process(bytes calldata metadata, bytes calldata message) external payable override {
        bytes32 messageId = keccak256(abi.encodePacked(metadata, message));
        deliveredMessages[messageId] = true;
        emit ProcessId(messageId);
        emit Process(localDomain, keccak256(metadata), msg.sender);
    }

    function recipientIsm(address recipient) external view override returns (IInterchainSecurityModule module) {
        return defaultIsm;
    }
}