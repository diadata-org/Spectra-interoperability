# Arch destination runbook

Sub-project 3 of the DIA × Arch Network integration.

## Prerequisites

- A deployed receiver + fee-hook program on the target Arch network. Use
  `arch-oracle-cli` (sibling repo): `arch-oracle deploy`, `arch-oracle init`,
  `arch-oracle configure set-signer`.
- A relayer keypair generated with `arch-oracle wallet create`. The 64-char
  `secretKeyHex` becomes the value of the env var named in `private_key_env`.
- A funded relayer account. Localnet/devnet: `arch-oracle wallet faucet`.
  Testnet/mainnet: fund out-of-band, at least 0.01 BTC equivalent in lamports.

## Config

Three YAML changes:

1. `chains.yaml`: add an entry with `kind: "arch"` and one of the reserved
   chain IDs (-1 testnet, -2 mainnet).
2. `contracts.yaml`: add a contract entry with `address` set to the receiver
   program ID and `fee_hook_program_id` set to the fee-hook program ID.
3. `routers/<name>.yaml`: add a router with `destinations[].method.kind:
   "arch-handle-intent-update"`.

Paste-ready stubs live under `config/examples/`.

## Behavior

The bridge sends one `HandleIntentUpdate` transaction per intent
(V1 — no batching). It signs with BIP-340 Schnorr (Taproot key-path),
waits for processed status (timeout 30s), then parses the receiver's
`DIA_ORACLE.*` log lines.

- Successful update → rejection-free `TxResult`; logged with outcome
  `delivered`. No DB writes from the Arch path beyond the upstream
  processed_events row.
- Per-intent rejection (UnauthorizedSigner, AlreadyProcessed,
  InvalidSignature) → one row per rejection in `dia_arch_rejections`;
  logged with outcome `partially_delivered`. (A `processed_events.status`
  column is planned but not yet schema-migrated.)
- Tx-level failure (BatchTooLarge, InvalidAccountList, RPC error) →
  logged with outcome `failed`. No persistence on the Arch path; the
  existing EVM-style metrics path handles transaction-failure surfacing.

## Metrics

| Metric | Type | Labels |
|---|---|---|
| `dia_arch_intent_updates_total` | counter | router, chain_id, symbol |
| `dia_arch_intent_stale_total` | counter | router, chain_id, symbol |
| `dia_arch_intent_rejected_total` | counter | router, chain_id, reason |
| `dia_arch_tx_confirmation_seconds` | histogram | router, chain_id, outcome |
| `dia_arch_fee_vault_lamports` | gauge | router, chain_id |
| `dia_arch_payer_balance_lamports` | gauge | router, chain_id |

The two gauges are polled every 30s by a per-destination goroutine. Use the
payer balance gauge for relayer-out-of-funds alerting.

## Testing

- Unit tests (no live infra): `go test ./internal/arch/ ./internal/bridge/`.
- Live end-to-end test: see `test/integration/README.md`.
