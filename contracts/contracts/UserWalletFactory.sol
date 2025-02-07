// SPDX-License-Identifier: MIT
pragma solidity ^0.8.17;

import "./UserWallet.sol";

 
 /// @title UserWalletFactory
/// @notice Deploys new UserWallet contracts using CREATE2 for deterministic addresses.
contract UserWalletFactory {

    event WalletDeployed(address indexed owner, address wallet);

     
      /// @notice Deploys a new UserWallet for the caller if one doesn't already exist.
    /// @return walletAddress The address of the deployed (or existing) wallet.
    function deployWallet() external returns (address walletAddress) {
        address owner = msg.sender;
 
        bytes32 salt = keccak256(abi.encodePacked(owner));

        
        walletAddress = getAddress(owner);
        if (_isContract(walletAddress)) {
             return walletAddress;
        }

         walletAddress = address(new UserWallet{salt: salt}(owner));

        emit WalletDeployed(owner, walletAddress);
    }

        /// @notice Computes the deterministic address for a wallet deployed by this factory.
    /// @param owner The owner address used in the wallet constructor.
    /// @return The deterministic wallet address.
    function getAddress(address owner) public view returns (address) {
       
        bytes memory userWalletBytecode = abi.encodePacked(
            type(UserWallet).creationCode,
            abi.encode(owner) // constructor arg
        );
        bytes32 bytecodeHash = keccak256(userWalletBytecode);

        
        bytes32 salt = keccak256(abi.encodePacked(owner));
        bytes32 rawHash = keccak256(
            abi.encodePacked(
                bytes1(0xff),
                address(this),
                salt,
                bytecodeHash
            )
        );

       
        return address(uint160(uint256(rawHash)));
    }

    
    function _isContract(address addr) private view returns (bool) {
        uint256 size;
        assembly {
            size := extcodesize(addr)
        }
        return size > 0;
    }
}