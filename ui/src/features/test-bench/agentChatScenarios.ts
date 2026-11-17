import type { ChatAgent, ChatEventLike, ChatItem, TranscriptLikeMessage } from "../../components/agent-chat";
import type { ScenarioLane } from "./syntheticLoop";

/**
 * Scripted daemon events for the Agent chat bench tab. Each scenario is a list
 * of REAL event envelopes (the shapes in `internal/protocol/conformance`), so
 * replaying one exercises `chatItemsFromEvents` exactly as a live loop would —
 * the bench tests the adapter, not a second rendering path.
 */
export interface Scenario {
  id: string;
  title: string;
  description: string;
  events: ChatEventLike[];
  agents?: Record<string, ChatAgent>;
  /** Extra detail the stream does not carry (a gate's diff), keyed by gate item id. */
  gateExtras?: Record<string, Partial<Extract<ChatItem, { kind: "gate" }>>>;
  /** Steer text the host would know from `step.steers`. */
  steerTexts?: Record<number, string>;
  /** Static items appended after the replay (things no event produces yet). */
  extraItems?: ChatItem[];
  /** A saved transcript rendered through `chatItemsFromTranscript` instead of events. */
  transcript?: { agent: string; messages: TranscriptLikeMessage[] };
  readOnly?: boolean;
  /** Opens at the start and plays itself, pausing on its waits. */
  interactive?: boolean;
  /** The loop's lanes, when the scenario is a whole loop — enables the bench's full-page layout. */
  lanes?: ScenarioLane[];
}

const TWO_STEP_LANES: ScenarioLane[] = [
  { id: "survey", kind: "model-step", produces: ["SURVEY.md"] },
  { id: "approve", kind: "gate" },
  { id: "summarise", kind: "model-step", produces: ["SUMMARY.md"] },
];

/**
 * A pause in an interactive scenario: playback stops on this marker until
 * the person has answered (in the chat itself), then continues — down the
 * `yes` or `no` branch when the scenario forks on the answer. The marker is
 * a bench-only event kind, so the adapter ignores it like any unknown kind.
 */
export const WAIT_KIND = "bench.wait";
export interface WaitSpec {
  /** Shown above the chat while playback waits. */
  label: string;
  /** Wait until every one of these questions is answered. */
  questions?: number[];
  /** Wait until this loop gate (its question id) is answered. */
  gate?: number;
  /** Events to play next, chosen by whether the (first) answer reads as yes. */
  branches?: { yes?: ChatEventLike[]; no?: ChatEventLike[] };
}

class Script {
  events: ChatEventLike[] = [];
  private seq = 1000;
  private t: number;
  constructor(start = "2026-10-24T10:42:00Z") { this.t = Date.parse(start); }
  push(kind: string, payload: Record<string, unknown>, dtMs = 600): this {
    this.t += dtMs;
    this.events.push({ seq: ++this.seq, kind, at: new Date(this.t).toISOString(), payload });
    return this;
  }
  wait(spec: WaitSpec): this {
    return this.push(WAIT_KIND, spec as unknown as Record<string, unknown>, 0);
  }
  /** A model reply arriving in chunks, as `model.delta` does. */
  say(stepId: number, agent: string, text: string, thinking = false): this {
    const words = text.split(/(?<= )/);
    for (let i = 0; i < words.length; i += 4) {
      this.push("model.delta", { stepId, agent, text: words.slice(i, i + 4).join(""), ...(thinking ? { tool: "thinking" } : {}) }, 90);
    }
    return this;
  }
  tool(stepId: number, agent: string, id: string, tool: string, summary: string, end: Record<string, unknown> = {}, ms = 900): this {
    // Bash carries its whole command on start and its output on end, as the
    // daemon sends them; the summary stays the short form.
    const command = tool === "bash" ? (end.command as string | undefined) ?? summary : undefined;
    this.push("step.tool.start", { stepId, agent, tool, summary, toolUseId: id, ...(command ? { command } : {}) }, 200);
    if (end.policy) this.push("step.policy.decided", { stepId, agent, tool, summary, toolUseId: id, ...(end.policy as object) }, 20);
    if (end.open) return this;
    const { policy: _p, open: _o, command: _c, ...rest } = end;
    // A parked call printed nothing — its "suspended…" result is not output.
    const printed = (end.output as string | undefined) ?? (String(end.result ?? "").startsWith("suspended") ? undefined : end.result as string | undefined);
    const output = tool === "bash" && printed ? { output: printed } : {};
    return this.push("step.tool.end", { stepId, agent, tool, summary, toolUseId: id, result: `${tool} ${summary}`, ...output, ...rest }, ms);
  }
}

