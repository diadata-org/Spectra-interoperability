// SPDX-License-Identifier: MIT
pragma solidity 0.8.26;

import "forge-std/Test.sol";
import "../contracts/DIAOracleV2metada.sol";

// Mock Oracle contract implementing IDIAOracleV2
contract MockDIAOracle is IDIAOracleV2 {
    uint128 private storedValue;
    uint128 private storedTimestamp;

    function setValue(
        string memory,
        uint128 value,
        uint128 timestamp
    ) external override {
        storedValue = value;
        storedTimestamp = timestamp;
    }

    function getValue(
        string memory
    ) external view override returns (uint128, uint128) {
        return (storedValue, storedTimestamp);
    }

    function updateOracleUpdaterAddress(address) external override {}
}

// Forge test contract
contract DIAOracleV2MetaTest is Test {
    DIAOracleV2Meta public oracleMeta;
    MockDIAOracle public oracle1;
    MockDIAOracle public oracle2;
    MockDIAOracle public oracle3;

    address public admin = address(0x123);

    function setUp() public {
        vm.startPrank(admin);
        oracleMeta = new DIAOracleV2Meta();
        oracle1 = new MockDIAOracle();
        oracle2 = new MockDIAOracle();
        oracle3 = new MockDIAOracle();
        vm.stopPrank();
    }

    function testAddOracle() public {
        vm.startPrank(admin);

        oracleMeta.addOracle(address(oracle1));
        oracleMeta.addOracle(address(oracle2));

        // Assert that numOracles increased
        assertEq(oracleMeta.numOracles(), 2);

        vm.stopPrank();
    }

    function testRemoveOracle() public {
        vm.startPrank(admin);

        oracleMeta.addOracle(address(oracle1));
        oracleMeta.addOracle(address(oracle2));
        oracleMeta.addOracle(address(oracle3));

        // Remove oracle2 and check numOracles
        oracleMeta.removeOracle(address(oracle2));
        assertEq(oracleMeta.numOracles(), 2);

        vm.stopPrank();
    }

    function testSetThreshold() public {
        vm.startPrank(admin);

        oracleMeta.setThreshold(2);
        assertEq(oracleMeta.threshold(), 2);

        vm.stopPrank();
    }

    function testSetTimeout() public {
        vm.startPrank(admin);

        oracleMeta.setTimeoutSeconds(100);
        assertEq(oracleMeta.timeoutSeconds(), 100);

        vm.stopPrank();
    }

    function testGetValueMedian() public {
        vm.startPrank(admin);

        oracleMeta.addOracle(address(oracle1));
        oracleMeta.addOracle(address(oracle2));
        oracleMeta.addOracle(address(oracle3));

        oracleMeta.setThreshold(2);
        oracleMeta.setTimeoutSeconds(1000);

        vm.stopPrank();

        // Set values in oracles
        oracle1.setValue("BTC", 100, uint128(block.timestamp));
        oracle2.setValue("BTC", 200, uint128(block.timestamp));
        oracle3.setValue("BTC", 300, uint128(block.timestamp));

        // Fetch median value
        (uint128 value, uint128 timestamp) = oracleMeta.getValue("BTC");

        assertEq(value, 200, "Median should be 200");
        assertEq(
            timestamp,
            uint128(block.timestamp),
            "Timestamp should match current time"
        );
    }

    // function testGetValueWithTimeout() public {
    //     vm.startPrank(admin);

    //     oracleMeta.addOracle(address(oracle1));
    //     oracleMeta.addOracle(address(oracle2));
    //     oracleMeta.addOracle(address(oracle3));

    //     oracleMeta.setThreshold(2);
    //     oracleMeta.setTimeoutSeconds(5);

    //     vm.stopPrank();

    //     //TODO fix
    //     uint128 currentTimestamp = uint128(block.timestamp);
    //     uint128 safeTimestamp = currentTimestamp > 200
    //         ? currentTimestamp - 200
    //         : 0;
    //     // Set values, but make oracle1 outdated
    //     oracle1.setValue("BTC", 100, safeTimestamp);
    //     oracle2.setValue("BTC", 200, uint128(block.timestamp));
    //     oracle3.setValue("BTC", 300, uint128(block.timestamp));

    //     // Fetch median value (should ignore outdated oracle1)
    //     (uint128 value, uint128 timestamp) = oracleMeta.getValue("BTC");

    //     assertEq(
    //         value,
    //         300,
    //         "Median should be 300 after filtering out outdated oracle1"
    //     );
    //     assertEq(
    //         timestamp,
    //         uint128(block.timestamp),
    //         "Timestamp should be current"
    //     );
    // }

    function testGetValueFailsWithoutEnoughOracles() public {
        vm.startPrank(admin);

        oracleMeta.addOracle(address(oracle1));
        oracleMeta.setThreshold(2);
        oracleMeta.setTimeoutSeconds(100);

        vm.stopPrank();

        // Set value
        oracle1.setValue("BTC", 100, uint128(block.timestamp));

        // Expect revert due to insufficient valid oracles
        vm.expectRevert();
        oracleMeta.getValue("BTC");
    }
}
