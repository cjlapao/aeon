# Privacy

What leaves the machine. One page, read before installing — that is the point.

## The whole of it

Your prompts and code go to the provider you configured, and nothing goes
anywhere else.

That is the whole of it, and it is why the product reads source code and runs
commands and still earns trust: a tool in that position that phones home has
spent the only asset it has.

## What is allowed to connect

The only hosts Aeon connects to are the ones you configured:

- the model route you set (`api.anthropic.com`, `api.openai.com`, …)
- the forge you configured (`forge.example.com`, …)
- the embedding route you configured (S22)
- an `openai-compatible` base URL you set (`localhost` for Ollama, vLLM, LiteLLM, …)

**Nothing else.** No analytics host, no crash reporter, no version check.

A version check is telemetry with a friendly name: it reports that this
machine is running this version at this time. It is not allowed.

## What is permanent

There is no flag that turns telemetry on, no first-run question, and no
"help us improve" path. It is permanent. The next person to want it will look
for the flag first — so this page and the test that guards it say it is
permanent, and the test fails if it stops being true.

## What Aeon stores, and where

Aeon stores your loops, plans, and policy locally, under your state
directory (XDG, `~/.local/state/aeon` by default). It holds a forge token,
but a token goes in and never comes out (the response type has nowhere to put
it). Nothing it stores goes anywhere else.

## How this is checked

A test (`internal/nettest`) runs a full loop with tools and a forge action and
asserts no connection except to the configured hosts, at both the transport
level and the process level (a dependency that makes its own connection would
not go through the transport wrapper). The trace runs in CI. A network
assertion nobody runs is a network assertion that stops being true.
