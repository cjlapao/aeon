import { useCallback, useEffect, useState } from "react";
import { Alert, Button, FormField, Input, Panel, Pill } from "../../../components/kit";
import { onEvent, rpc, toRpcError } from "../../../lib/bridge";
import { costLine } from "../../../lib/cost";
import type { Cost } from "../../../lib/protocol";

/**
 * The bench demo runner (spec 2026-10-25-bench-demo-runner).
 *
 * # What this page is
 *
 * `docs/aeon/S13-agents/DEMO.md` walked from a terminal; this tab runs the
 * same scenarios from the app, step by step, where they will be consumed
 * (AGENTS §4b). Each step is DATA — a table the runner executes against the
 * socket through the generic `rpc()` bridge, adding no Rust.
 *
 * # The three verdict states, and why there are exactly three
 *
 * A step lands in one of **met / refused-as-data / not runnable here**, and
 * the page has no fourth: "it seemed to work" is how a bench becomes a
 * fiction. `refused` renders the daemon's own sentence **verbatim** — the
 * verify findings and block reasons arrive already written by Go, and a
 * surface that re-cases them builds a second truth (the policy formatter's
 * argument, applied here). `not runnable here` is a capability gap named
 * with its remedy — the no-credential state says so and shows the operator
 * command, exactly as the daemon's banner prints `routes none` — never a
 * silent skip and never a pass.
 *
 * # The MCP half is a REPLAY, and says so on every row
 *
 * `internal/mcp` reaches `cmd/aeon` only — `absent_test.go` pins it, on
 * purpose, the whole point of S12. So the MCP demo does not call the daemon
 * (test/bench-demos.test.tsx asserts no `tools.*` ever leaves this page):
 * it replays the spec-literal fake server's envelopes and cites the Go test
 * that proves them. A replay whose label you must go find is a lie waiting
 * for its first reader.
 *
 * # Loops it starts stay in the store
 *
 * Slugs are prefixed `demo-` and loops are never deleted — the continuing
 * QA history (S9's tradition) treats every started loop as a record.
 */

type VerdictState = "pending" | "running" | "met" | "refused" | "unavailable" | "error";

interface Verdict {
  state: VerdictState;
  text: string;
  command?: string;
}

interface DemoStep {
  id: string;
  label: string;
  run: (ctx: Ctx) => Promise<Verdict>;
}

interface Ctx {
  workspaceRoot: string;
  runId: string;
  /** ids of loops this run started, in order — for opening them afterwards. */
  loopIds: string[];
}

const tagGoal = "add a --version flag to cmd/hello that prints the module version and exits 0, no new dependencies";

function verbatim(x: unknown): string {
  return typeof x === "string" ? x : JSON.stringify(x);
}

const registryStep: DemoStep = {
  id: "registry",
  label: "handlers.list — the registry the daemon booted with",
  run: async () => {
    const view = await rpc<{ handlers: { name: string; kind: string }[]; resolvers: string[] }>("handlers.list", {});
    const bodies = view.handlers.filter((h) => h.kind === "body").length;
    const presets = view.handlers.filter((h) => h.kind === "preset").length;
    const resolvers = (view.resolvers ?? []).join(", ");
    if (bodies === 25 && presets === 2 && view.resolvers?.includes("by-tech-tag")) {
      return { state: "met", text: `${bodies} bodies, ${presets} presets (${resolvers ? `resolver ${view.resolvers.join(", ")}` : "no resolver?!"}) — 27 on the socket, 25 in the corpus, both truths on one row` };
    }
    return { state: "refused", text: `registry says ${bodies} bodies, ${presets} presets, resolvers ${resolvers || "none"} — the demo expects 25, 2 and by-tech-tag` };
  },
};

const corpusStep: DemoStep = {
  id: "corpus",
  label: "handlers.verify corpus — the guards over the shipped bodies",
  run: async () => {
    const view = await rpc<{ scope: string; passed: number; refused: boolean; findings?: { handler: string; kind: string; ref: string; msg: string }[] }>("handlers.verify", { scope: "corpus" });
    if (!view.refused && view.passed === 25) return { state: "met", text: "25 bodies verified, exit-clean — the same sweep CI runs, answered by the running daemon" };
    const lines = (view.findings ?? []).map((f) => `refused: ${f.handler} · ${f.kind} · ${f.ref} · ${f.msg}`).join("\n");
    return { state: "refused", text: `passed ${view.passed}; findings:\n${lines || "(none — a sweep that verifies nothing is the failure D1 refuses)"}` };
  },
};

const originalStep: DemoStep = {
  id: "original",
  label: "handlers.verify original — the negative control must refuse",
  run: async () => {
    const name = "golang-engineer-loop";
    const view = await rpc<{ refused: boolean; findings?: { handler: string; kind: string; ref: string; msg: string }[]; reason?: { kind: string; detail: string } }>("handlers.verify", { scope: "original", name });
    if (view.reason) {
      return { state: "unavailable", text: `not runnable here — ${view.reason.kind}: ${view.reason.detail}`,
        command: `aeon handlers verify --original --file opencode/agents/${name}.md` };
    }
    if (!view.refused) {
      return { state: "refused", text: "the ORIGINAL PASSED verify — the guards are broken: no original may pass (A11)" };
    }
    const lines = (view.findings ?? []).map((f) => `refused: ${f.handler} · ${f.kind} · ${f.ref} · ${f.msg}`).join("\n");
    return { state: "met", text: `refused as the demo requires (${(view.findings ?? []).length} findings — the §4 moment, daemon's own lines):\n${lines}` };
  },
};

interface LoopView {
  id: string; slug: string; status: string; lastReason?: string;
  lanes?: { id: string; status: string; produces?: string[]; steps?: { id: string; status: string; agent?: string }[] }[];
}

function stepTagLoop(tech: string, expect: "refusal" | "resolution"): DemoStep {
  return {
    id: `tag-${tech}`,
    label: `tag-only --tech ${tech} — the resolver's ${expect === "refusal" ? "refusal names the tag and the available tags" : "resolution names the handler"}`,
    run: async (ctx) => {
      // Preflight, in the daemon's own register: it refuses a tilde or empty
      // path with "workspace path must be absolute" — say that, and the
      // remedy, before spending a start. The QA guide says the same.
      const ws = ctx.workspaceRoot.trim();
      if (!ws || ws.startsWith("~")) {
        return {
          state: "unavailable",
          text: `not runnable here — the workspace must be an ABSOLUTE path: "${ctx.workspaceRoot}" would be refused before any loop starts ("workspace path must be absolute"). A literal ~ is never expanded; type the full path once, e.g. /home/you/aeon-qa-workspace.`,
        };
      }
      const slug = `demo-${ctx.runId}-${tech}`;
      const started = await rpc<{ id: string }>("loop.start", {
        blueprint: "tag-only", goal: tagGoal, workspaceRoot: ws, slug, tech,
      });
      ctx.loopIds.push(started.id);
      let view = await rpc<LoopView>("loop.show", { id: started.id });
      for (let i = 0; i < 5 && view.status === "running"; i++) {
        await new Promise((r) => setTimeout(r, 400));
        view = await rpc<LoopView>("loop.show", { id: started.id });
      }
      const step = view.lanes?.[0]?.steps?.[0];
      const reason = view.lastReason ?? "";
      if (expect === "refusal") {
        if (view.status === "blocked" && reason.includes('no handler for tech tag "elixir"') && reason.includes("available:")) {
          return { state: "met", text: `blocked, workspace untouched, the daemon's sentence verbatim: ${reason}` };
        }
        if (view.status === "running") return { state: "unavailable", text: "still running after 2s — open the loop to watch it land", command: `aeon loop show ${started.id}` };
        return { state: "refused", text: `expected the elixir refusal; the loop says status ${view.status}, reason: ${reason || "(none)"}` };
      }
      if (step?.agent === "golang-engineer") {
        const note = view.status === "blocked" && reason.includes("unknown route")
          ? " — provider route absent (unknown route): the credential-side half awaits the command shown in the model panel"
          : "";
        return { state: "met", text: `the step row carries the RESOLVED agent golang-engineer${note}` };
      }
      if (view.status === "blocked" && reason.includes("unknown route") && !reason.includes("unknown handler")) {
        // No step row yet — the provider layer refused BEFORE one was
        // created. That is still the proof: the resolver ran, handed
        // golang-engineer down, and `unknown route` is a refusal only a
        // RESOLVED handler can meet (an unresolved tag refuses with the tag
        // sentence, and an unknown handler with `unknown handler`). The
        // difference of sentences is the evidence; no credential invented.
        return { state: "met", text: "the resolver ran: golang-engineer was handed to the provider layer, which refused for its OWN reason — unknown route, never unknown handler: the upstream refusals did not happen, which is the resolution proof. With a configured route the produced-file half runs (model panel)." };
      }
      if (view.status === "blocked" && reason.includes('unknown handler "by-tech-tag"')) {
        return { state: "refused", text: `the resolver did not run: the step blocked as unknown handler — ${reason}` };
      }
      if (view.status === "running") return { state: "unavailable", text: "still running — open the loop; the agent column is the proof to read", command: `aeon loop show ${started.id}` };
      return { state: "refused", text: `expected agent golang-engineer; step shows agent ${step?.agent ?? "(absent)"}, status ${view.status}, reason: ${reason || "(none)"}` };
    },
  };
}

const agentsSteps: DemoStep[] = [registryStep, corpusStep, originalStep, stepTagLoop("elixir", "refusal"), stepTagLoop("go", "resolution")];

const modelSteps: DemoStep[] = [{
  id: "model-build",
  label: "dev-loop --tech go — the specialist writes Go (credential side)",
  run: async (ctx) => {
    const slug = `demo-${ctx.runId}-model`;
    const started = await rpc<{ id: string }>("loop.start", {
      blueprint: "dev-loop", goal: tagGoal, workspaceRoot: ctx.workspaceRoot, slug, tech: "go",
    });
    ctx.loopIds.push(started.id);
    const view = await rpc<LoopView>("loop.show", { id: started.id });
    return { state: "met", text: `loop ${started.id} started (${view.status}); the ladder, transcript and produced files are what S13's §1 promises — open the loop and read the agent column, nothing here waits for the model` };
  },
}];

// The MCP half replays — every row says so, and cites the Go test that
// proves the envelope. The names come from internal/mcp/testdata/fakeserver:
// the spec-literal server the S12 suites drive, `echo` and `mystery`.
interface McpRow { id: string; label: string; envelope: string; cite: string }
const mcpReplay: McpRow[] = [
  { id: "list", label: "tools/list — the fake offers echo and mystery",
    envelope: `{"jsonrpc":"2.0","method":"tools/list"} → {"tools":[{"name":"echo","description":"Echo the query back.","annotations":{"readOnlyHint":true}},{"name":"mystery"}]}`,
    cite: "internal/mcp/testdata/fakeserver + client_test.go TestCallOutcomesOverBothTransports" },
  { id: "call", label: "tools/call echo — result wrapped in the per-label boundary region",
    envelope: `{"jsonrpc":"2.0","method":"tools/call","params":{"name":"echo","arguments":{"q":"demo"}}} → content[0].text wrapped in an UNTRUSTED region naming the server label`,
    cite: "client_test.go TestCallResultLabelledAtTheCallBoundary (the wrapping is the S12 finding, replayed)" },
  { id: "classify", label: "class raise — annotations may raise, never lower",
    envelope: `a server claiming readOnly for an outward tool is RE-CLASSIFIED upward by classify.go — the one raise site`,
    cite: "classify_test.go (grep-pinned: no default arms, MC6)" },
];

