// SPDX-License-Identifier: GPL-3.0
pragma solidity 0.8.29;

import { AccessControlEnumerable } from "@openzeppelin/contracts/access/AccessControlEnumerable.sol";
import { ReentrancyGuard } from "@openzeppelin/contracts/security/ReentrancyGuard.sol";
import { IMailbox } from "./interfaces/IMailbox.sol";
import { IOracleTrigger } from "./interfaces/oracle/IOracleTrigger.sol";
import { TypeCasts } from "./libs/TypeCasts.sol";


// Interface for OracleIntentRegistry
interface IOracleIntentRegistry {
    struct OracleIntent {
        // Metadata
        string intentType;
        string version;
        uint256 chainId;
        uint256 nonce;
        uint256 expiry;
        
        // Oracle data
        string symbol;
        uint256 price;
        uint256 timestamp;
        string source;
        
        // Signature data
        bytes signature;
        address signer;
    }
    
    function getLatestPrice(string memory symbol) external view returns (uint256 price, uint256 timestamp, string memory source);
    function getIntent(bytes32 intentHash) external view returns (OracleIntent memory);
    function latestIntentBySymbol(string memory) external view returns (bytes32);
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
    
    /// @notice Address of the OracleIntentRegistry contract.
    address public intentRegistryContract;

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

    /// @notice Delete chain from config
    /// @param _chainId The chain ID of the chain to query
    function deleteChain(
        uint32 _chainId
    ) public onlyRole(OWNER_ROLE) validateChain(_chainId) {
        delete chains[_chainId];
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
    
    /// @notice Updates the intent registry contract address
    /// @param newRegistry The new intent registry contract address
    function updateIntentRegistryContract(
        address newRegistry
    ) external onlyRole(OWNER_ROLE) validateAddress(newRegistry) {
        intentRegistryContract = newRegistry;
        emit IntentRegistryContractUpdated(newRegistry);
    }

    function _getLatestIntent(string memory key) internal view returns (IOracleIntentRegistry.OracleIntent memory intent, bytes32 intentHash)  {
        address registry = intentRegistryContract;
        if (registry == address(0)) revert InvalidAddress();
        
        IOracleIntentRegistry registryContract = IOracleIntentRegistry(registry);
        
        intentHash = registryContract.latestIntentBySymbol(key);
        if (intentHash == bytes32(0)) revert OracleError(key);

        intent = registryContract.getIntent(intentHash);
    }

    function _encodeIntentMessage(IOracleIntentRegistry.OracleIntent memory intent) internal pure returns (bytes memory) {
        return abi.encode(
            intent.intentType,
            intent.version,
            intent.chainId,
            intent.nonce,
            intent.expiry,
            intent.symbol,
            intent.price,
            intent.timestamp,
            intent.source,
            intent.signature,
            intent.signer
        );
    }

    /**
     * @dev See {IOracleTrigger-dispatchToChain}.
     * @notice Now gets the latest intent from the registry and sends it as the message
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
        (IOracleIntentRegistry.OracleIntent memory intent, bytes32 intentHash) = _getLatestIntent(key);

        bytes memory messageBody = _encodeIntentMessage(intent);

        address recipient = chains[_destinationDomain];

        bytes32 messageId = IMailbox(mailBox).dispatch{ value: msg.value }(
            _destinationDomain,
            recipient.addressToBytes32(),
            messageBody
        );

        emit MessageDispatched(_destinationDomain, recipient, messageId, intentHash, key);
    }

    /**
     * @dev See {IOracleTrigger-dispatch}.
     * @notice Now gets the latest intent from the registry and sends it as the message
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
        (IOracleIntentRegistry.OracleIntent memory intent, bytes32 intentHash) = _getLatestIntent(key);

        bytes memory messageBody = _encodeIntentMessage(intent);

        bytes32 messageId = IMailbox(mailBox).dispatch{ value: msg.value }(
            _destinationDomain,
            recipientAddress.addressToBytes32(),
            messageBody
        );

        emit MessageDispatched(_destinationDomain, recipientAddress, messageId, intentHash, key);
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
     * @notice Returns the address of the intent registry contract.
     */
    function getIntentRegistry() external view returns (address) {
        return intentRegistryContract;
    }


    
    // Event for intent registry updates
    event IntentRegistryContractUpdated(address indexed newRegistry);
}
