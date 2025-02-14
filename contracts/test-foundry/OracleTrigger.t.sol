// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import "forge-std/Test.sol";
import "forge-std/console.sol";
import "../contracts/OracleTrigger.sol";

contract MockMetadata is IDIAOracleV2 {
    mapping(string => uint128) public values;
    mapping(string => uint128) public timestamps;

    function setValue(string memory key, uint128 value, uint128 timestamp) external {
        values[key] = value;
        timestamps[key] = timestamp;
    }

    function getValue(string memory key) external view override returns (uint128, uint128) {
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
        vm.expectRevert(abi.encodeWithSignature("NotAuthorized(address)", address(this)));
        oracleTrigger.addChain(chainId, recipient);
    }
    
    function testSetMailbox() public {
        vm.prank(owner);
        oracleTrigger.setMailbox(mailbox);
        assertEq(oracleTrigger.mailBox(), mailbox);
    }
    
    function testSetMetadataContract() public {
        vm.prank(owner);
        oracleTrigger.updateMetadataContract(address(mockMetadata));
        assertEq(oracleTrigger.metadataContract(), address(mockMetadata));
    }
    
    function testAddOwner() public {
        vm.prank(owner);
        oracleTrigger.addOwner(newOwner);
        assertTrue(oracleTrigger.hasRole(oracleTrigger.OWNER_ROLE(), newOwner));
    }
    
    function testRemoveOwner() public {
        vm.prank(owner);
        oracleTrigger.addOwner(newOwner);
        
        vm.prank(owner);
        oracleTrigger.removeOwner(newOwner);
        assertFalse(oracleTrigger.hasRole(oracleTrigger.OWNER_ROLE(), newOwner));
    }
    
    function testCannotRemoveLastOwner() public {
        vm.prank(owner);
        vm.expectRevert(abi.encodeWithSignature("CannotRemoveLastOwner()"));
        oracleTrigger.removeOwner(owner);
    }
    
    function testDispatchToChain() public {
        vm.prank(owner);
        oracleTrigger.addChain(chainId, recipient);
        
        vm.prank(owner);
        oracleTrigger.setMailbox(mailbox);
        
        vm.prank(owner);
        oracleTrigger.updateMetadataContract(address(mockMetadata));
        
        vm.deal(owner, 1 ether);
        vm.prank(owner);
        oracleTrigger.dispatchToChain{value: 0.1 ether}(chainId, "BTC");
    }
}