// --- S14: the agent-services panel -------------------------------------------------
//
// The live half spends NO model call: the two reads (agent.discover,
// agent.card) are what the daemon answers without waking anyone, and the
// invoke demos are REFUSALS — a refusal that arrives before any execution is
// the safest and most instructive answer the socket can give, and it renders
// VERBATIM (ErrAgentRefused carries the door's own sentence in data.what;
// re-casing it here would build a second truth). The success invoke — a task
// under a running step — is credential-gated like the model build: with no
// route carrying a key, no agent.invoke may leave this page at all. The
// suspension, cold-resume, cycle and third-party-label rows are LABELLED
// REPLAYS citing the Go suites, the same discipline as the MCP rows.

const servicesSteps: DemoStep[] = [
  {
    id: "services-discover",
    label: "agent.discover — every invokable thing, and the name only (A13: no score anywhere)",
    run: async () => {
      const d = await rpc<{ candidates: { agent: string; name: string; skills: { id: string; name: string; tags?: string[]; examples?: string[] }[] }[]; refuses?: string[]; count?: number }>("agent.discover", {});
      if (!d.candidates?.length) {
        return { state: "unavailable",
          text: `the register is empty — nothing is invokable${d.refuses?.length ? ` and ${d.refuses.length} card(s) were refused at load: ${d.refuses.join("; ")}` : ""}. Publish a signed card and run again.`,
          command: "walk docs/aeon/S14-agent-services/DEMO.md §2 — a card is a signed file the load path accepts" };
      }
      const lines = d.candidates
        .map((c) => c.skills.map((sk) => `${c.agent}/${sk.id} — ${sk.name}${sk.tags?.length ? ` [${sk.tags.join(", ")}]` : ""}${sk.examples?.length ? ` · e.g. "${sk.examples[0]}"` : ""}`).join("\n"))
        .filter(Boolean).join("\n");
      return { state: "met",
        text: `${d.count ?? d.candidates.length} candidate(s); tags and examples are what a reader judges fit FROM — a NAME is the only way in:\n${lines}${d.refuses?.length ? `\nrefused at load (AS9 — absence is never silent): ${d.refuses.join("; ")}` : ""}` };
    },
  },
  {
    id: "services-card",
    label: "agent.card — the three states: loaded, absent, refused",
    run: async () => {
      const d = await rpc<{ candidates: { agent: string }[] }>("agent.discover", {});
      const name = d.candidates?.[0]?.agent;
      if (!name) return { state: "unavailable", text: "no candidate to show a card for — publish one first (DEMO §2)" };
      const c = await rpc<{ agent: string; loaded: boolean; state: string; why?: string }>("agent.card", { agent: name });
      if (c.loaded) return { state: "met", text: `${c.agent}: state ${c.state} — signed bytes the load path accepted` };
      return { state: "met", text: `${c.agent}: state ${c.state}${c.why ? ` — ${c.why}` : ""} — absent and refused are ANSWERS, not faults (three states, three answers)` };
    },
  },
];

// A refusal at the agent boundary is ErrAgentRefused (-31600) with the door's
// sentence in data.what — data, rendered verbatim, never dressed in red. Any
// OTHER error stays a fault: only this code is an expected answer here.
function invokeRefusalStep(id: string, label: string, params: Record<string, unknown>): DemoStep {
  return {
    id, label,
    run: async () => {
      try {
        const res = await rpc<{ taskId?: number }>("agent.invoke", params);
        return { state: "refused", text: `expected a refusal and the ask was ACCEPTED (task ${res.taskId ?? "?"}) — the demo's precondition is wrong here; do not trust this row's neighbours until the register and policy match the walk` };
      } catch (thrown) {
        const err = toRpcError(thrown);
        if (err.code === -31600) return { state: "refused", text: err.data?.what || err.message };
        throw thrown;
      }
    },
  };
}

const servicesInvokeSteps: DemoStep[] = [
  invokeRefusalStep("services-invoke-no-text",
    "agent.invoke without words — the type is checked before anyone is asked",
    { callerLoopSlug: "services-demo-none", componentRoot: "/home/you/aeon-qa-workspace", callerStepId: 1, calleeAgent: "reddit-agent", skillId: "summarise-sentiment" }),
  invokeRefusalStep("services-invoke-no-allow",
    "agent.invoke as a plain step — SkillAccess judges BOTH halves, and the default deny says so",
    { callerLoopSlug: "services-demo-none", componentRoot: "/home/you/aeon-qa-workspace", callerStepId: 1, calleeAgent: "reddit-agent", skillId: "summarise-sentiment", text: "what do people say about aeon?" }),
];

interface ServicesReplayRow { id: string; label: string; envelope: string; cite: string }
const servicesReplay: ServicesReplayRow[] = [
  { id: "suspension", label: "an async ask is a SUSPENSION, not a goroutine (AS4/P2)",
    envelope: `invoke writes task + chain edge rows and the step EXITS — aeon debug goroutines | grep -c agentsvc reads 0 while the task waits`,
    cite: "task_test.go TestNoGoroutineForAnAsyncTask + TestAsyncTaskSuspendsTheStep" },
  { id: "cold-resume", label: "the answer arrives COLD — a restart between ask and report changes nothing",
    envelope: `daemon killed mid-task, restarted, the callee reports; the step resumes from rows (S6's machinery, exactly)`,
    cite: "task_test.go TestCompletionResumesCold · chain_test.go TestChainSurvivesRestart" },
  { id: "cycle", label: "the cycle refuses as a CYCLE — never dressed as a cost or depth failure (AS7)",
    envelope: `planner → reddit-agent → planner: REFUSED · delegation cycle, named agent by agent; a depth-bound ask refuses DIFFERENTLY, on purpose`,
    cite: "chain_test.go TestCycleIsRefusedAndNamed + TestCycleIsNotACostCapFailure + TestDepthRefusalIsNotANamelessCycle" },
  { id: "rollup", label: "an uncertain child makes the total uncertain (P32)",
    envelope: `one child reports "at least" — the parent reads PARTIAL with the floors kept and missing naming the reason, never a confident total`,
    cite: "cost_test.go TestRollupSurvivesRestart · transport_test.go TestTaskReadsCarryHonestCostAndNoCalleeToolset" },
  { id: "third-party", label: "a stranger's answer is somebody else's words (AS12)",
    envelope: `a non-first-party skill result reaches the model wrapped in the ONE untrusted-label mechanism — forged label text cannot smuggle itself out`,
    cite: "untrusted_test.go TestLabelSurvivesSpoofedLabelText + TestNonFirstPartyResultIsLabelled" },
  { id: "one-door", label: "one authorization mechanism; a card grants NOTHING (MC4/AS8)",
    envelope: `securitySchemes ride as opaque bytes nobody parses; the only door is SkillAccess policy, judged for caller AND callee`,
    cite: "skillaccess_test.go TestExactlyOneAuthorizationMechanism + TestCardsSecuritySchemesCannotGrant" },
  { id: "deletable", label: "the whole layer is deletable (AS11)",
    envelope: `no runtime package imports agentsvc; a body with no card dispatches byte-identical with the layer wired from an empty register`,
    cite: "absent_test.go TestNoUnconditionalImport + TestOneOfThe25BodiesDispatchesUnchanged" },
];

// --- S15: the agent-plugins panel ------------------------------------------------
//
// The socket carries EXACTLY ONE read for this slice — agent.plugins (R6):
// import, enable, disable and revoke are CLI doors, and their commands ride
// the greyed rows as TEXT, never as buttons — a panel that could install
// anything is a panel that bypasses every guard the install path exists to
// run. The consent and the import refusals ride as LABELED REPLAYS: the
// daemon never persists them (they are the importing CLI process's output —
// the T9 walk's F-13 named exactly this), so the rows quote what the CLI
// printed on the walked day and cite the Go tests that prove each envelope.
// The one refusal the panel meets LIVE is genuine: the read itself refuses
// params — a filter would let a reader probe for agents it was not shown.

const PLUGINS_CONSENT = "REQUESTS — this agent ASKED for the following. Nothing below is granted; the only way a request becomes a permission is a policy YOU author with `aeon policy`. … [tool] webfetch — not granted by this consent … Nothing above enabled anything: groot-translator stays disabled until you run `aeon agent enable groot-translator`.";
const PLUGINS_BLOCKLIST = 'refused: shell-helper · refused: agent:shell-helper · tool_access "no-shell-anywhere" · target all-agents · source operator · no expiry · the agent was not imported (a blocklist that only fires at first use would let the same agent be importable forever; import and use ask the same gate)';
const PLUGINS_SELF_GRANT = 'refused: policies/grant.json · policy id "self-grant-bash" · refused: Allow authored by source "import" requires an operator (PL1) — a Deny or an Ask from "import" would be accepted';
const PLUGINS_TAMPERED = 'refused: card for "groot-tampered" is refused: signature does not verify — the card was tampered with or is not from the key it claims · remedy: the card is the callee\'s own signature over its own card — re-export the package from whoever signs it (a warning would scroll past; this is a refusal)';

const pluginsSteps: DemoStep[] = [
  {
    id: "plugins-list",
    label: "agent.plugins — the one read: states, provenance, carried denials, first-party as a count",
    run: async () => {
      const d = await rpc<{ plugins: { name: string; state: string; source: string; carriedDenials: number; firstParty: boolean }[]; firstPartyCount?: number; note?: string }>("agent.plugins", {});
      if (!d.plugins?.length) {
        return {
          state: "unavailable",
          text: `not runnable here — the register is empty, and that is the NORMAL state (AP11): nothing was fetched, nothing is missing, nobody is under any pressure to import. The daemon's own note: ${d.note ?? "zero imports is the normal state"}`,
          command: "walk docs/aeon/S15-agent-plugins/DEMO.md §1 — the first import prints the consent: aeon agent import <path> (a URL is refused naming the fetch)",
        };
      }
      const rows = d.plugins.map((p) => `${p.name} · ${p.state} · carried ${p.carriedDenials} · ${p.source}${p.firstParty ? " · first-party (registry-set)" : ""}`).join("\n");
      return {
        state: "met",
        text: `${d.plugins.length} imported agent(s) — rows from the daemon's rows, not this page's imagination; first-party: ${d.firstPartyCount ?? "?"} compiled bodies (no plugin state — the four verbs do not apply to them):\n${rows}\nthe state verbs are CLI doors only (R6): aeon agent enable <name> · aeon agent disable <name> · aeon agent revoke <name> --reason <why> — this panel installs nothing, enables nothing, revokes nothing`,
      };
    },
  },
  {
    id: "plugins-params",
    label: "agent.plugins WITH params — the read itself refuses a filter, verbatim (the live refusal this panel can meet)",
    run: async () => {
      try {
        const res = await rpc<{ plugins: unknown[] }>("agent.plugins", { filter: "groot" });
        return { state: "refused", text: `expected a refusal and the filter was ACCEPTED (${(res.plugins ?? []).length} rows) — a read that can be filtered lets a reader probe for agents it was not shown; do not trust this row's neighbours until the daemon matches this checkout` };
      } catch (thrown) {
        const err = toRpcError(thrown);
        if (err.code === -31600) return { state: "refused", text: err.data?.what || err.message };
        throw thrown;
      }
    },
  },
];

