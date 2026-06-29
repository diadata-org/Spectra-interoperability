# Arch destination test fixtures

Vendored from sub-projects 1 (`arch-oracle-program`) and 2 (`arch-oracle-cli`).
The Borsh and PDA helpers in this package assert byte-identical parity against
these fixtures so the wire format stays in lockstep with the on-chain receiver.

| File | Source | Use |
|---|---|---|
| `intent_a.json` | `arch-oracle-program/crates/shared/tests/data/intent_a.json` | `OracleIntent` Borsh round-trip |
| `intent_b.json` | `arch-oracle-program/crates/shared/tests/data/intent_b.json` | Same, second seed |
| `pda_vectors.json` | `arch-oracle-cli/test/fixtures/pda_vectors.json` | PDA derivation parity (5 helpers) |

To regenerate: see the sub-project READMEs. Do not edit by hand.
