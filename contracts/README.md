# Spectra-Interoperability

## Overview
This repository contains the Hyperlane configuration and smart contracts used for interchain Oracle value transfers. It enables secure and efficient cross-chain price updates via two types of Oracles.

## Oracle Types
### 1. PushOracle
In **PushOracle**, price updates occur at regular intervals or when the price deviation exceeds a predefined threshold. These updates are triggered automatically by a service based on predefined rules.
- **Transaction Initiation**: Off-chain service triggers updates.

### 2. RequestBasedOracle
In **RequestBasedOracle**, users initiate an Oracle update request on the destination chain. This request is then sent to the DIA chain, where the Oracle update occurs and is relayed back.
- **Transaction Initiation**: User submits a request on the destination chain.

## Smart Contracts
### 1. **OracleTrigger** (DIA Chain)
- Central contract for oracle price updates.
- Retrieves price updates from `OracleMetadata`.
- Dispatches updates to destination chains upon request.

### 2. **PushOracleReceiver** (Destination Chain)
- Receives oracle updates based on predefined rules.
- Updates the stored oracle values accordingly.

### 3. **RequestOracle** (Destination Chain)
- Accepts user-generated oracle update requests.
- Handles on-chain requests and triggers update transactions.

### 4. **OracleRequestRecipient** (DIA Chain)
- Receives requests from the destination chain.
- Forwards requests to `OracleTrigger` for processing.

### 4. **ProtocolFeeHook** (Destination Chain)
- For RequestBasedOracle, it collects fees upfront.
- For PushOracle, PushOracleReceiver contract must have sufficient funds to receive updates, and the fees are redirected to PaymentHook at the time of update.

## Workflow
### **Push-Based Oracle Flow**
```mermaid
sequenceDiagram
    participant Offchain Service
    participant OracleTrigger
    participant Hyperlane
    participant PushOracleReceiver

    Offchain Service->>OracleTrigger: Fetch latest prices
    OracleTrigger->>Hyperlane: Send updates
    Hyperlane->>PushOracleReceiver: Deliver updates
    PushOracleReceiver->>: Process updates
```

### **Push-Based Oracle Flow**

```mermaid
graph TD;
    OffchainService-->OracleTrigger;
    OracleTrigger-->Hyperlane;
    Hyperlane-->PushOracleReceiver;
    PushOracleReceiver-->UpdateStoredValue;

    User/SmartContract->>RequestOracle: Submit request
    RequestOracle->>Hyperlane: Forward request
    Hyperlane->>OracleRequestRecipient: Deliver request
    OracleRequestRecipient->>OracleTrigger: Fetch price
    OracleTrigger->>Hyperlane2: Send update
    Hyperlane2->>RequestOracleDest: Deliver update
    RequestOracleDest->>: Process update
```


## Permissions

`OracleTrigger`: Can only be called by addresses with the dispatcher role.
- All service addresses must be added as dispatchers.
- OracleRequestRecipient also requires the dispatcher role to interact with OracleTrigger.

`RequestOracle`: Open for anyone to submit requests, provided they pay the required fee for the update transaction.