/** A forked continuation: its own script, so its pacing is its own. */
function branch(build: (s: Script) => void): ChatEventLike[] {
  const s = new Script();
  build(s);
  return s.events;
}

const ALLOW = (who: string, what: string) => ({ verdict: "allowed", reason: `allowed: agent:${who} · "${what}" · target all-agents · source operator · no expiry` });

function twoAgents(): Scenario {
  const s = new Script();
  s.push("loop.started", { loopId: "loop-dm9x", blueprint: "two-step" }, 0)
    .push("loop.step.dispatched", { loopId: "loop-dm9x", lane: "survey", step: "survey", stepId: 11, agent: "surveyor", route: "anthropic/sonnet", reasoningEffort: "medium" })
    .push("loop.step.dispatched", { loopId: "loop-dm9x", lane: "survey", step: "survey", stepId: 12, agent: "linter", route: "anthropic/sonnet", reasoningEffort: "low" }, 40)
    .tool(12, "linter", "l1", "bash", "Find all unformatted files and the git status baseline", {
      command: "cd /home/cjlapao/Downloads/new_agents/aeon && gofmt -l ./internal ./cmd && git status --porcelain | head -20",
      result: "2 files unformatted · 14 untracked",
      output: "internal/protocol/wire_test.go\ninternal/daemon/survey.go\n M aeon/internal/daemon/step.go\n M aeon/internal/protocol/step_stop.go\n?? aeon/internal/protocol/wire.go\n?? aeon/internal/daemon/survey.go\n?? aeon/internal/daemon/survey_test.go\n?? docs/aeon/S31-loop-workspace/" })
    .say(12, "linter", "The working tree contains a large amount of uncommitted work from previous sessions: step_stop.go, survey.go and two test files. None of it is mine to judge.", true)
    .say(12, "linter", "The unformatted file is untracked work from a prior session. I'll `gofmt` it (mechanical, safe) and re-run the baseline.")
    .tool(12, "linter", "l2", "bash", "Format the unformatted test file", { result: "gofmt -w internal/protocol/wire_test.go" })
    .say(12, "linter", "The previous code-map run is still going. Restart `make ci` in the background and check the test harness.", true)
    .tool(12, "linter", "l3", "bash", "Re-run baseline make ci", { result: "started in background" }, 2400)
    .say(11, "surveyor", "While CI runs, let me check how `TASKS.md/PLAN.md` treat S31 and whether the task-split convention is expected.")
    .tool(11, "surveyor", "s1", "bash", "Find S31 mentions in TASKS/PLAN/ROADMAP", {
      command: "cd /home/cjlapao/Downloads/new_agents/docs/aeon && grep -n 'S31' TASKS.md PLAN.md ROADMAP.md | head -20",
      result: "3 matches in ROADMAP.md, 0 in TASKS.md",
      output: "ROADMAP.md:104:| **S31** | **Loop workspace** — the backend and shell for the chat-first loop screen |\nROADMAP.md:446:| **S9** app / S31 | Events carry `stepId`, `loopId`, `lane`, `agent` |\nROADMAP.md:447:| **S13** agents | I1 *\"nothing spawns\"* and N7 stand unchanged |" })
    .say(11, "surveyor", "The S31 row doesn't exist yet in TASKS.md — since this slice is new, it hasn't been added to the 232-task matrix.", true)
    .tool(11, "surveyor", "s2", "read", "cmd/hello/main.go", { result: "read cmd/hello/main.go · 42 lines · 840 B" })
    .say(11, "surveyor", "Cargo is available. I have enough context now to settle scope with you before writing the survey. A few questions:");
  const qs: [number, string, string[], string][] = [
    [301, "Which Go runtime should the survey target?", ["Go 1.23 standard library", "Go 1.26"], "Go 1.23 standard library"],
    [302, "How strict should the linter be?", ["golangci-lint default suite", "strict"], "golangci-lint default suite"],
    [303, "Should the slice's task split be included?", ["Include in TASKS.md", "Skip"], "Include in TASKS.md"],
    [304, "What should the review gate check?", ["Gate on write_file SURVEY.md", "No gate"], "Gate on write_file SURVEY.md"],
  ];
  for (const [id, body, options] of qs) s.push("question.asked", { id, stepId: 11, agent: "surveyor", body, kind: "clarify", options }, 120);
  for (const [id, , , answer] of qs) s.push("question.answered", { stepId: 11, questionId: id, answer }, 1500);
  s.say(11, "surveyor", "The user confirmed: Go 1.23 target, default golangci suite, task-split convention accepted.", true)
    .tool(12, "linter", "l4", "bash", "Find failing test in cmd/aeon", {
      command: "cd /home/cjlapao/Downloads/new_agents/aeon && go test ./cmd/aeon/ -run TestEveryStepEventKind -count=1",
      result: "--- FAIL: TestEveryStepEventKindRendersSomething", isError: true,
      output: "--- FAIL: TestEveryStepEventKindRendersSomething (0.00s)\n    step_run_test.go:335: step.policy.decided renders nothing\nFAIL\nFAIL\tgithub.com/cjlapao/aeon/cmd/aeon\t0.412s\nFAIL" })
    .say(12, "linter", "Baseline has one pre-existing failure in `cmd/aeon`. Let me see what it is — the team must know the honest starting point.")
    .push("step.steer.queued", { stepId: 11, agent: "surveyor", steerId: 7, hasText: true })
    .tool(11, "surveyor", "s3", "write_file", "SURVEY.md", { policy: ALLOW("surveyor", "survey-writes"), path: "SURVEY.md", linesAdded: 18, result: "wrote SURVEY.md (612 bytes)" })
    .push("step.steer.delivered", { stepId: 11, agent: "surveyor", steerId: 7, iteration: 6 })
    .say(11, "surveyor", "Added the Go version (1.23) to the survey header as you asked.")
    .tool(11, "surveyor", "s4", "write_file", "SURVEY.md", { policy: ALLOW("surveyor", "survey-writes"), path: "SURVEY.md", linesAdded: 1, linesRemoved: 1, result: "wrote SURVEY.md (631 bytes)" })
    .push("step.done", { stepId: 11, agent: "surveyor", outcome: "ok" })
    .push("step.done", { stepId: 12, agent: "linter", outcome: "ok" }, 300)
    .push("loop.advanced", { loopId: "loop-dm9x", lane: "survey", outcome: "ok" })
    .push("loop.waiting", { loopId: "loop-dm9x", questionId: 90, prompt: "Approve SURVEY.md (+18 lines)", answers: ["approve", "reject"], lane: "approve", step: "Step 3 Gate" });
  return {
    id: "two-agents", title: "Loop · two agents in parallel", lanes: TWO_STEP_LANES,
    description: "The screenshot run: an orchestrator dispatch to two agents, interleaved thinking/tools, a 4-question card, a steer queued then delivered, writes with policy verdicts, and an approval gate with a diff.",
    events: s.events, steerTexts: { 7: "Also mention the Go version." },
    agents: { surveyor: { id: "surveyor", role: "lead-explorer" }, linter: { id: "linter", role: "ast-vet" } },
    gateExtras: { "gate:90": {
      description: "The surveyor generated workspace baseline documentation. Human signoff is required before invoking agent summarise.",
      preview: "+ # Aeon QA Workspace Survey\n+ Module: example.com/hello · Go 1.23\n+ Status: 1 failing test identified in cmd/aeon (unrelated baseline)\n+\n+ ## Packages\n+ - cmd/hello — prints \"Hello, Aeon\"\n+ - internal/greet — the greeting\n+\n+ ## Findings\n+ 1. No tests for internal/greet",
      previewLines: 18,
    } },
  };
}

