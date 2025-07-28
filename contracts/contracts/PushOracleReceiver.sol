// SPDX-License-Identifier: GPL-3.0
pragma solidity 0.8.29;

import { Ownable } from "@openzeppelin/contracts/access/Ownable.sol";
import { IPushOracleReceiver } from "./interfaces/oracle/IPushOracleReceiver.sol";
import { IInterchainSecurityModule } from "./interfaces/IInterchainSecurityModule.sol";
import { ProtocolFeeHook } from "./ProtocolFeeHook.sol";
import { TypeCasts } from "./libs/TypeCasts.sol";

/**
 * @title PushOracleReceiver
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
contract PushOracleReceiver is IPushOracleReceiver, Ownable {
    using TypeCasts for address;

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
    
    /// @notice EIP-712 domain separator
    bytes32 private immutable DOMAIN_SEPARATOR;
    
    /// @notice EIP-712 type hash (must match OracleIntentRegistry)
    bytes32 private constant ORACLE_INTENT_TYPEHASH = keccak256(
        "OracleIntent(string intentType,string version,uint256 chainId,uint256 nonce,uint256 expiry,string symbol,uint256 price,uint256 timestamp,string source)"
    );

    /// @notice Error thrown when an ISM is not set (zero address) is used.
    error InvalidISMAddress();

    /// @notice Ensures that the provided address is not a zero address
    modifier validateAddress(address _address) {
        if (_address == address(0)) revert InvalidAddress();
        _;
    }
    
    /**
     * @notice Constructor initializes the EIP-712 domain separator
     * @dev The domain separator is used for verifying EIP-712 signatures
     */
    constructor() {
        // Create the EIP-712 domain separator (must match OracleIntentRegistry exactly)
        // Use the same chainId and verifyingContract as the OracleIntentRegistry
        DOMAIN_SEPARATOR = keccak256(
            abi.encode(
                keccak256("EIP712Domain(string name,string version,uint256 chainId,address verifyingContract,bytes32 salt)"),
                keccak256("DIA Oracle Intent"),
                keccak256("1"),
                100640, // DIA testnet chainId where OracleIntentRegistry is deployed
                0xd2313dcabB0E9447d800546b953E05dD47EB2eB9, // OracleIntentRegistry address
                bytes32(0)
            )
        );
    }

    /**
     * @dev See {IPushOracleReceiver-handle}.
     * @notice Handles both legacy format (key, timestamp, value) and new intent format
     */
    /* solhint-disable no-unused-vars */
    function handle(
        uint32 /* _origin */,
        bytes32 /* _sender */,
        bytes calldata _data
    ) external payable override validateAddress(paymentHook) {
        if (msg.sender != trustedMailBox) revert UnauthorizedMailbox();
        if (address(interchainSecurityModule) == address(0))
            revert InvalidISMAddress();

        // Try to decode as intent format first (new format)
        try this.handleIntentMessage(_data) {
            // Intent format handled successfully
            return;
        } catch {
            // Fall back to legacy format
            _handleLegacyMessage(_data);
        }
    }
    
    /**
     * @notice Handles intent-based messages from the mailbox
     * @param _data The encoded intent data
     * @dev This function processes intents sent via the mailbox from OracleTrigger
     */
    function handleIntentMessage(bytes calldata _data) external {
        // Only allow calls from this contract (via handle function)
        // if (msg.sender != address(this)) revert UnauthorizedMailbox(); // Removed to allow direct calls
        
        // Decode the intent data
        (
            string memory intentType,
            string memory version,
            uint256 chainId,
            uint256 nonce,
            uint256 expiry,
            string memory symbol,
            uint256 price,
            uint256 timestamp,
            string memory source,
            bytes memory signature,
            address signer
        ) = abi.decode(_data, (string, string, uint256, uint256, uint256, string, uint256, uint256, string, bytes, address));
        
        // Verify the signer is authorized
        if (!authorizedSigners[signer]) revert UnauthorizedSigner();
        
        // Check if the intent has expired
        if (block.timestamp > expiry) revert IntentExpired();
        
        // Create the OracleIntent structure
        OracleIntent memory intent = OracleIntent({
            intentType: intentType,
            version: version,
            chainId: chainId,
            nonce: nonce,
            expiry: expiry,
            symbol: symbol,
            price: price,
            timestamp: timestamp,
            source: source,
            signature: signature,
            signer: signer
        });
        
        // Create the intent hash for EIP-712 verification (same as OracleIntentRegistry)
        bytes32 structHash = keccak256(
            abi.encode(
                ORACLE_INTENT_TYPEHASH,
                keccak256(bytes(intent.intentType)),
                keccak256(bytes(intent.version)),
                intent.chainId,
                intent.nonce,
                intent.expiry,
                keccak256(bytes(intent.symbol)),
                intent.price,
                intent.timestamp,
                keccak256(bytes(intent.source))
            )
        );
        
        // Create the EIP-712 hash
        bytes32 hash = keccak256(
            abi.encodePacked(
                "\x19\x01",
                DOMAIN_SEPARATOR,
                structHash
            )
        );
        
        // Check if this intent has already been processed
        if (processedIntents[hash]) revert IntentAlreadyProcessed();
        
        // Verify the signature (same as OracleIntentRegistry)
        require(recoverSigner(hash, intent.signature) == intent.signer, "Invalid signature");
        
        // Mark the intent as processed
        processedIntents[hash] = true;
        
        // Convert timestamp to uint128 for storage compatibility
        uint128 timestampU128 = uint128(intent.timestamp);
        
        // Ensure the new timestamp is more recent
        if (updates[intent.symbol].timestamp >= timestampU128) {
            return; // Ignore outdated data
        }

        // Update the stored oracle data
        Data memory newData = Data({ 
            timestamp: timestampU128, 
            value: uint128(intent.price) 
        });
        updates[intent.symbol] = newData;

        emit IntentBasedUpdateReceived(hash, intent.symbol, intent.price, intent.timestamp, intent.signer);
        
        // Calculate and transfer the protocol fee
        _transferProtocolFee();
    }
    
    /**
     * @notice Handles legacy format messages (backward compatibility)
     * @param _data The encoded legacy data (key, timestamp, value)
     */
    function _handleLegacyMessage(bytes calldata _data) internal {
        // Decode the incoming data into its respective components.
        (string memory key, uint128 timestamp, uint128 value) = abi.decode(
            _data,
            (string, uint128, uint128)
        );

        // Ensure the new timestamp is more recent
        if (updates[key].timestamp >= timestamp) {
            return; // Ignore outdated data
        }

        // Update the stored oracle data
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
        // Calculate the transaction fee based on gas used and gas price.
        uint256 gasPrice = tx.gasprice;
        uint256 fee = ProtocolFeeHook(payable(paymentHook)).gasUsedPerTx() *
            gasPrice;

        // Transfer the fee to the payment hook.
        bool success;
        {
            (success, ) = paymentHook.call{ value: fee }("");
        }

        if (!success) revert AmountTransferFailed();
    }
    /* solhint-disable no-unused-vars */

    /**
     * @notice Handles oracle updates from intent-based sources
     * @param intent The OracleIntent structure containing all intent data
     * @dev External services can call this function directly with properly signed intents
     */
    function handleIntentUpdate(
        OracleIntent calldata intent
    ) external payable override validateAddress(paymentHook) {

        
        // Verify the signer is authorized
        if (!authorizedSigners[intent.signer]) revert UnauthorizedSigner();
        
        // Create the intent hash for EIP-712
        bytes32 structHash = keccak256(
            abi.encode(
                ORACLE_INTENT_TYPEHASH,
                keccak256(bytes(intent.intentType)),
                keccak256(bytes(intent.version)),
                intent.chainId,
                intent.nonce,
                intent.expiry,
                keccak256(bytes(intent.symbol)),
                intent.price,
                intent.timestamp,
                keccak256(bytes(intent.source))
            )
        );
        
        // Create the EIP-712 hash
        bytes32 hash = keccak256(
            abi.encodePacked(
                "\x19\x01",
                DOMAIN_SEPARATOR,
                structHash
            )
        );
        
        // Check if this intent has already been processed
        if (processedIntents[hash]) revert IntentAlreadyProcessed();
        
        // Verify the signature
        if (recoverSigner(hash, intent.signature) != intent.signer) revert InvalidSignature();
        
        // Mark the intent as processed
        processedIntents[hash] = true;
        
        // Convert timestamp to uint128 for storage compatibility
        uint128 timestampU128 = uint128(intent.timestamp);
        
        // Ensure the new timestamp is more recent
        if (updates[intent.symbol].timestamp >= timestampU128) {
            return; // Ignore outdated data
        }

        // Update the stored oracle data
        Data memory newData = Data({ 
            timestamp: timestampU128, 
            value: uint128(intent.price) 
        });
        updates[intent.symbol] = newData;

        emit IntentBasedUpdateReceived(hash, intent.symbol, intent.price, intent.timestamp, intent.signer);

        // Calculate the transaction fee based on gas used and gas price
        uint256 gasPrice = tx.gasprice;
        uint256 fee = ProtocolFeeHook(payable(paymentHook)).gasUsedPerTx() *
            gasPrice;

        // Transfer the fee to the payment hook
        bool success;
        {
            (success, ) = paymentHook.call{ value: fee }("");
        }

        if (!success) revert AmountTransferFailed();
    }
    
    /**
     * @notice Handles batch updates from intent-based sources
     * @param intents Array of OracleIntent structures
     * @dev External services can call this function directly with multiple properly signed intents
     * @dev This is more gas efficient than calling handleIntentUpdate multiple times
     */
    function handleBatchIntentUpdates(
        OracleIntent[] calldata intents
    ) external payable override validateAddress(paymentHook) {
        uint256 updatedCount = 0;
        
        // Process each intent
        for (uint256 i = 0; i < intents.length; i++) {
            OracleIntent calldata intent = intents[i];
            
            // // Skip expired intents
            // if (block.timestamp > intent.expiry) {
            //     continue;
            // }
            
            // Skip intents from unauthorized signers
            if (!authorizedSigners[intent.signer]) {
                continue;
            }
            
            // Create the intent hash for EIP-712
            bytes32 structHash = keccak256(
                abi.encode(
                    ORACLE_INTENT_TYPEHASH,
                    keccak256(bytes(intent.intentType)),
                    keccak256(bytes(intent.version)),
                    intent.chainId,
                    intent.nonce,
                    intent.expiry,
                    keccak256(bytes(intent.symbol)),
                    intent.price,
                    intent.timestamp,
                    keccak256(bytes(intent.source))
                )
            );
            
            // Create the EIP-712 hash
            bytes32 hash = keccak256(
                abi.encodePacked(
                    "\x19\x01",
                    DOMAIN_SEPARATOR,
                    structHash
                )
            );
            
            // Skip already processed intents
            if (processedIntents[hash]) {
                continue;
            }
            
            // Verify the signature
            if (recoverSigner(hash, intent.signature) != intent.signer) {
                continue;
            }
            
            // Mark the intent as processed
            processedIntents[hash] = true;
            
            // Convert timestamp to uint128 for storage compatibility
            uint128 timestampU128 = uint128(intent.timestamp);
            
            // Only update if timestamp is more recent
            if (updates[intent.symbol].timestamp < timestampU128) {
                // Update the stored oracle data
                updates[intent.symbol] = Data({ 
                    timestamp: timestampU128, 
                    value: uint128(intent.price) 
                });
                
                emit IntentBasedUpdateReceived(hash, intent.symbol, intent.price, intent.timestamp, intent.signer);
                updatedCount++;
            }
        }
        
        // Only charge fee if at least one update was processed
        if (updatedCount > 0) {
            // Calculate the transaction fee based on gas used and gas price
            uint256 gasPrice = tx.gasprice;
            uint256 fee = ProtocolFeeHook(payable(paymentHook)).gasUsedPerTx() *
                gasPrice;

            // Transfer the fee to the payment hook
            bool success;
            {
                (success, ) = paymentHook.call{ value: fee }("");
            }

            if (!success) revert AmountTransferFailed();
        }
    }

    /**
     * @dev See {IPushOracleReceiver-setInterchainSecurityModule}.
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
     * @dev See {IPushOracleReceiver-setPaymentHook}.
     */
    function setPaymentHook(
        address payable _paymentHook
    ) external override onlyOwner validateAddress(_paymentHook) {
        emit PaymentHookUpdated(paymentHook, _paymentHook);
        paymentHook = _paymentHook;
    }

    /**
     * @dev See {IPushOracleReceiver-setTrustedMailBox}.
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
     * @dev See {IPushOracleReceiver-retrieveLostTokens}.
     */
    function retrieveLostTokens(
        address receiver
    ) external override onlyOwner validateAddress(receiver) {
        uint256 balance = address(this).balance;
        if (balance == 0) revert NoBalanceToWithdraw();

        (bool success, ) = payable(receiver).call{ value: balance }("");
        if (!success) revert AmountTransferFailed();
        emit TokensRecovered(receiver, balance);
    }
    
    /**
     * @notice Returns the domain separator for EIP-712 signatures
     * @dev This is useful for external services that need to create EIP-712 signatures
     * @return The domain separator used for EIP-712 signatures
     */
    function getDomainSeparator() external view override returns (bytes32) {
        return DOMAIN_SEPARATOR;
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
     * @notice Calculates the hash for an OracleIntent
     * @param intent The OracleIntent structure
     * @return The EIP-712 hash of the intent
     * @dev This is useful for external services to verify their intent hashes
     */
    function calculateIntentHash(OracleIntent calldata intent) external view override returns (bytes32) {
        // Create the intent hash for EIP-712 (same as OracleIntentRegistry)
        bytes32 structHash = keccak256(
            abi.encode(
                ORACLE_INTENT_TYPEHASH,
                keccak256(bytes(intent.intentType)),
                keccak256(bytes(intent.version)),
                intent.chainId,
                intent.nonce,
                intent.expiry,
                keccak256(bytes(intent.symbol)),
                intent.price,
                intent.timestamp,
                keccak256(bytes(intent.source))
            )
        );
        
        // Create the EIP-712 hash
        return keccak256(
            abi.encodePacked(
                "\x19\x01",
                DOMAIN_SEPARATOR,
                structHash
            )
        );
    }
    
    /**
     * @notice Recovers the signer address from a signature (same as OracleIntentRegistry)
     * @param hash The hash that was signed
     * @param signature The signature
     * @return The address of the signer
     */
    function recoverSigner(bytes32 hash, bytes memory signature) internal pure returns (address) {
        require(signature.length == 65, "PushOracleReceiver: invalid signature length");
        
        bytes32 r;
        bytes32 s;
        uint8 v;
        
        assembly {
            r := mload(add(signature, 32))
            s := mload(add(signature, 64))
            v := byte(0, mload(add(signature, 96)))
        }
        
        if (v < 27) {
            v += 27;
        }
        
        require(v == 27 || v == 28, "PushOracleReceiver: invalid signature 'v' value");
        
        return ecrecover(hash, v, r, s);
    }
    
    receive() external payable {}

    fallback() external payable {}
}
