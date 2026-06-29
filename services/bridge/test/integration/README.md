# Arch destination — integration test

End-to-end test against a running Arch validator. Gated by environment
variables — `go test` skips cleanly when any are missing.

## Prerequisites

1. Run a local Arch validator following the
   `arch-oracle-program/README.md` "Local validator quickstart" section.
2. Build the receiver + fee-hook SBF artifacts (`cargo build-sbf` in
   `arch-oracle-program/`).
3. Use `arch-oracle-cli` to: create a wallet, faucet it, deploy both
   programs, init fee-hook, init receiver with at least the seed-0x11
   signer authorized:

   ```bash
   cd arch-oracle-cli
   npm install && npm run build
   arch-oracle wallet create --name bridge-relayer
   arch-oracle profile create local --rpc http://127.0.0.1:9002 --payer bridge-relayer --network localnet
   arch-oracle wallet faucet bridge-relayer --profile local --count 20
   arch-oracle deploy ../arch-oracle-program/target/deploy/dia_arch_fee_hook.so --profile local
   arch-oracle init fee-hook --fee 1000 --profile local
   arch-oracle deploy ../arch-oracle-program/target/deploy/dia_arch_oracle_receiver.so --profile local
   DS=$(arch-oracle domain-separator --name "DIA Oracle Intent Registry" --version 1 --chain-id 1 \
        --verifying-contract 0x0102030405060708090a0b0c0d0e0f1011121314)
   arch-oracle init receiver --domain-sep "$DS" --profile local
   arch-oracle configure set-signer 0x19e7e376e7c213b7e7e7e46cc70a5dd086daff2a --authorized true --profile local
   ```

4. Export the env vars the test reads:

   ```bash
   export ARCH_RPC_URL=http://127.0.0.1:9002
   export ARCH_RELAYER_PRIVATE_KEY=$(jq -r .secretKeyHex ~/.config/arch-oracle-cli/keypairs/bridge-relayer.json)
   export ARCH_RECEIVER_PROGRAM_ID=$(arch-oracle profile show local --json | jq -r .receiverProgramId)
   export ARCH_FEE_HOOK_PROGRAM_ID=$(arch-oracle profile show local --json | jq -r .feeHookProgramId)
   ```

5. Run the test:

   ```bash
   cd services/bridge
   go test ./test/integration/ -run TestArchBridge_EndToEnd -v
   ```

   The test sends the `intent_a.json` fixture, asserts the Price PDA gets
   populated, replays the same intent and asserts the replay is rejected
   with `AlreadyProcessed`.
