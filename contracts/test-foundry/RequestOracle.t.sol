// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import "forge-std/Test.sol";
import "../contracts/RequestOracle.sol";
import "../contracts/interfaces/IMailbox.sol";
import "../contracts/interfaces/IInterchainSecurityModule.sol";
import "../contracts/interfaces/hooks/IPostDispatchHook.sol";
import "../contracts/libs/TypeCasts.sol";
import "../contracts/mocks/MockMailbox.sol";

contract MockInterchainSecurityModule is IInterchainSecurityModule {
    function moduleType() external pure override returns (uint8) {
        return 1; // Mock return value
    }

    function verify(
        bytes calldata, // _message
        bytes calldata // _metadata
    ) external pure override returns (bool) {
        return true; // Always returns true for testing
    }
}

contract MockPostDispatchHook is IPostDispatchHook {
    uint8 private _hookType;
    bool private _metadataSupported;
    uint256 private _quoteAmount;

    constructor(
        uint8 hookType_,
        bool metadataSupported_,
        uint256 quoteAmount_
    ) {
        _hookType = hookType_;
        _metadataSupported = metadataSupported_;
        _quoteAmount = quoteAmount_;
    }

    function hookType() external view override returns (uint8) {
        return _hookType;
    }

    function supportsMetadata(
        bytes calldata /* metadata */
    ) external view override returns (bool) {
        return _metadataSupported;
    }

    function postDispatch(
        bytes calldata /* metadata */,
        bytes calldata /* message */
    ) external payable override {
        // Mock function - does nothing
    }

    function quoteDispatch(
        bytes calldata /* metadata */,
        bytes calldata /* message */
    ) external view override returns (uint256) {
        return _quoteAmount;
    }

    // Setter functions for testing purposes
    function setHookType(uint8 hookType_) external {
        _hookType = hookType_;
    }

    function setMetadataSupport(bool supported) external {
        _metadataSupported = supported;
    }

    function setQuoteAmount(uint256 amount) external {
        _quoteAmount = amount;
    }
}

