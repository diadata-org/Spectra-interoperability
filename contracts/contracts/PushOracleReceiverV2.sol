// SPDX-License-Identifier: GPL-3.0
pragma solidity 0.8.29;

import { Ownable } from "@openzeppelin/contracts/access/Ownable.sol";
import { ReentrancyGuard } from "@openzeppelin/contracts/security/ReentrancyGuard.sol";
import { IPushOracleReceiverV2 } from "./interfaces/oracle/IPushOracleReceiverV2.sol";
import { IInterchainSecurityModule } from "./interfaces/IInterchainSecurityModule.sol";
import { ProtocolFeeHook } from "./ProtocolFeeHook.sol";
import { OracleIntentUtils } from "./libs/OracleIntentUtils.sol";

/**
 * @title PushOracleReceiverV2
 * @author Diadata.org
 * @notice Handles incoming oracle data updates and ensures security via Hyperlane.
 * @dev Implements IMessageRecipient and ISpecifiesInterchainSecurityModule.
 *
 * ## Data Flow:
 * - Go Feeder Service → OracleTrigger (reads price from metadata) → Hyperlane → PushOracleReceiver
 * - OR: Intent-based Oracle → PushOracleReceiver (direct interaction)
 *
 * This contract receives and processes oracle updates from the DIA chain.
 *
 * ## Direct Interaction:
 * External services can directly call handleIntentUpdate or handleBatchIntentUpdates
 * with properly formatted and signed OracleIntent structures. The contract will verify:
 * 1. The intent has not expired
 * 2. The signer is authorized
 * 3. The intent has not been processed before
 * 4. The signature is valid
 *
 * ## Funding Mechanism:
 * - The contract should hold enough balance to cover transaction fees for updates.
 * - Each update requires two transactions: one on the DIA chain and another on the chain where PushOracleReceiver is deployed (Destination).
 * - The contract deducts the fee for each Destination transaction and transfers it to the ProtocolFeeHook.
 *
 * ## Security Constraints:
 * - PushOracleReceiver processes messages only from the trusted mailbox.
 * - The oracle trigger address must be whitelisted in the ISM (Interchain Security Module) of PushOracleReceiver.
 * - Intent-based updates must be signed by authorized signers.
 */

