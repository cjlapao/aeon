# Card fixtures (S14 T1)

Three states of one card, generated from `signedGood` in `card_test.go` with the
fixed test key (ed25519 seed = bytes `0x01..0x20`):

- `reddit-agent.json` — the valid envelope; `TestCommittedFixturesMatchTheirNames` verifies it.
- `reddit-agent-tampered.json` — one sentence of the card body altered after signing.
- `reddit-agent-unsigned.json` — the signature field emptied.

Regenerate (from `aeon/`): paste the generator from `git log -p -- internal/agentsvc` for this
task, run `AEON_GEN_FIXTURES=1 go test ./internal/agentsvc/ -run TestGenFixturesOnly`, delete the
generator again. The fixtures are only ever READ by the tests.