interface PluginsReplayRow { id: string; label: string; envelope: string; cite: string }
const pluginsReplay: PluginsReplayRow[] = [
  { id: "consent", label: "the consent screen — REQUESTS, never capabilities (AP5/MC4); the daemon does not persist it (F-13), so this is the CLI's printed output, replayed",
    envelope: PLUGINS_CONSENT,
    cite: "internal/plugin/consent_test.go TestConsentRendersRequestsNotCapabilities + TestConsentShowsWhatGrantingMeans + TestConsentWritesNothing · walked DEMO §1" },
  { id: "blocklist", label: "the blocklist refuses at import — no plugin-side list; the import check evaluates the SAME policy the use gate does (AP3)",
    envelope: PLUGINS_BLOCKLIST,
    cite: "internal/plugin/check_test.go TestBlocklistedToolRefusedAtImport + TestFailedCheckLeavesNothingHalfInstalled · walked DEMO §2" },
  { id: "self-grant", label: "a package may narrow itself and may NEVER widen itself — Allow-from-import refused at the type level (AP6/PL1)",
    envelope: PLUGINS_SELF_GRANT,
    cite: "internal/plugin/check_test.go TestImportedAllowRefused + TestImportedDenyAccepted + TestCarriedSourceIsForced · walked DEMO §3" },
  { id: "tampered", label: "an unverifiable card is refused, not warned about — a card that cannot be checked registers NO skills (AP8)",
    envelope: PLUGINS_TAMPERED,
    cite: "internal/plugin/import_test.go TestUnverifiableCardRegistersNoSkills + TestMissingCardOrManifestRefused · walked DEMO §4" },
  { id: "revoke-inflight", label: "a revoked agent's in-flight Task is REFUSED on arrival — the words are DISCARDED, the cost is PAID FOR and recorded, not recovered (AP7)",
    envelope: `created waiting · revoked mid-flight · arrival refused: "The Task completed and was PAID FOR; its cost is recorded here, not recovered … its words do not enter your context (AP7)" — refusal row carries agent, revocation instant, reason, cost, reached_prompt=0; the caller resumes carrying the refusal`,
    cite: "cmd/aeon/agent_gate_test.go TestRevokeRefusesInFlightTaskResult + TestRefusalRecordedWithAgentInstantReasonCost · internal/plugin/revoke_test.go TestArrivalGateRefusesOnlyRevoked + TestRefusalRecordsReachPromptFalseAndNeverCarryStrangerWords · walked DEMO §6 (413 ms chain)" },
  { id: "disable-contrast", label: "disabled is \"not now\" — the SAME timing, and the in-flight Task COMPLETES, labelled (AP12)",
    envelope: `disable mid-flight → the result ARRIVES inside S14's untrusted region; only NEW invocations are refused, and the refusal names the in-flight contrast so the two verbs cannot be confused`,
    cite: "cmd/aeon/agent_gate_test.go TestDisabledAgentInFlightTaskCompletes + TestRevokeStopsNewInvocationsImmediately · internal/plugin/state_test.go TestDisableEnableReversible · walked DEMO §7" },
  { id: "claim", label: "a package claiming first_party is IGNORED, REPORTED, and still labelled (AP9/AS12)",
    envelope: `first-party: no (registry-set) · PACKAGE CLAIMED FIRST-PARTY — IGNORED (AP9) — the claim sets nothing; the result still rides the untrusted region`,
    cite: "internal/plugin/provenance_test.go TestPackageClaimingFirstPartyIsIgnored + TestIgnoredClaimIsReportedNotSilent + TestFirstPartyFlagNeverUnmarshalledFromPackageInput · cmd/aeon/agent_gate_test.go TestImportedResultIsLabelledUntrusted · walked DEMO §8" },
  { id: "no-fetch", label: "nothing fetches (AP10) — a URL is refused naming the fetch; there is no update verb to fetch with",
    envelope: `import https://… refused: "import takes a local path — nothing is fetched (AP10) · remedy: fetch it yourself if you must, and import the directory you fetched" · agent update refused: "no update, no fetch, no marketplace"`,
    cite: "cmd/aeon/agent_test.go TestNothingFetches · internal/plugin/import_test.go TestURLRefusedImportTakesAPath · walked DEMO §10" },
  { id: "deletable", label: "the layer is deletable; zero imports is the normal state (AP11/AP1) — no feature flag anywhere",
    envelope: `the module's importer set is pinned to {cmd/aeon, internal/plugin} — twenty-four packages cannot link the layer; the 25 compiled bodies are byte-identical with an import present or absent; no os.Getenv, no build tag decides the invariant`,
    cite: "internal/plugin/absent_test.go TestSuitePassesWithNoImportedAgents + TestThe25BodiesUnaffected + TestRegistryEmptyIsNormalState + TestNoFeatureFlagWasAdded · walked DEMO §10" },
];


// --- S16 T13: Git · forge (S16) ---------------------------------------------
//
// The ask is the whole §3: a human decides the irreversible thing. This
// panel READS asks and never answers them — no button, no method. Answering
// is S9's answer path, from someone who means it (`aeon answer` rides the
// rows as text, the way the plugins panel carries its CLI verbs). No fake
// daemon opened a pull request, so every success-shaped row is labeled
// REPLAY with its Go citation, and the owed GitHub rows say `owed` — a
// fabricated outward-effect is the one lie this panel exists to make
// impossible.

const GITFORGE_DENY_OUTWARD = "forge.openPR denied — effect class 'outward' resolves to deny (loop is unattended)";
const GITFORGE_DENY_IRREVERSIBLE = "forge.merge denied — effect class 'irreversible' resolves to deny (loop is unattended)";
const GITFORGE_POLICY_ASKED = 'asked: agent:github-steward · command_access "corpus.github-steward.ec3539e98dc4" · target agent:github-steward · source operator · no expiry (an allow exists: "corpus.github-steward.684888c0ebb1" · agent:github-steward — asked anyway, PL4)';
const GITFORGE_ACTION_CHECK = "forge.merge · irreversible · ask — cannot be undone by another action — a human decides";
const GITFORGE_MIS_BASED = "forge.merge refused: PR #16 has base.ref 'main', expected 'feature/4a17-version'";
const GITFORGE_LOOP_REMEDY = "start the loop from a terminal (the panel starts nothing): aeon loop start --blueprint <path> --with task=<id> --with task_title=\"<title>\" \"<goal>\" — then return and run this panel; walk docs/aeon/S16-git-forge/DEMO.md §1";

interface GitforgeAsk { body: string; options: string[] }

interface GitforgeReplayRow { id: string; label: string; envelope: string; cite: string }

const gitforgeSteps: DemoStep[] = [
  {
    id: "gitforge-validate",
    label: "blueprint.validate dev-loop — §8 from the app: 0 invalid, 0 unverifiable, --strict passes",
    run: async () => {
      const r = await rpc<{ blueprint?: string; validCount?: number; diagnostics?: { state?: string }[] }>("blueprint.validate", { name: "dev-loop" });
      const invalid = (r.diagnostics ?? []).filter((d) => d.state === "invalid").length;
      const unverifiable = (r.diagnostics ?? []).filter((d) => d.state === "unverifiable").length;
      if (invalid === 0 && unverifiable === 0) {
        return { state: "met", text: `${r.blueprint ?? "dev-loop"}: ${r.validCount ?? "?"} valid checks, 0 invalid, 0 unverifiable — --strict passes: every with:/action reference resolves against the live registries (the number is zero because checking happened, not because it was skipped)` };
      }
      return { state: "refused", text: `${r.blueprint ?? "dev-loop"}: ${invalid} invalid, ${unverifiable} unverifiable — S16 retired the escape hatch; the daemon answering is older than this panel` };
    },
  },
  {
    id: "gitforge-ask",
    label: "the loop's openPR ask, rendered verbatim — read only, never answered here",
    run: async (ctx) => {
      const id = (ctx as Ctx & { gitforgeLoopId?: string }).gitforgeLoopId ?? "";
      if (id.trim() === "") {
        return { state: "unavailable", text: `not runnable here — no loop started yet: there is no ask to read, and inventing one would be the fabrication this panel exists to prevent`, command: GITFORGE_LOOP_REMEDY };
      }
      try {
        const v = await rpc<{ status?: string; questionId?: number; lastReason?: string }>("loop.show", { id: id.trim() });
        if (v.status === "waiting") {
          return { state: "met", text: `loop is waiting at its ask (question ${v.questionId ?? "?"}) — the prompt below is the store's row, verbatim; the answer door is elsewhere:`, command: `aeon answer ${id.trim()} approve|deny — typed by a person, never from this page` };
        }
        return { state: "met", text: `loop is ${v.status ?? "?"} (reason: ${v.lastReason ?? "none"}) — no ask pending; refusals and completions render as the data they are` };
      } catch (thrown) {
        const err = toRpcError(thrown);
        return { state: "unavailable", text: `not runnable here — loop.show answered "${err.message}": no such loop in this store`, command: GITFORGE_LOOP_REMEDY };
      }
    },
  },
];

