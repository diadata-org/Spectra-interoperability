// SPDX-License-Identifier: MIT OR Apache-2.0
pragma solidity >=0.8.0;

import {Ownable} from "@openzeppelin/contracts/access/Ownable.sol";

import {IMessageRecipient} from "./interfaces/IMessageRecipient.sol";
import {IOracleTrigger} from "./interfaces/IOracleTrigger.sol";
import {TypeCasts} from "./libs/TypeCasts.sol";

import {IInterchainSecurityModule, ISpecifiesInterchainSecurityModule} from "./interfaces/IInterchainSecurityModule.sol";
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
    address public oracleTriggerAddress;

    /// @notice Emitted when a valid oracle request update is received
    /// @param caller The address that sent the request
    /// @param key The decoded key from the request data
    event ReceivedCall(address indexed caller, string key);


    event WhitelistUpdated(uint32 indexed origin, bytes32 indexed sender, bool status);

    event OracleTriggerUpdated(address indexed oldAddress, address indexed newAddress);

    event ISMUpdated(address indexed oldISM, address indexed newISM);

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
        require(_data.length > 0, "Oracle request data cannot be empty");
        require(
            oracleTriggerAddress != address(0),
            "Oracle trigger address not set"
        );
        require(whitelistedSenders[_origin][_sender], "Sender not whitelisted for this origin");

        address sender = address(uint160(uint256(_sender)));

 
        require(
            msg.sender == IOracleTrigger(oracleTriggerAddress).getMailBox(),
            "Unauthorized caller"
        );

        string memory key = abi.decode(_data, (string));

        emit ReceivedCall(sender, key);

         IOracleTrigger(oracleTriggerAddress).dispatch{value: msg.value}(_origin, sender, key);

    }


     function addToWhitelist(uint32 _origin, bytes32 _sender) onlyOwner external {
        require(_sender != bytes32(0), "Invalid sender address");
        require(!whitelistedSenders[_origin][_sender], "Already whitelisted");
        
        whitelistedSenders[_origin][_sender] = true;
        emit WhitelistUpdated(_origin, _sender, true);

     }

    function removeFromWhitelist(uint32 _origin, bytes32 _sender) onlyOwner external {
        require(_sender != bytes32(0), "Invalid sender address");
        whitelistedSenders[_origin][_sender] = false;
        emit WhitelistUpdated(_origin, _sender, false);

     }

    /**
     * @notice Sets the interchain security module (ISM) address.
     * @dev Can only be called by the contract owner.
     * @param _ism Address of the new ISM contract
     */
    function setInterchainSecurityModule(address _ism) external onlyOwner {
        require(_ism != address(0), "Invalid ISM address");
        emit ISMUpdated(address(interchainSecurityModule), _ism);

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
        require(_oracleTrigger != address(0), "Invalid oracle trigger address");
        emit OracleTriggerUpdated(oracleTriggerAddress, _oracleTrigger);

        oracleTriggerAddress = _oracleTrigger;
    }

    /**
     * @notice Allow ETH transfers to the contract this is to recover funds if something fails in handle.
     */
    receive() external payable {
     }

    /**
    * @notice Withdraw ETH to reover stuck funds 
     */
    function withdrawETH(address payable recipient) external onlyOwner {
    require(recipient != address(0), "Invalid recipient");
     recipient.transfer(address(this).balance);
    }

    /**
     * @notice Retrieves the current interchain security module address.
     * @return Address of the ISM contract
     */
    function getInterchainSecurityModule() external view returns (address) {
        return address(interchainSecurityModule);
    }

    /**
     * @notice Retrieves the current Oracle Trigger contract address.
     * @return Address of the Oracle Trigger contract
     */

    function getOracleTriggerAddress() external view returns (address) {
        return oracleTriggerAddress;
    }
}
