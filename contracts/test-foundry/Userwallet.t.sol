// SPDX-License-Identifier: MIT
pragma solidity ^0.8.17;

import "forge-std/Test.sol";
import "../contracts/UserWalletFactory.sol";
import "../contracts/UserWallet.sol";
import "forge-std/console.sol";


contract UserWalletTest is Test {
    UserWalletFactory factory;
    UserWallet wallet;
    address owner;
    address whitelistedUser;
    address nonWhitelistedUser;

    function setUp() public {
        owner = address(this);
        whitelistedUser = address(0x123);
        nonWhitelistedUser = address(0x456);

        // Deploy UserWalletFactory
        factory = new UserWalletFactory();

        // Deploy a new wallet for the owner
        address walletAddress = factory.deployWallet();
        wallet = UserWallet(payable(walletAddress));

        // Check that wallet is correctly deployed
        assertEq(wallet.owner(), owner);
    }
        
        receive() external payable {} //workaround for withdraw test


    function testDepositAndWithdraw() public {
        // Deposit 1 ETH into the wallet
        payable(address(wallet)).transfer(1 ether);
        assertEq(wallet.getBalance(), 1 ether);
        console.log("testDepositAndWithdraw");

        // Withdraw 0.5 ETH
        vm.prank(owner);
        wallet.withdraw(0.5 ether);

        assertEq(wallet.getBalance(), 0.5 ether);
    }

    function testWhitelistFunctionality() public {
        // Add whitelisted address
        vm.prank(owner);
        wallet.addToWhitelist(whitelistedUser);

        assertEq(wallet.whitelist(whitelistedUser), true);

        // Remove whitelisted address
        vm.prank(owner);
        wallet.removeFromWhitelist(whitelistedUser);

        assertEq(wallet.whitelist(whitelistedUser), false);
    }

    function testDeductFeeByWhitelisted() public {
        vm.prank(owner);
        wallet.addToWhitelist(whitelistedUser);

        // Deposit 1 ETH
        payable(address(wallet)).transfer(1 ether);

        // Deduct 0.2 ETH as fee
        vm.prank(whitelistedUser);
        wallet.deductFee(0.2 ether);

        assertEq(wallet.getBalance(), 0.8 ether);
    }

    function testFailDeductFeeByNonWhitelisted() public {
        // Deposit 1 ETH
        payable(address(wallet)).transfer(1 ether);

        // This should fail as nonWhitelistedUser is not whitelisted
        vm.prank(nonWhitelistedUser);
        wallet.deductFee(0.2 ether);
    }

    function testFailWithdrawByNonOwner() public {
        // Deposit 1 ETH
        payable(address(wallet)).transfer(1 ether);

        // Attempt withdraw as a non-owner
        vm.prank(nonWhitelistedUser);
        wallet.withdraw(0.5 ether);
    }
}