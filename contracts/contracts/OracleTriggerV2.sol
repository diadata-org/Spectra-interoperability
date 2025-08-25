// SPDX-License-Identifier: GPL-3.0
pragma solidity 0.8.29;

import { AccessControlEnumerable } from "@openzeppelin/contracts/access/AccessControlEnumerable.sol";
import { ReentrancyGuard } from "@openzeppelin/contracts/security/ReentrancyGuard.sol";
import { IMailbox } from "./interfaces/IMailbox.sol";
import { IOracleTriggerV2 } from "./interfaces/oracle/IOracleTriggerV2.sol";
import { TypeCasts } from "./libs/TypeCasts.sol";
import { OracleIntentUtils } from "./libs/OracleIntentUtils.sol";


// Interface for OracleIntentRegistry using shared struct
interface IOracleIntentRegistry {
    function getLatestPrice(string memory symbol) external view returns (uint256 price, uint256 timestamp, string memory source);
    function getIntent(bytes32 intentHash) external view returns (OracleIntentUtils.OracleIntent memory);
    function latestIntentBySymbol(string memory) external view returns (bytes32);
}

/// @title OracleTriggerV2
/// @notice Intent-based version that reads the latest oracle intent from registry and dispatches it to the desired chain.
/// @dev Provides access control for managing chains and secure dispatching mechanisms.
/// @dev Only addresses with the DISPATCHER_ROLE can call dispatch functions.
contract OracleTriggerV2 is
    IOracleTriggerV2,
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

    
    /// @notice Address of the OracleIntentRegistry contract.
    address public intentRegistryContract;
    
    /// @notice EIP-712 domain separator for signature validation
    bytes32 public DOMAIN_SEPARATOR;

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
        address recipient = chains[_chainId];
        delete chains[_chainId];
        emit ChainDeleted(_chainId, recipient);
    }

    /// @notice Retrieves the recipient address for a specific chain
    /// @param _chainId The chain ID of the chain to query
    /// @return The address of the recipient contract on the specified chain
    function viewChain(
        uint32 _chainId
    ) public view validateChain(_chainId) returns (address) {
        return chains[_chainId];
    }

    
    /// @notice Updates the intent registry contract address
    /// @param newRegistry The new intent registry contract address
    function updateIntentRegistryContract(
        address newRegistry
    ) external onlyRole(OWNER_ROLE) validateAddress(newRegistry) {
        intentRegistryContract = newRegistry;
        emit IntentRegistryContractUpdated(newRegistry);
    }
    
    /// @notice Sets the EIP-712 domain separator for signature validation
    /// @param domainName The domain name for EIP-712
    /// @param domainVersion The domain version for EIP-712  
    /// @param sourceChainId The source chain ID for the domain
    /// @dev CRITICAL: This domain separator must match exactly with PushOracleReceiverV2's immutable domain separator
    /// @dev for signature validation to work correctly across the system
    function setDomainSeparator(
        string memory domainName,
        string memory domainVersion,
        uint256 sourceChainId
    ) external onlyRole(OWNER_ROLE) {
        bytes32 newDomainSeparator = OracleIntentUtils.createDomainSeparator(
            domainName,
            domainVersion,
            sourceChainId,
            address(this)
        );
        
        if (newDomainSeparator == bytes32(0)) {
            revert DomainSeparatorZero();
        }
        
        DOMAIN_SEPARATOR = newDomainSeparator;
        emit DomainSeparatorUpdated(
            DOMAIN_SEPARATOR,
            domainName,
            domainVersion,
            sourceChainId,
            address(this)
        );
    }

    function _getLatestIntent(string memory _key) internal view returns (OracleIntentUtils.OracleIntent memory intent, bytes32 intentHash)  {
        address registry = intentRegistryContract;
        if (registry == address(0)) revert RegistryUnavailable(_key);
        
        IOracleIntentRegistry registryContract = IOracleIntentRegistry(registry);
        
        intentHash = registryContract.latestIntentBySymbol(_key);
        if (intentHash == bytes32(0)) revert RegistryUnavailable(_key);

        intent = registryContract.getIntent(intentHash);
        
        // Validate basic intent data
        if (bytes(intent.symbol).length == 0) revert IntentDataInvalid(_key, "Empty symbol");
        if (intent.price == 0) revert IntentDataInvalid(_key, "Zero price");
        if (intent.timestamp == 0) revert IntentDataInvalid(_key, "Zero timestamp");
        if (intent.signer == address(0)) revert IntentDataInvalid(_key, "Invalid signer");
        if (intent.signature.length == 0) revert IntentDataInvalid(_key, "Empty signature");
        
        // Validate signature if domain separator is set
        if (DOMAIN_SEPARATOR != bytes32(0)) {
            bool isValid = OracleIntentUtils.validateSignature(intent, DOMAIN_SEPARATOR);
            if (!isValid) revert InvalidSignature(_key);
        }
    }

    function _encodeIntentMessage(OracleIntentUtils.OracleIntent memory intent) internal pure returns (bytes memory) {
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
        string memory _key
    )
        external
        payable
        onlyRole(DISPATCHER_ROLE)
        validateChain(_destinationDomain)
        validateAddress(mailBox)
        nonReentrant
    {
        (OracleIntentUtils.OracleIntent memory intent, bytes32 intentHash) = _getLatestIntent(_key);

        bytes memory messageBody = _encodeIntentMessage(intent);

        address recipient = chains[_destinationDomain];

        bytes32 messageId = IMailbox(mailBox).dispatch{ value: msg.value }(
            _destinationDomain,
            recipient.addressToBytes32(),
            messageBody
        );

        emit MessageDispatched(_destinationDomain, recipient, messageId, intentHash, _key);
    }

    /**
     * @dev See {IOracleTrigger-dispatch}.
     * @notice Now gets the latest intent from the registry and sends it as the message
     */
    function dispatch(
        uint32 _destinationDomain,
        address _recipientAddress,
        string memory _key
    )
        external
        payable
        onlyRole(DISPATCHER_ROLE)
        nonReentrant
        validateAddress(mailBox)
        validateAddress(_recipientAddress)
    {
        (OracleIntentUtils.OracleIntent memory intent, bytes32 intentHash) = _getLatestIntent(_key);

        bytes memory messageBody = _encodeIntentMessage(intent);

        bytes32 messageId = IMailbox(mailBox).dispatch{ value: msg.value }(
            _destinationDomain,
            _recipientAddress.addressToBytes32(),
            messageBody
        );

        emit MessageDispatched(_destinationDomain, _recipientAddress, messageId, intentHash, _key);
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

}