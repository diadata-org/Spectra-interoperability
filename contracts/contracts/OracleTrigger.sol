// SPDX-License-Identifier: MIT OR Apache-2.0
pragma solidity 0.8.29;

import {IMailbox} from "./interfaces/IMailbox.sol";
 import {TypeCasts} from "./libs/TypeCasts.sol";
import {AccessControlEnumerable} from "@openzeppelin/contracts/access/AccessControlEnumerable.sol";
import {ReentrancyGuard} from "@openzeppelin/contracts/security/ReentrancyGuard.sol";
 
interface IDIAOracleV2 {
    function getValue(
        string memory key
    ) external view returns (uint128, uint128);
}

using TypeCasts for address;

/// @notice Error thrown when a provided address is the zero address.
error ZeroAddress();

/// @notice Error thrown when the provided chain configuration is invalid.
error InvalidChainConfig();

/// @notice Error thrown when trying to interact with a chain that has not been configured.
/// @param chainId The chain ID that is not configured.
error ChainNotConfigured(uint32 chainId);


/// @notice Error thrown when there is an issue retrieving a value from the oracle.
/// @param key The oracle key that caused the error.
error OracleError(string key);

/// @notice Error thrown when an unauthorized account attempts to perform a restricted action.
/// @param account The address of the unauthorized account.
error NotAuthorized(address account);

/// @notice Error thrown when trying to add an existing admin again.
/// @param account The address of the existing admin.
error ExistingAdmin(address account);

/// @notice Error thrown when attempting to remove the last owner, which is not allowed.
error CannotRemoveLastOwner();

/// @notice Error thrown when trying to add a chain that already exists.
/// @param chainId The chain ID that is already configured.
error ChainAlreadyExists(uint32 chainId);


event TokensRecovered(address indexed recipient, uint256 amount);


