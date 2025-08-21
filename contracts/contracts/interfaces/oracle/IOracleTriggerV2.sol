// SPDX-License-Identifier: GPL-3.0
pragma solidity 0.8.29;

interface IOracleTriggerV2 {
    /// @notice Error thrown when a provided address is the zero address
    error InvalidAddress();

    /// @notice Error thrown when trying to interact with a chain that has not been configured
    /// @param chainId The chain ID that is not configured
    error ChainNotConfigured(uint32 chainId);

    /// @notice Error thrown when there is an issue retrieving a value from the oracle
    /// @param key The oracle key that caused the error
    error OracleError(string key);

    /// @notice Error thrown when trying to add a chain that already exists
    /// @param chainId The chain ID that is already configured
    error ChainAlreadyExists(uint32 chainId);

    // @notice Thrown when there is no balance in the contract to withdraw from
    error NoBalanceToWithdraw();

    // @notice Thrown when the transfer of any amount fails
    error AmountTransferFailed();

    /// @notice Emitted when a new chain is added
    /// @param chainId The chain ID of the newly added chain
    /// @param recipientAddress Address of the recipient contract on the chain
    event ChainAdded(uint32 indexed chainId, address recipientAddress);

    /// @notice Emitted when a chain configuration is updated
    /// @param chainId The chain ID being updated
    /// @param oldRecipientAddress Old recipient address
    /// @param recipientAddress New recipient address
    event ChainUpdated(
        uint32 indexed chainId,
        address oldRecipientAddress,
        address recipientAddress
    );

    /// @notice Emitted when a message is dispatched to a destination chain (V2 format with intent data)
    /// @param chainId The destination chain ID
    /// @param recipientAddress The recipient contract address on the destination chain
    /// @param messageId The message ID
    /// @param intentHash The hash of the oracle intent being sent
    /// @param symbol The symbol of the oracle data
    event MessageDispatched(
        uint32 chainId,
        address recipientAddress,
        bytes32 indexed messageId,
        bytes32 intentHash,
        string symbol
    );

    /// @notice Emitted when the mailbox contract address is updated
    /// @param newMailbox The new mailbox contract address
    event MailboxUpdated(address indexed newMailbox);

    /// @notice Emitted when the metadata contract address is updated
    /// @param newMetadata The new metadata contract address
    event MetadataContractUpdated(address indexed newMetadata);
    
    /// @notice Emitted when the intent registry contract address is updated
    /// @param newRegistry The new intent registry contract address
    event IntentRegistryContractUpdated(address indexed newRegistry);

    /// @notice Emitted when tokens are recovered
    /// @param receiver The address of the receiver
    /// @param amount The amount of tokens recovered
    event TokensRecovered(address receiver, uint256 amount);

    /// @notice Dispatches a message to a destination chain with the latest intent for the given symbol
    /// @param _destinationDomain The destination chain ID
    /// @param key The symbol to fetch the latest intent for
    function dispatchToChain(
        uint32 _destinationDomain,
        string memory key
    ) external payable;

    /// @notice Dispatches a message to a destination chain with the latest intent for the given symbol
    /// @param _destinationDomain The destination chain ID
    /// @param _recipientAddress The address of the recipient contract on the destination chain
    /// @param _key The symbol to fetch the latest intent for
    function dispatch(
        uint32 _destinationDomain,
        address _recipientAddress,
        string memory _key
    ) external payable;

    /// @notice Retrieves the mailbox contract address
    /// @return The address of the mailbox contract
    function getMailBox() external view returns (address);
    
    /// @notice Returns the address of the intent registry contract
    function getIntentRegistry() external view returns (address);
}