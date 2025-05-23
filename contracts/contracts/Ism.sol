// SPDX-License-Identifier: MIT OR Apache-2.0
pragma solidity 0.8.29;

import { IInterchainSecurityModule } from "./interfaces/IInterchainSecurityModule.sol";
import { Message } from "./libs/Message.sol";
import { TypeCasts } from "./libs/TypeCasts.sol";

import { IMailbox } from "./interfaces/IMailbox.sol";

import { Ownable } from "@openzeppelin/contracts/access/Ownable.sol";

using TypeCasts for address;

/// @title Interchain Security Module (ISM)
/// @notice A simple ISM implementation that verifies messages based on the sender address.
/// @dev The contract allows the owner to set the expected sender address and a flag to allow all messages.
contract Ism is IInterchainSecurityModule, Ownable {
    uint8 public constant override moduleType = uint8(Types.NULL);

    /// @notice Expected senders per origin domain. This can be OracleTrigger or RequestOracle contract address
    mapping(uint32 => mapping(address => bool)) private senderShouldBe;

    /// @notice Address of the trusted mailbox.
    address public trustedMailBox;

    /// @notice Emitted when the expected sender address is updated.
    /// @param originDomain origin chain address.
    /// @param previousSender The previous expected sender address.
    /// @param newSender The new expected sender address.
    event SenderShouldBeUpdated(
        uint32 indexed originDomain,
        address indexed previousSender,
        address indexed newSender
    );

    /// @notice Event emitted when the trusted mailbox is updated.
    event TrustedMailBoxUpdated(
        address indexed previousMailBox,
        address indexed newMailBox
    );

    /// @notice Emitted when a sender is added for an origin domain.
    event SenderAdded(uint32 indexed originDomain, address indexed sender);

    /// @notice Emitted when a sender is removed for an origin domain.
    event SenderRemoved(uint32 indexed originDomain, address indexed sender);

    error NoChangeInSenderAddress();

    error NoChangeInValue();

    error SenderAlreadyExists();
    error SenderDoesNotExist();

    error InvalidAddress();

    /// @notice Ensures that the provided address is not a zero address.
    modifier validateAddress(address _address) {
        if (_address == address(0)) revert InvalidAddress();
        _;
    }

    /// @notice Check if an address is a valid sender for a given origin domain.
    function isSenderAllowed(
        uint32 _originDomain,
        address _sender
    ) external view returns (bool) {
        return senderShouldBe[_originDomain][_sender];
    }

    /// @notice Add a sender for a specific origin domain.
    function addSenderShouldBe(
        uint32 _originDomain,
        address _sender
    ) external onlyOwner {
        if (senderShouldBe[_originDomain][_sender]) {
            revert SenderAlreadyExists();
        }
        senderShouldBe[_originDomain][_sender] = true;
        emit SenderAdded(_originDomain, _sender);
    }

    /// @notice Remove a sender for a specific origin domain.
    function removeSenderShouldBe(
        uint32 _originDomain,
        address _sender
    ) external onlyOwner {
        if (!senderShouldBe[_originDomain][_sender]) {
            revert SenderDoesNotExist();
        }
        delete senderShouldBe[_originDomain][_sender];
        emit SenderRemoved(_originDomain, _sender);
    }

    /// @notice Verifies a message based on the sender address.
    /// @param /* unused */
    /// @param _message The encoded message data from which the sender address is extracted.
    /// @return True if the message is verified, false otherwise.
    function verify(
        bytes calldata,
        bytes calldata _message
    ) public view returns (bool) {
        uint32 originDomain = Message.origin(_message);
        address sender = Message.senderAddress(_message);

        return senderShouldBe[originDomain][sender];
    }

    /// @notice Sets the trusted mailbox address.
    function setTrustedMailBox(
        address _mailbox
    ) external onlyOwner validateAddress(_mailbox) {
        emit TrustedMailBoxUpdated(trustedMailBox, _mailbox);
        trustedMailBox = _mailbox;
    }
}
