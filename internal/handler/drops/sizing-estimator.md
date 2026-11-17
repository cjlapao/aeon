# sizing-estimator — drop record

original: opencode/agents/investigator-loop.md  (82 lines)
rewrite:  aeon/internal/handler/bodies/sizing-estimator.md  (42 lines)
delta:    82 lines removed, 42 added

## LINEAGE (why an ancestor is named for an authored body)

This body was authored fresh for S24 T5 — there is no `opencode/agents/sizing*.md`, and inventing
one would fabricate provenance. The ancestor named above is the investigator-loop: the body whose
*vocabulary of measured size* this one carries in order to *narrow it to a single report*. The
investigator researches broadly — codebase, web, context7 — and writes a discovery document whose
"MEASURED effort-and-size hotspots (real wc -l counts)" are what the planner sizes tasks from
(its description, L3). `sizing-estimator` keeps exactly that discipline — ground the number in
what you actually read — and sheds everything else: no web, no writes, no discovery document, no
FINAL REPORT line. Its output is one `report_size` call (D9: the report is a tool call with a
known shape, not prose to parse) and a handoff that mirrors it. The temperature — 0.3, the
investigator's (L6) — is ported as-is by the pin in `t4_bodies_test.go`: a measurement, like a
discovery, is not the place for temperature; precision is. The delta reads **82 removed, 42
added** precisely because **no line was ported** — every line here is new authorship.

## RITUAL

## BECAME MECHANISM (deleted — the store/engine does this)

- L4 (`mode: subagent`) + L5 (`variant`) → the registry dispatches; a handler is named by a step
  and built by `internal/handler`, not declared a subagent to an orchestrator that no longer
  exists (A5 retired the orchestrator's skill-loading in favor of the closed registry).
- L18-L20 (decline-anyone-but-dev-loops caller guard) → the queue reactor is the caller, and a
  step that arrived is authorised by construction; a body that second-guesses its inviter is a
  second gate with a worse view.
- L34-L52 (write discovery.md to the loop directory; FINAL REPORT terminator) → the handoff schema
  (`size_min`/`size_max`/`size_basis`) is the report, and `report_size` is its single door: the
  tool's refusal (one call, range-with-basis) is *mechanism* — a convention the prompt merely
  asks for would be the same promise unenforced (D9).

## DOMAIN (kept — the judgements this body runs on)

- L3: the investigator's core judgement survives intact: **a size is only honest when it names
  what was inspected** — paths read, call sites counted (its "real wc -l counts", distilled). The
  body keeps that as the basis rule (D8: a range without its basis is a bare number wearing a
  hyphen) and adds the honesty rules the queue needs: widen the range when the ground is
  uncertain; say so in the basis when named code is absent.
- L3: the read-mostly stance (its "NEVER edits code") is kept *tighter*: no `write`, `edit`,
  `bash`, `webfetch` in the toolset at all — the estimator sizes; it does not touch (CG5's shape:
  nothing reachable from this body can append a queue item either; the exemption is structural).

## DEFERRED (kept OUT — the capability is not in the body, and why)

- L4-L5: MCP / context7 (its library-docs tooling) and L34-L52: the discovery-writing research
  ritual that consumed it → the MCP client (S12) is not built; the sizing run stays native
  (brief §5: "Sizing is native, not MCP"), and a size guessed from un-fetched library docs would
  violate the basis rule anyway. When S12 lands, widening the toolset is this record's amendment
  line, not a silent front-matter edit.
- L3: the web (`webfetch`, its "fetching web pages") → deferred with the same argument: sizing
  reads THIS repository (design.md CG5); external colour without inspection is exactly what a
  basis must not be.

## REWORDED (A17 — clarity edits on kept intent, line-referenced against the original)

- L3         the description's research-portfolio clauses (web/GitHub/context7 discovery, the
  discovery document) → the description names the queue's ONE report (`report_size`, the range
  with its basis); same honesty rule (what it does, what it refuses), different service — no
  ported line, since the narrowed purpose changed every sentence's shape. Authorship, not
  rewording — the DOMAIN above names the judgements carried over in new words.