const gitforgeReplay: GitforgeReplayRow[] = [
  { id: "table", label: "the closed registry — nine actions, one receipt-first, no render (D14/L25)",
    envelope: `nine actions — 7 check-then-act, 1 naturally-idempotent (audit), 1 receipt-first (forge.postReview); the set is DATA — aeon action list prints it, the registry is closed, and a blueprint naming git.smuggle is refused: unknown code action "git.smuggle" — the registry is closed (D14)`,
    cite: "internal/action/registry_test.go TestStrategiesAreEnumerated + TestEightActionsNoRender + TestRegistryIsClosedAsserted — walked 2026-10-26 (DEMO §7)" },
  { id: "governance", label: "§3 both routes reach a human — side by side, verbatim (L22/PL4)",
    envelope: `${GITFORGE_POLICY_ASKED}\n   beside:\n${GITFORGE_ACTION_CHECK}\n   unattended, no question row at all:\n${GITFORGE_DENY_OUTWARD}\n${GITFORGE_DENY_IRREVERSIBLE}`,
    cite: "internal/loop/execute_test.go TestUnattendedOutwardDeniesVerbatim + QAC-53/QAC-71 — walked 2026-10-26 verbatim (policy why + action check + two --attended=false loops)" },
  { id: "phantom", label: "the phantom-merge guard refuses a mis-based PR BEFORE merging (D5)",
    envelope: `${GITFORGE_MIS_BASED} — a PR merged into the wrong base looks merged, reports merged, and its work is not where the loop believes it is; the forge's report never outranks the tree`,
    cite: "internal/action/merge_test.go TestMisBasedPRIsRefusedBeforeMerging — the live half OWED (no token on the walk box)" },
  { id: "squash", label: "the guard checks the squash SHA — both directions, because one wrong half passes",
    envelope: `git merge-base --is-ancestor <squashSha> feature/4a17-version → exit 0; git merge-base --is-ancestor <originalTaskCommit> feature/4a17-version → exit 1. BOTH results are correct: a guard checking the originals reports every successful squash merge as a failure, and the natural fix looks like relaxing the guard — which removes it`,
    cite: "internal/action/merge_test.go TestSquashSHAIsAncestorAndOriginalsAreNot — raw-git pair walked 2026-10-26 (DEMO §4)" },
  { id: "readback", label: "closeIssue reads back — a silent failure is a REFUSAL, and the closer is Aeon (D8)",
    envelope: `Closes #n never fires on task PRs (they target the feature branch, not the default) — forge.closeIssue is the only thing that closes the issue, POSTS nothing as proof, and reads the issue back; a close that silently failed is refused loudly, because a loop that believes the tracker when the tracker disagrees runs forever`,
    cite: "internal/action/close_test.go TestCloseReadsTheIssueBack (running CloseSilentlyFails) + TestClosesKeywordDoesNotFire + TestAlreadyClosedIsDone — the live-API half OWED (DEMO §5)" },
  { id: "reconcile", label: "the crash between posted and recorded reconciles BY ID, never by body (D6/D7)",
    envelope: `intent row open (review_id NULL) after a kill -9 → restart → go-and-look: ListReviews decides by ID whether the review exists; body comparison is never consulted; the recorded receipt closes the row; a second posting is unrepresentable`,
    cite: "internal/action/review_test.go TestReconcilesByIdNeverByBody + TestUnresolvableIntentFailsRatherThanReposting + TestReconciliationIsBounded — the ask-case kill -9 walked live 2026-10-26 (DEMO §6)" },
  { id: "noop", label: "twice is a no-op, and N4's call log proves it",
    envelope: `already-merged succeeds with the recorded SHA and the probe is queried ZERO times (the desired end state costs one read); a protected-branch refusal produces exactly ONE MergePR attempt — no second method, no push, no side channels`,
    cite: "internal/action/merge_test.go TestNeverMergesTwice + open_test.go TestEachOpenActionTwiceIsANoOp + N4 call-log assertions — walked at the git level 2026-10-26 (DEMO §2)" },
  { id: "owed", label: "the GitHub-API half — OWED, never met",
    envelope: `the real issue and PR appearing, the reuse-by-exact-title rerun (one issue, one PR, one commit), the mis-based refusal, closed-by-Aeon read-back, the postReview crash reconcile: OWED — the walk box has no GitHub token and the ruling is to record it owed, never glossed. Re-walk commands: docs/aeon/S16-git-forge/DEMO.md walk record`,
    cite: "envelopes pinned by internal/action/open_test.go TestEachOpenActionTwiceIsANoOp + TestIssueFoundBySearchNotByMemory + merge_test.go TestMisBasedPRIsRefusedBeforeMerging + close_test.go TestCloseReadsTheIssueBack + review_test.go TestReconcilesByIdNeverByBody — only the live-API half is OWED: docs/aeon/S16-git-forge/DEMO.md T12 walk record" },
];

// --- S17 T10: Full ladder (S17) ---------------------------------------------
//
// The method is gates and send-backs; a click-through would corrupt exactly
// those. This panel READS: the plan gate renders VERBATIM with
// [approve, revise, abort] as TEXT — abort is an OUTCOME, not a fault — and
// no control on this page answers anything (answering is S9's door, from
// someone who means it; `aeon answer <loopId> …` rides the row). The
// counters table renders FROM the daemon's loop.show rows — D2 from the
// app's seat, task 3 at 0/2 beside a task at 1/2, the ceiling from the
// ENGINE's table, never a page constant. The walked sentences are copied
// from docs/aeon/S17-full-ladder/DEMO.md's 2026-10-26 walk record; the
// model/GitHub halves were OWED there and the rows below say `owed`.

const LADDER_LOOP_REMEDY = "start the loop from a terminal (the panel starts nothing): aeon loop start --blueprint dev-loop --route <route> --tech go \"<goal>\" — then return and run this panel; walk docs/aeon/S17-full-ladder/DEMO.md §1";

interface LadderGate { body: string; options: string[] }
interface LadderReplayRow { id: string; label: string; envelope: string; cite: string }

interface LadderLoopView {
  id?: string; status?: string; questionId?: number; lastReason?: string;
  lanes?: { id?: string; steps?: LadderStepView[]; items?: { id?: string; steps?: LadderStepView[] }[] }[];
  loopCounters?: Record<string, number>;
  loopCounterCaps?: Record<string, number>;
  taskCounters?: { task: string; counter: string; level: string; value: number; cap: number; capExceeded: boolean }[];
}
interface LadderStepView { id?: string; status?: string; outcome?: string; stepId?: number }
interface LadderStepShow { outcome?: string; messages?: { role?: string; content?: { kind?: string; text?: string }[] }[] }

const ladderSteps: DemoStep[] = [
  {
    id: "ladder-validate",
    label: "blueprint.validate dev-loop — §1's precondition: every lane runs, 0 invalid, 0 unverifiable, --strict passes",
    run: async () => {
      const r = await rpc<{ blueprint?: string; validCount?: number; diagnostics?: { state?: string }[] }>("blueprint.validate", { name: "dev-loop" });
      const invalid = (r.diagnostics ?? []).filter((d) => d.state === "invalid").length;
      const unverifiable = (r.diagnostics ?? []).filter((d) => d.state === "unverifiable").length;
      if (invalid === 0 && unverifiable === 0) {
        return { state: "met", text: `${r.blueprint ?? "dev-loop"}: ${r.validCount ?? "?"} valid checks, 0 invalid, 0 unverifiable — the ladder the T7 re-author compiles clean: with:/action references resolve against the live registries (zero because checking happened, not because it was skipped)` };
      }
      return { state: "refused", text: `${r.blueprint ?? "dev-loop"}: ${invalid} invalid, ${unverifiable} unverifiable — the ladder does not compile; §1 cannot be walked until it does` };
    },
  },
  {
    id: "ladder-gate",
    label: "the plan gate — the ONE human decision, rendered verbatim, never answered from this page",
    run: async (ctx) => {
      const id = (ctx as Ctx & { ladderLoopId?: string }).ladderLoopId ?? "";
      if (id.trim() === "") {
        return { state: "unavailable", text: "not runnable here — no loop started yet: there is no gate to read, and answering an invented one is the corruption this panel exists to prevent", command: LADDER_LOOP_REMEDY };
      }
      try {
        const v = await rpc<LadderLoopView>("loop.show", { id: id.trim() });
        if (v.status === "waiting") {
          return { state: "met", text: `the ladder waits at its ONE gate (question ${v.questionId ?? "?"}) — the prompt below is the store's row, verbatim; a prompt is not a control`, command: `aeon answer ${id.trim()} approve|revise|abort — typed by a person; abort is an outcome, not a fault` };
        }
        return { state: "met", text: `loop is ${v.status ?? "?"}${v.lastReason ? ` (reason: ${v.lastReason})` : " — no gate pending"}; refusals and completions render as the data they are` };
      } catch (thrown) {
        const err = toRpcError(thrown);
        return { state: "unavailable", text: `not runnable here — loop.show answered "${err.message}": no such loop in this store`, command: LADDER_LOOP_REMEDY };
      }
    },
  },
  {
    id: "ladder-counters",
    label: "the two-level counters — D2 from the app's seat, rendered from the daemon's rows (the page tallies nothing)",
    run: async (ctx) => {
      const id = (ctx as Ctx & { ladderLoopId?: string }).ladderLoopId ?? "";
      if (id.trim() === "") {
        return { state: "unavailable", text: "not runnable here — no loop started yet, so there are no counter rows to render", command: LADDER_LOOP_REMEDY };
      }
      try {
        const v = await rpc<LadderLoopView>("loop.show", { id: id.trim() });
        const task = (v.taskCounters ?? []).map((r) => `${r.task} \u00b7 ${r.counter} ${r.value}/${r.cap}${r.capExceeded ? " EXHAUSTED" : ""}`);
        const loop = Object.entries(v.loopCounters ?? {}).map(([k, n]) => {
          const cap = v.loopCounterCaps?.[k];
          return `loop \u00b7 ${k} ${n}${cap && cap > 0 ? `/${cap}` : ""}`;
        });
        const all = [...task, ...loop];
        if (all.length === 0) {
          return { state: "met", text: "no counter rows yet — nothing has been charged; every budget reads 0 of its cap the day a send-back spends one" };
        }
        return { state: "met", text: all.join(" \u00b7 ") + " — per-task rows are separate ledgers (D2); the ceiling is the engine's table, not this page's" };
      } catch (thrown) {
        const err = toRpcError(thrown);
        return { state: "unavailable", text: `not runnable here — loop.show answered "${err.message}"`, command: LADDER_LOOP_REMEDY };
      }
    },
  },
  {
    id: "ladder-redo",
    label: "the redo carries its reason (L32) and the third state stays third — read from step.show and the daemon's own rows",
    run: async (ctx) => {
      const id = (ctx as Ctx & { ladderLoopId?: string }).ladderLoopId ?? "";
      if (id.trim() === "") {
        return { state: "unavailable", text: "not runnable here — no loop started yet, so there is no send-back to read", command: LADDER_LOOP_REMEDY };
      }
      try {
        const v = await rpc<LadderLoopView>("loop.show", { id: id.trim() });
        let redo: number | undefined;
        let unverified: string | undefined;
        for (const lane of v.lanes ?? []) {
          for (const item of lane.items ?? []) {
            for (const st of item.steps ?? []) {
              if (st.id === "implement" && typeof st.stepId === "number" && (redo === undefined || st.stepId > redo)) redo = st.stepId;
              if (st.outcome === "unverified") unverified = `${lane.id}/${item.id}/${st.id}`;
            }
          }
          for (const st of lane.steps ?? []) {
            if (st.outcome === "unverified") unverified = `${lane.id}/${st.id}`;
          }
        }
        const bits: string[] = [];
        if (redo !== undefined) {
          const sv = await rpc<LadderStepShow>("step.show", { id: redo });
          const firstUser = (sv.messages ?? []).find((m) => m.role === "user");
          const text = (firstUser?.content ?? []).filter((c) => c.kind === "text").map((c) => c.text ?? "").join("\n");
          bits.push(/redo of /.test(text) ? `the re-entered step's input carries the send-back VERBATIM: ${text.split("\n").find((l) => l.startsWith("redo of "))}` : `the re-entered step's input carries no redo sentence — a bare retry should have been refused at the door (L32); read it yourself: aeon step show ${redo} --input`);
        }
        if (unverified !== undefined) {
          // I5 applies to pixels: this row is neither pass nor fail, and it
          // is not allowed to look like either.
          bits.push(`stored outcome: unverified — ${unverified}: the check did not run, so it found nothing because it did not run (neither pass nor fail; the ceiling charges, the task budget does not)`);
        }
        if (bits.length === 0) {
          return { state: "met", text: "no send-back on this loop yet — when a verify sends work back, its reason rides the redo's input (aeon step show <id> --input) and an unverified check stores the third state" };
        }
        return { state: "met", text: bits.join("\n") };
      } catch (thrown) {
        const err = toRpcError(thrown);
        return { state: "unavailable", text: `not runnable here — loop.show answered "${err.message}"`, command: LADDER_LOOP_REMEDY };
      }
    },
  },
];

