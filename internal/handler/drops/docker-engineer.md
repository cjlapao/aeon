# docker-engineer — drop record

original: opencode/agents/docker-engineer-loop.md  (87 lines)
rewrite:  aeon/internal/handler/bodies/docker-engineer.md  (50 lines)
delta:    39 lines removed, 2 added
## RITUAL (deleted — the engine does this)
- L44-L46  caller guard checking who dispatched            → the blueprint dispatches; a body that declines its dispatcher contradicts the step that runs it
- L48-L50  "Load the dev-loop-protocol skill first" + the 1000-line rule  → nothing loads skills here; the COVERAGE CARVE-OUT half of L50 is domain and survives (the analogies to ui-design/docs named the old routing; dropped with it)
- L54      `pwd` + `loopSlug` + the loop-file dir + read-back framing → context arrives resolved; the project-root-relative PATH RULE itself survives (Boundaries)
- L55      loop_get + loop_readFile(plan) + loop_readTask(taskid) reads → the step's request carries the task, its criteria and files_expected; the "primary contract" role survives
- L56      **Re-read prior failures** via loop_get counters/evidence/lessons → the loop-state read is gone; the don't-repeat-the-failure instruction survives generically (Working method 1)
- L68      scratch note via `loop_writeFile` (its content list: size before/after, layers, times, scan, base-image why, delegated follow-ups) → those contents are exactly the `size`/`scan` handoff fields + summary (REWORDED); the delegated-follow-up routing (CI→deployment-engineer, cluster→kubernetes-specialist) rides with the out-of-scope rule below
- L69      `memory_append` gotcha + "never hand-write memory.md (the MCP renders it)" → no memory tool; the rendering half is DEFERRED
- L70      `event_append` agent-return checkpoint → the daemon emits step events; a body cannot write its own timeline row
- L71      FINAL REPORT line (half) → the handoff is structured; the verification=pass CONDITION survives verbatim as prose (REWORDED), the key-values do not
- L84      "You NEVER mutate loop state... state tools... loop_get" (half) → the handoff IS the output; there is no state within reach to mutate

## BECAME MECHANISM (deleted — a tool guarantees it now)
- L4-L5    `mode: subagent`, `variant: coder-fast`          → A1 removed spawning; `variant` measured to have zero consumers (Q2) — recorded by the loader, never ported
- L7-L21   tools map with true/false entries                → allow-list `[read, write, edit, bash, glob, grep, todo, webfetch]`: task DROPPED (A1, never renamed), todowrite→todo (A19), codesearch/lsp/patch retired (W1), question not re-granted; webfetch GRANTED for the docs-resolution step that the original ran through a server (rewritten below)
- L22-L35  permission.bash deny table — including THE TWO MID-PATTERN WILDCARDS `docker buildx build*--push*` / `docker buildx bake*--push*` → these are S11 corpus content byte-identically (the only mid-wildcard rules in the entire corpus — the reason the matcher cannot be prefix-only, G9); `TestDockerPolicyMatchesTheCorpus` re-parses the original and pins byte-for-byte including `*` positions
- L66      **"NEVER `docker push` / `--push` (permission-denied) — publishing is delegated to CI/the pipeline"** → A12's middle category: the corpus denies all three push shapes and the policy engine refuses them at spawn; prose restating enforcement is a second, staler copy of the guard. `TestDockerPushProseIsGone` keeps the body clean; the One Rule's craft line ("the image you push is the image you'll debug at 3am") is craft, not an instruction, and stays

## DOMAIN (kept)
- L40      the role — production-grade artifacts, small/pinned/scanned/non-root/healthchecked, one task at a time → role line
- L42      THE ONE RULE verbatim → after the role
- L50      (half) the coverage carve-out: the ≥80% unit-coverage mandate does NOT apply to container artifacts; the gate is build + scan + healthcheck + size → Carve-out
- L57      inspect the runtime first — language, dep manager, runtime user, ports, shutdown signal, health endpoint, PID-1 reaping — and existing Dockerfile/compose/bake before writing → Procedure 2
- L60      the modern-standards line verbatim: syntax directive 1.7, multi-stage, digest-pinned bases, numeric non-root USER, exec-form ENTRYPOINT/CMD, HEALTHCHECK, OCI labels, `--mount=type=secret` never ARG/ENV, Compose with no `version:` key, explicit networks, read_only+tmpfs, cap_drop ALL, no-new-privileges, depends_on service_healthy → Procedure 5
- L61-L65  the verification suite verbatim in substance: bounded commands, tear-down always (`compose down`, `rm -f`) even on failure, build/`--load`/`compose config`, detached run as non-root + health poll ≤60s + clean SIGTERM, scan with no unapproved HIGH/CRITICAL or a justified exception, size + layers + cold/warm times (the scanner COMMANDS named at L64 are DEFERRED; the honesty clause "a missing scanner is a stated gap, not a clean result" keeps L63's spirit)
- L67      a timeout is a FAIL with evidence — investigate, never re-run unchanged → Procedure 6
- L75-L79  container craft verbatim: layer ordering, one RUN per concern, never apt-get upgrade, COPY not ADD; multi-stage with base-per-workload trade-offs (distroless/Alpine/Chainguard/Debian-slim/scratch); digest/runtime pins/-trimpath/.dockerignore list; tini/dumb-init + STOPSIGNAL + cache mounts; SBOM/provenance/cosign as design intent described for the CI hand-off (the "describe in scratch" half → summary) → craft section
- L83      one task per turn; do not implement neighbours visible in plan.md → Boundaries (line shared with terraform's original verbatim)
- L86      stay in the container artifact; k8s/CI YAML/IaC/app code out of scope, blocked naming the right engineer → Boundaries
- L87      impossible/contradictory criterion → blocked describing why, never reinterpret → Boundaries (reworded per-body to keep each body's line traceable)

## DEFERRED (kept OUT — the capability is not in the body, and why)
- L58      "Resolve docs via context7" → the context7 MCP half rides with S12 per ROADMAP §7 (an operator who CONFIGURES the server gets it); the body keeps the same judgement — resolve docs only when it earns it — through `webfetch`, the tool the registry actually has
- L64      the named scanners `docker scout cves` / `trivy image --severity HIGH,CRITICAL` → capabilities an installation may lack (A18): the body requires "the security scan the environment offers" + honest gap-reporting; the named commands are recorded here so the judgement can be re-armed where they exist
- L69/L84  the loop-memory rendering and the loop-state MCP (memory.md rendering, orchestrator state recording) → S12's territory; the handoff outcome replaces the state write

## REWORDED (O3 — A17's ledger; meaning unchanged, 0 added lines)
- L3       description's dev-loops framing compressed; the never-pushes clause dropped with its policy counterpart
- L55      dispatch reads restated as the step's request; `loop_readTask` "primary contract" role kept on the spec
- L68/L71  scratch content list + FINAL REPORT evidence restated as the `size` and `scan` handoff fields (base-image why, scan headline, times)
- L79      "describe them in scratch for the CI hand-off" → "describe them in your summary" (the handoff is what the next step reads)
