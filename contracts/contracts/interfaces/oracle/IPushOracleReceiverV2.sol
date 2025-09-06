// SPDX-License-Identifier: GPL-3.0
pragma solidity 0.8.29;

import { IMessageRecipient } from "../IMessageRecipient.sol";
import { ISpecifiesInterchainSecurityModule } from "../IInterchainSecurityModule.sol";
import { OracleIntentUtils } from "../../libs/OracleIntentUtils.sol";

/** @title IPushOracleReceiverV2
* @author DiaData
 * @notice Interface for a contract that receives oracle updates via interchain messages,
 *         including support for intent-based updates with signature verification.
 * @dev Extends IMessageRecipient and ISpecifiesInterchainSecurityModule for interchain messaging and security module specification.
 */
interface IPushOracleReceiverV2 is
    IMessageRecipient,
    ISpecifiesInterchainSecurityModule
{
    // @notice Thrown when the address is invalid
    error InvalidAddress();

    // @notice Thrown when the mailbox address is unauthorized
    error UnauthorizedMailbox();
    
    // @notice Thrown when the signer is unauthorized
    error UnauthorizedSigner();
    
    // @notice Thrown when an intent has expired
    error IntentExpired();
    
    // @notice Thrown when an intent has already been processed
    error IntentAlreadyProcessed();
    
    // @notice Thrown when a signature is invalid
    error InvalidSignature();

    // @notice Thrown when there is no balance in the contract to withdraw from
    error NoBalanceToWithdraw();

    // @notice Thrown when requested amount exceeds available balance
    error InsufficientBalance();

    // @notice Thrown when the transfer of any amount fails
    error AmountTransferFailed();


    // @notice InsufficientGasForPayment is thrown when there is not enough gas to pay for the message
    error InsufficientGasForPayment();
    
    
    // @notice Thrown when batch size exceeds maximum allowed
    error BatchTooLarge();
    
    // @notice Thrown when domain name is empty
    error InvalidDomainName();
    
    // @notice Thrown when domain version is empty
    error InvalidDomainVersion();
    
    // @notice Thrown when chain ID is zero
    error InvalidChainId();
    
    // @notice Thrown when attempting to set a zero domain separator
    error DomainSeparatorZero();

    /**
     * @notice Emitted when stuck funds are recovered
     * @param recipient The address that received the funds
     * @param amount The amount of funds recovered
     */
    event TokensRecovered(address indexed recipient, uint256 amount);

    /**
     * @notice Emitted when a message is received for the new update value
     * @param key The key of the update
     * @param timestamp The timestamp of the update
     * @param value The value of the update
     */
    event ReceivedMessage(string key, uint128 timestamp, uint128 value);

    /**
     * @notice Emitted when an intent-based update is received and applied
     * @param intentHash The hash of the intent
     * @param symbol The symbol of the update
     * @param price The price value
     * @param timestamp The timestamp of the update
     * @param signer The address of the signer
     */
    event IntentBasedUpdateReceived(
        bytes32 indexed intentHash,
        string indexed symbol,
        uint256 price,
        uint256 timestamp,
        address indexed signer
    );

    /**
     * @notice Emitted when an intent-based update is received but rejected due to stale timestamp
     * @param intentHash The hash of the intent
     * @param symbol The symbol of the update
     * @param price The price value
     * @param timestamp The timestamp of the update (stale)
     * @param existingTimestamp The existing newer timestamp
     * @param signer The address of the signer
     */
    event IntentBasedStaleUpdateReceived(
        bytes32 indexed intentHash,
        string indexed symbol,
        uint256 price,
        uint256 timestamp,
        uint256 existingTimestamp,
        address indexed signer
    );
    
    /** @notice Emitted when a signer authorization is changed
     * @param signer The address of the signer
     * @param isAuthorized Whether the signer is authorized
     */
    event SignerAuthorizationChanged(address indexed signer, bool isAuthorized);

    /** @notice Emitted when the trusted mailbox is updated
     * @param previousMailBox The previous mailbox address
     * @param newMailBox The new mailbox address
     */
    event TrustedMailBoxUpdated(
        address indexed previousMailBox,
        address indexed newMailBox
    );

    /** @notice Emitted when the interchain security module is updated
     * @param previousISM The previous interchain security module address
     * @param newISM The new interchain security module address
     */
    event InterchainSecurityModuleUpdated(
        address indexed previousISM,
        address indexed newISM
    );

    /** @notice Emitted when the payment hook is updated
     * @param previousPaymentHook The previous payment hook address
     * @param newPaymentHook The new payment hook address
     */
    event PaymentHookUpdated(
        address indexed previousPaymentHook,
        address indexed newPaymentHook
    );
    
    /** @notice Emitted when the EIP-712 domain separator is updated
     * @param domainSeparator The new domain separator
     * @param domainName The domain name used
     * @param domainVersion The domain version used
     * @param sourceChainId The source chain ID used
     * @param verifyingContract The verifying contract address used
     */
    event DomainSeparatorUpdated(
        bytes32 indexed domainSeparator,
        string domainName,
        string domainVersion,
        uint256 sourceChainId,
        address indexed verifyingContract
    );

    struct Data {
        uint128 timestamp;
        uint128 value;
    }
    
    // Use shared OracleIntent struct from library - no need for duplicate definition
    // Functions use OracleIntentUtils.OracleIntent directly

    /**
     * @notice Handles incoming interchain messages by decoding the payload and updating state
     * @param _origin The origin domain identifier
     * @param _sender The sender's address (in bytes32 format)
     * @param _data The encoded payload containing the oracle data
     */
    function handle(
        uint32 _origin,
        bytes32 _sender,
        bytes calldata _data
    ) external payable;
    
    /**
     * @notice Handles oracle updates from intent-based sources
     * @param intent The OracleIntent structure containing all intent data
     * @dev External services can call this function directly with properly signed intents
     */
    function handleIntentUpdate(
        OracleIntentUtils.OracleIntent calldata intent
    ) external payable;
    
    /**
     * @notice Handles batch updates from intent-based sources
     * @param intents Array of OracleIntent structures
     * @dev External services can call this function directly with multiple properly signed intents
     * @dev This is more gas efficient than calling handleIntentUpdate multiple times
     */
    function handleBatchIntentUpdates(
        OracleIntentUtils.OracleIntent[] calldata intents
    ) external payable;

    /**
     * @notice Sets the interchain security module.
     * @dev restricted to onlyOwner
     * @param _ism The address of the new interchain security module.
     */
    function setInterchainSecurityModule(address _ism) external;

    /**
     * @notice Sets the payment hook address
     * @dev restricted to onlyOwner
     * @param _paymentHook The address of the new payment hook.
     */
    function setPaymentHook(address payable _paymentHook) external;

    /**
     * @notice Sets the trusted mailbox address.
     * @dev restricted to onlyOwner
     * @param _mailbox The address of the new trusted mailbox.
     */
    function setTrustedMailBox(address _mailbox) external;
    
    /**
     * @notice Sets the authorization status for a signer
     * @param _signer The address of the signer
     * @param _isAuthorized Whether the signer is authorized
     * @dev Only the contract owner can authorize signers
     */
    function setSignerAuthorization(
        address _signer,
        bool _isAuthorized
    ) external;
    
    /**
     * @notice Sets the EIP-712 domain separator for signature validation
     * @param domainName The domain name for EIP-712
     * @param domainVersion The domain version for EIP-712  
     * @param sourceChainId The source chain ID for the domain
     * @param verifyingContract The verifying contract address
     * @dev Only the contract owner can update domain separator
     * @dev CRITICAL: This domain separator must match exactly with OracleTriggerV2's domain separator
     */
    function setDomainSeparator(
        string memory domainName,
        string memory domainVersion,
        uint256 sourceChainId,
        address verifyingContract
    ) external;

    /**
     * @notice Withdraws specific amount of stuck funds to the specified address
     * @dev restricted to onlyOwner
     * @param receiver The address to receive the funds.
     * @param amount The amount to withdraw (must be <= balance)
     */
    function retrieveLostTokens(address receiver, uint256 amount) external;
    
    /**
     * @notice Returns the domain separator for EIP-712 signatures
     * @dev This is useful for external services that need to create EIP-712 signatures
     * @return The domain separator used for EIP-712 signatures
     */
    function getDomainSeparator() external view returns (bytes32);
    
    /**
     * @notice Checks if a signer is authorized
     * @param _signer The address to check
     * @return Whether the signer is authorized
     */
    function isAuthorizedSigner(address _signer) external view returns (bool);
    
    /**
     * @notice Checks if an intent has been processed
     * @param _intentHash The hash of the intent to check
     * @return Whether the intent has been processed
     */
    function isProcessedIntent(bytes32 _intentHash) external view returns (bool);
    
    /**
     * @notice Calculates the hash for an OracleIntent
     * @param intent The OracleIntent structure
     * @return The EIP-712 hash of the intent
     * @dev This is useful for external services to verify their intent hashes
     */
    function calculateIntentHash(OracleIntentUtils.OracleIntent calldata intent) external view returns (bytes32);
}