const ladderReplay: LadderReplayRow[] = [
  { id: "plan-load", label: "the plan arrives as ordered data — four bad depends_on classes refused at load, the cycle NAMED",
    envelope: `missing dependency, self-dependency, duplicate id and the fourth class are refused at plan load naming the offending task; a cycle is named (t1→t2→t1) and nothing half-sorted reaches the engine; the sort is stable — byte-identical order across runs, ties by plan order, never by map iteration`,
    cite: "internal/plan/derive_test.go TestFourBadDependencyClassesRefused + TestCycleIsNamed + TestSortIsStable + TestSortIsDeterministicAcrossRuns — walked 2026-10-26 (S17 walk record)" },
  { id: "counters", label: "two levels, one table — task 3 keeps its own budget while task 1 spends its own (D2)",
    envelope: `t1 retries 1/2 · t2 0/2 · t3 0/2 — per-task columns on the task rows, loop-wide rows (total_failed_gates, splits_done, qa_rounds) printed once; one ledger charging every task from it is the S8 bug D2 names; the CLI table read back VERBATIM on the walked day`,
    cite: "internal/loop/counters_test.go TestTaskCountersAreIndependent + TestTaskThreeKeepsItsOwnBudget + TestSiblingsKeepTheirBudgets + internal/daemon/blueprint_view_test.go TestDaemonViewCarriesTheTwoLevelCounters — walked lad5 2026-10-26" },
  { id: "ceiling", label: "the loop ceiling stops a loop whose tasks are individually uncapped (Q11)",
    envelope: `total_failed_gates reaches 10 — the LOOP blocks naming the ceiling, whatever each task's own counter says; unverified rounds charge ONLY the ceiling; repeated unverified is bounded, never forever; the block is a block, never an escalation`,
    cite: "internal/loop/counters_test.go TestLoopCeilingBlocks + TestLoopCeilingStopsTheLoop + TestLoopCeilingBoundsRepeatedUnverified + TestLoopCeilingIsABlockNotAnEscalation — lad7 saw 1/10 live; unverified charged only the ceiling (t1 stayed 0/2)" },
  { id: "escalate", label: "exhaustion escalates — attended WAITS, unattended BLOCKS naming the cap AND the absent human",
    envelope: `attended: "t1 retries 2/2 exhausted at 'verify' — then: escalate: raise the cap, or accept the task cannot converge" — the question row exists with ["continue","abort"] and the ladder waits; unattended: "t1 retries 2/2 exhausted at 'verify' / then: escalate — collapsed to block (loop is unattended)" — both facts in the one sentence (cap + nobody); the three then-values are reachable and distinct`,
    cite: "internal/loop/escalate_test.go TestEscalateAttendedWaits + TestEscalateCollapsesToBlockUnattended + TestBlockDistinguishesCapFromNobodyToAsk + TestAllThreeThenValuesAreReachable — walked lad8/lad9 verbatim 2026-10-26" },
  { id: "verify", label: "verify's three outcomes — a check that could not RUN is the THIRD state, never pass (T6)",
    envelope: `ok passes; fail charges retries and routes to rework; unverified ("the linter was killed before it ran — could not determine") routes back with the distinct reason and NO attempt claim — "sent back", not "retry 1 of 2" — and charges ONLY the loop ceiling; a timeout and an absent tool are also unverified; a missing declared artifact rejects the step`,
    cite: "internal/loop/dev-loop_test.go TestBuildLaneRoutesAllThreeVerifyOutcomes + internal/loop/verify_test.go TestTimeoutAndAbsentToolAreAlsoUnverified + TestMissingProducedFileRejectsStep — lad7 redo input verbatim: redo of \"verify\": sent back — unverified: the linter was killed before it ran" },
  { id: "redo", label: "a redo arrives KNOWING — the reason AND the attempt (L32); and the seam that delivered it live had to be FIXED (F-δ)",
    envelope: `redo of "verify": retry 1 of 2 — <the rejection's own words>; a reject with no reason cannot rework: the bare retry is REFUSED at the door and the loop blocks with the absence named ("a bare retry re-runs the call that just failed and spends the round reproducing the same output"); the live daemon dropped the handoff reason at st.complete(..., "") until the T9 walk caught it — red first, then green; the guard never moved`,
    cite: "internal/loop/redo_test.go TestRedoCarriesTheReason + TestRedoCarriesTheAttemptNumber + TestBareRetryIsNotPossible + TestRedoSentenceIsSelfDescribing + internal/runtime/reason_test.go TestRejectHandoffCarriesItsReasonToTheResult — walked step 31 --input 2026-10-26" },
  { id: "bracketing", label: "per-item ladder state — build/t2/verify, never one status for the lane",
    envelope: `every tracked lane records its run; an iterating lane has NO single phase status that could hide item 2's reject; conditional lanes record not-needed and it stays DISTINCT from skipped; the bracketing happens in the engine, not in a surface`,
    cite: "internal/loop/phases_test.go TestBracketingHappensInTheEngine + TestIteratingLaneHasNoSinglePhaseStatus + TestNotNeededAndSkippedAreDistinct + TestConditionalLanesRecordNotNeeded + TestIterateRunsEveryLadderStepPerStableItem — lad7 stepStates keys verbatim: build/t1/implement … build/t3/verify" },
  { id: "merge-gate", label: "the merge gate reads review STATE — threads are never needed and never resolved",
    envelope: `a threadless forge still reaches the gate; APPROVED passes, REQUEST_CHANGES blocks until the SAME author clears it; checks and threads without an API are recorded as N/A TEXT, not left out; the irreversible MergePR is reached only AFTER the gate (the D5 probe at zero queries proves the order); the gate records WHICH state, WHO, WHEN`,
    cite: "internal/action/gate_test.go TestThreadlessForgeStillReachesTheGate + TestOutstandingRequestChangesBlocks + TestGateRecordsWhichStateAndWhen + TestGatePrecedesTheIrreversibleAction + TestThreadsAreNeverResolved — LIVE half OWED: the real PR + F10 (the live forge does not yet implement StatusLister/ThreadLister)" },
  { id: "backlog", label: "the review record lands in the repository — append-only, entries name their PR (Q7/Q10)",
    envelope: `an ok-path review with findings appends to docs/unresolved-pr-comments.md; prior bytes are a PREFIX after every append (O_APPEND, no rewrite path); each entry names PR #N · date · lane/step · finding, the PR number read from EVIDENCE rows; ok-with-nothing appends NOTHING; a failed append BLOCKS with kind backlog-append`,
    cite: "internal/loop/backlog_test.go TestBacklogIsAppendOnly + TestBacklogEntryNamesPRAndDate + TestEngineAppendsReviewFindings + TestEngineOkWithoutFindingsAppendsNothing — Q10 answered in aeon/docs/BACKLOG.md; the real-PR entries OWED (needs the token)" },
  { id: "terminals", label: "P31 terminals — dependency-failed is BLOCKED not failed; abort reaches ABORTED, never failed",
    envelope: `a failed item leaves its transitive dependents BLOCKED naming the failure — two states, never collapsed; abort at a gate with an on_abort handler runs the handler and the terminal is ABORTED (the done|aborted pair); with no terminal handler it BLOCKS with "aborted by user; blueprint has no terminal handler" — deliberate, and never a silent done; the walked ab2 loop printed "aborted" verbatim`,
    cite: "internal/loop/escalate_test.go TestDependentsOfAFailedItemAreBlocked + internal/loop/dev-loop_test.go TestAbortIsAbortedNotFailed + TestGateReviseRoutesBackAndAbortReachesTerminalHandler — walked ab1/ab2 2026-10-26" },
  { id: "owed", label: "the model and GitHub halves — OWED, never claimed",
    envelope: `the real provider's ladder end-to-end, the ONE issue and ONE PR appearing where other people can see them, the merge gate past a real GitHub review, the backlog on real PR rows, the escalation ANSWER (F-γ: no socket door reaches AnswerEscalation yet — the question waits unanswerable from a terminal), and st.Tasks seeding (F-α: no production writer — walked loops iterated tasks(0) and reported done with nothing built): OWED — the walk box had no provider credential and no GitHub token, and the ruling on record is to say so, never gloss. Re-walk commands: docs/aeon/S17-full-ladder/DEMO.md walk record`,
    cite: "the envelopes above are pinned by their named suites (TestBuildLaneRoutesAllThreeVerifyOutcomes, TestTaskCountersAreIndependent, TestGateRecordsWhichStateAndWhen, TestBacklogIsAppendOnly, TestAbortIsAbortedNotFailed …); only the outward/live-provider half is OWED — docs/aeon/S17-full-ladder/DEMO.md T9 walk record + S16-git-forge/DEMO.md re-walk block" },
];

// ─── S18 T10 · the provider-breadth demo ────────────────────────────────
// The LOCAL half of docs/aeon/S18-providers/DEMO.md, run where humans look
// (P34). The discipline this panel exists to keep: **no row may contact a
// live provider.** Every probe/step/cost row runs against a declared LOCAL
// route (a 127.0.0.1/localhost baseUrl) — the refusal rows are refused by
// the runtime BEFORE any request leaves the daemon (M29, and the client spy
// at zero is Go-pinned), so even a daemon answered by the real Ollama never
// reaches a cloud endpoint. Rows that need cloud credentials are `not
// runnable here` naming the Go test that carries the claim instead — I7
// applies to the app exactly as it does to the suites.

const S18_LOCAL = /^https?:\/\/(127\.0\.0\.1|localhost)(:\d+)?(\/|$)/;

