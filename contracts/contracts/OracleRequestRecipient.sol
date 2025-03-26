// SPDX-License-Identifier: GPL-3.0
pragma solidity 0.8.29;

import { Ownable } from "@openzeppelin/contracts/access/Ownable.sol";

import { IMessageRecipient } from "./interfaces/IMessageRecipient.sol";
import { IOracleTrigger } from "./interfaces/oracle/IOracleTrigger.sol";
import { TypeCasts } from "./libs/TypeCasts.sol";

import { IInterchainSecurityModule, ISpecifiesInterchainSecurityModule } from "./interfaces/IInterchainSecurityModule.sol";
import "@openzeppelin/contracts/security/ReentrancyGuard.sol";

using TypeCasts for address;

/**
 * @title OracleRequestRecipient
 * @notice This contract receives and processes oracle request messages from an interchain network. Whitelisted in Hyperlane
 * @dev Implements security measures and enforces valid sender verification.
 */
contract OracleRequestRecipient is
    Ownable,
    IMessageRecipient,
    ISpecifiesInterchainSecurityModule,
    ReentrancyGuard
{


    /// @notice Address of the interchain security module (ISM)
    IInterchainSecurityModule public interchainSecurityModule;

    /// @notice Address of the whitelisted RequestOracle
    mapping(uint32 => mapping(bytes32 => bool)) public whitelistedSenders;

    /// @notice Address of the Oracle Trigger contract
    address private oracleTriggerAddress;

    /// @notice Emitted when a valid oracle request update is received
    /// @param caller The address that sent the request
    /// @param key The decoded key from the request data
    event ReceivedCall(address indexed caller, string key);

    /// @notice Emitted when a sender's whitelist status is updated
    /// @param origin The source chain ID
    /// @param sender The sender's address in bytes32 format
    /// @param status The new whitelist status (true for whitelisted, false otherwise)
    event WhitelistUpdated(
        uint32 indexed origin,
        bytes32 indexed sender,
        bool status
    );

    /// @notice Emitted when the Oracle Trigger contract address is updated
    /// @param oldAddress Previous Oracle Trigger contract address
    /// @param newAddress New Oracle Trigger contract address
    event OracleTriggerUpdated(
        address indexed oldAddress,
        address indexed newAddress
    );

    /// @notice Emitted when the interchain security module (ISM) address is updated
    /// @param previousISM Previous ISM contract address
    /// @param newISM New ISM contract address
    event InterchainSecurityModuleUpdated(
        address indexed previousISM,
        address indexed newISM
    );


    event TokensRecovered(address indexed recipient, uint256 amount);

    error EmptyOracleRequestData();
    error OracleTriggerNotSet();
    error SenderNotWhitelisted(bytes32 sender, uint32 origin);
    error UnauthorizedCaller(address caller);
    error InvalidSenderAddress();
    error AlreadyWhitelisted(bytes32 sender, uint32 origin);
    error InvalidISMAddress();
    error InvalidOracleTriggerAddress();
    error InvalidReceiver();
    error NoBalanceToWithdraw();
    error TransferFailed();

    /**
     * @notice Handles incoming oracle requests from the interchain network.
     * @dev Ensures only authorized senders can invoke this function and prevents reentrancy attacks.
     * @param _origin The source chain ID from where the request originated
     * @param _sender The sender address in bytes32 format
     * @param _data Encoded payload containing the oracle request key
     */
    function handle(
        uint32 _origin,
        bytes32 _sender,
        bytes calldata _data
    ) external payable virtual override nonReentrant {
        if (_data.length == 0) revert EmptyOracleRequestData();
        if (oracleTriggerAddress == address(0)) revert OracleTriggerNotSet();

        if (!whitelistedSenders[_origin][_sender])
            revert SenderNotWhitelisted(_sender, _origin);

        address sender = address(uint160(uint256(_sender)));

        if (msg.sender != IOracleTrigger(oracleTriggerAddress).getMailBox()) {
            revert UnauthorizedCaller(msg.sender);
        }

        string memory key = abi.decode(_data, (string));

        emit ReceivedCall(sender, key);

        IOracleTrigger(oracleTriggerAddress).dispatch{ value: msg.value }(
            _origin,
            sender,
            key
        );
    }

    /**
     * @notice Adds a sender to the whitelist for a given origin chain.
     * @dev Only callable by the contract owner.
     * @param _origin The source chain ID
     * @param _sender The sender address in bytes32 format
     */

    function addToWhitelist(
        uint32 _origin,
        bytes32 _sender
    ) external onlyOwner {
        if (_sender == bytes32(0)) {
            revert InvalidSenderAddress();
        }

        if (whitelistedSenders[_origin][_sender]) {
            revert AlreadyWhitelisted(_sender, _origin);
        }

        whitelistedSenders[_origin][_sender] = true;
        emit WhitelistUpdated(_origin, _sender, true);
    }

    /**
     * @notice Removes a sender from the whitelist for a given origin chain.
     * @dev Only callable by the contract owner.
     * @param _origin The source chain ID
     * @param _sender The sender address in bytes32 format
     */

    function removeFromWhitelist(
        uint32 _origin,
        bytes32 _sender
    ) external onlyOwner {
        if (_sender == bytes32(0)) {
            revert InvalidSenderAddress();
        }
        whitelistedSenders[_origin][_sender] = false;
        emit WhitelistUpdated(_origin, _sender, false);
    }

    /**
     * @notice Sets the interchain security module (ISM) address.
     * @dev Can only be called by the contract owner.
     * @param _ism Address of the new ISM contract
     */
    function setInterchainSecurityModule(address _ism) external onlyOwner {
        if (_ism == address(0)) {
            revert InvalidISMAddress();
        }
        emit InterchainSecurityModuleUpdated(
            address(interchainSecurityModule),
            _ism
        );

        interchainSecurityModule = IInterchainSecurityModule(_ism);
    }

    /**
     * @notice Sets the Oracle Trigger contract address.
     * @dev Can only be called by the contract owner.
     * @param _oracleTrigger Address of the new Oracle Trigger contract
     */
    function setOracleTriggerAddress(
        address _oracleTrigger
    ) external onlyOwner {
        if (_oracleTrigger == address(0)) {
            revert InvalidOracleTriggerAddress();
        }
        emit OracleTriggerUpdated(oracleTriggerAddress, _oracleTrigger);

        oracleTriggerAddress = _oracleTrigger;
    }

    /**
     * @notice Allow ETH transfers to the contract this is to recover funds if something fails in handle.
     */
    receive() external payable {}

    /**
     * @notice Withdraw ETH to reover stuck funds
     */
    function retrieveLostTokens(address receiver) external onlyOwner {
        if (receiver == address(0)) {
            revert InvalidReceiver();
        }
        uint256 balance = address(this).balance;
        if (balance == 0) {
            revert NoBalanceToWithdraw();
        }
        (bool success, ) = payable(receiver).call{ value: balance }("");
        if (!success) {
            revert TransferFailed();
        }
        emit TokensRecovered(receiver, balance);
    }

    /**
     * @notice Retrieves the current Oracle Trigger contract address.
     * @return Address of the Oracle Trigger contract
     */

    function getOracleTriggerAddress() external view returns (address) {
        return oracleTriggerAddress;
    }
}