function toolStates(): Scenario {
  const s = new Script();
  s.push("step.started", { stepId: 21, agent: "builder" }, 0)
    .tool(21, "builder", "a1", "read", "README.md", { policy: ALLOW("builder", "reads"), result: "read README.md · 2.1 KiB" })
    .tool(21, "builder", "a2", "bash", "go test ./...", { policy: ALLOW("builder", "tests"), result: "exit 1 · 3 failures", isError: true })
    .tool(21, "builder", "a3", "bash", "rm -rf /tmp/cache", { policy: { verdict: "denied", reason: "refused: agent:builder · \"no-rm\" · target all-agents · source operator · no expiry" },
      result: "refused: agent:builder · \"no-rm\" · target all-agents · source operator · no expiry", isError: true })
    .tool(21, "builder", "a4", "bash", "git push origin main", { policy: { verdict: "asked", reason: "ask: agent:builder · \"pushes-ask\" · command git push" },
      result: "suspended: awaiting a permission answer" })
    .push("question.asked", { id: 410, stepId: 21, agent: "builder", toolUseId: "a4", kind: "permission",
      body: "builder wants to run `git push origin main` — rule \"pushes-ask\" asks before any push. Allow it?", options: ["approve", "deny"] })
    .tool(21, "builder", "a5", "unparseable", "", { result: "unparseable arguments", isError: true })
    .push("step.tool.start", { stepId: 21, agent: "builder", tool: "grep", summary: "TODO in internal/", toolUseId: "a6" }, 300)
    .push("step.tool.start", { stepId: 21, agent: "builder", tool: "bash", summary: "make build", toolUseId: "a7" }, 100)
    .push("step.tool.skipped", { stepId: 21, agent: "builder", tool: "bash", toolUseId: "a7", reason: "steered" }, 50)
    .tool(21, "builder", "a8", "list", "internal/", { result: "list internal · 18 entries" })
    .tool(21, "builder", "a9", "write_file", "internal/greet/greet_test.go", { path: "internal/greet/greet_test.go", linesAdded: 42, result: "wrote internal/greet/greet_test.go (1.1 KiB)" })
    .tool(21, "builder", "a10", "write_file", "internal/greet/greet.go", { path: "internal/greet/greet.go", linesAdded: 3, linesRemoved: 7, result: "wrote internal/greet/greet.go (402 B)" })
    .push("step.tool.start", { stepId: 21, agent: "builder", tool: "bash", summary: "make test-race (long)", toolUseId: "a11" }, 400);
  return { id: "tool-states", title: "Tool call states",
    description: "Every status a call can have: done, failed, denied by policy, awaiting a permission answer (with its permission card), unparseable, still running, skipped by a steer, file writes with line counts.",
    events: s.events, agents: { builder: { id: "builder", role: "golang-engineer" } } };
}

