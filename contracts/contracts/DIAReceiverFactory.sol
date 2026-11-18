// SPDX-License-Identifier: GPL-3.0
pragma solidity 0.8.29;

import { PushOracleReceiverV2 } from "./PushOracleReceiverV2.sol";

/**
 * @title DIAReceiverFactory
 * @author Diadata.org
 * @notice Deploys PushOracleReceiverV2 via CREATE2 and hands ownership to the caller
 *         within the same transaction.
 * @dev Solves the Ownable-vs-CREATE2 problem: deploying an Ownable contract through a
 *      factory (e.g. the Arachnid deterministic-deployment-proxy) makes the factory the
 *      owner, permanently bricking every onlyOwner setter. This factory deploys the
 *      receiver and immediately calls transferOwnership(msg.sender) before returning —
 *      there is never a block where the factory holds ownership.
 *
 *      The CREATE2 salt is namespaced by caller: keccak256(abi.encode(msg.sender, userSalt)).
 *      Replayed calldata therefore deploys to a different address with the replayer as
 *      owner — no frontrun/grief vector.
 *
 *      This contract is deliberately NOT Ownable, holds no storage and no privileges,
 *      so it is safe to deploy through the Arachnid proxy itself.
 */
contract DIAReceiverFactory {
    event ReceiverDeployed(address indexed receiver, address indexed owner, bytes32 indexed userSalt);

    /**
     * @notice Deploy a PushOracleReceiverV2 owned by the caller.
     * @param userSalt Caller-chosen salt; final CREATE2 salt is keccak256(abi.encode(msg.sender, userSalt))
     * @param domainName EIP-712 domain name for the receiver
     * @param domainVersion EIP-712 domain version for the receiver
     * @param sourceChainId Chain id of the source OracleIntentRegistry
     * @param verifyingContract OracleIntentRegistry address on the source chain
     * @return receiverAddr The deployed receiver address
     */
    function deployReceiver(
        bytes32 userSalt,
        string calldata domainName,
        string calldata domainVersion,
        uint256 sourceChainId,
        address verifyingContract
    ) external returns (address receiverAddr) {
        bytes32 salt = keccak256(abi.encode(msg.sender, userSalt));
        PushOracleReceiverV2 receiver = new PushOracleReceiverV2{ salt: salt }(
            domainName,
            domainVersion,
            sourceChainId,
            verifyingContract
        );
        receiver.transferOwnership(msg.sender);
        emit ReceiverDeployed(address(receiver), msg.sender, userSalt);
        return address(receiver);
    }
}