/// @title OracleTrigger
/// @notice Reads the latest oracle value from metadata and dispatches it to the desired chain.
/// @dev Provides access control for managing chains and secure dispatching mechanisms.
/// @dev Only addresses with the DISPATCHER_ROLE can call dispatch functions.
contract OracleTrigger is
    AccessControlEnumerable,
     ReentrancyGuard
{
    struct ChainConfig {
        address RecipientAddress;
    }
   

    /// @notice Address of the mailbox contract responsible for interchain messaging.
    address private mailBox;

    /// @notice Mapping of chain IDs to their corresponding recipient addresses.
    mapping(uint32 => ChainConfig) public chains;

    /// @notice Role identifier for contract owners.
    bytes32 public constant OWNER_ROLE = keccak256("OWNER_ROLE");

    /// @notice Role identifier for Dispatch function callers, i.e Feeder Service and OracleRequestReceipent.
    bytes32 public constant DISPATCHER_ROLE = keccak256("DISPATCHER_ROLE");

    /// @notice Address of the DIA oracle metadata contract.
    address public metadataContract;

    /// @notice Emitted when a new chain is added.
    /// @param chainId The chain ID of the newly added chain.
    /// @param RecipientAddress Address of the recipient contract on the chain.
    event ChainAdded(uint32 indexed chainId, address RecipientAddress);
    /// @notice Emitted when a chain configuration is updated.
    /// @param chainId The chain ID being updated.
    /// @param oldRecipientAddress Old recipient address.
    /// @param recipientAddress New recipient address.
    event ChainUpdated(
        uint32 indexed chainId,
        address oldRecipientAddress,
        address recipientAddress
    );

    /// @notice Emitted when a message is dispatched to a destination chain.
    /// @param chainId The destination chain ID.
    /// @param recipientAddress The recipient contract address on the destination chain.
    /// @param messageId The message ID.
    event MessageDispatched(
        uint32 chainId,
        address recipientAddress,
        bytes32 indexed messageId
    );

    /// @notice Emitted when an owner is added.
    /// @param account The address of the new owner.
    /// @param addedBy The address that added the new owner.
    /// @param timestamp The timestamp of the addition.
    event OwnerAdded(
        address indexed account,
        address indexed addedBy,
        uint256 timestamp
    );

    /// @notice Emitted when an owner is removed.
    /// @param account The address of the removed owner.
    /// @param removedBy The address that removed the owner.
    /// @param timestamp The timestamp of the removal.
    event OwnerRemoved(
        address indexed account,
        address indexed removedBy,
        uint256 timestamp
    );

    /// @notice Emitted when the mailbox contract address is updated.
    /// @param newMailbox The new mailbox contract address.
    event MailboxUpdated(address indexed newMailbox);
    /// @notice Emitted when the interchain security module (ISM) address is updated.
    /// @param newModule The new ISM address.
 
    /// @notice Emitted when the metadata contract address is updated.
    /// @param newMetadata The new metadata contract address.
    event MetadataContractUpdated(address indexed newMetadata);

    /// @notice Ensures that the provided address is not a zero address.
    modifier validateAddress(address _address) {
        if (_address == address(0)) revert ZeroAddress();
        _;
    }

    /// @notice Ensures that the given chain is configured.
    modifier validateChain(uint32 _chainId) {
        if (chains[_chainId].RecipientAddress == address(0))
            revert ChainNotConfigured(_chainId);
        _;
    }

    

    /// @notice Contract constructor that initializes the contract and assigns the deployer as the first owner.
    constructor() {
        _grantRole(DEFAULT_ADMIN_ROLE, msg.sender);
        _grantRole(OWNER_ROLE, msg.sender);
    }

    /// @notice Adds a new chain recipient address.
    /// @param chainId The chain ID to be added.
    /// @param recipientAddress The recipient address for the chain.
    function addChain(
        uint32 chainId,
        address recipientAddress
    ) public onlyRole(OWNER_ROLE) validateAddress(recipientAddress) {
        if (chains[chainId].RecipientAddress != address(0)) {
            revert ChainAlreadyExists(chainId);
        }
        chains[chainId] = ChainConfig(recipientAddress);
        emit ChainAdded(chainId, recipientAddress);
    }

    /// @notice Updates the recipient address for a specific chain.
    /// @param chainId The chain ID to be updated.
    /// @param recipientAddress The new recipient address.
    function updateChain(
        uint32 chainId,
        address recipientAddress
    )
        public
        onlyRole(OWNER_ROLE)
        validateAddress(recipientAddress)
        validateChain(chainId)
    {
        address oldRecipientAddress = chains[chainId].RecipientAddress;

        chains[chainId] = ChainConfig(recipientAddress);
        emit ChainUpdated(chainId, oldRecipientAddress, recipientAddress);
    }

    /// @notice Returns the recipient address for a given chain.
    /// @param _chainId The chain ID to query.
    function viewChain(
        uint32 _chainId
    ) public view validateChain(_chainId) returns (address) {
        return chains[_chainId].RecipientAddress;
    }

    /// @notice Updates the metadata contract address.
    /// @param newMetadata The new metadata contract address.
    function updateMetadataContract(
        address newMetadata
    ) external onlyRole(OWNER_ROLE) validateAddress(newMetadata) {
        metadataContract = newMetadata;
        emit MetadataContractUpdated(newMetadata);
    }

    /**
     *  @notice Dispatches a message to a configured destination chain.
     *  @param _destinationDomain The destination chain ID.
     *  @param key The key used to fetch the oracle value.
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
        ChainConfig storage config = chains[_destinationDomain];

        (uint128 currValue, uint128 currTimestamp) = _getOracleValue(key);

        bytes memory messageBody = abi.encode(key, currTimestamp, currValue);

        address recipient = config.RecipientAddress;

        bytes32 messageId = IMailbox(mailBox).dispatch{value: msg.value}(
            _destinationDomain,
            recipient.addressToBytes32(),
            messageBody
        );

        emit MessageDispatched(_destinationDomain, recipient, messageId);
    }

    /// @notice Dispatches a message to a configured destination chain.
    /// @param _destinationDomain The destination chain ID.
    /// @param key The key used to fetch the oracle value.
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

        bytes32 messageId = IMailbox(mailBox).dispatch{value: msg.value}(
            _destinationDomain,
            recipientAddress.addressToBytes32(),
            messageBody
        );

        emit MessageDispatched(_destinationDomain, recipientAddress, messageId);
    }

    /**
     * @notice Sets the mailbox address.
     * @param _mailbox The new mailbox address.
     */
    function setMailBox(
        address _mailbox
    ) external onlyRole(OWNER_ROLE) validateAddress(_mailbox) {
        mailBox = _mailbox;
        emit MailboxUpdated(_mailbox);
    }

    /**
     * @notice Gets the mailbox address.
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
        if (metadataContract == address(0)) revert ZeroAddress();

        try IDIAOracleV2(metadataContract).getValue(key) returns (
            uint128 value,
            uint128 timestamp
        ) {
            return (value, timestamp);
        } catch {
            revert OracleError(key);
        }
    }


    /**
     * @notice Withdraw ETH to recover stuck funds
     */
     
      function retrieveLostTokens(address receiver) external onlyRole(OWNER_ROLE) {
        require(receiver != address(0), "Invalid receiver");
        uint256 balance = address(this).balance;
        require(balance > 0, "No balance to withdraw");

        (bool success, ) = payable(receiver).call{value: balance}("");
        require(success, "transfer failed");
        emit TokensRecovered(receiver, balance);
    }

    
}
