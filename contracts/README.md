

#DIA Bridge
Diadata utilizes Hyperlane bridges to transfer or provide data to destination chains. Currently, the supported testnets are Fuji,alfajores, sepolia and BSC testnet.

This repository contains the DIA Bridge smart contract, Hyperlane configuration, and setup instructions.

### OracleTrigger Smart Contract
The OracleTrigger smart contract exists on the Diadata chain. It receives asset prices from the DIA metadata smart contract and propagates them to destination chains via Hyperlane.

### OracleUpdateRecipient Smart Contract
This smart contract resides on the destination chain and retrieves DIA oracle prices.
