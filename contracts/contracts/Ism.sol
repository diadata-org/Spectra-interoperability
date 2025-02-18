// SPDX-License-Identifier: MIT OR Apache-2.0
pragma solidity >=0.8.0;

import {IInterchainSecurityModule} from "./interfaces/IInterchainSecurityModule.sol";
import {Message} from "./libs/Message.sol";
import {TypeCasts} from "./libs/TypeCasts.sol";

import {Ownable} from "@openzeppelin/contracts/access/Ownable.sol";



using TypeCasts for address;


/// @title Interchain Security Module (ISM)
/// @notice A simple ISM implementation that verifies messages based on the sender address.
/// @dev The contract allows the owner to set the expected sender address and a flag to allow all messages.
contract Ism is IInterchainSecurityModule,Ownable {
    uint8 public constant override moduleType = uint8(Types.NULL);

    /// @notice Expected sender address for message verification. This can be OracleTrigger or RequestOracle contract address
    mapping(uint32 => address) private senderShouldBe;

    /// @notice Flag that, when set to true, allows all messages to pass verification.
    bool public allowAll;  

    /// @notice Emitted when the expected sender address is updated.
    /// @param originDomain origin chain address.
    /// @param previousSender The previous expected sender address.
    /// @param newSender The new expected sender address.
    event SenderShouldBeUpdated(uint32 indexed originDomain, address indexed previousSender, address indexed newSender);
    
    /// @notice Emitted when the allowAll flag is updated.
    /// @param previousValue The previous value of allowAll.
    /// @param newValue The new value of allowAll.
    event AllowAllUpdated(bool previousValue, bool newValue);


    /// @notice Retrieves the expected sender address for verification.
    /// @return The address expected to send valid messages.
    function getSenderShouldBe(uint32 _originDomain)  external view returns(address){
        return senderShouldBe[_originDomain] ;
    }

    /// @notice Sets the expected sender address for message verification.
    /// @dev Only callable by the owner.
    /// @param _originDomain Origin chain id.
    /// @param _sender The new expected sender address.
    function setSenderShouldBe(uint32 _originDomain, address _sender) onlyOwner external{
        emit SenderShouldBeUpdated(_originDomain, senderShouldBe[_originDomain], _sender);
        senderShouldBe[_originDomain] = _sender;

     }

    /// @notice Sets the flag to allow all messages to pass verification.
    /// @dev Only callable by the owner.
    /// @param _allowAll Boolean value to enable or disable the allowAll flag.
     function setAllowAll(bool _allowAll) external onlyOwner {
        emit AllowAllUpdated(allowAll, _allowAll);
        allowAll = _allowAll;
    }


    /// @notice Verifies a message based on the sender address.
    /// @dev If allowAll is true, the message always passes verification.
    /// @param /* unused */ 
    /// @param _message The encoded message data from which the sender address is extracted.
    /// @return True if the message is verified, false otherwise.
    function verify(
        bytes calldata,
        bytes calldata _message
    ) public view returns (bool) {
        if (allowAll) {
            return true;  
        }else{
    uint32 originDomain = Message.origin(_message);
         return senderShouldBe[originDomain] == Message.senderAddress(_message);
        }
        
    }
}
