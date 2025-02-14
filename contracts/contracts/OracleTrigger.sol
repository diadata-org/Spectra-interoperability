// SPDX-License-Identifier: MIT OR Apache-2.0
pragma solidity >=0.8.0;

import {IInterchainGasPaymaster} from "./interfaces/IInterchainGasPaymaster.sol";
 import {IMailbox} from "./interfaces/IMailbox.sol";
import {IPostDispatchHook} from "./interfaces/hooks/IPostDispatchHook.sol";
import {IInterchainSecurityModule, ISpecifiesInterchainSecurityModule} from "./interfaces/IInterchainSecurityModule.sol";
import {TypeCasts} from "./libs/TypeCasts.sol";
import {AccessControlEnumerable} from "@openzeppelin/contracts/access/AccessControlEnumerable.sol";
import {ReentrancyGuard} from "@openzeppelin/contracts/security/ReentrancyGuard.sol";

interface IDIAOracleV2 {
    function getValue(
        string memory key
    ) external view returns (uint128, uint128);
}

using TypeCasts for address;

error ZeroAddress();
error InvalidChainConfig();
error ChainNotConfigured(uint32 chainId);
error OracleError(string key);

error NotAuthorized(address account);
error ExistingAdmin(address account);

error CannotRemoveLastOwner();

contract OracleTrigger is
    AccessControlEnumerable,
    ISpecifiesInterchainSecurityModule,
    ReentrancyGuard
{
    struct ChainConfig {
        address RecipientAddress;
    }
    IInterchainSecurityModule public interchainSecurityModule;

    address public mailBox;

    mapping(uint32 => ChainConfig) public chains;

    bytes32 public constant OWNER_ROLE = keccak256("OWNER_ROLE");

    address public metadataContract;

    event ChainAdded(uint32 indexed chainId, address RecipientAddress);
    event ChainUpdated(uint32 indexed chainId, address RecipientAddress);

    event MessageDispatched(
        uint32 chainId,
        address recipientAddress,
        bytes32 indexed messageId
    );

    modifier validateAddress(address _address) {
        if (_address == address(0)) revert ZeroAddress();
        _;
    }

    modifier validateChain(uint32 _chainId) {
        if (chains[_chainId].RecipientAddress == address(0))
            revert ChainNotConfigured(_chainId);
        _;
    }

    modifier onlyOwner() {
        if (!hasRole(OWNER_ROLE, msg.sender)) revert NotAuthorized(msg.sender);
        _;
    }

    event OwnerAdded(
        address indexed account,
        address indexed addedBy,
        uint256 timestamp
    );

    event OwnerRemoved(
        address indexed account,
        address indexed removedBy,
        uint256 timestamp
    );

    event MailboxUpdated(address indexed newMailbox);
    event InterchainSecurityModuleUpdated(address indexed newModule);
    event MetadataContractUpdated(address indexed newMetadata);

    constructor() {
        _setupRole(DEFAULT_ADMIN_ROLE, msg.sender);
        _setupRole(OWNER_ROLE, msg.sender);
    }

    /**
     * @notice Adds a new chain recipient address. This is optional
     */
    function addChain(
        uint32 chainId,
        address recipientAddress
    ) public onlyOwner validateAddress(recipientAddress) {
        chains[chainId] = ChainConfig(recipientAddress);
        emit ChainAdded(chainId, recipientAddress);
    }

    /**
     * @notice Updates recipient address (Destination chain oracle) for a specific chain.
     */
    function updateChain(
        uint32 chainId,
        address recipientAddress
    )
        public
        onlyOwner
        validateAddress(recipientAddress)
        validateChain(chainId)
    {
        chains[chainId] = ChainConfig(recipientAddress);
        emit ChainUpdated(chainId, recipientAddress);
    }

    /**
     * @notice Returns the recipient address for a given chain.
     */

    function viewChain(
        uint32 _chainId
    ) public view validateChain(_chainId) returns (address) {
        return chains[_chainId].RecipientAddress;
    }

    /**
     * @notice Updates the metadata contract address.
     */
    function updateMetadataContract(
        address newMetadata
    ) external onlyOwner validateAddress(newMetadata) {
        metadataContract = newMetadata;
       emit MetadataContractUpdated(newMetadata);

    }

    /**
     * @notice Dispatches a message to a configured destination chain.
     */

    function dispatchToChain(
        uint32 _destinationDomain,
        string memory key
    )
        external
        payable
        onlyOwner
        validateChain(_destinationDomain)
        validateAddress(mailBox)
        nonReentrant
    {
         ChainConfig storage config = chains[_destinationDomain];

        (uint128 currValue, uint128 currTimestamp) = _getOracleValue(key);

        bytes memory messageBody = abi.encode(key, currTimestamp, currValue);
        bytes32 messageId = IMailbox(mailBox).dispatch{value: msg.value}(
            _destinationDomain,
            config.RecipientAddress.addressToBytes32(),
            messageBody
        );

        emit MessageDispatched(
            _destinationDomain,
            config.RecipientAddress,
            messageId
        );
    }

    /**
     * @notice Dispatches a message to a PushOracleReceiver .
     */

    function dispatch(
        uint32 _destinationDomain,
        address recipientAddress,
        string memory key
    ) external payable onlyOwner nonReentrant validateAddress(mailBox) validateChain(_destinationDomain) validateAddress(recipientAddress) {
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
     * @notice Sets the interchain security module.
     */
    function setInterchainSecurityModule(
        address _ism
    ) external onlyOwner validateAddress(_ism) {
        interchainSecurityModule = IInterchainSecurityModule(_ism);
        emit InterchainSecurityModuleUpdated(_ism);
    }

    /**
     * @notice Sets the mailbox address.
     */
    function setMailbox(
        address _mailbox
    ) external onlyOwner validateAddress(_mailbox) {
        mailBox = _mailbox;
        emit MailboxUpdated(_mailbox);
    }

    /**
     * @notice Fetches value from the oracle.
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
     * @notice Adds an owner.
     */
    function addOwner(
        address newOwner
    ) external validateAddress(newOwner) onlyOwner {
        if (hasRole(OWNER_ROLE, newOwner)) revert ExistingAdmin(newOwner);
        grantRole(OWNER_ROLE, newOwner);
        emit OwnerAdded(newOwner, msg.sender, block.timestamp);
    }

    /**
     * @notice Removes an owner but ensures at least one remains.
     */
    function removeOwner(
        address owner
    ) external validateAddress(owner) onlyOwner {
        if(getRoleMemberCount(OWNER_ROLE) <=1) revert CannotRemoveLastOwner();
        revokeRole(OWNER_ROLE, owner);
        emit OwnerRemoved(owner, msg.sender, block.timestamp);
    }

    /**
     * @notice Checks if an address is an owner.
     */
    function isOwner(address account) external view returns (bool) {
        return hasRole(OWNER_ROLE, account);
    }

    /**
     * @notice Returns a list of all owners.
     */
    function getOwners() external view returns (address[] memory) {
        uint256 ownerCount = getRoleMemberCount(OWNER_ROLE);
        address[] memory owners = new address[](ownerCount);

        for (uint256 i = 0; i < ownerCount; i++) {
            owners[i] = getRoleMember(OWNER_ROLE, i);
        }

        return owners;
    }
}
