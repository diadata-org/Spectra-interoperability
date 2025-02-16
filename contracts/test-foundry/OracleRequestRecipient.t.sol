// SPDX-License-Identifier: MIT OR Apache-2.0
pragma solidity >=0.8.0;

import "forge-std/Test.sol";
import "../contracts/OracleRequestRecipient.sol";
import "../contracts/RequestOracle.sol";

import "../contracts/interfaces/IOracleTrigger.sol";
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
        assertEq(address(recipient.getInterchainSecurityModule()), address(0), "ISM should be initially unset");
        assertEq(address(recipient.getOracleTriggerAddress()), address(0), "OracleTriggerAddress should be initially unset");
    }

    /// @notice Tests setting the interchain security module (ISM)
    function testSetInterchainSecurityModule() public {
        vm.prank(owner);
        recipient.setInterchainSecurityModule(mockISM);

        assertEq(address(recipient.getInterchainSecurityModule()), mockISM, "ISM should be updated");
    }

    /// @notice Tests unauthorized attempt to set ISM
    function testSetInterchainSecurityModuleFail() public {
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

        // Expect call to `dispatch` on the mock OracleTrigger contract
        vm.mockCall(mockOracleTrigger, abi.encodeWithSelector(IOracleTrigger.mailBox.selector), abi.encode(mockMailbox));

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

        vm.mockCall(mockOracleTrigger, abi.encodeWithSelector(IOracleTrigger.mailBox.selector), abi.encode(mockMailbox));

        vm.prank(nonOwner);  

        bytes32 sender = bytes32(uint256(uint160(nonOwner)));

 
        bytes memory data = abi.encode("test-key");

        vm.expectRevert("Sender not whitelisted for this origin");
        recipient.handle(1, sender, data);
    }

    /// @notice Tests that `handle` reverts if called by an unauthorized mailbox
    function testHandleUnauthorizedMailbox() public {
        vm.prank(owner);
        recipient.setOracleTriggerAddress(mockOracleTrigger);

        vm.mockCall(mockOracleTrigger, abi.encodeWithSelector(IOracleTrigger.mailBox.selector), abi.encode(mockMailbox));

        vm.prank(nonOwner); // Unauthorized sender
                bytes32 sender = bytes32(uint256(uint160(address(requestOracle1))));

        bytes memory data = abi.encode("test-key");

        vm.expectRevert("Unauthorized caller");
        recipient.handle(1, sender, data);
    }

    /// @notice Tests that `handle` reverts if `_data` is empty
    function testHandleInvalidDataLength() public {
        vm.prank(owner);
        recipient.setOracleTriggerAddress(mockOracleTrigger);

        vm.prank(mockMailbox);
        bytes32 sender = bytes32(uint256(uint160(nonOwner)));
        bytes memory data = ""; // Empty data

        vm.expectRevert("Invalid data length");
        recipient.handle(1, sender, data);
    }

    /// @notice Tests that `handle` reverts if `oracleTriggerAddress` is not set
    function testHandleOracleTriggerNotSet() public {
        vm.prank(mockMailbox);
        bytes32 sender = bytes32(uint256(uint160(nonOwner)));
        bytes memory data = abi.encode("test-key");

        vm.expectRevert("Oracle trigger address not set");
        recipient.handle(1, sender, data);
    }

   
}