function steering(): Scenario {
  const s = new Script();
  s.push("step.started", { stepId: 31, agent: "writer" }, 0)
    .say(31, "writer", "Drafting the release notes from the merged PRs.")
    .push("step.steer.queued", { stepId: 31, agent: "writer", steerId: 1, hasText: true })
    .push("step.tool.start", { stepId: 31, agent: "writer", tool: "bash", summary: "gh pr list --state merged", toolUseId: "w1" })
    .push("step.tool.start", { stepId: 31, agent: "writer", tool: "read", summary: "CHANGELOG.md", toolUseId: "w2" }, 50)
    .push("step.tool.skipped", { stepId: 31, agent: "writer", tool: "bash", toolUseId: "w1", reason: "steered" }, 50)
    .push("step.tool.skipped", { stepId: 31, agent: "writer", tool: "read", toolUseId: "w2", reason: "steered" }, 10)
    .push("step.steer.delivered", { stepId: 31, agent: "writer", steerId: 1, iteration: 2, skippedTools: ["w1", "w2"] })
    .say(31, "writer", "Understood — only the user-facing changes, grouped by area. Re-reading the list with that filter.")
    .push("step.steer.queued", { stepId: 31, agent: "writer", steerId: 2, route: "anthropic/opus", reasoningEffort: "high" })
    .push("step.steer.delivered", { stepId: 31, agent: "writer", steerId: 2, iteration: 3 })
    .push("step.route.changed", { stepId: 31, agent: "writer", route: "anthropic/opus", reasoningEffort: "high", fromIteration: 3 })
    .push("step.steer.queued", { stepId: 31, agent: "writer", steerId: 3, hasText: true })
    .push("step.steer.cancelled", { stepId: 31, agent: "writer", steerId: 3 })
    .push("step.stopped", { stepId: 31, agent: "writer" })
    .push("loop.waiting", { loopId: "loop-r", reason: "stopped", stepId: 31 })
    .push("step.continued", { stepId: 31, agent: "writer" }, 4000)
    .push("step.steer.queued", { stepId: 31, agent: "writer", steerId: 4, hasText: true });
  return { id: "steering", title: "Steer · stop · continue",
    description: "A text steer that closes two pending tool calls, a provider/effort change, a withdrawn steer, a stop that parks the loop, a continue, and a steer still queued.",
    events: s.events, steerTexts: { 1: "Skip internal refactors — user-facing changes only, grouped by area.", 3: "Actually, also include the refactors.", 4: "Keep it under 200 words." } };
}

