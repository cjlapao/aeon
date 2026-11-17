# S74 — design

Rules are numbered TA1… so tasks can cite them.

## 1. What exists today (read on 2026-11-07)

- `internal/policy/spec_toolaccess.go`: `ToolAccessSpec{toolName, state}`, `toolName` matched exactly, unknown fields refused, deny wins across policies.
- `internal/tools/meta.go`: every tool declares `Meta{Family, Capabilities, …}`. The capability vocabulary is closed: `workspace_read`, `workspace_write`, `spawn_process`, `network_egress`, `asks_human`, `messages_agent`, `org_records_read`, `org_records_write`, `starts_loop`, `index_read`, `graph_read`. The records tools also carry `Mutates`.
- S47: `tools.*` socket reads, the Tools page with family and capabilities, MCP tools included from S46's rows.
- `ui/src/features/settings/policy/SpecFields.tsx`: renders `<select>` when the contract's field control has `options`, an `<Input>` otherwise. `toolName` has no options, so it is text.

## 2. The address (TA1–TA4)

- **TA1** The address is `<category>.<tool>.<access>`, three segments, lower case. For system tools `category` is `Meta.Family`. For MCP tools the category is `mcp` and the tool segment is `<server>.<tool>`; the one structural exception is that an MCP address therefore has four segments. Patterns over MCP tools use the same wildcard rule (`mcp.github.*.read`; a three-segment pattern such as `*.*.read` reaches an MCP tool with `<server>.<tool>` taken as one tool segment).
- **TA2** `access` is `write` when the tool declares any of `workspace_write`, `spawn_process`, `network_egress`, `org_records_write`, `starts_loop`, `messages_agent`; otherwise `read`. A tool with no capabilities (`todo`, `report_size`) is `read`. Asking a person (`asks_human`) is `read`. The rule is one function in `internal/tools` and a table test pins the answer for every registered tool, so a new tool cannot arrive without an answer. Q1 asks whether `network_egress` should be `write`.
- **TA3** MCP tools: `access` is `read` when the server declares the tool read-only (the MCP `readOnlyHint` annotation, if S46 stores it), otherwise `write`. A tool with no declaration is `write`, because absent is not safe (P31).
- **TA4** The address is computed, not stored. It is a function of the registry and the MCP rows, so it cannot drift from them. `tools.list` and `tools.view` return it as `address`, `category` and `access`.

## 3. Wildcards (TA5–TA9)

- **TA5** `ToolAccessSpec.toolName` accepts either a bare tool name (no dot, matched exactly as today) or an address pattern (dotted).
- **TA6** In a pattern, a segment is a literal or exactly `*`. `*` matches one whole segment. No `wo*`, no `**`, no regex, no character classes. An empty segment, a partial star, or the wrong segment count (three for system tools; the MCP form is validated against its four) is refused at the write boundary with the offending segment named.
- **TA7** Matching lives in one function, `policy.MatchToolAddress(pattern, address)`, beside `MatchPattern`. The gate, the form's preview (a daemon method, TA10) and the in-force explorer all call it. The UI never matches patterns itself.
- **TA8** A bare name `bash` matches the tool segment of any address whose tool is `bash`, which is the same set it matched before. Nothing about old rows changes.
- **TA9** Why this amends S10's "no globs": the objection was a second rule language nobody audits. A whole-segment star over a closed, daemon-computed vocabulary is not a language, it is a way to name a set that the daemon can list. The preview (TA10) is how the operator audits it, and TA6 keeps the grammar smaller than `MatchPattern`'s. Record the amendment in `S10-policy/design.md` PL notes.
- **TA9b** Specificity and precedence are unchanged: deny wins between policies (PL4); the more specific target wins within an action (PL4). A wildcard does not make a policy more specific.

## 4. The preview and the pickers (TA10–TA13)

- **TA10** New read method `tools.matchPattern {pattern}` (named under `tools.` rather than `policy.` so the `policy.` authoring-names fence of S45 PM1/PM10 stays untouched) returning the matching addresses and a count, computed with TA7. It is a read, outside the write door, and works without the form being valid beyond the pattern itself. Zero matches is an answer, shown as "matches no tool today" (three-state honesty: it may still match a tool that is added later).
- **TA11** The policy form's field controls come from the daemon's `policy.vocabulary`/spec description, not from a list in the form. A `toolName` field gets `control: "tool-address"`; the form renders the picker for that control. An unknown control still falls back to text with the existing "unknown kind" warning.
- **TA12** Tool picker: a searchable list grouped by category, each tool row showing address and a read/write badge, with three extra rows at the top of each group: "all <category> tools", "all read tools in <category>", "all write tools in <category>", and at the very top "all read tools" / "all write tools" / "all tools". Choosing a row fills the pattern; the operator can still edit the pattern text.
- **TA13** Target pickers: agent (from `agent.list`), component and project (from the org reads), skill (from `skills.list`), MCP server (from S46). Each is a select with a type-to-filter. Each keeps a "type a value" escape that shows the existing "unknown value" warning.

## 5. The catalog page (TA14)

- **TA14** The Configuration → Tools page shows `category`, `access`, and the address, with filters on category and access, and each row's drawer gets a "Write a policy for this tool" link that opens the policy form with the address filled. The S47 drift banner is untouched.

## 6. Defaults (TA15)

- **TA15** The slice ships no default allow. T5 writes a proposal, not a policy: a seed record offering `allow *.*.read` for `all-agents`, off until the operator turns it on, because granting every agent every read tool is the operator's ruling to make (Q2).

## Operator answers (2026-11-07)

- **Q1** `network_egress` makes a tool `write`. `webfetch` is therefore `egress.webfetch.write`. TA2 already lists `network_egress` as a write capability; this confirms it.
- **Q2** Yes: ship the disabled seed `allow *.*.read` for `all-agents` (TA15, T5). It stays off until the operator turns it on.
- **Q3** Keep the family names as the category (`workspace`, `egress`, `process`, `interaction`, `collaboration`, `planning`, `sizing`, `retrieval`, `code-graph`). No `system` umbrella.
- **Q4 (2026-11-07, after the policy-page work)** A three-segment pattern names system tools only and never reaches an MCP tool. `*.*.*` is "every system tool"; MCP tools are named with four segments (`mcp.*.*.*`, `mcp.github.*.read`). This replaces the earlier collapse in TA6 that let `*.*.read` reach MCP tools: a broad grant must not silently cover a third-party server. `MatchToolAddress` and its table test carry the rule.
