# Arch destination config examples

To enable Arch as a destination in your bridge deployment:

1. Use `arch-oracle-cli` (sub-project 2) to deploy the receiver + fee-hook
   programs, initialize them, and authorize at least one signer.
2. Copy the two `address` / `fee_hook_program_id` values from
   `arch-oracle profile show <name>` into `arch-testnet.contracts.yaml`.
3. Merge `arch-testnet.chains.yaml` into your `chains.yaml`.
4. Merge `arch-testnet.contracts.yaml` into your `contracts.yaml`.
5. Drop `arch-testnet.router.yaml` into your `routers/` directory.
6. Generate a relayer keypair (`arch-oracle wallet create --name arch-relayer`)
   and set `ARCH_RELAYER_PRIVATE_KEY` to its `secretKeyHex` field. On localnet
   /devnet fund it via `arch-oracle wallet faucet`; on testnet/mainnet fund
   out-of-band.
7. Restart the bridge.

For mainnet, mirror the same steps with `chain_id: -2`, the mainnet RPC URL,
the mainnet receiver/fee-hook program IDs, and `enabled: true`.
