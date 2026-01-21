# Contract Scripts

This directory contains deployment and utility scripts for the Spectra Interoperability contracts.

## Directory Structure

### `/deploy`
Contains all deployment scripts for various contracts:
- **Core Contracts**:
  - `deployDiaContracts.ts` - Main DIA contracts deployment
  - `deployPushoracle.ts` - PushOracleReceiver deployment
  - `deployOracleIntentRegistryEIP712.ts` - EIP712 Intent Registry
  - `deployOracleIntentConsumerEIP712.ts` - EIP712 Intent Consumer
  - `deployOracleTriggerDIA.ts` - DIA Oracle Trigger
  - `deployToOptimismSepolia.ts` - Optimism Sepolia deployment

- **Infrastructure**:
  - `deployIsm.ts` - Interchain Security Module
  - `deployIsmWithValidators.ts` - ISM with validator configuration
  - `deployProtcolFeeHook.ts` - Protocol fee hook contract

### `/utils`
Contains utility scripts for contract management and operations:

- **DIA Bridge Operations**:
  - `getDIASigners.ts` - Get authorized signers from DIA testnet
  - `readDIAIntentData.ts` - Read intent data from DIA testnet
  - `readDIAIntentEvents.ts` - Read IntentRegistered events

- **Contract Management**:
  - `authorizeNewSigner.ts` - Authorize new signers on contracts
  - `authorizePushOracleSigner.ts` - Authorize signers on PushOracleReceiver
  - `updatePushoracleSigners.ts` - Update PushOracle authorized signers
  - `fundContract.ts` - Fund contracts with ETH
  - `fundNewAccount.ts` - Fund new accounts

- **Configuration Updates**:
  - `updateChainConfig.ts` - Update chain configuration
  - `updateOracleTriggerDestination.ts` - Update trigger destinations
  - `updateOracleTriggerReceiver.ts` - Update trigger receivers
  - `updateOracleTriggerRegistry.ts` - Update trigger registry
  - `setInterchainSecurityModule.ts` - Set ISM configuration

- **Account Management**:
  - `generateOpSepoliaAccount.ts` - Generate new Optimism Sepolia account

- **Validators**:
  - `getAllValidators.ts` - Get all validators configuration
  - `mergeValidators.ts` - Merge validator configurations

- **Verification**:
  - `verifyPushoracle.ts` - Verify PushOracle deployment
  - `verifyContracts.ts` - Verify deployed contracts

## Usage

All scripts should be run using Hardhat:

```bash
npx hardhat run scripts/<directory>/<script-name>.ts --network <network-name>
```

Example:
```bash
npx hardhat run scripts/deploy/deployPushoracle.ts --network optimismSepolia
```