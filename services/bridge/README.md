# Bridge service

The bridge service relays signed oracle intents from Lasernet to destination
chains. It is configured via YAML files in `config/` and exposes Prometheus
metrics for operational observability.

## Supported destinations

### EVM destinations

The bridge supports arbitrary EVM-compatible destinations. Each destination is
identified by its chain ID in `chains.yaml` and wired to a receiver contract in
`contracts.yaml`.

### Arch Network destinations

The bridge supports `kind: "arch"` destinations alongside the existing EVM
backends. An Arch destination relays a signed `OracleIntent` to the receiver
program deployed via the `arch-oracle-cli` (sub-project 2). Two reserved
chain-ID sentinels:

- `-1` — Arch testnet
- `-2` — Arch mainnet

See `docs/ARCH_DESTINATION.md` for the operator runbook and
`config/examples/` for a paste-ready YAML stub.
