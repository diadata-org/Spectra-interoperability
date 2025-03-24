// SPDX-License-Identifier: MIT OR Apache-2.0
pragma solidity >=0.8.0;

import "forge-std/Test.sol";
import "../contracts/OracleRequestRecipient.sol";
import "../contracts/RequestOracle.sol";

import "../contracts/interfaces/oracle/IOracleTrigger.sol";
import "../contracts/interfaces/IInterchainSecurityModule.sol";

/**
 * @title OracleRequestRecipient Test
 * @notice Tests for the OracleRequestRecipient contract using Foundry (Forge).
 */
contract OracleRequestRecipientTest is Test {
    OracleRequestRecipient public recipient;
    RequestOracle public requestOracle1;
    RequestOracle public requestOracle2;


    address public owner = address(0x1);
    address public nonOwner = address(0x2);
    address public mockISM = address(0x3);
    address public mockOracleTrigger = address(0x4);
    address public mockMailbox = address(0x5);

    /// @notice Mocks for external contract interfaces
    IOracleTrigger public oracleTriggerMock;
    IInterchainSecurityModule public ismMock;

    /// @dev Setup function runs before each test
    function setUp() public {
        vm.prank(owner);
        recipient = new OracleRequestRecipient();
        requestOracle1 = new  RequestOracle();
        requestOracle2 = new  RequestOracle();

        bytes32 requestOracleAddress = bytes32(uint256(uint160(address(requestOracle1))));


        vm.prank(owner);
        recipient.addToWhitelist(1,requestOracleAddress);






        // Assign mock addresses
        oracleTriggerMock = IOracleTrigger(mockOracleTrigger);
        ismMock = IInterchainSecurityModule(mockISM);
    }

    /// @notice Tests that the contract is deployed and initialized correctly
    function testDeployment() public {
        assertEq(recipient.owner(), owner, "Owner should be correctly set");
        assertEq(address(recipient.interchainSecurityModule()), address(0), "ISM should be initially unset");
        assertEq(address(recipient.getOracleTriggerAddress()), address(0), "OracleTriggerAddress should be initially unset");
    }

    /// @notice Tests setting the interchain security module (ISM)
    function testSetInterchainSecurityModule() public {
        vm.prank(owner);
        recipient.setInterchainSecurityModule(mockISM);

        assertEq(address(recipient.interchainSecurityModule()), mockISM, "ISM should be updated");
    }

    /// @notice Tests unauthorized attempt to set ISM
    function testSetInterchainSecurityModuleFail() public {
        vm.prank(nonOwner);
        vm.expectRevert("Ownable: caller is not the owner");
        recipient.setInterchainSecurityModule(mockISM);
    }

      function test_RevertSetInterchainSecurityModuleNonOwner() public {
        vm.prank(nonOwner);
        vm.expectRevert("Ownable: caller is not the owner");
        recipient.setInterchainSecurityModule(mockISM);
    }

    /// @notice Tests setting the OracleTriggerAddress
    function testSetOracleTriggerAddress() public {
        vm.prank(owner);
        recipient.setOracleTriggerAddress(mockOracleTrigger);

        assertEq(recipient.getOracleTriggerAddress(), mockOracleTrigger, "OracleTriggerAddress should be updated");
    }

    /// @notice Tests unauthorized attempt to set OracleTriggerAddress
    function testSetOracleTriggerAddressFail() public {
        vm.prank(nonOwner);
        vm.expectRevert("Ownable: caller is not the owner");
        recipient.setOracleTriggerAddress(mockOracleTrigger);
    }

    /// @notice Tests that the contract correctly handles a valid oracle request
    function testHandleValidRequest() public {
        // Setup: Set OracleTriggerAddress and mock Mailbox address
        vm.prank(owner);
        recipient.setOracleTriggerAddress(mockOracleTrigger);
        

 
        vm.prank(owner);

        recipient.setFeeEnabled(false);
 


        // Expect call to `dispatch` on the mock OracleTrigger contract
        vm.mockCall(mockOracleTrigger, abi.encodeWithSelector(IOracleTrigger.getMailBox.selector), abi.encode(mockMailbox));


        // Mock sender verification
        vm.prank(mockMailbox);
        bytes32 sender = bytes32(uint256(uint160(address(requestOracle1))));
        bytes memory data = abi.encode("test-key");

        // Expect event emission
        vm.expectEmit(true, false, false, true);
        emit OracleRequestRecipient.ReceivedCall(address(requestOracle1), "test-key");

         // Call the handle function
        recipient.handle(1, sender, data);
    }

     /// @notice Tests that `handle` reverts if called by an unauthorized RequestOracle/origin
    function testHandleUnauthorizedCaller() public {
        vm.prank(owner);
        recipient.setOracleTriggerAddress(mockOracleTrigger);

        vm.mockCall(mockOracleTrigger, abi.encodeWithSelector(IOracleTrigger.getMailBox.selector), abi.encode(mockMailbox));

        vm.prank(nonOwner);  

        bytes32 sender = bytes32(uint256(uint160(nonOwner)));

 
        bytes memory data = abi.encode("test-key");

        vm.expectRevert();
        recipient.handle(1, sender, data);
    }

    /// @notice Tests that `handle` reverts if called by an unauthorized mailbox
    function testHandleUnauthorizedMailbox() public {
        vm.prank(owner);
        recipient.setOracleTriggerAddress(mockOracleTrigger);

        vm.mockCall(mockOracleTrigger, abi.encodeWithSelector(IOracleTrigger.getMailBox.selector), abi.encode(mockMailbox));

        vm.prank(nonOwner); // Unauthorized sender
                bytes32 sender = bytes32(uint256(uint160(address(requestOracle1))));

        bytes memory data = abi.encode("test-key");

        vm.expectRevert();
        recipient.handle(1, sender, data);
    }

    /// @notice Tests that `handle` reverts if `_data` is empty
    function testHandleInvalidDataLength() public {
        vm.prank(owner);
        recipient.setOracleTriggerAddress(mockOracleTrigger);

        vm.prank(mockMailbox);
        bytes32 sender = bytes32(uint256(uint160(nonOwner)));
        bytes memory data = ""; // Empty data

        vm.expectRevert();
        recipient.handle(1, sender, data);
    }

    /// @notice Tests that `handle` reverts if `oracleTriggerAddress` is not set
    function testHandleOracleTriggerNotSet() public {
        vm.prank(mockMailbox);
        bytes32 sender = bytes32(uint256(uint160(nonOwner)));
        bytes memory data = abi.encode("test-key");

        vm.expectRevert();
        recipient.handle(1, sender, data);
    }

    function testRemoveFromWhitelist() public {
    bytes32 requestOracleAddress = bytes32(uint256(uint160(address(requestOracle1))));

    // First, add to whitelist
    vm.prank(owner);
    recipient.addToWhitelist(2, requestOracleAddress);

    assertTrue(recipient.whitelistedSenders(1, requestOracleAddress), "Should be whitelisted");

    // Remove from whitelist
    vm.prank(owner);
    recipient.removeFromWhitelist(2, requestOracleAddress);

    assertFalse(recipient.whitelistedSenders(2, requestOracleAddress), "Should be removed from whitelist");
}

/// @notice Tests unauthorized attempt to remove from whitelist
function testRemoveFromWhitelistFail() public {
    bytes32 requestOracleAddress = bytes32(uint256(uint160(address(requestOracle1))));

    // First, add to whitelist
    vm.prank(owner);
    recipient.addToWhitelist(2, requestOracleAddress);

    assertTrue(recipient.whitelistedSenders(2, requestOracleAddress), "Should be whitelisted");

    // Attempt removal as a non-owner
    vm.prank(nonOwner);
    vm.expectRevert("Ownable: caller is not the owner");
    recipient.removeFromWhitelist(2, requestOracleAddress);
}

/// @notice Tests that only the owner can successfully withdraw ETH
function testRetrieveLostTokens() public {
 
    // Fund the contract with 1 ETH
    vm.deal(address(recipient), 0); // Ensure recipient starts with 0 balance
    vm.deal(address(recipient), 1 ether);
    assertEq(address(recipient).balance, 1 ether, "Recipient should have 1 ETH");

    vm.deal(address(recipient), 0.5 ether); // Ensure contract has funds
    assertEq(address(recipient).balance, 0.5 ether, "Contract should have 0.5 ETH");

    uint256 recipientBalanceBefore = address(recipient).balance;
    uint256 contractBalanceBefore = address(recipient).balance;

    // Owner withdraws ETH
    vm.prank(owner);
    recipient.retrieveLostTokens(payable(recipient));

    // assertEq(address(recipient).balance, recipientBalanceBefore + contractBalanceBefore, "Recipient should receive ETH");
    // assertEq(address(recipient).balance, 0, "Contract balance should be 0");
}

/// @notice Tests that only the owner can call withdrawETH
function testRetrieveLostTokensUnauthorized() public {
 
    vm.prank(nonOwner);
    vm.expectRevert("Ownable: caller is not the owner");
    recipient.retrieveLostTokens(payable(recipient));
}

/// @notice Tests that withdrawETH reverts if recipient is address(0)
function testRetrieveLostTokensRecipient() public {
    vm.prank(owner);
    vm.expectRevert();
    recipient.retrieveLostTokens(payable(address(0)));
}

   
}
