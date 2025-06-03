# DIA Lumina Smart Contracts

`@dia-data/contracts-spectra` is a suite of smart contracts used in the Lumina oracle stack for secure and efficient cross-chain messaging. This package also includes contract ABIs used for off-chain integrations.

## Installation

### PNPM
```bash
pnpm add @dia-data/contracts-spectra
```

### NPM
```bash
npm install @dia-data/contracts-spectra
```

## Usage
You can import the smart contracts from `@dia-data/contracts-spectra` into your solidity code as follows:

```solidity
import { PushOracleReceiver } from "@dia-data/contracts-spectra/PushOracleReceiver.sol";
```

To access the smart contract ABIs in `@dia-data/contracts-spectra/abis`:
```javascript
const PushOracleReceiverABI = require("@dia-data/contracts-spectra/abis/PushOracleReceiver.json");
```

## Directory Structure

```
@dia-data/contracts-spectra
├── abis
│   ├── OracleRequestRecipient.json
│   ├── OracleTrigger.json
│   ├── ProtocolFeeHook.json
│   ├── PushOracleReceiver.json
│   └── RequestOracle.json
├── interfaces/
├── libs/
├── OracleRequestRecipient.sol
├── OracleTrigger.sol
├── ProtocolFeeHook.sol
├── PushOracleReceiver.sol
└── RequestOracle.sol
└── ..
```