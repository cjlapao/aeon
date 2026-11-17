# Quickstart

Four steps. A local model works with no key at all, so step 2 is optional.

## The path

1. **Install** — one binary. `make install`, or put `aeon` on your `PATH`.
2. **`aeon init`** — pick a provider, paste a key. A local model (Ollama,
   vLLM) needs no key at all: `aeon init --provider openai-compatible`.
3. **`cd <a repo>`** — go to the code you want Aeon to work on.
4. **`aeon step "…"`** — run one step, or `aeon loop start "…"` to run a loop.

The key goes to the environment or a dedicated file, never into a config file
and never on the command line. `aeon init` tells you where it put it.

```
aeon init --provider anthropic --model claude --key-file ~/.config/aeon/a.key --non-interactive
cd <a repo>
aeon step "fix the failing test"
```

That is the whole path. A local model works with no key at all.

## What this is

A coding platform that runs a declared sequence of model steps against a
repository, with a human at the gates. The method is data, not prose; the
loop asks rather than guessing; costs are reported honestly, including when
unknown.

## What it does not do

No telemetry. No account. No auto-update. A tool that reads your source code
and runs commands is one you install on trust, so this is stated here, not
only in `PRIVACY.md`.

More — the design, the reasoning — is in `docs/aeon/`. This page is the path,
not the architecture.
