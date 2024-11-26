pragma solidity >=0.8.0;


interface IOracleTrigger {


   function dispatchToChain(
        uint32 _destinationDomain,
        string memory key
    ) external payable ;

     function dispatch(
        address _mailbox,
        uint32 _destinationDomain,
        address recipientAddress,
        string memory key
    ) external payable;

}

  