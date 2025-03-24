// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import "forge-std/Test.sol";
import "../contracts/Ism.sol";
import "../contracts/libs/Message.sol";

import "../contracts/mocks/MockMailbox.sol";


 

contract IsmTest is Test {
    Ism ism;
    address owner;
    address addr1;
    address addr2;
    uint32 domainA = 1000;
    uint32 domainB = 2000;
    MockMailbox mockMailbox;

    function setUp() public {

        mockMailbox = new MockMailbox();
        owner = address(this);
        addr1 = address(0x123);
        addr2 = address(0x456);

        // Deploy the ISM contract
        ism = new Ism();
    }

    /// @notice Helper function to encode a Hyperlane message using Message.sol.
    function encodeMessage(
        uint32 origin,
        address sender,
        bytes  calldata messageBody  
    ) internal pure returns (bytes memory) {
 
        return Message.formatMessage(
            1,          
            12345,       
            origin,     
            bytes32(uint256(uint160(sender))), // _sender
            2000,        
            bytes32(uint256(uint160(address(0x999)))),  
            messageBody  
        );
    }

    function testInitialOwner() public {
        assertEq(ism.owner(), owner);
    }

    function testSetSenderShouldBe() public {
        // vm.expectEmit(true, true, false, true);
        // emit Ism.SenderShouldBeUpdated(domainA, address(0), addr1);
        ism.addSenderShouldBe(domainA, addr1);
        assertEq(ism.isSenderAllowed(domainA,addr1), true);
    }

    // function testSetAllowAll() public {
    //     vm.expectEmit(true, true, false, true);
    //     emit AllowAllUpdated(false, true);
    //     ism.setAllowAll(true);
    //     assertTrue(ism.allowAll());
    // }

    function testVerifyWithCorrectSender(bytes calldata body) public {
                ism.setTrustedMailBox(address(mockMailbox));
        // ism.addTrustedRelayer(address(0x999));

        ism.addSenderShouldBe(domainA, owner);
 
        bytes memory message = encodeMessage(domainA, owner, body);
        assertTrue(ism.verify("", message));
    }

    function testVerifyWithIncorrectSender(bytes calldata body) public {
        ism.setTrustedMailBox(address(mockMailbox));
        ism.addSenderShouldBe(domainA, addr1);
        bytes memory message = encodeMessage(domainA, addr2,body);
        assertFalse(ism.verify("", message));
    }

    function testVerifyWithDifferentOriginFails(bytes calldata body) public {

        ism.setTrustedMailBox(address(mockMailbox));
        // ism.addTrustedRelayer()
        ism.addSenderShouldBe(domainA, addr1);
        bytes memory message = encodeMessage(domainB, addr1,body);
        assertFalse(ism.verify("", message));
    }
 

    

  
}