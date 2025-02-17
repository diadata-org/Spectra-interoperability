pragma solidity 0.8.26;
import {IOracleTrigger} from "../interfaces/IOracleTrigger.sol";


contract MockOracleTrigger is IOracleTrigger {
    // Recorded parameters for verification.
    address public lastMailBox;
    uint32 public lastOrigin;
    address public lastSender;
    string public lastKey;

    event DispatchCalled( uint32 origin, address sender, string key);

       function dispatchToChain(
        uint32 _destinationDomain,
        string memory key
    ) external payable {

    }

    function getMailBox() external pure returns (address){
        return address(uint160(1));
    }

    function dispatch(
         uint32 _origin,
        address _sender,
        string calldata key
    ) external payable  {
         lastOrigin = _origin;
        lastSender = _sender;
        lastKey = key;
        emit DispatchCalled( _origin, _sender, key);
    }
}