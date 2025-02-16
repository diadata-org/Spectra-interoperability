pragma solidity ^0.8.26;

import {IMailbox} from "../interfaces/IMailbox.sol";
import {IInterchainSecurityModule} from "../interfaces/IInterchainSecurityModule.sol";
import {IPostDispatchHook} from "../interfaces/hooks/IPostDispatchHook.sol";


contract MockMailbox is IMailbox {
    function dispatch(
        uint32,
        bytes32,
        bytes calldata,
        bytes calldata,
        IPostDispatchHook
    ) external payable returns (bytes32) {
        return bytes32(uint256(1));
    }

    function process(bytes calldata, bytes calldata) external payable {}

    function dispatch(
        uint32,
        bytes32,
        bytes calldata
    ) external payable returns (bytes32) {
        return bytes32(uint256(1));
    }

    function dispatch(
        uint32,
        bytes32,
        bytes calldata,
        bytes calldata
    ) external payable returns (bytes32) {
        return bytes32(uint256(1));
    }

    function quoteDispatch(
        uint32,
        bytes32,
        bytes calldata
    ) external pure returns (uint256) {
        return 0;
    }

    function quoteDispatch(
        uint32,
        bytes32,
        bytes calldata,
        bytes calldata
    ) external pure returns (uint256) {
        return 0;
    }

    function quoteDispatch(
        uint32,
        bytes32,
        bytes calldata,
        bytes calldata,
        IPostDispatchHook
    ) external pure returns (uint256) {
        return 0;
    }

    function delivered(bytes32) external pure returns (bool) {
        return false;
    }

    function recipientIsm(
        address
    ) external pure returns (IInterchainSecurityModule) {
        return IInterchainSecurityModule(address(0));
    }

    function defaultIsm() external pure returns (IInterchainSecurityModule) {
        return IInterchainSecurityModule(address(0));
    }

    function defaultHook() external pure returns (IPostDispatchHook) {
        return IPostDispatchHook(address(0));
    }

    function requiredHook() external pure returns (IPostDispatchHook) {
        return IPostDispatchHook(address(0));
    }

    function localDomain() external pure returns (uint32) {
        return 1;
    }

    function latestDispatchedId() external pure returns (bytes32) {
        return bytes32(0);
    }
}