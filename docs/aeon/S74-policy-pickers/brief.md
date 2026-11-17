# S74 — policy pickers: tool addresses, wildcards, and select boxes

Written 2026-11-07. Stage 5 · Fleet. Depends on S45 (policy management), S47 (tools catalog), S72 (Configuration shell).

## The problem

An agent starts with access to nothing (default deny). The operator wrote that down as the right default and it stays. The trouble is the next step. A brainstorm run on 2026-11-06 had its `glob` and `read` calls refused, and the way out is to author policies, which is hard for three reasons:

1. The policy form takes the tool name as free text. The operator does not know which tools exist, how they are spelled, or which are safe to allow. The Tools catalog (S47) lists them, but the policy form does not use it.
2. `ToolAccess` matches `toolName` exactly. There is no way to say "every read tool". One policy per tool per agent is what the operator faces today.
3. Every other field (agent, component, skill, MCP server) is a text input too, although the values all exist in the daemon.

## What this slice is

1. **A tool address.** Every tool gets one dotted address, `<category>.<tool>.<access>`, for example `workspace.glob.read`, `workspace.write.write`, `mcp.github.create_issue.write` (MCP tools carry the server as part of the category). `category` is the tool's existing catalog family. `access` is `read` or `write`, derived once from the tool's declared capabilities (design §2).
2. **Wildcards in `ToolAccess`.** A `toolName` may be a bare tool name (as today) or an address pattern where any whole segment may be `*`: `workspace.*.read`, `*.*.read`, `workspace.*.*`. The only wildcard is a whole-segment `*`; no partial segments. This amends the S10 ruling "no globs in ToolAccess" and says why (design §3).
3. **Select boxes in the policy form.** Every value field is a picker fed by the daemon: tools (by address, with the wildcard forms offered as rows), agents, components, skills, MCP servers. A free-text escape stays for values that do not exist yet.
4. **A live match preview.** While the operator writes a pattern, the form shows which tools it matches today ("matches 6 tools: workspace.glob.read, …") and says plainly when it matches none.
5. **The catalog shows the address.** The Configuration → Tools page gains the `category`, `access` and address columns and a filter on each, so the operator finds the tools before authoring.

## What it is not

- It does not change default deny, `deny wins` (PL4), or human-door-only authoring (PL15). A wildcard `allow` loses to any `deny`.
- It does not add a policy type. `ToolAccess` keeps its `{toolName, state}` shape; only what `toolName` may hold widens.
- It does not choose the seeded defaults for the operator. T5 only proposes them (design TA15; operator said yes to a disabled seed).

## Acceptance

1. A bare-name `ToolAccess` row behaves exactly as before, byte for byte, with the existing tests unchanged.
2. `workspace.*.read` allows `glob`, `grep`, `list`, `read` for the target and nothing that writes; a `deny` on `workspace.read.read` still wins over it.
3. A pattern with a partial-segment star, an empty segment, or more than three segments is refused at the write boundary naming the offence.
4. The policy form offers no free-text tool field as the only choice; the preview matches the evaluator's answer for the same pattern (one matcher, in the daemon).
5. The brainstormer's refused `glob` and `read` from 2026-11-06 are allowed by one authored policy (`*.*.read` for that agent), proved on a running app in T5.