contract PushOracleReceiverV2 is IPushOracleReceiverV2, Ownable, ReentrancyGuard {

    /// @notice Maximum number of intents that can be processed in a single batch
    uint256 public constant MAX_BATCH_SIZE = 100;

    /// @notice Reference to the interchain security module
    IInterchainSecurityModule public interchainSecurityModule;

    /// @notice Address for the post-dispatch payment hook
    address payable public paymentHook;

    /// @notice only Message from this mailbox will be handled
    address public trustedMailBox;

    /// @notice Mapping of oracle data updates by key
    mapping(string => Data) public updates;
    
    /// @notice Mapping of authorized signers for intent-based updates
    mapping(address => bool) public authorizedSigners;
    
    /// @notice Mapping to track processed intents
    mapping(bytes32 => bool) public processedIntents;
    
    /// @notice When each intent was processed (block timestamp)
    mapping(bytes32 => uint64) public processedAt;
    
    /// @notice EIP-712 domain separator
    bytes32 public domainSeparator;

    /// @notice Validation status for intent checks used to avoid logic duplication
    enum ValidationStatus { Ok, Expired, UnauthorizedSigner, AlreadyProcessed, InvalidSignature }

    /// @notice Error thrown when an ISM is not set (zero address) is used.
    error InvalidISMAddress();

    /// @notice Ensures that the provided address is not a zero address
    modifier validateAddress(address _address) {
        if (_address == address(0)) revert InvalidAddress();
        _;
    }
    
    /**
     * @notice Constructor initializes the EIP-712 domain separator with configurable parameters
     * @param _domainName The name for the EIP-712 domain
     * @param _domainVersion The version for the EIP-712 domain
     * @param _sourceChainId The chain ID where the source registry is deployed
     * @param _verifyingContract The address of the verifying contract (OracleIntentRegistry)
     * @dev The domain separator must match exactly with the one used by the attestor service
     */
    constructor(
        string memory _domainName,
        string memory _domainVersion,
        uint256 _sourceChainId,
        address _verifyingContract
    ) {
        // Validate constructor parameters
        if (bytes(_domainName).length == 0) revert InvalidDomainName();
        if (bytes(_domainVersion).length == 0) revert InvalidDomainVersion();
        if (_sourceChainId == 0) revert InvalidChainId();
        if (_verifyingContract == address(0)) revert InvalidAddress();
        
        // Create the EIP-712 domain separator using shared library
        domainSeparator = OracleIntentUtils.createDomainSeparator(
            _domainName,
            _domainVersion,
            _sourceChainId,
            _verifyingContract
        );
        
        // Emit event for domain separator verification
        emit DomainSeparatorUpdated(
            domainSeparator,
            _domainName,
            _domainVersion,
            _sourceChainId,
            _verifyingContract
        );
    }

    /**
     * @dev See {IPushOracleReceiverV2-handle}.
     * @notice Handles both ISM-validated format (key, timestamp, value) and new intent format
     * @param _data The encoded payload containing the oracle data or intent
     */
    function handle(
        uint32 /* _origin */,
        bytes32 /* _sender */,
        bytes calldata _data
    ) external payable override validateAddress(paymentHook) nonReentrant {
        if (msg.sender != trustedMailBox) revert UnauthorizedMailbox();
        if (address(interchainSecurityModule) == address(0))
            revert InvalidISMAddress();

        // Try to detect format using library function - no hardcoded assumptions
        if (OracleIntentUtils.isIntentFormat(_data)) {
            _handleIntentMessage(_data);
        } else {
            _handleISMValidatedMessage(_data);
        }
    }
    

    /**
     * @notice Handles intent-based messages from the mailbox (internal)
     * @param _data The encoded intent data
     * @dev This function processes intents sent via the mailbox from OracleTrigger
     */
    function _handleIntentMessage(bytes calldata _data) internal {
        // Decode the intent data
        OracleIntentUtils.OracleIntent memory intent = _decodeIntentData(_data);
        
        // Use unified validation logic
        bytes32 intentHash = _validateIntentCommonFromMemory(intent);
        
        // Mark as processed and update data
        processedIntents[intentHash] = true;
        processedAt[intentHash] = uint64(block.timestamp);
        _updateOracleDataUnified(intent.symbol, intent.price, intent.timestamp, intentHash, intent.signer);
        
        // Calculate and transfer the protocol fee
        _transferProtocolFee();
    }
    
    /**
     * @notice Handles ISM-validated messages (backward compatibility)
     * @param _data The encoded oracle data (key, timestamp, value)
     * @dev This function processes messages that have already passed ISM security validation
     * including cross-chain authenticity, sender authorization, and message integrity checks
     */
    function _handleISMValidatedMessage(bytes calldata _data) internal {
        // At this point, ISM has validated:
        // 1. Message came from trusted source chain
        // 2. Message passed ISM security checks  
        // 3. Sender is authorized at the ISM level
        // 4. Message integrity is verified
        
        // Decode the incoming data into its respective components
        (string memory key, uint128 timestamp, uint128 value) = abi.decode(
            _data,
            (string, uint128, uint128)
        );

        // Ensure the new timestamp is more recent (freshness validation)
        if (updates[key].timestamp >= timestamp) {
            return; // Ignore outdated data
        }

        // Update the stored oracle data - we can trust this data due to ISM validation
        Data memory newData = Data({ timestamp: timestamp, value: value });
        updates[key] = newData;

        emit ReceivedMessage(key, timestamp, value);
        
        // Calculate and transfer the protocol fee
        _transferProtocolFee();
    }
    
    /**
     * @notice Calculates and transfers the protocol fee
     */
    function _transferProtocolFee() internal {
        // Calculate the transaction fee based on gas used and gas price with overflow protection
        uint256 gasPrice = tx.gasprice;
        uint256 gasUsed = ProtocolFeeHook(payable(paymentHook)).gasUsedPerTx();
        
        // Prevent overflow in fee calculation
        if (gasUsed > type(uint256).max / gasPrice) {
            revert AmountTransferFailed(); // Fee calculation would overflow
        }
        
        uint256 fee = gasUsed * gasPrice;
        
        // Check contract balance and adjust fee if necessary
        uint256 contractBalance = address(this).balance;
        if (fee > contractBalance) {
            fee = contractBalance; // Pay what we can afford
        }
        
        // Only transfer if we have something to transfer
        if (fee > 0) {
            bool success;
            {
                (success, ) = paymentHook.call{ value: fee }("");
            }
            if (!success) revert AmountTransferFailed();
        }
    }

    /**
     * @notice Handles oracle updates from intent-based sources
     * @param intent The OracleIntent structure containing all intent data
     * @dev External services can call this function directly with properly signed intents
     */
    function handleIntentUpdate(
        OracleIntentUtils.OracleIntent calldata intent
    ) external payable override validateAddress(paymentHook) nonReentrant {
        // Process single intent with revert-on-failure behavior
        _processIntent(intent, true);
        
        // Calculate and transfer the protocol fee
        _transferProtocolFee();
    }
    
    /**
     * @notice Handles batch updates from intent-based sources
     * @param intents Array of OracleIntent structures
     * @dev External services can call this function directly with multiple properly signed intents
     * @dev This is more gas efficient than calling handleIntentUpdate multiple times
     */
    function handleBatchIntentUpdates(
        OracleIntentUtils.OracleIntent[] calldata intents
    ) external payable override validateAddress(paymentHook) nonReentrant {
        if (intents.length > MAX_BATCH_SIZE) revert BatchTooLarge();
        
        uint256 updatedCount = 0;
        
        // Process each intent
        for (uint256 i = 0; i < intents.length; ) {
            if (_processIntent(intents[i], false)) {
                ++updatedCount;
            }
            unchecked { ++i; }
        }
        
        // Only charge fee if at least one update was processed
        if (updatedCount > 0) {
            _transferProtocolFee();
        }
    }

    /**
     * @notice Sets the interchain security module.
     * @dev restricted to onlyOwner
     * @param _ism The address of the new interchain security module.
     */
    function setInterchainSecurityModule(
        address _ism
    ) external override onlyOwner validateAddress(_ism) {
        emit InterchainSecurityModuleUpdated(
            address(interchainSecurityModule),
            _ism
        );
        interchainSecurityModule = IInterchainSecurityModule(_ism);
    }

    /**
     * @notice Sets the payment hook address
     * @param _paymentHook The address of the new payment hook.
     */
    function setPaymentHook(
        address payable _paymentHook
    ) external override onlyOwner validateAddress(_paymentHook) {
        emit PaymentHookUpdated(paymentHook, _paymentHook);
        paymentHook = _paymentHook;
    }

    /**
     * @notice Sets the trusted mailbox address.
     * @dev restricted to onlyOwner
     * @param _mailbox The address of the new trusted mailbox.
     */
    function setTrustedMailBox(
        address _mailbox
    ) external override onlyOwner validateAddress(_mailbox) {
        emit TrustedMailBoxUpdated(trustedMailBox, _mailbox);
        trustedMailBox = _mailbox;
    }
    
    /**
     * @notice Sets the authorization status for a signer
     * @param _signer The address of the signer
     * @param _isAuthorized Whether the signer is authorized
     * @dev Only the contract owner can authorize signers
     */
    function setSignerAuthorization(
        address _signer,
        bool _isAuthorized
    ) external override onlyOwner validateAddress(_signer) {
        authorizedSigners[_signer] = _isAuthorized;
        emit SignerAuthorizationChanged(_signer, _isAuthorized);
    }
    
    /**
     * @notice Sets the EIP-712 domain separator for signature validation
     * @param domainName The domain name for EIP-712
     * @param domainVersion The domain version for EIP-712  
     * @param sourceChainId The source chain ID for the domain
     * @param verifyingContract The verifying contract address
     * @dev CRITICAL: This domain separator must match exactly with OracleTriggerV2's domain separator
     * @dev for signature validation to work correctly across the system
     */
    function setDomainSeparator(
        string calldata domainName,
        string calldata domainVersion,
        uint256 sourceChainId,
        address verifyingContract
    ) external override onlyOwner {
        bytes32 newDomainSeparator = OracleIntentUtils.createDomainSeparator(
            domainName,
            domainVersion,
            sourceChainId,
            verifyingContract
        );
        
        if (newDomainSeparator == bytes32(0)) {
            revert DomainSeparatorZero();
        }
        
        domainSeparator = newDomainSeparator;
        emit DomainSeparatorUpdated(
            domainSeparator,
            domainName,
            domainVersion,
            sourceChainId,
            verifyingContract
        );
    }

    /**
     * @notice Withdraws stuck funds to the specified address
     * @dev restricted to onlyOwner
     * @param receiver The address to receive the funds.
     */
    function retrieveLostTokens(
        address receiver
    ) external override onlyOwner validateAddress(receiver) nonReentrant {
        uint256 balance = address(this).balance;
        if (balance <= 0) revert NoBalanceToWithdraw();

        emit TokensRecovered(receiver, balance);
        
        (bool success, ) = payable(receiver).call{ value: balance }("");
        if (!success) revert AmountTransferFailed();
    }
    
    /**
     * @notice Returns the domain separator for EIP-712 signatures
     * @dev This is useful for external services that need to create EIP-712 signatures
     * @return The domain separator used for EIP-712 signatures
     */
    function getDomainSeparator() external view override returns (bytes32) {
        return domainSeparator;
    }
    
    /**
     * @notice Checks if a signer is authorized
     * @param _signer The address to check
     * @return Whether the signer is authorized
     */
    function isAuthorizedSigner(address _signer) external view override returns (bool) {
        return authorizedSigners[_signer];
    }
    
    /**
     * @notice Checks if an intent has been processed
     * @param _intentHash The hash of the intent to check
     * @return Whether the intent has been processed
     */
    function isProcessedIntent(bytes32 _intentHash) external view override returns (bool) {
        return processedIntents[_intentHash];
    }
    
    
    
    /**
     * @notice Unified validation for memory intents with expiry check
     * @param intent The OracleIntent to validate
     * @return intentHash The calculated intent hash
     */
    function _validateIntentCommonFromMemory(OracleIntentUtils.OracleIntent memory intent) internal view returns (bytes32 intentHash) {
        // Check if the intent has expired
        if (block.timestamp > intent.expiry) revert IntentExpired();
        
        // Verify signer is authorized
        if (!authorizedSigners[intent.signer]) revert UnauthorizedSigner();
        
        // Calculate intent hash
        intentHash =  OracleIntentUtils.calculateIntentHash(intent, domainSeparator);

        
        // Check if already processed
        if (processedIntents[intentHash]) revert IntentAlreadyProcessed();
        
        // Verify signature
        if (OracleIntentUtils.recoverSigner(intentHash, intent.signature) != intent.signer) revert InvalidSignature();
        
        return intentHash;
    }


    
    /**
     * @notice Unified oracle data update function
     * @param symbol The oracle symbol to update
     * @param price The price value 
     * @param timestamp The timestamp of the update
     * @param intentHash The hash of the intent for events
     * @param signer The signer address for events
     * @return updated Whether the data was actually updated
     */
    function _updateOracleDataUnified(
        string memory symbol,
        uint256 price,
        uint256 timestamp,
        bytes32 intentHash,
        address signer
    ) internal returns (bool updated) {
        uint128 timestampU128 = uint128(timestamp);
        uint128 priceU128 = uint128(price);
        
        if (updates[symbol].timestamp >= timestampU128) {
            // Emit event for stale data to provide transparency
            emit IntentBasedStaleUpdateReceived(
                intentHash, 
                symbol, 
                price, 
                timestamp, 
                uint256(updates[symbol].timestamp), 
                signer
            );
            return false;
        }

        updates[symbol] = Data({
            timestamp: timestampU128,
            value: priceU128
        });

        emit IntentBasedUpdateReceived(intentHash, symbol, price, timestamp, signer);
        return true;
    }

    /**
     * @notice Decodes intent data from calldata into OracleIntent structure
     * @param _data The encoded intent data
     * @return intent The decoded OracleIntent
     */
    function _decodeIntentData(bytes calldata _data) internal pure returns (OracleIntentUtils.OracleIntent memory intent) {
        (
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
    ) = abi.decode(
        _data,
        (string, string, uint256, uint256, uint256, string, uint256, uint256, string, bytes, address)
    );
    }
    
    /**
     * @notice Processes a single intent for batch operations
     * @param intent The OracleIntent to process
     * @return updated Whether the intent was processed and data updated
     * @param revertOnFailure Whether to revert on failure or just return false
     */
    function _processIntent(OracleIntentUtils.OracleIntent calldata intent, bool revertOnFailure) internal returns (bool updated) {
        (ValidationStatus status, bytes32 intentHash) = _validateIntentStatus(intent);
        if (status != ValidationStatus.Ok) {
            if (revertOnFailure) {
                if (status == ValidationStatus.Expired) revert IntentExpired();
                if (status == ValidationStatus.UnauthorizedSigner) revert UnauthorizedSigner();
                if (status == ValidationStatus.AlreadyProcessed) revert IntentAlreadyProcessed();
                revert InvalidSignature();
            }
            return false;
        }

        processedIntents[intentHash] = true;
        processedAt[intentHash] = uint64(block.timestamp);
        return _updateOracleDataUnified(intent.symbol, intent.price, intent.timestamp, intentHash, intent.signer);
    }
    
    /**
     * @notice Performs the actual data update for an intent (unified wrapper)
     * @param intent The OracleIntent containing the data
     * @param intentHash The hash of the intent for events
     * @return updated Whether the data was actually updated
     */
    
    

   

   /**
    * @notice Validates the status of an OracleIntent
    * @param intent The OracleIntent structure to validate
    * @return status The validation status
    * @return intentHash The calculated intent hash if status is Ok, else zero
    */
    function _validateIntentStatus(OracleIntentUtils.OracleIntent calldata intent)
        internal
        view
        returns (ValidationStatus status, bytes32 intentHash)
    {
        if (block.timestamp > intent.expiry) {
            return (ValidationStatus.Expired, bytes32(0));
        }
        if (!authorizedSigners[intent.signer]) {
            return (ValidationStatus.UnauthorizedSigner, bytes32(0));
        }

        bytes32 hash = OracleIntentUtils.calculateIntentHash(intent, domainSeparator);
        if (processedIntents[hash]) {
            return (ValidationStatus.AlreadyProcessed, bytes32(0));
        }

        if (OracleIntentUtils.recoverSigner(hash, intent.signature) != intent.signer) {
            return (ValidationStatus.InvalidSignature, bytes32(0));
        }

        return (ValidationStatus.Ok, hash);
    }

    /**
     * @notice Fetches the latest oracle value for a given key
     * @param key The oracle key to query
     * @return value The latest oracle value
     * @return timestamp The timestamp of the latest update
     */
    function getValue(string calldata key) external view returns (uint128, uint128) {
        uint128 value = updates[key].value;
        uint128 timestamp = updates[key].timestamp;
        return (value, timestamp);
    }

    /**
     * @notice Calculates the hash for an OracleIntent
     * @param intent The OracleIntent structure
     * @return The EIP-712 hash of the intent
     * @dev This is useful for external services to verify their intent hashes
     */
    function calculateIntentHash(OracleIntentUtils.OracleIntent calldata intent) external view override returns (bytes32) {
        return OracleIntentUtils.calculateIntentHash(intent, domainSeparator);
    }
    
    
    receive() external payable {}

    fallback() external payable {}
}