function finished(): Scenario {
  const s = new Script();
  s.push("loop.started", { loopId: "loop-f", blueprint: "two-step" }, 0)
    .push("loop.step.dispatched", { loopId: "loop-f", lane: "summarise", step: "summarise", stepId: 41, agent: "summariser" })
    .say(41, "summariser", "Reading the approved survey before writing three findings.", true)
    .tool(41, "summariser", "f1", "read", "SURVEY.md", { result: "read SURVEY.md · 19 lines" })
    .tool(41, "summariser", "f2", "write_file", "SUMMARY.md", { policy: ALLOW("summariser", "summary-writes"), path: "SUMMARY.md", linesAdded: 12, result: "wrote SUMMARY.md (540 B)" })
    .say(41, "summariser", "Wrote **SUMMARY.md** with three findings:\n\n1. `internal/greet` has no tests\n2. `cmd/hello` hard-codes the greeting\n3. The baseline has one unrelated failing test")
    .push("step.done", { stepId: 41, agent: "summariser", outcome: "ok" })
    .push("loop.advanced", { loopId: "loop-f", lane: "summarise", outcome: "ok" })
    .push("loop.finished", { loopId: "loop-f", status: "done" });
  return { id: "finished", title: "Finished loop (history only)", readOnly: true, lanes: TWO_STEP_LANES,
    description: "A completed loop: the composer is gone and nothing can be answered — the design's rule for finished loops. Everything stays browsable.",
    events: s.events, agents: { summariser: { id: "summariser", role: "summarise" } } };
}

function blocked(): Scenario {
  const s = new Script();
  s.push("loop.started", { loopId: "loop-b", blueprint: "build" }, 0)
    .push("loop.step.dispatched", { loopId: "loop-b", lane: "build", step: "build", stepId: 51, agent: "builder" })
    .tool(51, "builder", "b1", "bash", "make ci", { result: "exit 2 · vet failed", isError: true }, 4000)
    .push("step.compacted", { stepId: 51, agent: "builder", start: 2, end: 30, estimateBefore: 180000, estimateAfter: 42000 })
    .push("step.done", { stepId: 51, agent: "builder", outcome: "blocked", reason: "exceeded MaxIterations (40)" })
    .push("loop.blocked", { loopId: "loop-b", reason: { kind: "step-blocked", detail: "exceeded MaxIterations (40)" } });
  return { id: "blocked", title: "Blocked loop", readOnly: true, description: "A failing tool, a compaction, a step that ran out of iterations and the loop blocking on it.", events: s.events };
}

function stress(): Scenario {
  const s = new Script();
  s.push("loop.started", { loopId: "loop-s", blueprint: "stress" }, 0);
  const agents = ["alpha", "beta", "gamma"];
  agents.forEach((a, i) => s.push("loop.step.dispatched", { loopId: "loop-s", lane: "work", step: "work", stepId: 60 + i, agent: a }, 10));
  for (let n = 0; n < 120; n++) {
    const a = agents[n % 3]!;
    const step = 60 + (n % 3);
    if (n % 4 === 0) s.say(step, a, `Step ${n}: looking at file ${n} before deciding what to change.`, true);
    s.tool(step, a, `x${n}`, n % 5 === 0 ? "read" : "bash", n % 5 === 0 ? `internal/pkg${n}/file.go` : `go test ./internal/pkg${n}/...`, { result: "ok" }, 100);
  }
  return { id: "stress", title: "Stress · 3 agents, ~600 events", description: "Three interleaved agents and hundreds of events, to check grouping, scrolling and the stick-to-bottom behaviour.", events: s.events };
}

