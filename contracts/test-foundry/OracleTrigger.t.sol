// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import "forge-std/Test.sol";
import "forge-std/console.sol";
import "../contracts/OracleTrigger.sol";

contract MockMetadata is IDIAOracleV2 {
    mapping(string => uint128) public values;
    mapping(string => uint128) public timestamps;

    function setValue(
        string memory key,
        uint128 value,
        uint128 timestamp
    ) external {
        values[key] = value;
        timestamps[key] = timestamp;
    }

    function getValue(
        string memory key
    ) external view override returns (uint128, uint128) {
        return (values[key], timestamps[key]);
    }
}

contract OracleTriggerTest is Test {
    OracleTrigger oracleTrigger;
    MockMetadata mockMetadata;
    address owner = address(0x1);
    address newOwner = address(0x2);
    address recipient = address(0x3);
    address mailbox = address(0x4);
    uint32 chainId = 1;

    function setUp() public {
        vm.prank(owner);
        oracleTrigger = new OracleTrigger();
        mockMetadata = new MockMetadata();
    }

    function testOwnerInitialization() public {
        assertTrue(oracleTrigger.hasRole(oracleTrigger.OWNER_ROLE(), owner));
        assertTrue(
            oracleTrigger.hasRole(oracleTrigger.DEFAULT_ADMIN_ROLE(), owner)
        );
    }

    function testAddChain() public {
        vm.prank(owner);
        oracleTrigger.addChain(chainId, recipient);

        address storedRecipient = oracleTrigger.viewChain(chainId);
        assertEq(storedRecipient, recipient);
    }

    function testUpdateChain() public {
        vm.prank(owner);
        oracleTrigger.addChain(chainId, recipient);
        address newRecipient = address(0x6);

        vm.prank(owner);
        oracleTrigger.updateChain(chainId, newRecipient);
        assertEq(oracleTrigger.viewChain(chainId), newRecipient);
    }

    function testCannotAddChainWithoutOwner() public {
        vm.expectRevert();
        oracleTrigger.addChain(chainId, recipient);
    }

    function testsetMailBox() public {
        vm.prank(owner);
        oracleTrigger.setMailBox(mailbox);
        assertEq(oracleTrigger.getMailBox(), mailbox);
    }

    function testsetMailBoxToZeroAddress() public {
        vm.prank(owner);
        vm.expectRevert(abi.encodeWithSignature("ZeroAddress()"));
        oracleTrigger.setMailBox(address(0x0));
        // assertEq(oracleTrigger.getMailBox(), mailbox);
    }

    function testSetMetadataContract() public {
        vm.prank(owner);
        oracleTrigger.updateMetadataContract(address(mockMetadata));
        assertEq(oracleTrigger.metadataContract(), address(mockMetadata));
    }

    function testCannotSetInvalidMetadataContract() public {
        vm.prank(owner);
        vm.expectRevert();
        oracleTrigger.updateMetadataContract(address(0));
    }

    function testAddOwner() public {
        vm.prank(owner);
        console.log("-------------------");
        oracleTrigger.grantRole(keccak256("OWNER_ROLE"), newOwner);
        assertTrue(oracleTrigger.hasRole(oracleTrigger.OWNER_ROLE(), newOwner));
    }

    function testRemoveOwner() public {
        vm.prank(owner);
        oracleTrigger.grantRole(keccak256("OWNER_ROLE"), newOwner);

        vm.prank(owner);
        oracleTrigger.revokeRole(keccak256("OWNER_ROLE"), newOwner);

        assertFalse(oracleTrigger.hasRole(keccak256("OWNER_ROLE"), newOwner));
    }

    function testCannotRemoveLastOwner() public {
        vm.prank(owner);
        oracleTrigger.revokeRole(keccak256("OWNER_ROLE"), newOwner);

        vm.expectRevert();
        oracleTrigger.revokeRole(keccak256("OWNER_ROLE"), owner);
    }

    function testDispatchToChain() public {
        vm.prank(owner);

        oracleTrigger.addChain(chainId, recipient);

        vm.prank(owner);
        oracleTrigger.setMailBox(mailbox);

        vm.prank(owner);
        oracleTrigger.updateMetadataContract(address(mockMetadata));

        vm.deal(owner, 1 ether);
        vm.prank(owner);
        oracleTrigger.grantRole(keccak256("DISPATCHER_ROLE"), owner);

        vm.prank(owner);
        oracleTrigger.dispatchToChain{value: 0.1 ether}(chainId, "BTC");
    }

    function testDispatch() public {
        vm.prank(owner);
        oracleTrigger.grantRole(keccak256("DISPATCHER_ROLE"), owner);

        vm.prank(owner);

        oracleTrigger.addChain(chainId, recipient);

        vm.prank(owner);
        oracleTrigger.setMailBox(mailbox);

        vm.prank(owner);
        oracleTrigger.updateMetadataContract(address(mockMetadata));

        vm.deal(owner, 1 ether);
        vm.prank(owner);
        oracleTrigger.dispatch{value: 0.1 ether}(
            chainId,
            0xb8565867A5616544d13595fBe30a5693b2207fa0,
            "BTC"
        );
    }

    function testCannotDispatchWithoutDispatcherRole() public {
        vm.prank(owner);
        oracleTrigger.addChain(chainId, recipient);

        vm.prank(owner);
        oracleTrigger.setMailBox(mailbox);

        vm.prank(owner);
        oracleTrigger.updateMetadataContract(address(mockMetadata));

        vm.deal(owner, 1 ether);

        vm.expectRevert();
        vm.prank(owner);
        oracleTrigger.dispatchToChain{value: 0.1 ether}(chainId, "BTC");
    }

    function testMetadataValueStorage() public {
        mockMetadata.setValue("BTC", 50000, uint128(block.timestamp));
        (uint128 price, uint128 timestamp) = mockMetadata.getValue("BTC");

        assertEq(price, 50000);
        assertEq(timestamp, block.timestamp);
    }

    function testRemoveDispatcher() public {
        vm.prank(owner);
        oracleTrigger.grantRole(keccak256("DISPATCHER_ROLE"), newOwner);

        vm.prank(owner);
        oracleTrigger.revokeRole(keccak256("DISPATCHER_ROLE"), newOwner);

        // Since isDispatcher() does not exist, we will attempt a dispatch operation
        vm.deal(newOwner, 1 ether);
        vm.prank(newOwner);
        vm.expectRevert();
        oracleTrigger.dispatchToChain{value: 0.1 ether}(chainId, "BTC");
    }

    function testCannotRemoveDispatcherIfNotOwner() public {
        vm.prank(owner);
        oracleTrigger.grantRole(keccak256("DISPATCHER_ROLE"), newOwner);

        vm.prank(newOwner);
        vm.expectRevert();
        oracleTrigger.revokeRole(keccak256("DISPATCHER_ROLE"), newOwner);
    }

    function testIsOwner() public {
        vm.prank(owner);
        oracleTrigger.grantRole(keccak256("OWNER_ROLE"), newOwner);

        assertTrue(oracleTrigger.hasRole(keccak256("OWNER_ROLE"), owner));
        assertTrue(oracleTrigger.hasRole(keccak256("OWNER_ROLE"), newOwner));

        address nonOwner = address(0x7);
        assertFalse(oracleTrigger.hasRole(keccak256("OWNER_ROLE"), nonOwner));
    }

    function testCannotAddDuplicateChain() public {
        vm.prank(owner);
        oracleTrigger.addChain(chainId, recipient);

        vm.prank(owner);
        vm.expectRevert(
            abi.encodeWithSignature("ChainAlreadyExists(uint32)", chainId)
        );
        oracleTrigger.addChain(chainId, address(0x8));
    }

    function testDispatchToUnconfiguredChainFails() public {
        vm.prank(owner);
        oracleTrigger.setMailBox(mailbox);

        vm.prank(owner);
        oracleTrigger.updateMetadataContract(address(mockMetadata));

        vm.deal(owner, 1 ether);
        vm.prank(owner);

        oracleTrigger.grantRole(keccak256("DISPATCHER_ROLE"), owner);

        vm.prank(owner);
        vm.expectRevert(
            abi.encodeWithSignature("ChainNotConfigured(uint32)", chainId)
        );
        oracleTrigger.dispatchToChain{value: 0.1 ether}(chainId, "BTC");
    }

    // add new owner and let new owner add new dispatcher role

    function testNewOwnerCanAddDispatcher() public {
        vm.prank(owner);

        console.log(owner);

        oracleTrigger.grantRole(
            0x0000000000000000000000000000000000000000000000000000000000000000,
            newOwner
        ); // Grant admin role to newOwner

        vm.prank(owner);
        oracleTrigger.grantRole(keccak256("OWNER_ROLE"), newOwner); // Grant owner role to newOwner

        // Now newOwner can grant DISPATCHER_ROLE
        vm.prank(newOwner);
        oracleTrigger.grantRole(keccak256("DISPATCHER_ROLE"), address(0x5));

        // Verify dispatcher has DISPATCHER_ROLE
        assertTrue(
            oracleTrigger.hasRole(keccak256("DISPATCHER_ROLE"), address(0x5))
        );
    }

  

    /// @notice Tests that only the owner can successfully withdraw ETH
    function testRetrieveLostTokens() public {
        // Fund the contract with 1 ETH
        vm.deal(address(oracleTrigger), 0); // Ensure recipient starts with 0 balance
        vm.deal(address(oracleTrigger), 1 ether);
        assertEq(
            address(oracleTrigger).balance,
            1 ether,
            "Recipient should have 1 ETH"
        );

        vm.deal(address(oracleTrigger), 0.5 ether); // Ensure contract has funds
        assertEq(
            address(oracleTrigger).balance,
            0.5 ether,
            "Contract should have 0.5 ETH"
        );

        uint256 recipientBalanceBefore = address(oracleTrigger).balance;
        uint256 contractBalanceBefore = address(oracleTrigger).balance;

        // Owner withdraws ETH
        vm.prank(owner);
        oracleTrigger.retrieveLostTokens(payable(recipient));

        // assertEq(address(recipient).balance, recipientBalanceBefore + contractBalanceBefore, "Recipient should receive ETH");
        // assertEq(address(recipient).balance, 0, "Contract balance should be 0");
    }

    /// @notice Tests that only the owner can call withdrawETH
    function testRetrieveLostTokensUnauthorized() public {
        vm.prank(newOwner);
        vm.expectRevert();
        oracleTrigger.retrieveLostTokens(payable(recipient));
    }

    /// @notice Tests that withdrawETH reverts if recipient is address(0)
    function testRetrieveLostTokensRecipient() public {
        vm.prank(owner);
        vm.expectRevert("Invalid receiver");
        oracleTrigger.retrieveLostTokens(payable(address(0)));
    }
}
