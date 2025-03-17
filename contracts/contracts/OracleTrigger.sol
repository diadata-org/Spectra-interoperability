// SPDX-License-Identifier: GPL-3.0
pragma solidity 0.8.29;

import { AccessControlEnumerable } from "@openzeppelin/contracts/access/AccessControlEnumerable.sol";
import { ReentrancyGuard } from "@openzeppelin/contracts/security/ReentrancyGuard.sol";
import { IMailbox } from "./interfaces/IMailbox.sol";
import { IOracleTrigger } from "./interfaces/oracle/IOracleTrigger.sol";
import { TypeCasts } from "./libs/TypeCasts.sol";

interface IDIAOracleV2 {
    function getValue(
        string memory key
    ) external view returns (uint128, uint128);
}

/// @title OracleTrigger
/// @notice Reads the latest oracle value from metadata and dispatches it to the desired chain.
/// @dev Provides access control for managing chains and secure dispatching mechanisms.
/// @dev Only addresses with the DISPATCHER_ROLE can call dispatch functions.
contract OracleTrigger is
    IOracleTrigger,
    AccessControlEnumerable,
    ReentrancyGuard
{
    using TypeCasts for address;
    /// @notice Address of the mailbox contract responsible for interchain messaging.
    address private mailBox;

    /// @notice Mapping of chain IDs to their corresponding recipient addresses.
    mapping(uint32 => address) public chains;

    /// @notice Role identifier for contract owners.
    bytes32 public constant OWNER_ROLE = keccak256("OWNER_ROLE");

    /// @notice Role identifier for Dispatch function callers, i.e Feeder Service and OracleRequestReceipent.
    bytes32 public constant DISPATCHER_ROLE = keccak256("DISPATCHER_ROLE");

    /// @notice Address of the DIA oracle metadata contract.
    address public metadataContract;

    /// @notice Ensures that the provided address is not a zero address.
    modifier validateAddress(address _address) {
        if (_address == address(0)) revert InvalidAddress();
        _;
    }

    /// @notice Ensures that the given chain is configured.
    modifier validateChain(uint32 _chainId) {
        if (chains[_chainId] == address(0)) revert ChainNotConfigured(_chainId);
        _;
    }

    /// @notice Contract constructor that initializes the contract and assigns the deployer as the first owner.
    constructor() {
        _grantRole(DEFAULT_ADMIN_ROLE, msg.sender);
        _grantRole(OWNER_ROLE, msg.sender);
    }

    /// @notice Adds a new chain to the configuration
    /// @param chainId The chain ID of the new chain
    /// @param recipientAddress The address of the recipient contract on the new chain
    function addChain(
        uint32 chainId,
        address recipientAddress
    ) public onlyRole(OWNER_ROLE) validateAddress(recipientAddress) {
        if (chains[chainId] != address(0)) {
            revert ChainAlreadyExists(chainId);
        }
        chains[chainId] = recipientAddress;
        emit ChainAdded(chainId, recipientAddress);
    }

    /// @notice Updates the recipient address for a specific chain
    /// @param chainId The chain ID of the chain to update
    /// @param recipientAddress The new address of the recipient contract
    function updateChain(
        uint32 chainId,
        address recipientAddress
    )
        public
        onlyRole(OWNER_ROLE)
        validateAddress(recipientAddress)
        validateChain(chainId)
    {
        address oldRecipientAddress = chains[chainId];

        chains[chainId] = recipientAddress;
        emit ChainUpdated(chainId, oldRecipientAddress, recipientAddress);
    }

    /// @notice Retrieves the recipient address for a specific chain
    /// @param _chainId The chain ID of the chain to query
    /// @return The address of the recipient contract on the specified chain
    function viewChain(
        uint32 _chainId
    ) public view validateChain(_chainId) returns (address) {
        return chains[_chainId];
    }

    /// @notice Updates the metadata contract address
    /// @param newMetadata The new metadata contract address
    function updateMetadataContract(
        address newMetadata
    ) external onlyRole(OWNER_ROLE) validateAddress(newMetadata) {
        metadataContract = newMetadata;
        emit MetadataContractUpdated(newMetadata);
    }

    /**
     * @dev See {IOracleTrigger-dispatchToChain}.
     */
    function dispatchToChain(
        uint32 _destinationDomain,
        string memory key
    )
        external
        payable
        onlyRole(DISPATCHER_ROLE)
        validateChain(_destinationDomain)
        validateAddress(mailBox)
        nonReentrant
    {
        (uint128 currValue, uint128 currTimestamp) = _getOracleValue(key);

        bytes memory messageBody = abi.encode(key, currTimestamp, currValue);

        address recipient = chains[_destinationDomain];

        bytes32 messageId = IMailbox(mailBox).dispatch{ value: msg.value }(
            _destinationDomain,
            recipient.addressToBytes32(),
            messageBody
        );

        emit MessageDispatched(_destinationDomain, recipient, messageId);
    }

    /**
     * @dev See {IOracleTrigger-dispatch}.
     */
    function dispatch(
        uint32 _destinationDomain,
        address recipientAddress,
        string memory key
    )
        external
        payable
        onlyRole(DISPATCHER_ROLE)
        nonReentrant
        validateAddress(mailBox)
        validateAddress(recipientAddress)
    {
        (uint128 currValue, uint128 currTimestamp) = _getOracleValue(key);

        bytes memory messageBody = abi.encode(key, currTimestamp, currValue);

        bytes32 messageId = IMailbox(mailBox).dispatch{ value: msg.value }(
            _destinationDomain,
            recipientAddress.addressToBytes32(),
            messageBody
        );

        emit MessageDispatched(_destinationDomain, recipientAddress, messageId);
    }

    /// @notice Sets the mailbox contract address
    /// @param _mailbox The new mailbox contract address
    function setMailBox(
        address _mailbox
    ) external onlyRole(OWNER_ROLE) validateAddress(_mailbox) {
        mailBox = _mailbox;
        emit MailboxUpdated(_mailbox);
    }

    /// @notice Retrieves lost tokens
    /// @param receiver The address of the receiver
    function retrieveLostTokens(
        address receiver
    ) external onlyRole(OWNER_ROLE) validateAddress(receiver) {
        uint256 balance = address(this).balance;
        if (balance == 0) revert NoBalanceToWithdraw();

        (bool success, ) = payable(receiver).call{ value: balance }("");
        if (!success) revert AmountTransferFailed();

        emit TokensRecovered(receiver, balance);
    }

    /**
     * @dev See {IOracleTrigger-getMailBox}.
     */
    function getMailBox() external view returns (address) {
        return mailBox;
    }

    /**
     * @notice Fetches value from the oracle.
     * @param key The oracle key to query.
     */
    function _getOracleValue(
        string memory key
    ) internal view returns (uint128, uint128) {
        if (metadataContract == address(0)) revert InvalidAddress();

        try IDIAOracleV2(metadataContract).getValue(key) returns (
            uint128 value,
            uint128 timestamp
        ) {
            return (value, timestamp);
        } catch {
            revert OracleError(key);
        }
    }
}