contract RequestOracleTest is Test {
    using TypeCasts for address;

    RequestOracle public requestOracle;
    RequestOracle public requestOracleInvalidIsm;

    MockMailbox public mailbox;
    MockInterchainSecurityModule public interchainSecurityModule;
    MockPostDispatchHook public paymentHook;

    address public owner = address(0x123);
    address public receiver = address(0x456);
    uint32 public destinationDomain = 100;
    bytes public sampleMessage =
        abi.encode("BTC", uint128(1710000000), uint128(45000));

    function setUp() public {
        vm.prank(owner);
        requestOracle = new RequestOracle();
        requestOracleInvalidIsm = new RequestOracle();

        mailbox = new MockMailbox();
        interchainSecurityModule = new MockInterchainSecurityModule();
        paymentHook = new MockPostDispatchHook(1, true, 1);

        vm.prank(owner);
        requestOracle.setInterchainSecurityModule(
            address(interchainSecurityModule)
        );
        vm.prank(owner);

        requestOracle.setTrustedMailBox(address(mailbox));

        requestOracleInvalidIsm.setTrustedMailBox(address(mailbox));
        requestOracleInvalidIsm.setPaymentHook(address(paymentHook));

        vm.prank(owner);
        requestOracle.setPaymentHook(address(paymentHook));
    }

    function testSetTrustedMailBoxUnauthorized() public {
        address newMailBox = address(0x999);
        vm.expectRevert("Ownable: caller is not the owner");
        requestOracle.setTrustedMailBox(newMailBox);
    }

    function testDeployment() public {
        assertEq(
            address(requestOracle.interchainSecurityModule()),
            address(interchainSecurityModule)
        );
        assertEq(requestOracle.paymentHook(), address(paymentHook));
    }

    function testRequestOracle() public {
        vm.prank(owner);
        vm.deal(owner, 10 ether); // Give owner some ETH

        requestOracle.addToWhitelist(destinationDomain, receiver);

        bytes32 messageId = requestOracle.request{ value: 5 ether }(
            receiver,
            destinationDomain,
            sampleMessage
        );

        assertTrue(messageId != bytes32(0));
    }

    function testHandleMessage() public {
        bytes32 sender = receiver.addressToBytes32();

        vm.prank(address(mailbox));
        requestOracle.handle(destinationDomain, sender, sampleMessage);

        (string memory key, uint128 timestamp, uint128 value) = abi.decode(
            sampleMessage,
            (string, uint128, uint128)
        );

        (uint128 storedTimestamp, uint128 storedValue) = requestOracle.updates(
            key
        );

        assertEq(storedTimestamp, timestamp);
        assertEq(storedValue, value);
    }

    function testSetInterchainSecurityModule() public {
        MockInterchainSecurityModule newModule = new MockInterchainSecurityModule();

        vm.prank(owner);
        requestOracle.setInterchainSecurityModule(address(newModule));

        assertEq(
            address(requestOracle.interchainSecurityModule()),
            address(newModule)
        );
    }

    function testSetPaymentHook() public {
        MockPostDispatchHook newHook = new MockPostDispatchHook(1, true, 1);

        vm.prank(owner);
        requestOracle.setPaymentHook(address(newHook));

        assertEq(requestOracle.paymentHook(), address(newHook));
    }

    function testReceiveEther() public {
        vm.deal(address(this), 1 ether);
        (bool sent, ) = address(requestOracle).call{ value: 1 ether }("");
        assertTrue(sent);
    }

    function testHandleUnauthorized() public {
        bytes32 sender = receiver.addressToBytes32();
        vm.expectRevert();
        requestOracle.handle(destinationDomain, sender, sampleMessage);
    }

    function testHandleNotWhitelisted() public {
        vm.prank(owner);

        vm.expectRevert(
            abi.encodeWithSignature(
                "ReceiverNotWhitelisted(address,uint32)",
                receiver,
                destinationDomain
            )
        );

        requestOracle.request(receiver, destinationDomain, sampleMessage);
    }

    function testPauseContract() public {
        vm.prank(owner);
        requestOracle.pauseContract();

        assertTrue(requestOracle.paused());

        vm.expectRevert("Pausable: paused");
        requestOracle.request(receiver, destinationDomain, sampleMessage);
    }

    function testUnpauseContract() public {
        vm.prank(owner);
        requestOracle.pauseContract();
        assertTrue(requestOracle.paused(), "Contract should be paused");

        vm.prank(owner);
        requestOracle.unpauseContract();
        assertFalse(requestOracle.paused(), "Contract should be unpaused");

        // Ensure that `request` function works after unpausing
        vm.prank(owner);
        vm.deal(owner, 10 ether);

        requestOracle.addToWhitelist(destinationDomain, receiver);

        bytes32 messageId = requestOracle.request{ value: 5 ether }(
            receiver,
            destinationDomain,
            sampleMessage
        );

        assertTrue(
            messageId != bytes32(0),
            "Request should succeed after unpausing"
        );
    }

    function testRetrieveLostTokensFailsIfNoBalance() public {
        vm.startPrank(owner);
        vm.expectRevert(RequestOracle.NoBalanceToWithdraw.selector);
        requestOracle.retrieveLostTokens(address(owner));
        vm.stopPrank();
    }
    function testRetrieveLostTokens_TransferFailed() public {
        NonPayableReceiver pr = new NonPayableReceiver();

        vm.deal(address(requestOracle), 1 ether);
        vm.expectRevert(RequestOracle.AmountTransferFailed.selector);
        vm.prank(owner);

        requestOracle.retrieveLostTokens(address(pr));
    }

    function testRetrieveLostTokens() public {
        // Fund the contract with 1 ETH
        vm.deal(address(requestOracle), 1 ether);
        assertEq(
            address(requestOracle).balance,
            1 ether,
            "Recipient should have 1 ETH"
        );

        // Owner withdraws ETH
        vm.prank(owner);
        vm.expectEmit(true, true, false, true);
        emit RequestOracle.TokensRecovered(owner, 1 ether);
        requestOracle.retrieveLostTokens(payable(owner));
    }

    function test_addToWhitelist_RevertsOnZeroAddress() public {
        vm.prank(owner);
        vm.expectRevert(abi.encodeWithSignature("InvalidAddress()"));
        requestOracle.addToWhitelist(1, address(0x0));
    }

    function test_addToWhitelist_RevertsIfAlreadyWhitelisted() public {
        address validReceiver = address(0x321);
        vm.prank(owner);
        requestOracle.addToWhitelist(1, validReceiver);

        vm.prank(owner);
        vm.expectRevert(
            abi.encodeWithSignature(
                "AlreadyWhitelisted(address,uint32)",
                validReceiver,
                1
            )
        );
        requestOracle.addToWhitelist(1, validReceiver);
    }

    function test_removeFromWhitelist_RevertsOnZeroAddress() public {
        vm.prank(owner);
        vm.expectRevert(abi.encodeWithSignature("InvalidAddress()"));
        requestOracle.removeFromWhitelist(1, address(0x0));
    }

    function testHandleMessageInvalisISM() public {
        bytes32 sender = receiver.addressToBytes32();

        vm.prank(address(mailbox));
        vm.expectRevert(abi.encodeWithSignature("InvalidISMAddress()"));
        requestOracleInvalidIsm.handle(
            destinationDomain,
            sender,
            sampleMessage
        );
    }

    function testHandleStaleMessage() public {
        bytes32 sender = receiver.addressToBytes32();

        vm.prank(address(mailbox));
        requestOracle.handle(destinationDomain, sender, sampleMessage);

        // send stake message
        vm.prank(address(mailbox));

        requestOracle.handle(
            destinationDomain,
            sender,
            abi.encode("BTC", uint128(1700000000), uint128(41000))
        );

        // No updates in value

        (string memory key, uint128 timestamp, uint128 value) = abi.decode(
            sampleMessage,
            (string, uint128, uint128)
        );

        (uint128 storedTimestamp, uint128 storedValue) = requestOracle.updates(
            key
        );

        assertEq(storedTimestamp, timestamp);
        assertEq(storedValue, value);
    }

    function test_removeFromWhitelist_Success() public {
        uint32 origin = 1;
        address receiver = address(0x123);

        // Add to whitelist first
        vm.prank(owner);
        requestOracle.addToWhitelist(origin, receiver);
        assertTrue(requestOracle.whitelistedReceivers(origin, receiver));

        // Remove from whitelist and check event
        vm.prank(owner);
        vm.expectEmit(true, true, false, true);
        emit RequestOracle.WhitelistUpdated(origin, receiver, false);

        requestOracle.removeFromWhitelist(origin, receiver);

        // Confirm removal
        assertFalse(requestOracle.whitelistedReceivers(origin, receiver));
    }
}

contract NonPayableReceiver {
    fallback() external payable {
        revert("Cannot receive ETH");
    }
}
