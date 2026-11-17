# Parked workflows

**These do not run.** GitHub reads workflows only from a repository's root `.github/workflows/`, and this
is `aeon/.github/workflows/` inside a monorepo whose root `.github/` belongs to the v1 system that is
building Aeon.

That is intentional. They are authored now so that extracting Aeon to its own repository is a **move**,
not an authoring job — and so the build contract is reviewable while the code it checks is still small
enough to read.

## Activating them

When Aeon moves to its own repository:

1. `git mv aeon/.github .github` (or extract with `git subtree split`, which carries it)
2. Delete this file
3. Check the three things below, because they are the ones that will not simply work

## The three things to check first

**1. Runners.** These inherit `runs-on: [self-hosted, linux]` from the scaffolding, because that is what
the reference repository uses. **Six-target cross-compilation needs more than one linux host**, so this
is a real decision and not a copy: either GitHub-hosted runners per OS (`macos-latest`,
`windows-latest`), or self-hosted hosts for each. Pick before trusting a green build.

**2. CGO.** The scaffolding builds with `CGO_ENABLED=0`. **Aeon is the opposite** (B3) — `sqlite-vec` is
a C extension. So the scaffolding's build job is the wrong template for Aeon's, and `build-all` is
**measured at 1 of 6 targets** once a real cgo dependency is in the tree. S1 T4 owns closing that; until
it does, the six-target job is expected to fail and `ci.yml` keeps it non-blocking so the rest of the
gate still reports.

**3. `GOPRIVATE`.** Required for every Go command — see `../../UPSTREAM.md`. Already set in `ci.yml`.

## What `ci.yml` checks

| Job | Gate |
| --- | --- |
| `build` | format, vet, the no-upstream-copy guard, test, native build |
| `registry` | **the service set matches `services.golden`** — the one that catches a framework upgrade widening the daemon (B11) |
| `cross` | the six release targets. **Non-blocking until T4**, and it reports the count so the gap is a number rather than a feeling |
