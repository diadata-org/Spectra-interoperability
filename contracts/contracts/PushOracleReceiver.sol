// SPDX-License-Identifier: MIT OR Apache-2.0
pragma solidity 0.8.29;

import {Ownable} from "@openzeppelin/contracts/access/Ownable.sol";

import {IMessageRecipient} from "./interfaces/IMessageRecipient.sol";
import {IInterchainSecurityModule, ISpecifiesInterchainSecurityModule} from "./interfaces/IInterchainSecurityModule.sol";
import {IMailbox} from "./interfaces/IMailbox.sol";
import {IPostDispatchHook} from "./interfaces/hooks/IPostDispatchHook.sol";
import {ProtocolFeeHook} from "./ProtocolFeeHook.sol";

import {TypeCasts} from "./libs/TypeCasts.sol";
 // import "forge-std/console.sol";

using TypeCasts for address;



/**
 * @title PushOracleReceiver
 * @notice Handles incoming oracle data updates.Whitelisted in Hyperlane
 */
contract PushOracleReceiver is
    Ownable,
    IMessageRecipient,
    ISpecifiesInterchainSecurityModule
{
    /// @notice Reference to the interchain security module.
    IInterchainSecurityModule public interchainSecurityModule;

    /// @notice Address for the post-dispatch payment hook.
    address payable public paymentHook;

    address public walletFactory;

    /// @notice only Message from this mailbox will be handled
    address public trustedMailBox;

    error ZeroAddress();

    /**
     * @notice Structure representing an oracle data update.
     * @param key The identifier for the data.
     * @param timestamp The timestamp when the data was recorded.
     * @param value The numerical value associated with the key.
     */
    struct Data {
        uint128 timestamp;
        uint128 value;
    }

    uint256 public gasUsedPerTx = 97440; // Default gas used

    /// @notice Mapping of oracle data updates by key.
    mapping(string => Data) public updates;

    /// @notice Emitted when a new oracle data message is received.
    event ReceivedMessage(string key, uint128 timestamp, uint128 value);

    /// @notice Emitted when a call is received (currently unused).
    event ReceivedCall(address indexed caller, uint256 amount, string message);


    event TrustedMailBoxUpdated(address indexed previousMailBox, address indexed newMailBox);

    event InterchainSecurityModuleUpdated(address indexed previousISM, address indexed newISM);
    
    event PaymentHookUpdated(address indexed previousPaymentHook, address indexed newPaymentHook);



    /// @notice Ensures that the provided address is not a zero address.
    modifier validateAddress(address _address) {
        if (_address == address(0)) revert ZeroAddress();
        _;
    }

  

    /**
     * @notice Handles incoming interchain messages by decoding the payload and updating state.
     * @param _origin The origin domain identifier.
     * @param _sender The sender's address (in bytes32 format).
     * @param _data The encoded payload containing the oracle data.
     */
    function handle(
        uint32 _origin,
        bytes32 _sender,
        bytes calldata _data
    ) external payable override validateAddress(paymentHook) {
        require(msg.sender == trustedMailBox, "Unauthorized Mailbox");

        // Decode the incoming data into its respective components.
        (string memory key, uint128 timestamp, uint128 value) = abi.decode(
            _data,
            (string, uint128, uint128)
        );

        // Ensure the new timestamp is more recent
        if (updates[key].timestamp >= timestamp) {
            return; // Ignore outdated data
        }

        // Update the stored oracle data.
        Data memory newData = Data({
            timestamp: timestamp,
            value: value
        });
        updates[key] = newData;

        emit ReceivedMessage(key, timestamp, value);

        uint256 gasPrice = tx.gasprice;

        uint256 fee = ProtocolFeeHook(payable(paymentHook)).gasUsedPerTx() *
            gasPrice;

        // console.log("gasPrice", gasPrice);

        // Send the fee to the payment hook

        bool success;
        {
            (success, ) = paymentHook.call{value: fee}("");
        }
        // console.log("address success", success);

        require(success, "Fee transfer failed");
    }

    /**
     * @notice Sets the interchain security module.
     * @param _ism The address of the interchain security module.
     */
    function setInterchainSecurityModule(address _ism) external onlyOwner validateAddress(_ism) {
        emit InterchainSecurityModuleUpdated(address(interchainSecurityModule), _ism);  
        interchainSecurityModule = IInterchainSecurityModule(_ism);
    }

    function setPaymentHook(address payable _paymentHook) external onlyOwner validateAddress(_paymentHook) {
        emit PaymentHookUpdated(paymentHook, _paymentHook);  
        paymentHook = _paymentHook;
    }

      function setTrustedMailBox(address _mailbox) external onlyOwner validateAddress(_mailbox) {
        emit TrustedMailBoxUpdated(trustedMailBox, _mailbox);  
        trustedMailBox = _mailbox;
    }

     

    receive() external payable {}

    fallback() external payable {}
}
