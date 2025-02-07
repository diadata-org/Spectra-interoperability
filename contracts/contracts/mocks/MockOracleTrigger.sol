pragma solidity 0.8.26;
import {IOracleTrigger} from "../interfaces/IOracleTrigger.sol";


contract MockOracleTrigger is IOracleTrigger {
    // Recorded parameters for verification.
    address public lastMailBox;
    uint32 public lastOrigin;
    address public lastSender;
    string public lastKey;

    event DispatchCalled(address mailbox, uint32 origin, address sender, string key);

       function dispatchToChain(
        uint32 _destinationDomain,
        string memory key
    ) external payable {

    }

    function dispatch(
        address _mailBox,
        uint32 _origin,
        address _sender,
        string calldata key
    ) external payable  {
        lastMailBox = _mailBox;
        lastOrigin = _origin;
        lastSender = _sender;
        lastKey = key;
        emit DispatchCalled(_mailBox, _origin, _sender, key);
    }
}