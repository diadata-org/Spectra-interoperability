// SPDX-License-Identifier: GPL-3.0
pragma solidity 0.8.29;

interface IOracleTrigger {
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

    /// @notice Emitted when a message is dispatched to a destination chain
    /// @param chainId The destination chain ID
    /// @param recipientAddress The recipient contract address on the destination chain
    /// @param messageId The message ID
    event MessageDispatched(
        uint32 chainId,
        address recipientAddress,
        bytes32 indexed messageId
    );

    /// @notice Emitted when the mailbox contract address is updated
    /// @param newMailbox The new mailbox contract address
    event MailboxUpdated(address indexed newMailbox);

    /// @notice Emitted when the metadata contract address is updated
    /// @param newMetadata The new metadata contract address
    event MetadataContractUpdated(address indexed newMetadata);

    /// @notice Emitted when tokens are recovered
    /// @param receiver The address of the receiver
    /// @param amount The amount of tokens recovered
    event TokensRecovered(address receiver, uint256 amount);

    /// @notice Adds a new chain to the configuration
    /// @param chainId The chain ID of the new chain
    /// @param recipientAddress The address of the recipient contract on the new chain
    function addChain(uint32 chainId, address recipientAddress) external;

    /// @notice Updates the recipient address for a specific chain
    /// @param chainId The chain ID of the chain to update
    /// @param recipientAddress The new address of the recipient contract
    function updateChain(uint32 chainId, address recipientAddress) external;

    /// @notice Retrieves the recipient address for a specific chain
    /// @param _chainId The chain ID of the chain to query
    /// @return The address of the recipient contract on the specified chain
    function viewChain(uint32 _chainId) external view returns (address);

    /// @notice Updates the metadata contract address
    /// @param newMetadata The new metadata contract address
    function updateMetadataContract(address newMetadata) external;

    /// @notice Dispatches a message to a destination chain
    /// @param _destinationDomain The destination chain ID
    /// @param key The key used to fetch the oracle value
    function dispatchToChain(
        uint32 _destinationDomain,
        string memory key
    ) external payable;

    /// @notice Dispatches a message to a destination chain
    /// @param _destinationDomain The destination chain ID
    /// @param _recipientAddress The address of the recipient contract on the destination chain
    /// @param _key The key used to fetch the oracle value
    function dispatch(
        uint32 _destinationDomain,
        address _recipientAddress,
        string memory _key
    ) external payable;

    /// @notice Sets the mailbox contract address
    /// @param _mailbox The new mailbox contract address
    function setMailBox(address _mailbox) external;

    /// @notice Retrieves lost tokens
    /// @param receiver The address of the receiver
    function retrieveLostTokens(address receiver) external;

    /// @notice Retrieves the mailbox contract address
    /// @return The address of the mailbox contract
    function getMailBox() external view returns (address);
}
