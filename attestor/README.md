# Oracle Attestor Service

This service reads oracle values from a DIA Oracle contract, creates signed messages with the values and timestamps, and attests them to a SignedOracle contract.

## Requirements

- Go 1.20 or higher
- Access to an Ethereum node (RPC URL)
- Private key with sufficient funds for transactions
- SignedOracle contract address

## Installation

1. Clone the repository:
   ```bash
   git clone https://github.com/diadata.org/Spectra-interoperability.git
   cd Spectra-interoperability/attestor
   ```

2. Install dependencies:
   ```bash
   go mod download
   ```

3. Create a `.env` file based on the example:
   ```bash
   cp .env.example .env
   ```

4. Edit the `.env` file and fill in the required values:
   ```
   RPC_URL=https://testnet-rpc.diadata.org
   ORACLE_ADDRESS=0x0087342f5f4c7AB23a37c045c3EF710749527c88
   SIGNED_ORACLE_ADDRESS=<your-signed-oracle-contract-address>
   PRIVATE_KEY=<your-private-key>
   SYMBOL=BTC/USD
   POLLING_TIME=60
   ```

## Usage

Run the attestor service:

```bash
go run main.go
```

The service will:
1. Connect to the specified RPC URL
2. Read the oracle value for the specified symbol
3. Create a signed transaction to attest the value to the SignedOracle contract
4. Send the transaction to the blockchain
5. Wait for the specified polling time and repeat

## Configuration

The service can be configured using environment variables:

- `RPC_URL`: The RPC URL of the Ethereum node (default: https://testnet-rpc.diadata.org)
- `ORACLE_ADDRESS`: The address of the Oracle contract (default: 0x0087342f5f4c7AB23a37c045c3EF710749527c88)
- `SIGNED_ORACLE_ADDRESS`: The address of the SignedOracle contract (required)
- `PRIVATE_KEY`: The private key used to sign transactions (required)
- `SYMBOL`: The symbol to query from the Oracle (default: BTC/USD)
- `POLLING_TIME`: The time in seconds between attestations (default: 60)

## Process Flow

1. The service reads the current value from the Oracle contract using the `getValue(string)` function
2. It creates a transaction to call the `updateData(uint256, uint256, string)` function on the SignedOracle contract
3. The transaction is signed using the provided private key
4. The signed transaction is sent to the blockchain
5. The service waits for the specified polling time and repeats the process

## Logs

The service logs information about each attestation, including:
- The retrieved price and timestamp
- The transaction hash of the attestation
- Any errors that occur during the process

## Security Considerations

- The private key is used to sign transactions and should be kept secure
- The service should be run in a secure environment
- Consider using a dedicated account with limited funds for the attestation process 