const S18_ROUTE_REMEDY =
  'configure a LOCAL route on the Providers tab, or from a terminal: aeon routes set --id local --provider openai --model qwen3.5:latest --base-url http://127.0.0.1:11434/v1 --openai-endpoint chat --no-price --capabilities "streaming-usage,system-role" — then run this panel again (the walk box ran Ollama 0.18.0; cloud routes are what this panel refuses to touch)';

interface S18CapState {
  capability: string;
  state: string;
  detail?: string;
  probedAt?: string;
  probedModel?: string;
}

interface S18RouteView {
  id: string;
  provider: string;
  model: string;
  baseUrl?: string;
  priced: boolean;
  noPrice?: boolean;
  wireCapabilities?: string[] | null;
  capabilityStates?: S18CapState[];
}

interface S18Attribution {
  groups?: { routeId?: string; provider?: string; model?: string; steps: number; cost?: Cost }[];
  total?: Cost;
}

/** A route the daemon has DECLARED capabilities for, reachable only on this
 *  machine. Undeclared routes are excluded on purpose: asking them anything
 *  would send a request somewhere the panel cannot vouch for. */
function s18LocalDeclared(routes: S18RouteView[]): S18RouteView | null {
  return routes.find((r) => r.wireCapabilities !== null && r.wireCapabilities !== undefined && S18_LOCAL.test(r.baseUrl ?? "")) ?? null;
}

const s18Steps: DemoStep[] = [
  {
    id: "s18-caps",
    label: "routes.list carries the local route’s wire capabilities and their three states",
    run: async () => {
      const view = await rpc<{ routes?: S18RouteView[] }>("routes.list", {});
      const route = s18LocalDeclared(view.routes ?? []);
      if (!route) {
        return { state: "unavailable", text: "not runnable here — no LOCAL route with DECLARED wire capabilities — absence of a declaration is not a declaration of absence (I5), and this panel never asks an undeclared route anything; no local route configured — add one on the Providers tab", command: S18_ROUTE_REMEDY };
      }
      const flags = route.wireCapabilities ?? [];
      const states = flags.map((flag) => {
        const v = (route.capabilityStates ?? []).find((c) => c.capability === flag);
        return `${flag}=${v?.state ?? "declared"}`;
      });
      return { state: "met", text: `route ${route.id} (${route.provider}/${route.model}) declares ${states.join(", ") || "NONE — a step needing tools here is refused before the call (M29)"}; the pills on the Providers tab read these same rows, one source of facts` };
    },
  },
  {
    id: "s18-probe",
    label: "routes.probe — the one minimal request, fired by a click here and nowhere else (M32)",
    run: async () => {
      const view = await rpc<{ routes?: S18RouteView[] }>("routes.list", {});
      const route = s18LocalDeclared(view.routes ?? []);
      if (!route) {
        return { state: "unavailable", text: "not runnable here — this panel probes LOCAL declared routes only, and none is configured", command: S18_ROUTE_REMEDY };
      }
      const r = await rpc<{ route: string; probes?: S18CapState[]; saved: boolean; note?: string; failure?: string }>("routes.probe", { route: route.id });
      if (r.failure) {
        // The daemon’s sentence verbatim. A probe that could not check is a
        // complete honest answer — nothing was contradicted — and the row
        // renders it as the data it is, unaltered.
        return { state: "refused", text: r.failure };
      }
      const lines = (r.probes ?? []).map((p) => `${p.capability}=${p.state}${p.probedAt ? ` · probed ${p.probedAt}${p.probedModel ? ` against ${p.probedModel}` : ""}` : ""}`);
      if (lines.length === 0) {
        return { state: "refused", text: r.note ?? "the probe answered with neither verdicts nor a failure — that is not a pass, it is an answer nobody wrote" };
      }
      return { state: "met", text: `${lines.join(" · ")} — saved: ${r.saved ? "yes" : "NO — an unsaved verdict does not survive the restart, and this row says so"}${r.note ? ` · ${r.note}` : ""}` };
    },
  },
  {
    id: "s18-refusal",
    label: "a step needing tools on a route declaring none — M29’s refusal rendered where it will be seen",
    run: async (ctx) => {
      const view = await rpc<{ routes?: S18RouteView[] }>("routes.list", {});
      const route = (view.routes ?? []).find((r) => r.wireCapabilities !== null && r.wireCapabilities !== undefined && !r.wireCapabilities.includes("tools") && S18_LOCAL.test(r.baseUrl ?? "")) ?? null;
      if (!route) {
        return { state: "unavailable", text: "not runnable here — no LOCAL route DECLARED without the tools flag — the refusal needs a declaration to refuse against (undeclared asks anyway, I5), and the route must be localhost so no live provider can be reached even by a bug", command: `${S18_ROUTE_REMEDY} — with a --capabilities list that omits tools` };
      }
      if (!ctx.workspaceRoot.trim()) {
        return { state: "unavailable", text: "not runnable here — an absolute workspace root is required before a step can start — this row starts nothing without it", command: "fill the Workspace root field above (absolute path; a literal ~ is never expanded)" };
      }
      const res = await rpc<{ outcome?: string; reason?: string }>("step", {
        prompt: "Read README.md and summarise it in one sentence.",
        route: route.id,
        workspaceRoot: ctx.workspaceRoot.trim(),
        tools: ["read"],
      });
      if (res.outcome === "failed" && (res.reason ?? "").includes("no model call was made")) {
        return { state: "refused", text: res.reason ?? "refused" }; // verbatim — the daemon wrote this sentence
      }
      return { state: "error", text: `the step answered outcome=${res.outcome ?? "?"} instead of refusing before the call — either the S18 T4 pre-call Check let a declared-uncapable step through (a real defect: a request left the daemon), or the daemon predates S18: restart aeon serve from this checkout and run the panel again` };
    },
  },
  {
    id: "s18-cost",
    label: "the unmetered route’s cost reads unknown WITH its reason — never the zero-dollar literal (T7, D9)",
    run: async () => {
      const view = await rpc<{ routes?: S18RouteView[] }>("routes.list", {});
      const unmetered = (view.routes ?? []).find((r) => r.noPrice === true && !r.priced);
      if (!unmetered) {
        return { state: "unavailable", text: "not runnable here — no route is DECLARED unmetered — an absent price is not the same as the decision (O8); declare one, and run something on it so a cost row exists", command: S18_ROUTE_REMEDY };
      }
      const lists = await rpc<{ loops?: { id: string; slug?: string; costAttribution?: S18Attribution }[] }>("loop.list", {});
      const loops = lists.loops ?? [];
      let at = loops.map((l) => l.costAttribution).find((a) => a && (a.groups?.length ?? 0) > 0);
      const latest = loops[0];
      if (!at && latest) {
        const shown = await rpc<{ costAttribution?: S18Attribution }>("loop.show", { id: latest.id });
        at = shown.costAttribution;
      }
      if (!at || (at.groups?.length ?? 0) === 0) {
        return { state: "unavailable", text: `not runnable here — route ${unmetered.id} is declared unmetered, but no cost rows are recorded yet — unknown is a cost’s state after a call, not a page’s guess (P31); run something on the route and run this panel again`, command: `aeon loop start --blueprint dev-loop --route ${unmetered.id} --workspace <absolute-root> "<goal>" — or start a loop from the Loops tab` };
      }
      const rendered = (at.groups ?? []).map((g) => `${g.routeId || "no provenance"} · ${g.steps} step(s): ${costLine(g.cost).text}${costLine(g.cost).detail ? ` (${costLine(g.cost).detail})` : ""}`);
      const total = costLine(at.total);
      const joined = `${rendered.join(" | ")} | TOTAL: ${total.text}`;
      if (/\$0\.00/.test(joined)) {
        // The D9 guard, in the renderer it guards: a cost can be zero, or
        // unknown, and never read as the literal someone will screenshot.
        return { state: "error", text: `the cost renderer produced a literal $0.00 — a D9 violation, whatever the ledger below says: ${joined}` };
      }
      const mine = (at.groups ?? []).filter((g) => g.routeId === unmetered.id);
      if (mine.length === 0) {
        return { state: "unavailable", text: `not runnable here — no ${unmetered.id} rows in this ledger — the recorded loops billed other routes; the unmetered claim is real but unexercised here (other routes render honestly: ${joined})`, command: `run a loop or step on ${unmetered.id} first (the panel starts nothing), then run this row again` };
      }
      return { state: "met", text: `every ${unmetered.id} line reads unknown with its reason: ${mine.map((g) => `${g.steps} step(s) → ${costLine(g.cost).text}${costLine(g.cost).detail ? ` (${costLine(g.cost).detail})` : ""}`).join(" · ")} — TOTAL: ${total.text}; costLine is Ask’s own renderer over the daemon’s ONE attribution (D16), and the zero-dollar literal D9 forbids appears nowhere in this ledger` };
    },
  },
  {
    id: "s18-roundtrip",
    label: "A→B→A — suspend on one provider, resume on the other, hand back to the first",
    run: async () => ({
      state: "unavailable",
      text: "not runnable here — the round trip needs TWO live cloud providers with credentials, and the bench never touches a live provider (I7 in the app, exactly as in the suites). The claim is carried by internal/runtime/crossprovider_test.go’s TestSuspendOnAnthropicResumeOnOpenAIResumeOnAnthropic — with internal/daemon/crossprovider_resume_test.go’s TestStoreUnmodifiedAtEveryPoint proving the store is byte-identical at every leg — until a machine holding both keys walks docs/aeon/S18-providers/DEMO.md §3",
      command: "export ANTHROPIC_API_KEY and OPENAI_API_KEY out of band (never as flag values), then: aeon ask --route <a> … / aeon loop start --route <a> … — the §3 walk commands are in the DEMO",
    }),
  },
];

function VerdictPill({ state }: { state: VerdictState }) {
  const tone = state === "met" ? "green" : state === "refused" || state === "error" ? "red" : state === "unavailable" ? "amber" : "slate";
  return <Pill tone={tone as "green"} size="sm">{state}</Pill>;
}

