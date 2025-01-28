// SPDX-License-Identifier: MIT OR Apache-2.0
pragma solidity >=0.8.0;

import {IInterchainSecurityModule} from "./interfaces/IInterchainSecurityModule.sol";
import {Message} from "./libs/Message.sol";
import {TypeCasts} from "./libs/TypeCasts.sol";


using TypeCasts for address;


contract Ism is IInterchainSecurityModule {
    uint8 public constant override moduleType = uint8(Types.NULL);
    bytes32 public senderShouldBe;

    function setSenderShouldBe(address sender) external{
        senderShouldBe = sender.addressToBytes32();
    }

    function verify(
        bytes calldata,
        bytes calldata _message
    ) external view override returns (bool) {
        return senderShouldBe == Message.sender(_message);

         return true;
        
    }
}