function transcript(): Scenario {
  return { id: "transcript", title: "Saved transcript (step.show)", readOnly: true,
    description: "A finished step's saved messages rendered through chatItemsFromTranscript — the history path, with no timestamps or policy verdicts (those live only on the stream).",
    events: [], transcript: { agent: "surveyor", messages: [
      { role: "user", content: [{ kind: "text", text: "Inspect go.mod and cmd/hello/main.go, then write SURVEY.md." }] },
      { role: "assistant", content: [
        { kind: "thinking", text: "I should list the workspace before reading anything." },
        { kind: "tool_use", id: "t1", name: "list", input: { path: "." } },
      ] },
      { role: "user", content: [{ kind: "tool_result", toolUseId: "t1", content: "go.mod\ncmd/hello/main.go" }] },
      { role: "assistant", content: [
        { kind: "text", text: "Two files. Reading both." },
        { kind: "tool_use", id: "t2", name: "read", input: { path: "go.mod" } },
        { kind: "tool_use", id: "t3", name: "bash", input: { command: "go vet ./...", description: "Vet the module" } },
      ] },
      { role: "user", content: [
        { kind: "tool_result", toolUseId: "t2", content: "module example.com/hello\n\ngo 1.23" },
        { kind: "tool_result", toolUseId: "t3", content: "not run: the person steered before this tool ran — read their message below and decide whether to call it again", isError: true },
        { kind: "text", text: "Don't run vet, just read." },
      ] },
      { role: "assistant", content: [
        { kind: "tool_use", id: "t4", name: "write_file", input: { path: "SURVEY.md", content: "# Survey" } },
      ] },
      { role: "user", content: [{ kind: "tool_result", toolUseId: "t4", content: "wrote SURVEY.md (612 bytes)" }] },
      { role: "assistant", content: [{ kind: "text", text: "Done — SURVEY.md lists the module, Go 1.23 and what the program prints." }] },
    ] } };
}

function gallery(): Scenario {
  const since = new Date(Date.now() - 12 * 60_000 - 12_000).toISOString();
  return { id: "gallery", title: "Gallery · items no event produces yet",
    description: "Static items: a long-running activity marker, a free-form question, an answered gate, every divider tone — for host features that will feed the chat later.",
    events: [], extraItems: [
      { id: "g1", kind: "divider", tone: "orchestrator", text: "Orchestrator → step review · dispatched reviewer + security", highlights: ["reviewer", "security"] },
      { id: "g2", kind: "activity", agent: "reviewer", label: "Deep diving", since },
      { id: "g3", kind: "question", agent: "reviewer", questions: [{ id: "free-1", prompt: "Anything specific you want the review to focus on?", kind: "free" }] },
      { id: "g4", kind: "divider", tone: "policy", text: "policy allowed write_file" },
      { id: "g5", kind: "gate", title: "Merge the review fixes?", stepLabel: "merge", options: ["approve", "reject"], state: "answered", answer: "approve" },
      { id: "g6", kind: "divider", tone: "neutral", text: "reviewer compacted its context" },
      { id: "g7", kind: "divider", tone: "success", text: "Loop finished · done" },
      { id: "g8", kind: "divider", tone: "danger", text: "Loop blocked · step-failed · vet failed" },
      // S69 AT11: the three receipt states with no daemon — a previewable
      // png, a non-preview chip (pdf), and a revision the store has lost
      // (rev 900: the scripted read door rejects it — the broken chip
      // must name document and revision, never go blank).
      { id: "g9", kind: "message", author: "human", text: "The screenshot, the report, and one file that is gone.", attachments: [
        { name: "screenshot.png", document: "@attachments/0a1b2c3d-screenshot.png", rev: 1, mediaType: "image/png", size: 18432, sha256: "0a1b2c3d00000000000000000000000000000000000000000000000000000000" },
        { name: "quarterly-report.pdf", document: "@attachments/beef0001-quarterly-report.pdf", rev: 2, mediaType: "application/pdf", size: 1050624, sha256: "beef000100000000000000000000000000000000000000000000000000000000" },
        { name: "gone.png", document: "@attachments/deadbeef-gone.png", rev: 900, mediaType: "image/png", size: 1024, sha256: "deadbeef00000000000000000000000000000000000000000000000000000000" },
      ] },
    ] };
}

const BIG_THOUGHT = `The goal asks for a survey of this workspace and then, after approval, a summary with three findings. Before I touch anything I want a clear picture of what is here, because the survey is the artifact the next lane depends on — if it is wrong, the summary inherits the mistake.

What I know so far: there is a go.mod, which makes this a Go module, and the goal names cmd/hello/main.go explicitly, so there is at least one binary. I do not yet know the Go version, whether there are other packages, or whether the program prints anything other than a greeting. Each of those belongs in the survey, so each needs a read rather than a guess.

Order of work: list the root first, because a listing is cheap and tells me which reads are worth making. Then read go.mod for the module path and the go directive. Then read main.go. I should not read files I have no reason to read — the context window is shared with everything that comes after.

There is one more thing I would like before writing: a clean "go vet" run, so the survey can state the baseline honestly instead of assuming it. Running a command is not something I can do silently here — policy asks before any command runs — so I will ask, and if the person declines I will say in the survey that vet was not run rather than implying the code is clean.

Finally, two details I cannot infer and should not invent: which Go version the survey should target, and whether there is anything the person wants called out. I will ask both at once rather than interrupting twice.`;