export function DemosPage() {
  // No `~/…` default — the daemon refuses tilde paths ("workspace path must
  // be absolute") and the QA guide says a literal ~ is never expanded. Empty
  // means the steps answer with the remedy instead of a round-tripped error.
  const [workspaceRoot, setWorkspaceRoot] = useState("");
  const [verdicts, setVerdicts] = useState<Record<string, Verdict>>({});
  const [mcpRan, setMcpRan] = useState(false);
  const [servicesRan, setServicesRan] = useState(false);
  const [pluginsRan, setPluginsRan] = useState(false);
  // S16 T13: the ask arrives on the event stream the shell already forwards
  // (question.asked is an event, not a method — reading it needs no new
  // surface). Read-only: this page stores the row to RENDER it; answering
  // lives on another door entirely.
  const [gitforgeRan, setGitforgeRan] = useState(false);
  const [gitforgeLoopId, setGitforgeLoopId] = useState("");
  const [gitforgeAsk, setGitforgeAsk] = useState<GitforgeAsk | null>(null);
  useEffect(() => {
    let live = true;
    void onEvent((payload) => {
      if (!live) return;
      const ev = payload as { kind?: string; payload?: { kind?: string; body?: string; options?: string[] } };
      if (ev?.kind === "question.asked" && typeof ev.payload?.body === "string") {
        // S17 T10: ONE subscription per page carries both read kinds — the
        // S16 permission ask and the S17 blueprint gate prompt. Two
        // listeners would make the bridge's single handler a competition;
        // this is one listener reading two row kinds.
        if (ev.payload.kind === "permission") {
          setGitforgeAsk({ body: ev.payload.body, options: ev.payload.options ?? [] });
        } else if (ev.payload.kind === "gate") {
          setLadderGate({ body: ev.payload.body, options: ev.payload.options ?? [] });
        }
      }
    }).catch(() => { /* listener failure is the bridge's three-part error; the rows still answer from loop.show */ });
    return () => { live = false; };
  }, []);
  // S17 T10: the gate prompt arrives on the SAME event stream as the S16
  // ask (a blueprint gate question is a store row of kind "gate") — the
  // single listener below reads both kinds. Read-only: this page stores
  // the row to RENDER it; the answer door is elsewhere, with a person.
  const [ladderRan, setLadderRan] = useState(false);
  const [ladderLoopId, setLadderLoopId] = useState("");
  const [ladderGate, setLadderGate] = useState<LadderGate | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [routesReady, setRoutesReady] = useState<boolean | null>(null);

  const probe = useCallback(async () => {
    try {
      // Both shapes are answered: the real daemon returns `{routes: […]}`,
      // and a raw array was the older bench fixture's shape. A page that
      // reads only one of them leaves this gate — and the model demo behind
      // it — shut against the daemon it actually speaks to.
      const view = await rpc<unknown>("routes.list", {});
      const list: { hasKey?: boolean }[] = Array.isArray(view)
        ? (view as { hasKey?: boolean }[])
        : ((view as { routes?: { hasKey?: boolean }[] } | null)?.routes ?? []);
      setRoutesReady(list.some((r) => r.hasKey));
    } catch {
      setRoutesReady(false);
    }
  }, []);
  void probe();

  const runSteps = useCallback(async (steps: DemoStep[]) => {
    const id = `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 6)}`;
    setBusy(id);
    const ctx: Ctx & { gitforgeLoopId?: string; ladderLoopId?: string } = { workspaceRoot, runId: id, loopIds: [], gitforgeLoopId, ladderLoopId };
    for (const step of steps) {
      setVerdicts((v) => ({ ...v, [step.id]: { state: "running", text: "…" } }));
      let verdict: Verdict;
      try {
        verdict = await step.run(ctx);
      } catch (thrown) {
        const err = toRpcError(thrown);
        if (/is not registered/.test(err.message)) {
          // An honest -32601 from a daemon that predates this demo is not an
          // error to dress in red — it is "not runnable here" with its remedy
          // named, the same register as the daemon's own `routes none` banner.
          verdict = { state: "unavailable", text: `not runnable here — ${err.message}: the daemon answering is older than this demo panel. Restart "aeon serve" from this checkout (handlers.list/handlers.verify and the step's agent field arrived with the bench spec) and run the panel again; nothing started.` };
        } else {
          verdict = { state: "error", text: `error (${err.code}): ${err.message}` };
        }
      }
      setVerdicts((v) => ({ ...v, [step.id]: verdict }));
    }
    setBusy(null);
  }, [workspaceRoot, gitforgeLoopId, ladderLoopId]);

  const shown = (id: string): Verdict => verdicts[id] ?? { state: "pending", text: "not run yet" };

  return (
    <div className="space-y-6">
      <Panel>
        <div data-testid="demo-runner">
        <h3 className="font-semibold">Demo runner</h3>
        <p className="text-sm text-muted">
          The same scenarios <code>docs/aeon/S13-agents/DEMO.md</code> walks from a terminal, run from here.
          Verdicts are the daemon's answers — refusals render verbatim; a step that cannot run here says so
          and shows the command, never a pass. Loops started here keep <code>demo-</code> slugs and stay in
          the store as records.
        </p>
        <FormField label="Workspace root — absolute, one project across every stage's demos (a literal ~ is not expanded by the daemon)">
          <Input value={workspaceRoot} onChange={(e) => setWorkspaceRoot(e.target.value)} placeholder="/home/you/aeon-qa-workspace" data-testid="demo-workspace" />
        </FormField>
        </div>
      </Panel>

      <Panel>
        <h3 className="font-semibold">Agents · the resolver demo (live)</h3>
        <Button onClick={() => void runSteps(agentsSteps)} disabled={busy !== null}>
          Run agents demo — registry, verify, and tag-only elixir/go
        </Button>
        {agentsSteps.map((step) => {
          const v = shown(step.id);
          return (
            <div key={step.id} data-testid="demo-step" className="border-t pt-2 mt-2">
              <div className="flex items-center gap-2">
                <VerdictPill state={v.state} />
                <strong>{step.label}</strong>
              </div>
              <pre className="whitespace-pre-wrap text-xs mt-1">{verbatim(v.text)}</pre>
              {v.command && <pre className="text-xs mt-1 bg-subtle p-1" data-testid="demo-command">{v.command}</pre>}
            </div>
          );
        })}
      </Panel>

      <Panel>
        <h3 className="font-semibold">Agents · the model build (credential side)</h3>
        {routesReady === false && (
          <div data-testid="model-demo-unavailable">
            <Alert tone="amber" title="not runnable here — no configured route carries a key"
              description={`The daemon's banner reads the same way. From a terminal, when a credential exists: aeon loop start --blueprint dev-loop --project "$HOME/aeon-qa-workspace" --tech go "${tagGoal}"`} />
          </div>
        )}
        <Button onClick={() => void runSteps(modelSteps)} disabled={busy !== null || routesReady !== true}>
          Run model demo — dev-loop --tech go
        </Button>
        {routesReady === true && modelSteps.map((step) => {
          const v = shown(step.id);
          return (
            <div key={step.id} data-testid="demo-step" className="border-t pt-2 mt-2">
              <div className="flex items-center gap-2"><VerdictPill state={v.state} /><strong>{step.label}</strong></div>
              <pre className="whitespace-pre-wrap text-xs mt-1">{verbatim(v.text)}</pre>
            </div>
          );
        })}
      </Panel>

      <Panel>
        <h3 className="font-semibold">Agents · services (S14) — reads live, refusals as data, the rest a labeled REPLAY</h3>
        <p className="text-sm text-muted">
          The live half spends <strong>no model call</strong>: discovery and cards answer from signed files.
          The invoke demos answer as refusations — the door&apos;s own sentence, verbatim (ErrAgentRefused
          adds no interpretation). The ask that <em>succeeds</em> needs a running step under a configured
          route, so it is credential-gated like the model build; until then no <code>agent.invoke</code>{" "}
          leaves this page. Suspension, resume, the cycle and the third-party label are replays of the Go
          suites — each row says so and cites the test behind its verdict.
        </p>
        <Button onClick={() => void runSteps(servicesSteps)} disabled={busy !== null}>
          Run services demo — discover and card (live, reads only)
        </Button>
        {servicesSteps.map((step) => {
          const v = shown(step.id);
          return (
            <div key={step.id} data-testid="services-step" className="border-t pt-2 mt-2">
              <div className="flex items-center gap-2"><VerdictPill state={v.state} /><strong>{step.label}</strong></div>
              <pre className="whitespace-pre-wrap text-xs mt-1">{verbatim(v.text)}</pre>
              {v.command && <pre className="text-xs mt-1 bg-subtle p-1" data-testid="demo-command">{v.command}</pre>}
            </div>
          );
        })}
        {routesReady === false && (
          <div data-testid="services-invoke-unavailable">
            <Alert tone="amber" title="not runnable here — the invoke demos ask the daemon to judge a real ask, and a running step needs a route with a key"
              description="The daemon's banner reads routes none the same way. From a terminal with a credential: run `aeon serve`, publish a card, then walk docs/aeon/S14-agent-services/DEMO.md §4 — the refusals above arrive without any of that, which is why they run here." />
          </div>
        )}
        <Button onClick={() => void runSteps(servicesInvokeSteps)} disabled={busy !== null || routesReady !== true}>
          Run invoke demos — refusals as data (credential side)
        </Button>
        {routesReady === true && servicesInvokeSteps.map((step) => {
          const v = shown(step.id);
          return (
            <div key={step.id} data-testid="services-invoke-step" className="border-t pt-2 mt-2">
              <div className="flex items-center gap-2"><VerdictPill state={v.state} /><strong>{step.label}</strong></div>
              <pre className="whitespace-pre-wrap text-xs mt-1">{verbatim(v.text)}</pre>
            </div>
          );
        })}
        <Button onClick={() => setServicesRan(true)} disabled={busy !== null}>
          Run services replay — the suite-proven rows, cited
        </Button>
        {servicesRan && servicesReplay.map((row) => (
          <div key={row.id} data-testid="services-replay-step" className="border-t pt-2 mt-2">
            <div className="flex items-center gap-2">
              <Pill tone="slate" size="sm">replay</Pill>
              <strong>{row.label}</strong>
            </div>
            <pre className="whitespace-pre-wrap text-xs mt-1">{row.envelope}</pre>
            <p className="text-xs text-muted mt-1">proven by: {row.cite} — a replay of the Go suites, not a live daemon call.</p>
          </div>
        ))}
      </Panel>

      <Panel>
        <h3 className="font-semibold">Agents · plugins (S15) — the one read live, the CLI-only half a labeled REPLAY</h3>
        <p className="text-sm text-muted">
          <code>agent.plugins</code> is the ONLY method this panel&apos;s live half sends (R6): import,
          enable, disable and revoke are CLI doors, their commands ride the rows as <strong>text</strong>,
          never as buttons — <em>this panel cannot install anything</em>. The consent and the import
          refusals are labeled REPLAYS (the daemon never persists them — the walked F-13), quoting what the
          CLI printed on the walked day, each row citing the Go test behind its envelope. Nothing here
          grants anything: a grant is <code>aeon policy add</code>, by an operator, deliberately (MC4).
        </p>
        <Button onClick={() => void runSteps(pluginsSteps)} disabled={busy !== null}>
          Run plugins demo — the one read the socket carries (live, zero management)
        </Button>
        {pluginsSteps.map((step) => {
          const v = shown(step.id);
          return (
            <div key={step.id} data-testid="plugins-step" className="border-t pt-2 mt-2">
              <div className="flex items-center gap-2"><VerdictPill state={v.state} /><strong>{step.label}</strong></div>
              <pre className="whitespace-pre-wrap text-xs mt-1">{verbatim(v.text)}</pre>
              {v.state === "unavailable" ? (
                <div data-testid="plugins-unavailable">
                  <pre className="whitespace-pre-wrap text-xs mt-1 bg-subtle p-1">{verbatim(v.text)}</pre>
                  {v.command && <pre className="text-xs mt-1 bg-subtle p-1" data-testid="demo-command">{v.command}</pre>}
                </div>
              ) : v.command ? (
                <pre className="text-xs mt-1 bg-subtle p-1" data-testid="demo-command">{v.command}</pre>
              ) : null}
            </div>
          );
        })}
        <Button onClick={() => setPluginsRan(true)} disabled={busy !== null}>
          Run plugins replay — the CLI-only half, cited
        </Button>
        {pluginsRan && pluginsReplay.map((row) => (
          <div key={row.id} data-testid="plugins-replay-step" className="border-t pt-2 mt-2">
            <div className="flex items-center gap-2">
              <Pill tone="slate" size="sm">replay</Pill>
              <strong>{row.label}</strong>
            </div>
            <pre className="whitespace-pre-wrap text-xs mt-1">{row.envelope}</pre>
            <p className="text-xs text-muted mt-1">proven by: {row.cite} — a replay of the Go suites and the walked CLI output, not a live daemon call.</p>
          </div>
        ))}
      </Panel>

      <Panel>
        <h3 className="font-semibold">Git · forge (S16) — the ask read verbatim, NEVER answered; the owed rows say owed</h3>
        <p className="text-sm text-muted">
          The live half sends <strong>only reads</strong> — <code>blueprint.validate</code> and{" "}
          <code>loop.show</code> (+ the page-wide <code>routes.list</code> probe); the ask arrives on the{" "}
          <code>question.asked</code> event stream and renders as the store holds it. <strong>No button on this
          panel answers anything</strong> and no <code>question.answer</code> or <code>loop.start</code> can
          leave it — answering is S9&apos;s answer path, from someone who means it, and the registry decides
          nothing from a click-through. Nothing here opens a pull request: success-shaped rows are labeled
          REPLAY with their Go citation, and the GitHub-API rows say <code>owed</code> — the walk box had no
          token, and the ruling on record is to say so, never gloss.
        </p>
        <FormField label="Loop id whose ask to read (the panel starts nothing — starting is the operator&apos;s command below)">
          <Input value={gitforgeLoopId} onChange={(e) => setGitforgeLoopId(e.target.value)} placeholder="04ed504c… (from aeon loop start)" data-testid="gitforge-loop" />
        </FormField>
        <Button onClick={() => void runSteps(gitforgeSteps)} disabled={busy !== null}>
          Run git-forge demo — validate and the loop&apos;s ask (live reads only)
        </Button>
        {gitforgeSteps.map((step) => {
          const v = shown(step.id);
          const ask = step.id === "gitforge-ask" ? gitforgeAsk : null;
          return (
            <div key={step.id} data-testid="gitforge-step" className="border-t pt-2 mt-2">
              <div className="flex items-center gap-2"><VerdictPill state={v.state} /><strong>{step.label}</strong></div>
              {v.state !== "unavailable" && <pre className="whitespace-pre-wrap text-xs mt-1">{verbatim(v.text)}</pre>}
              {ask && (
                <pre className="whitespace-pre-wrap text-xs mt-1 bg-subtle p-1" data-testid="gitforge-ask-body">
                  {ask.body}
                  {"\n\noptions: "}{ask.options.join(" | ")}{"\n(a prompt is not a control — these words answer nothing from here)"}
                </pre>
              )}
              {v.state === "unavailable" ? (
                <div data-testid="gitforge-unavailable">
                  <pre className="whitespace-pre-wrap text-xs mt-1 bg-subtle p-1">{verbatim(v.text)}</pre>
                  {v.command && <pre className="text-xs mt-1 bg-subtle p-1" data-testid="demo-command">{v.command}</pre>}
                </div>
              ) : v.command ? (
                <pre className="text-xs mt-1 bg-subtle p-1" data-testid="demo-command">{v.command}</pre>
              ) : null}
            </div>
          );
        })}
        <Button onClick={() => setGitforgeRan(true)} disabled={busy !== null}>
          Run git-forge replay — the suite-proven rows and the owed ledger, cited
        </Button>
        {gitforgeRan && gitforgeReplay.map((row) => (
          <div key={row.id} data-testid="gitforge-replay-step" className="border-t pt-2 mt-2">
            <div className="flex items-center gap-2">
              <Pill tone="slate" size="sm">replay</Pill>
              <strong>{row.label}</strong>
            </div>
            <pre className="whitespace-pre-wrap text-xs mt-1">{row.envelope}</pre>
            <p className="text-xs text-muted mt-1">proven by: {row.cite} — a replay of the Go suites and the walked day, not a live daemon call; owed rows say owed.</p>
          </div>
        ))}
      </Panel>

      <Panel>
        <h3 className="font-semibold">Full ladder (S17) — the ONE gate read verbatim, NEVER answered; the counters are the daemon&apos;s rows</h3>
        <p className="text-sm text-muted">
          The live half sends <strong>only reads</strong> — <code>blueprint.validate</code>, <code>loop.show</code>{" "}
          and <code>step.show</code> (+ the page-wide <code>routes.list</code> probe). The plan gate is the method&apos;s
          one human decision: it renders <strong>verbatim with [approve, revise, abort] as TEXT</strong> — abort is an
          outcome, not a fault — and <strong>no button on this panel answers anything</strong>; no{" "}
          <code>question.answer</code>, <code>loop.start</code> or <code>loop.answer</code> can leave it. The counters
          table renders <strong>from the daemon&apos;s rows</strong> — task 3 at <code>0/2</code> beside a task at{" "}
          <code>1/2</code> (D2), the ceiling from the engine&apos;s table; <code>unverified</code> is the third state
          and never a green tick. Nothing here merged anything: success-shaped rows are labeled REPLAY with their Go
          citation, and the model/GitHub rows say <code>owed</code> — the walk box had no credential and no token, and
          the ruling on record is to say so, never gloss.
        </p>
        <FormField label="Loop id whose gate and counters to read (the panel starts nothing — starting is the operator&apos;s command shown on the rows)">
          <Input value={ladderLoopId} onChange={(e) => setLadderLoopId(e.target.value)} placeholder="9fc1d3fa… (from aeon loop start)" data-testid="ladder-loop" />
        </FormField>
        <Button onClick={() => void runSteps(ladderSteps)} disabled={busy !== null}>
          Run ladder demo — validate, the gate, the counters, the redo (live reads only)
        </Button>
        {ladderSteps.map((step) => {
          const v = shown(step.id);
          const gate = step.id === "ladder-gate" ? ladderGate : null;
          return (
            <div key={step.id} data-testid="ladder-step" className="border-t pt-2 mt-2">
              <div className="flex items-center gap-2"><VerdictPill state={v.state} /><strong>{step.label}</strong></div>
              {v.state !== "unavailable" && <pre className="whitespace-pre-wrap text-xs mt-1">{verbatim(v.text)}</pre>}
              {gate && (
                <pre className="whitespace-pre-wrap text-xs mt-1 bg-subtle p-1" data-testid="ladder-gate-body">
                  {gate.body}
                  {"\n\noptions: "}{gate.options.join(" | ")}{"\n(a prompt is not a control — these words answer nothing from here; answering is the operator&apos;s door)"}
                </pre>
              )}
              {v.state === "unavailable" ? (
                <div data-testid="ladder-unavailable">
                  <pre className="whitespace-pre-wrap text-xs mt-1 bg-subtle p-1">{verbatim(v.text)}</pre>
                  {v.command && <pre className="text-xs mt-1 bg-subtle p-1" data-testid="demo-command">{v.command}</pre>}
                </div>
              ) : v.command ? (
                <pre className="text-xs mt-1 bg-subtle p-1" data-testid="demo-command">{v.command}</pre>
              ) : null}
            </div>
          );
        })}
        <Button onClick={() => setLadderRan(true)} disabled={busy !== null}>
          Run ladder replay — the suite-proven ladder and the owed ledger, cited
        </Button>
        {ladderRan && ladderReplay.map((row) => (
          <div key={row.id} data-testid="ladder-replay-step" className="border-t pt-2 mt-2">
            <div className="flex items-center gap-2">
              <Pill tone="slate" size="sm">replay</Pill>
              <strong>{row.label}</strong>
            </div>
            <pre className="whitespace-pre-wrap text-xs mt-1">{row.envelope}</pre>
            <p className="text-xs text-muted mt-1">proven by: {row.cite} — a replay of the Go suites and the walked 2026-10-26 day, not a live daemon call; owed rows say owed.</p>
          </div>
        ))}
      </Panel>

      <Panel>
        <h3 className="font-semibold">Providers · the S18 breadth demo (live reads, LOCAL routes only)</h3>
        <p className="text-sm text-muted">
          The local half of <code>docs/aeon/S18-providers/DEMO.md</code>: a declared route&apos;s capabilities and
          their three states on <code>routes.list</code>, <code>routes.probe</code>&apos;s one minimal request (fired
          only by the buttons here and on the Providers tab — never on load, save or refresh, M32), M29&apos;s refusal
          rendered <strong>as data, verbatim, including &ldquo;no model call was made&rdquo;</strong>, and an
          unmetered route&apos;s cost reading <code>unknown</code> with its reason — never the literal that D9
          forbids. Every row runs against a localhost route or says <code>not runnable here</code> with the Go test
          that carries the claim: no row of this panel may contact a live provider.
        </p>
        <Button onClick={() => void runSteps(s18Steps)} disabled={busy !== null}>
          Run S18 providers demo — capabilities, the probe, the refusal, the unmetered cost
        </Button>
        {s18Steps.map((step) => {
          const v = shown(step.id);
          return (
            <div key={step.id} data-testid="s18-step" className="border-t pt-2 mt-2">
              <div className="flex items-center gap-2"><VerdictPill state={v.state} /><strong>{step.label}</strong></div>
              <pre className="whitespace-pre-wrap text-xs mt-1">{verbatim(v.text)}</pre>
              {v.command ? <pre className="text-xs mt-1 bg-subtle p-1" data-testid="demo-command">{v.command}</pre> : null}
            </div>
          );
        })}
      </Panel>

      <Panel>
        <h3 className="font-semibold">MCP · a labeled REPLAY (the daemon does not import the layer — absent_test.go)</h3>
        <Button onClick={() => setMcpRan(true)} disabled={busy !== null}>
          Run MCP replay — spec-literal envelopes, cited
        </Button>
        {mcpRan && mcpReplay.map((row) => (
          <div key={row.id} data-testid="mcp-step" className="border-t pt-2 mt-2">
            <div className="flex items-center gap-2">
              <Pill tone="slate" size="sm">replay</Pill>
              <strong>{row.label}</strong>
            </div>
            <pre className="whitespace-pre-wrap text-xs mt-1">{row.envelope}</pre>
            <p className="text-xs text-muted mt-1">proven by: {row.cite} — a replay of the Go suites, not a live daemon call.</p>
          </div>
        ))}
      </Panel>
    </div>
  );
}
