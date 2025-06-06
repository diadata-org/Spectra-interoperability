// SPDX-License-Identifier: MIT OR Apache-2.0
pragma solidity 0.8.29;

interface IMessageRecipient {
    function handle(
        uint32 _origin,
        bytes32 _sender,
        bytes calldata _message
    ) external payable;
}