function interactive(): Scenario {
  const s = new Script();
  const loop = "loop-demo";
  s.push("loop.started", { loopId: loop, blueprint: "two-step" }, 0)
    .push("loop.step.dispatched", { loopId: loop, lane: "survey", step: "survey", stepId: 71, agent: "surveyor", route: "anthropic/sonnet", reasoningEffort: "medium" })
    .say(71, "surveyor", BIG_THOUGHT, true)
    .tool(71, "surveyor", "i1", "list", ".", { policy: ALLOW("surveyor", "reads"), result: "list . · go.mod, cmd/hello/main.go" })
    .tool(71, "surveyor", "i2", "read", "go.mod", { policy: ALLOW("surveyor", "reads"), result: "read go.mod · module example.com/hello · go 1.23" })
    .say(71, "surveyor", "Before writing anything I want `go vet` to confirm the baseline — running a command needs your approval.")
    .tool(71, "surveyor", "i3", "bash", "go vet ./...", {
      policy: { verdict: "asked", reason: "ask: agent:surveyor · \"commands-ask\" · command go vet ./... · source operator" },
      result: "suspended: awaiting a permission answer" })
    .push("question.asked", { id: 601, stepId: 71, agent: "surveyor", toolUseId: "i3", kind: "permission", options: ["deny", "approve"],
      body: "surveyor wants to run `go vet ./...` in ~/code/aeon-qa-workspace. Rule \"commands-ask\" asks before any command runs. Allow it?" })
    .wait({ label: "Approve or decline the `go vet` call", questions: [601], branches: {
      yes: branch((b) => b
        .push("step.tool.start", { stepId: 71, agent: "surveyor", tool: "bash", summary: "go vet ./...", command: "cd ~/code/aeon-qa-workspace && go vet ./...", toolUseId: "i3" }, 300)
        .push("step.policy.decided", { stepId: 71, agent: "surveyor", tool: "bash", toolUseId: "i3", verdict: "allowed", reason: "allowed: answered by you" }, 20)
        .push("step.tool.end", { stepId: 71, agent: "surveyor", tool: "bash", summary: "go vet ./...", toolUseId: "i3", result: "exit 0 · no issues", output: "# example.com/hello\nok  \texample.com/hello/cmd/hello\t(vet: no issues)\nok  \texample.com/hello/internal/greet\t(vet: no issues)" }, 1400)
        .say(71, "surveyor", "Vet is clean — the survey can say so.")),
      no: branch((b) => b.say(71, "surveyor", "Understood — I won't run it. The survey will say vet was **not** run, rather than imply the code is clean.")),
    } })
    .say(71, "surveyor", "Two quick questions before I write the survey:")
    .push("question.asked", { id: 602, stepId: 71, agent: "surveyor", kind: "clarify", body: "Which Go version should the survey target?", options: ["Go 1.23 (go.mod)", "Go 1.26 (latest)"] }, 200)
    .push("question.asked", { id: 603, stepId: 71, agent: "surveyor", kind: "free", body: "Anything you want the survey to call out?" }, 120)
    .wait({ label: "Answer both of the surveyor's questions", questions: [602, 603] })
    .say(71, "surveyor", "Using your answers. Writing SURVEY.md with the module, the version and the vet baseline.", true)
    .tool(71, "surveyor", "i4", "write_file", "SURVEY.md", { policy: ALLOW("surveyor", "survey-writes"), path: "SURVEY.md", linesAdded: 18, result: "wrote SURVEY.md (612 bytes)" }, 1200)
    .say(71, "surveyor", "SURVEY.md is written. Handing it to you for approval.")
    .push("step.done", { stepId: 71, agent: "surveyor", outcome: "ok" })
    .push("loop.advanced", { loopId: loop, lane: "survey", outcome: "ok" })
    .push("loop.waiting", { loopId: loop, questionId: 90, prompt: "Approve SURVEY.md (+18 lines)", answers: ["approve", "reject"], lane: "approve", step: "Step 2 Gate" })
    .wait({ label: "Approve or reject SURVEY.md at the gate", gate: 90, branches: {
      yes: branch((b) => b
        .push("loop.step.dispatched", { loopId: loop, lane: "summarise", step: "summarise", stepId: 72, agent: "summariser", route: "anthropic/sonnet", reasoningEffort: "low" }, 400)
        .say(72, "summariser", "Reading the approved survey, then three findings.", true)
        .tool(72, "summariser", "j1", "read", "SURVEY.md", { policy: ALLOW("summariser", "reads"), result: "read SURVEY.md · 19 lines" })
        .tool(72, "summariser", "j2", "bash", "git diff --stat", {
          policy: { verdict: "asked", reason: "ask: agent:summariser · \"commands-ask\" · command git diff --stat" },
          result: "suspended: awaiting a permission answer" })
        .push("question.asked", { id: 604, stepId: 72, agent: "summariser", toolUseId: "j2", kind: "permission", options: ["deny", "approve"],
          body: "summariser wants to run `git diff --stat` to confirm only SURVEY.md changed. Allow it? (Try turning on Auto-approve before this one.)" })
        .wait({ label: "Approve or decline `git diff --stat` — or turn on Auto-approve", questions: [604], branches: {
          yes: branch((c) => c
            .push("step.tool.start", { stepId: 72, agent: "summariser", tool: "bash", summary: "git diff --stat", toolUseId: "j2" }, 300)
            .push("step.policy.decided", { stepId: 72, agent: "summariser", tool: "bash", toolUseId: "j2", verdict: "allowed", reason: "allowed: answered" }, 20)
            .push("step.tool.end", { stepId: 72, agent: "summariser", tool: "bash", summary: "git diff --stat", toolUseId: "j2", result: "1 file changed, 18 insertions(+)" }, 900)),
          no: branch((c) => c.say(72, "summariser", "Skipping the diff check.")),
        } })
        .tool(72, "summariser", "j3", "write_file", "SUMMARY.md", { policy: ALLOW("summariser", "summary-writes"), path: "SUMMARY.md", linesAdded: 12, result: "wrote SUMMARY.md (540 B)" }, 1000)
        .say(72, "summariser", "Wrote **SUMMARY.md**:\n\n1. `internal/greet` has no tests\n2. `cmd/hello` hard-codes the greeting\n3. The vet baseline is recorded in the survey")
        .push("step.done", { stepId: 72, agent: "summariser", outcome: "ok" })
        .push("loop.advanced", { loopId: loop, lane: "summarise", outcome: "ok" })
        .push("loop.finished", { loopId: loop, status: "done" })),
      no: branch((b) => b
        .push("loop.step.dispatched", { loopId: loop, lane: "survey", step: "survey (revise)", stepId: 73, agent: "surveyor" }, 400)
        .say(73, "surveyor", "Revising the survey with your feedback.")
        .tool(73, "surveyor", "k1", "write_file", "SURVEY.md", { policy: ALLOW("surveyor", "survey-writes"), path: "SURVEY.md", linesAdded: 4, linesRemoved: 2, result: "wrote SURVEY.md (690 bytes)" })
        .push("step.done", { stepId: 73, agent: "surveyor", outcome: "ok" })
        .push("loop.finished", { loopId: loop, status: "done" })),
    } });
  return {
    id: "interactive", title: "Interactive walkthrough (you drive it)", interactive: true, lanes: TWO_STEP_LANES,
    description: "Plays from the start and stops whenever the loop needs you: a tool approval, a two-question card, the approval gate, then a second approval in the summariser (try Auto-approve there). Your answers pick the path. Expand the long thought and the tool rows to see the animations.",
    events: s.events, agents: { surveyor: { id: "surveyor", role: "lead-explorer" }, summariser: { id: "summariser", role: "summarise" } },
    gateExtras: { "gate:90": {
      description: "The surveyor wrote the workspace baseline. Your signoff starts the summariser.",
      preview: "+ # Aeon QA Workspace Survey\n+ Module: example.com/hello · Go 1.23\n+ Vet baseline: recorded\n+\n+ ## Packages\n+ - cmd/hello — prints \"Hello, Aeon\"\n+ - internal/greet — the greeting\n+\n+ ## Open questions\n+ - none",
      previewLines: 18,
    } },
  };
}

export const SCENARIOS: Scenario[] = [interactive(), twoAgents(), toolStates(), steering(), finished(), blocked(), transcript(), gallery(), stress()];
