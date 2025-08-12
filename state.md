# Spectra Interoperability System

## Attestor Service
**Path:** `attestor/`

Reads prices from DIA Oracle V2 (`0x0087342f5f4c7AB23a37c045c3EF710749527c88`), signs EIP-712 intents

```solidity
struct OracleIntent {
    string intentType;
    string version;
    uint256 chainId; // 100640 for DIA testnet
    uint256 nonce;
    uint256 expiry;
    string name;
    uint256 price;
    uint256 timestamp;
    string source;
    bytes signature;
    address signer;
}
```

## OracleIntentRegistry
**Contract:** `contracts/contracts/OracleIntentRegistry.sol`  
**Address:** `0xC1ca83b5df6ce7e21Fb462C86f0C90E182d6db5d`

Stores signed OracleIntents used by OracleTrigger

## OracleTrigger
**Contract:** `contracts/contracts/OracleTrigger.sol`  
**Address:** `0x43f1032b7cBa5DA1069a7e40adD529ACdbe9E77C`

Dispatches Hyperlane messages to destination chains

```solidity
function dispatch(
    uint32 _destinationDomain,
    address recipientAddress, // PushOracleReceiverV2 address
    string memory key
)
```

## Hyperlane-Monitor
**Path:** `hyperlane-monitor/`

Monitors `MessageDispatched` events, triggers Bridge failover on delivery timeout

## Bridge Service
**Path:** `bridge/`

Provides failover via direct intent updates to PushOracleReceiverV2 using EIP-712 signatures

## PushOracleReceiverV2
**Contract:** `contracts/contracts/PushOracleReceiverV2.sol`

Receives data via `handle()` (Hyperlane) or `handleIntentUpdate()` (direct failover)

**Deployed instances:**
- **Apple:** `0xe60ccF4248640a2838eDf04516161d706e14bCAF`
- **Ball:** `0x474F45415504f46f143Eb09Ea461F46270F7372f`
- **Cat:** `0x20ab239e69edAA1a24593742fB838e7B2e98128B`

## TODO

1. **Improve logs** - Enhance logging across all services for better debugging and monitoring
   - `attestor/` - Add structured logging
   - `bridge/` - Improve error and event logging
   - `hyperlane-monitor/` - Enhanced monitoring logs

2. **Merge EIP-712 logic** - Create single library for EIP-712 functionality and use it in:
   - `contracts/contracts/OracleIntentRegistry.sol`
   - `contracts/contracts/PushOracleReceiverV2.sol`
   - `contracts/contracts/OracleTrigger.sol`

3. **Solidity lint** - Run linting on all smart contracts for code quality
   - `contracts/contracts/` - All contract files

4. **Add interfaces** - Create proper interfaces for all contracts
   - `contracts/contracts/interfaces/` - New interface directory

5. **Remove require statements** - Replace `require()` with custom errors for gas efficiency
   - `contracts/contracts/` - All contract files