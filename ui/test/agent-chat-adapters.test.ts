import { describe, expect, it } from "vitest";
import { chatItemsFromEvents, chatItemsFromLoopTranscriptPage, chatItemsFromTranscript, deriveSession, type ChatEventLike, type ChatItem, type LoopTranscriptMessageLike } from "../src/components/agent-chat";
import { parsePolicySentence } from "../src/components/agent-chat/policyText";
import { approvalLabel } from "../src/components/agent-chat/items";

// The pure halves of the agent chat: daemon events → items, a saved transcript
// → items, and the wiring state (which step each agent runs). Everything the
// renderer shows comes through these, so they carry the weight of the tests.

let seq = 0;
const ev = (kind: string, payload: Record<string, unknown> = {}, at?: string): ChatEventLike =>
  ({ seq: ++seq, kind, at: at ?? new Date(Date.UTC(2026, 9, 24, 10, 0, seq)).toISOString(), payload });
const of = <K extends ChatItem["kind"]>(items: ChatItem[], kind: K) => items.filter((i): i is Extract<ChatItem, { kind: K }> => i.kind === kind);

describe("chatItemsFromEvents", () => {
  it("merges two dispatches to the same blueprint step into one orchestrator divider", () => {
    const items = chatItemsFromEvents([
      ev("loop.step.dispatched", { loopId: "l", step: "survey", stepId: 1, agent: "surveyor" }),
      ev("loop.step.dispatched", { loopId: "l", step: "survey", stepId: 2, agent: "linter" }),
    ]);
    expect(items).toHaveLength(1);
    expect(items[0]).toMatchObject({ kind: "divider", tone: "orchestrator", highlights: ["surveyor", "linter"],
      text: "Orchestrator → step survey · dispatched surveyor + linter" });
  });

  it("keeps separate dividers for different steps", () => {
    const items = chatItemsFromEvents([
      ev("loop.step.dispatched", { step: "survey", stepId: 1, agent: "surveyor" }),
      ev("loop.step.dispatched", { step: "summarise", stepId: 2, agent: "summariser" }),
    ]);
    expect(of(items, "divider")).toHaveLength(2);
  });

  it("joins model deltas into one text item per run and splits on kind change", () => {
    const items = chatItemsFromEvents([
      ev("model.delta", { stepId: 1, agent: "a", tool: "thinking", text: "Let me " }),
      ev("model.delta", { stepId: 1, agent: "a", tool: "thinking", text: "think." }),
      ev("model.delta", { stepId: 1, agent: "a", text: "Hello " }),
      ev("model.delta", { stepId: 1, agent: "a", text: "there." }),
    ]);
    expect(items.map((i) => i.kind)).toEqual(["thinking", "text"]);
    expect(of(items, "thinking")[0]).toMatchObject({ text: "Let me think.", streaming: false });
    expect(of(items, "text")[0]).toMatchObject({ text: "Hello there.", streaming: true });
  });

  it("does not glue two agents' interleaved deltas together, and each keeps one row", () => {
    const items = chatItemsFromEvents([
      ev("model.delta", { stepId: 1, agent: "a", text: "A1 " }),
      ev("model.delta", { stepId: 2, agent: "b", text: "B1 " }),
      ev("model.delta", { stepId: 1, agent: "a", text: "A2" }),
    ]);
    expect(of(items, "text").map((t) => [t.agent, t.text])).toEqual([["a", "A1 A2"], ["b", "B1 "]]); // each agent keeps its own growing row; neither is glued to the other
  });

  it("pairs tool start and end by toolUseId, with duration, even with two calls of one tool", () => {
    const items = chatItemsFromEvents([
      ev("step.tool.start", { stepId: 1, agent: "a", tool: "read", summary: "a.go", toolUseId: "t1" }, "2026-10-24T10:00:00.000Z"),
      ev("step.tool.start", { stepId: 1, agent: "a", tool: "read", summary: "b.go", toolUseId: "t2" }, "2026-10-24T10:00:00.100Z"),
      ev("step.tool.end", { stepId: 1, agent: "a", tool: "read", toolUseId: "t2", result: "read b.go" }, "2026-10-24T10:00:00.400Z"),
      ev("step.tool.end", { stepId: 1, agent: "a", tool: "read", toolUseId: "t1", result: "read a.go" }, "2026-10-24T10:00:01.500Z"),
    ]);
    const tools = of(items, "tool");
    expect(tools.map((t) => [t.summary, t.status, t.result, t.durationMs])).toEqual([
      ["a.go", "done", "read a.go", 1500], ["b.go", "done", "read b.go", 300],
    ]);
  });

  it("carries a bash call's whole command from its start and its output from its end", () => {
    const tools = of(chatItemsFromEvents([
      ev("step.tool.start", { stepId: 1, agent: "a", tool: "bash", summary: "grep -n …", command: "grep -n 'func Test' x_test.go", toolUseId: "b" }),
      ev("step.tool.end", { stepId: 1, agent: "a", tool: "bash", toolUseId: "b", result: "14:func TestA", output: "14:func TestA\n27:func TestB", outputTruncated: true }),
    ]), "tool");
    expect(tools[0]).toMatchObject({ command: "grep -n 'func Test' x_test.go", output: "14:func TestA\n27:func TestB", outputTruncated: true });
  });

  it("renders an end with no start it saw (a cursor that began mid-call)", () => {
    const tools = of(chatItemsFromEvents([ev("step.tool.end", { stepId: 1, agent: "a", tool: "bash", toolUseId: "x", result: "ok" })]), "tool");
    expect(tools).toHaveLength(1);
    expect(tools[0]).toMatchObject({ tool: "bash", status: "done" });
  });

  it("marks a failed call, a policy denial, a permission wait and a steered skip differently", () => {
    const items = chatItemsFromEvents([
      ev("step.tool.start", { stepId: 1, agent: "a", tool: "bash", toolUseId: "e" }),
      ev("step.tool.end", { stepId: 1, agent: "a", tool: "bash", toolUseId: "e", result: "exit 1", isError: true }),
      ev("step.tool.start", { stepId: 1, agent: "a", tool: "bash", toolUseId: "d" }),
      ev("step.policy.decided", { stepId: 1, tool: "bash", toolUseId: "d", verdict: "denied", reason: "refused: no-rm" }),
      ev("step.tool.end", { stepId: 1, agent: "a", tool: "bash", toolUseId: "d", result: "refused: no-rm", isError: true }),
      ev("step.tool.start", { stepId: 1, agent: "a", tool: "bash", toolUseId: "w" }),
      ev("step.policy.decided", { stepId: 1, tool: "bash", toolUseId: "w", verdict: "asked", reason: "ask: pushes" }),
      ev("step.tool.end", { stepId: 1, agent: "a", tool: "bash", toolUseId: "w", result: "suspended: awaiting a permission answer" }),
      ev("step.tool.start", { stepId: 1, agent: "a", tool: "bash", toolUseId: "s" }),
      ev("step.tool.skipped", { stepId: 1, agent: "a", tool: "bash", toolUseId: "s", reason: "steered" }),
      ev("step.tool.start", { stepId: 1, agent: "a", tool: "bash", toolUseId: "r" }),
    ]);
    expect(of(items, "tool").map((t) => [t.toolUseId, t.status])).toEqual([
      ["e", "error"], ["d", "denied"], ["w", "awaiting"], ["s", "skipped"], ["r", "running"],
    ]);
    expect(of(items, "tool")[1]!.verdict).toEqual({ verdict: "denied", reason: "refused: no-rm" });
  });

  it("puts the 'policy allowed write_file' divider after the write ends, and only for listed tools", () => {
    const items = chatItemsFromEvents([
      ev("step.tool.start", { stepId: 1, agent: "a", tool: "write_file", toolUseId: "w" }),
      ev("step.policy.decided", { stepId: 1, tool: "write_file", toolUseId: "w", verdict: "allowed", reason: "allowed: x" }),
      ev("model.delta", { stepId: 2, agent: "b", text: "meanwhile" }),
      ev("step.tool.end", { stepId: 1, agent: "a", tool: "write_file", toolUseId: "w", path: "SURVEY.md", linesAdded: 18, result: "wrote" }),
      ev("step.tool.start", { stepId: 1, agent: "a", tool: "read", toolUseId: "r" }),
      ev("step.policy.decided", { stepId: 1, tool: "read", toolUseId: "r", verdict: "allowed", reason: "allowed: y" }),
      ev("step.tool.end", { stepId: 1, agent: "a", tool: "read", toolUseId: "r", result: "read" }),
    ]);
    expect(items.map((i) => i.kind)).toEqual(["tool", "text", "divider", "tool"]);
    expect(of(items, "divider")[0]).toMatchObject({ tone: "policy", text: "policy allowed write_file" });
    expect(of(items, "tool")[0]!.file).toEqual({ path: "SURVEY.md", linesAdded: 18, linesRemoved: undefined });
  });

  it("follows a steer from queued (with the host's text) to delivered, and a withdrawn one", () => {
    const items = chatItemsFromEvents([
      ev("step.steer.queued", { stepId: 1, agent: "a", steerId: 7, hasText: true }),
      ev("step.steer.queued", { stepId: 1, agent: "a", steerId: 8, hasText: true }),
      ev("step.steer.delivered", { stepId: 1, steerId: 7, iteration: 3 }),
      ev("step.steer.cancelled", { stepId: 1, steerId: 8 }),
    ], { steerTexts: { 7: "Also mention the Go version." } });
    expect(of(items, "steer").map((s) => [s.steerId, s.state, s.text, s.target])).toEqual([
      [7, "delivered", "Also mention the Go version.", "a"], [8, "cancelled", undefined, "a"],
    ]);
  });

  it("groups consecutive questions of one agent into one card and fills answers by question id", () => {
    const items = chatItemsFromEvents([
      ev("question.asked", { id: 1, stepId: 9, agent: "a", body: "Q one?", kind: "clarify", options: ["x", "y"] }),
      ev("question.asked", { id: 2, stepId: 9, agent: "a", body: "Q two?", kind: "clarify" }),
      ev("question.answered", { stepId: 9, questionId: 2, answer: "free text" }),
    ]);
    const qs = of(items, "question");
    expect(qs).toHaveLength(1);
    expect(qs[0]!.questions.map((q) => [q.id, q.answer])).toEqual([["1", undefined], ["2", "free text"]]);
  });

  it("turns a permission question about a rendered call into that call's approval, then resumes it in place", () => {
    const parked = [
      ev("step.tool.start", { stepId: 1, agent: "a", tool: "bash", summary: "go vet", toolUseId: "v" }),
      ev("step.policy.decided", { stepId: 1, tool: "bash", toolUseId: "v", verdict: "asked", reason: "ask: commands" }),
      ev("step.tool.end", { stepId: 1, agent: "a", tool: "bash", toolUseId: "v", result: "suspended: awaiting a permission answer" }),
      ev("question.asked", { id: 7, stepId: 1, agent: "a", toolUseId: "v", kind: "permission", body: "Allow go vet?", options: ["deny", "approve"] }),
    ];
    const waiting = chatItemsFromEvents(parked);
    expect(of(waiting, "question")).toHaveLength(0);
    expect(of(waiting, "tool")[0]).toMatchObject({ status: "awaiting", approval: { id: "7", prompt: "Allow go vet?", kind: "permission" } });

    const approved = chatItemsFromEvents([...parked,
      ev("question.answered", { stepId: 1, questionId: 7, answer: "approve" }),
      ev("step.tool.start", { stepId: 1, agent: "a", tool: "bash", summary: "go vet", toolUseId: "v" }),
      ev("step.tool.end", { stepId: 1, agent: "a", tool: "bash", toolUseId: "v", result: "exit 0" }),
    ]);
    expect(of(approved, "tool")).toHaveLength(1);
    expect(of(approved, "tool")[0]).toMatchObject({ status: "done", result: "exit 0", approval: { answer: "approve" } });

    const declined = chatItemsFromEvents([...parked, ev("question.answered", { stepId: 1, questionId: 7, answer: "deny" })]);
    expect(of(declined, "tool")[0]).toMatchObject({ status: "denied", result: "declined by you", approval: { answer: "deny" } });
  });

  it("keeps a permission question with no call it can attach to on its own card", () => {
    const items = chatItemsFromEvents([
      ev("question.asked", { id: 1, stepId: 9, agent: "a", body: "clarify?", kind: "clarify" }),
      ev("question.asked", { id: 2, stepId: 9, agent: "a", body: "push?", kind: "permission", options: ["approve", "deny"] }),
    ]);
    expect(of(items, "question")).toHaveLength(2);
  });

  it("turns a loop gate into a gate card and closes it when the loop advances", () => {
    const waiting = chatItemsFromEvents([ev("loop.waiting", { loopId: "l", questionId: 90, prompt: "Approve?", answers: ["approve", "reject"], lane: "approve", step: "approve" })]);
    expect(of(waiting, "gate")[0]).toMatchObject({ id: "gate:90", title: "Approve?", stepLabel: "approve", state: "awaiting", options: ["approve", "reject"] });
    const answered = chatItemsFromEvents([
      ev("loop.waiting", { loopId: "l", questionId: 91, prompt: "Approve?", answers: ["approve", "reject"] }),
      ev("loop.advanced", { loopId: "l", lane: "approve", questionId: 91, answer: "approve" }),
    ]);
    expect(of(answered, "gate")[0]).toMatchObject({ state: "answered", answer: "approve" });
  });

  it("treats a stopped wait as a pause, not a gate", () => {
    const items = chatItemsFromEvents([
      ev("loop.step.dispatched", { step: "s", stepId: 4, agent: "writer" }),
      ev("loop.waiting", { reason: "stopped", stepId: 4 }),
    ]);
    expect(of(items, "gate")).toHaveLength(0);
    expect(items[1]).toMatchObject({ kind: "divider", text: "Paused · writer was stopped" });
  });

  it("names the agent from an earlier event when a later one carries none", () => {
    const items = chatItemsFromEvents([
      ev("step.started", { stepId: 5, agent: "builder" }),
      ev("step.tool.start", { stepId: 5, tool: "bash", toolUseId: "t" }),
    ]);
    expect(of(items, "tool")[0]!.agent).toBe("builder");
  });

  it("sorts by seq, closes streams at step.done, and ignores kinds it does not know", () => {
    const a = ev("model.delta", { stepId: 1, agent: "a", text: "hi" });
    const b = ev("step.done", { stepId: 1, agent: "a", outcome: "ok" });
    const c = ev("feature.from.the.future", { anything: true });
    const items = chatItemsFromEvents([c, b, a]);
    expect(items.map((i) => i.kind)).toEqual(["text", "divider"]);
    expect(of(items, "text")[0]!.streaming).toBe(false);
    expect(of(items, "divider")[0]).toMatchObject({ tone: "success", text: "a finished · ok" });
  });

  it("renders loop lifecycle dividers with the right tones", () => {
    const items = chatItemsFromEvents([
      ev("loop.started", { blueprint: "two-step" }),
      ev("step.route.changed", { stepId: 1, agent: "a", route: "anthropic/opus", reasoningEffort: "high" }),
      ev("loop.blocked", { reason: { kind: "step-failed", detail: "vet" } }),
      ev("loop.finished", { status: "done" }),
    ]);
    expect(of(items, "divider").map((d) => [d.tone, d.text])).toEqual([
      ["orchestrator", "Orchestrator · loop started · two-step"],
      ["neutral", "a now on anthropic/opus · effort high"],
      ["danger", "Loop blocked · step-failed · vet"],
      ["success", "Loop finished · done"],
    ]);
  });
});

describe("chatItemsFromTranscript", () => {
  const messages = [
    { role: "user", content: [{ kind: "text", text: "Do the survey." }] },
    { role: "assistant", content: [
      { kind: "thinking", text: "" , signature: "sig" },
      { kind: "tool_use", id: "t1", name: "bash", input: { command: "go vet ./...", description: "Vet" } },
      { kind: "tool_use", id: "t2", name: "write_file", input: { path: "S.md", content: "x" } },
    ] },
    { role: "user", content: [
      { kind: "tool_result", toolUseId: "t1", content: "not run: the person steered before this tool ran — …", isError: true },
      { kind: "tool_result", toolUseId: "t2", content: "wrote S.md (1 bytes)" },
      { kind: "text", text: "Skip vet." },
    ] },
    { role: "assistant", content: [{ kind: "tool_use", id: "t3", name: "read", input: { path: "go.mod" } }] },
  ];

  it("pairs tool results, reads steered skips, and turns later user text into delivered steers", () => {
    const items = chatItemsFromTranscript(messages, { agent: "surveyor" });
    expect(items.map((i) => i.kind)).toEqual(["text", "thinking", "tool", "tool", "steer", "tool"]);
    const tools = of(items, "tool");
    expect(tools.map((t) => [t.tool, t.summary, t.status])).toEqual([
      ["bash", "Vet", "skipped"], ["write_file", "S.md", "done"], ["read", "go.mod", "awaiting"],
    ]);
    expect(tools[1]!.file).toEqual({ path: "S.md" });
    expect(of(items, "thinking")[0]!.text).toBe("Thinking occurred.");
    expect(of(items, "steer")[0]).toMatchObject({ text: "Skip vet.", state: "delivered", target: "surveyor" });
  });

  it("leaves an unanswered call running on a live step, and can hide the prompt", () => {
    const items = chatItemsFromTranscript(messages, { agent: "s", running: true, includePrompt: false });
    expect(items[0]!.kind).toBe("thinking");
    expect(of(items, "tool").at(-1)!.status).toBe("running");
  });
});

describe("chatItemsFromLoopTranscriptPage", () => {
  const msg = (over: Partial<LoopTranscriptMessageLike>): LoopTranscriptMessageLike =>
    ({ id: 1, stepId: 12, agent: "surveyor", phase: "work", role: "user", createdAt: "2026-10-29T10:00:00Z", ...over });

  it("spans two steps, tracking the task-vs-steer distinction per step rather than globally", () => {
    const items = chatItemsFromLoopTranscriptPage([
      msg({ id: 1, stepId: 12, agent: "surveyor", role: "user", content: [{ kind: "text", text: "Survey the repo." }] }),
      msg({ id: 2, stepId: 12, agent: "surveyor", role: "assistant", content: [{ kind: "text", text: "On it." }] }),
      msg({ id: 3, stepId: 12, agent: "surveyor", role: "user", content: [{ kind: "text", text: "Also check go.mod." }] }),
      // A second step's FIRST user turn is its own task, never read as a steer
      // carried over from step 12 — the whole reason this tracks per step id.
      msg({ id: 4, stepId: 13, agent: "linter", role: "user", content: [{ kind: "text", text: "Lint the repo." }] }),
    ]);
    // Each step opens with the orchestrator's dispatch divider, as it does live.
    expect(items.map((i) => i.kind)).toEqual(["divider", "divider", "text", "steer", "divider", "divider"]);
    expect(items[1]).toMatchObject({ kind: "divider", text: "Brief for surveyor: Survey the repo.", detail: "Survey the repo." });
    expect(items[2]).toMatchObject({ kind: "text", text: "On it." });
    expect(items[3]).toMatchObject({ kind: "steer", target: "surveyor", text: "Also check go.mod.", state: "delivered" });
    expect(items[5]).toMatchObject({ kind: "divider", text: "Brief for linter: Lint the repo.", detail: "Lint the repo." });
  });

  it("pairs a tool call with its result across the page, same as a single step's own transcript", () => {
    const items = chatItemsFromLoopTranscriptPage([
      msg({ id: 1, role: "assistant", content: [{ kind: "tool_use", id: "t1", name: "bash", input: { command: "go vet ./..." } }] }),
      msg({ id: 2, role: "user", content: [{ kind: "tool_result", toolUseId: "t1", content: "ok" }] }),
    ]);
    expect(of(items, "tool")[0]).toMatchObject({ tool: "bash", status: "done", result: "ok" });
  });

  it("names a row it cannot read instead of guessing or losing the rest of the page", () => {
    const items = chatItemsFromLoopTranscriptPage([
      msg({ id: 1, role: "assistant", unreadable: "written by a newer Aeon — format 3" }),
      msg({ id: 2, role: "user", content: [{ kind: "text", text: "still readable" }] }),
    ]);
    expect(items[0]).toMatchObject({ kind: "divider", text: "written by a newer Aeon — format 3" });
    expect(items[2]).toMatchObject({ kind: "divider", text: "Brief for surveyor: still readable" });
  });

  it("parks a call with no matching result rather than showing it as still running — a page is never the live tail", () => {
    const items = chatItemsFromLoopTranscriptPage([
      msg({ id: 1, role: "assistant", content: [{ kind: "tool_use", id: "t1", name: "read", input: { path: "go.mod" } }] }),
    ]);
    expect(of(items, "tool")[0]!.status).toBe("awaiting");
  });
});

describe("deriveSession", () => {
  it("tracks each agent's step, route, effort and state, and which step owns a question", () => {
    const s = deriveSession([
      ev("loop.step.dispatched", { loopId: "l", step: "survey", stepId: 1, agent: "surveyor", route: "anthropic/sonnet", reasoningEffort: "medium" }),
      ev("loop.step.dispatched", { loopId: "l", step: "survey", stepId: 2, agent: "linter" }),
      ev("question.asked", { id: 30, stepId: 1, agent: "surveyor", body: "?" }),
      ev("step.route.changed", { stepId: 1, route: "anthropic/opus", reasoningEffort: "high" }),
      ev("step.stopped", { stepId: 1 }),
      ev("step.done", { stepId: 2, agent: "linter", outcome: "ok" }),
      ev("loop.waiting", { loopId: "l", questionId: 90, answers: ["approve"], lane: "approve" }),
    ]);
    expect(s.runs.map((r) => [r.agent, r.stepId, r.state, r.route, r.reasoningEffort])).toEqual([
      ["surveyor", 1, "stopped", "anthropic/opus", "high"], ["linter", 2, "done", undefined, undefined],
    ]);
    expect(s.questionStep.get(30)).toBe(1);
    expect(s.gates.get(90)).toEqual({ loopId: "l", lane: "approve" });
    expect([...s.loopIds]).toEqual(["l"]);
    // Parked at a gate with nobody running is waiting, not finished.
    expect(s.loopStatus).toBe("waiting");
  });

  it("follows the loop's own status to finished or blocked", () => {
    expect(deriveSession([ev("loop.started"), ev("loop.finished", { status: "done" })]).loopStatus).toBe("finished");
    expect(deriveSession([ev("loop.started"), ev("loop.blocked", { reason: "x" })]).loopStatus).toBe("blocked");
    expect(deriveSession([]).loopStatus).toBe("unknown");
  });

  it("reports the agent's latest run when it is dispatched again", () => {
    const s = deriveSession([
      ev("loop.step.dispatched", { step: "a", stepId: 1, agent: "x" }),
      ev("step.done", { stepId: 1, agent: "x", outcome: "ok" }),
      ev("loop.step.dispatched", { step: "b", stepId: 2, agent: "x" }),
    ]);
    expect(s.runs).toHaveLength(1);
    expect(s.runs[0]).toMatchObject({ stepId: 2, state: "running" });
  });
});

describe("parsePolicySentence", () => {
  it("pulls the quoted rule forward and keeps every other part verbatim, in order", () => {
    expect(parsePolicySentence(`allowed: agent:surveyor · "reads" · target all-agents · source operator · no expiry`)).toEqual({
      rule: "reads", details: ["agent:surveyor", "target all-agents", "source operator", "no expiry"],
    });
    expect(parsePolicySentence(`refused: agent:builder · "no-rm" · target all-agents`).rule).toBe("no-rm");
    expect(parsePolicySentence(`ask: agent:surveyor · "commands-ask" · command go vet ./...`).details).toContain("command go vet ./...");
  });

  it("shows what does not split as a single part, and nothing for nothing", () => {
    expect(parsePolicySentence("allowed: answered by you")).toEqual({ details: ["answered by you"] });
    expect(parsePolicySentence(undefined)).toEqual({ details: [] });
  });
});

// --- S39 T11: the thread and the run ledger adapt too ------------------------------------------

describe("chatItemsFromEvents — the thread and the runs (S39)", () => {
  it("a loop.message.posted from a person becomes a message item", () => {
    const items = chatItemsFromEvents([
      ev("loop.message.posted", { loopId: "l", messageId: 7, seq: 3, author: "human", body: "can you also check the tests?", source: "user" }),
    ]);
    const messages = of(items, "message");
    expect(messages).toHaveLength(1);
    expect(messages[0]).toMatchObject({ kind: "message", author: "human", text: "can you also check the tests?", source: "user", messageId: 7 });
  });

  it("an agent-authored message is not drawn (the stream already shows it)", () => {
    const items = chatItemsFromEvents([
      ev("model.delta", { stepId: 1, agent: "chat", text: "the answer" }),
      ev("loop.message.posted", { loopId: "l", messageId: 8, seq: 4, author: "agent", body: "the answer" }),
    ]);
    expect(of(items, "message")).toHaveLength(0);
    expect(of(items, "text")).toHaveLength(1); // the reply, exactly once
  });

  it("a repeated messageId is drawn once", () => {
    // The ring replays what a live window already saw; the row is the same row.
    const items = chatItemsFromEvents([
      ev("loop.message.posted", { loopId: "l", messageId: 9, seq: 5, author: "human", body: "ping" }),
      ev("loop.message.posted", { loopId: "l", messageId: 9, seq: 5, author: "human", body: "ping" }),
    ]);
    expect(of(items, "message")).toHaveLength(1);
  });

  it("a template-sourced message carries its source, a typed one none", () => {
    const items = chatItemsFromEvents([
      ev("loop.message.posted", { loopId: "l", messageId: 10, seq: 6, author: "human", body: "kickoff text", source: "template" }),
    ]);
    expect(of(items, "message")[0]).toMatchObject({ source: "template" });
  });

  it("run.started then run.finished make one run card, patched in place", () => {
    const items = chatItemsFromEvents([
      ev("loop.run.started", { loopId: "l", runSeq: 2, blueprint: "implement", version: 3, startedByKind: "user" }),
      ev("loop.step.dispatched", { loopId: "l", step: "implement", stepId: 4, agent: "implementor" }),
      ev("loop.run.finished", { loopId: "l", runSeq: 2, outcome: "ok", status: "idle", cost: { usdAtLeast: 0.5, exact: false, partial: 1, unknown: 0, steps: 2 } }),
    ]);
    const runs = of(items, "run");
    expect(runs).toHaveLength(1);
    expect(runs[0]).toMatchObject({ id: "run:2", runSeq: 2, blueprint: "implement", version: 3, state: "done", startedBy: "user",
      cost: { usdAtLeast: 0.5, exact: false } });
    // One card, patched in place — and the run's step divider streamed
    // beside it: the card is the run's anchor, never a second row.
    expect(items.map((i) => i.kind)).toEqual(["run", "divider"]);
  });

  it("a cancelled run says cancelled", () => {
    const items = chatItemsFromEvents([
      ev("loop.run.started", { loopId: "l", runSeq: 3, blueprint: "implement", startedByKind: "agent" }),
      ev("loop.run.finished", { loopId: "l", runSeq: 3, outcome: "cancelled", status: "idle" }),
    ]);
    expect(of(items, "run")).toHaveLength(1);
    expect(of(items, "run")[0]).toMatchObject({ state: "cancelled", startedBy: "agent" });
    expect(of(items, "run")[0]?.cost).toBeUndefined(); // absent means unknown, never $0
  });

  it("run.finished with no start in the window still draws a card", () => {
    const items = chatItemsFromEvents([
      ev("loop.run.finished", { loopId: "l", runSeq: 4, outcome: "failed", status: "idle", cost: { usdAtLeast: 1.25, exact: true, partial: 0, unknown: 0, steps: 1 } }),
    ]);
    const runs = of(items, "run");
    expect(runs).toHaveLength(1);
    expect(runs[0]).toMatchObject({ id: "run:4", runSeq: 4, state: "failed", blueprint: "", cost: { usdAtLeast: 1.25, exact: true } });
  });
});

describe("chatItemsFromEvents — the run_start ask (S39 T12)", () => {
  // Step 1 of T12, verified at runtime/types.go:409-434: the StepEvent wire
  // carries Tool, Summary and a bash-only Command — NO structured tool input.
  // So no adapter parses runStart today (inventing fields is the failure
  // mode); the card falls back to naming the tool. The one test that would
  // exercise a structured parse stays skipped with that reason, and the
  // leak test runs for real.
  it.skip("a permission question for run_start carries runStart when the input is known", () => {
    // Skipped: the event stream has no structured input to parse (see above).
    // When a daemon adds StepEvent.Input, this test parses {"blueprint":…,
    // "goal":…} off the tool-start and asserts question.runStart.
  });

  it("a permission question for another tool has no runStart", () => {
    const items = chatItemsFromEvents([
      ev("step.tool.start", { stepId: 1, agent: "chat", tool: "bash", summary: "git push", toolUseId: "b1" }),
      ev("question.asked", { stepId: 1, agent: "chat", id: 41, kind: "permission", body: 'chat wants to run: git push', options: ["approve", "deny"], toolUseId: "b1" }),
    ]);
    const tool = of(items, "tool")[0];
    expect(tool?.approval).toBeTruthy();
    expect(tool?.approval?.runStart).toBeUndefined(); // no leak onto bash
  });
});

describe("deriveSession — the rest state (S39)", () => {
  it("loop.run.finished -> idle; loop.run.started -> running", () => {
    const resting = deriveSession([
      ev("loop.started", { loopId: "l" }),
      ev("loop.run.finished", { loopId: "l", runSeq: 1, outcome: "ok", status: "idle" }),
    ]);
    expect(resting.loopStatus).toBe("idle");
    const busy = deriveSession([
      ev("loop.run.finished", { loopId: "l", runSeq: 1, outcome: "ok", status: "idle" }),
      ev("loop.run.started", { loopId: "l", runSeq: 2, blueprint: "implement", startedByKind: "user" }),
    ]);
    expect(busy.loopStatus).toBe("running");
  });

  it("a conversation turn is a step in a resting loop, not the loop running", () => {
    const afterTurn = deriveSession([
      ev("loop.run.finished", { loopId: "l", runSeq: 1, outcome: "ok", status: "idle" }),
      ev("loop.step.dispatched", { loopId: "l", lane: "", step: "turn", stepId: 5, agent: "chat" }),
      ev("step.done", { loopId: "l", stepId: 5, agent: "chat", outcome: "blocked" }),
    ]);
    expect(afterTurn.loopStatus).toBe("idle");
    // A turn in a loop whose rest the window never saw leaves the status unknown, so the loop's own row answers.
    expect(deriveSession([ev("loop.step.dispatched", { loopId: "l", lane: "", step: "turn", stepId: 5, agent: "chat" })]).loopStatus).toBe("unknown");
  });
});

describe("the journal's replay fields", () => {
  const e = (seq: number, kind: string, payload: Record<string, unknown>): ChatEventLike => ({ seq, kind, at: new Date(Date.UTC(2026, 10, 15, 10, 0, seq)).toISOString(), payload });
  it("draws a finished turn from the journal when its chunks were not kept, and never twice when they were", () => {
    const turn = { stepId: 1, agent: "investigator", iteration: 1, text: "The answer.", thinking: "Hmm." };
    const replay = chatItemsFromEvents([e(1, "step.turn.completed", turn)]);
    expect(replay.map((i) => i.kind)).toEqual(["thinking", "text", "turn"]); // the turn's own cost line follows its words
    const live = chatItemsFromEvents([
      e(1, "model.delta", { stepId: 1, agent: "investigator", iteration: 1, text: "The answer." }),
      e(2, "step.turn.completed", turn),
    ]);
    expect(live.filter((i) => i.kind === "text")).toHaveLength(1);
    expect(live.some((i) => i.kind === "thinking")).toBe(false); // streamed text only: the turn adds nothing it already drew
  });

  it("takes a call's duration and result from the journal, and says who approved it", () => {
    const items = chatItemsFromEvents([
      e(1, "step.tool.start", { stepId: 1, agent: "a", tool: "bash", toolUseId: "t1", summary: "ls" }),
      e(2, "step.policy.decided", { stepId: 1, toolUseId: "t1", tool: "bash", verdict: "asked", reason: "asked" }),
      e(3, "step.policy.decided", { stepId: 1, toolUseId: "t1", tool: "bash", verdict: "allowed", reason: "full access", approvedBy: "auto" }),
      e(4, "step.tool.end", { stepId: 1, toolUseId: "t1", tool: "bash", result: "ok", durationMs: 1234, detail: "a\nb" }),
    ]);
    const tool = items.find((i) => i.kind === "tool");
    expect(tool).toMatchObject({ status: "done", durationMs: 1234, detail: "a\nb", approvedBy: "auto" });
    expect(approvalLabel(tool as never)).toBe("approved by you, auto");
  });

  it("labels a person's answer 'approved by you', a decline nothing, and a client's answer as the client's", () => {
    const base = { kind: "tool", id: "x", tool: "bash", status: "done" } as never;
    expect(approvalLabel({ ...(base as object), approval: { id: "1", prompt: "", kind: "permission", answer: "approve" } } as never)).toBe("approved by you");
    expect(approvalLabel({ ...(base as object), approval: { id: "1", prompt: "", kind: "permission", answer: "deny" } } as never)).toBeUndefined();
    expect(approvalLabel({ ...(base as object), approvedBy: "client" } as never)).toBe("approved by a client");
    expect(approvalLabel({ ...(base as object), approvedBy: "policy" } as never)).toBeUndefined();
  });
});

describe("replay equals live", () => {
  const at = (n: number) => new Date(Date.UTC(2026, 10, 15, 10, 0, n)).toISOString();
  let n = 0;
  const e = (kind: string, payload: Record<string, unknown>): ChatEventLike => ({ seq: ++n, kind, at: at(n), payload });

  // One loop's whole story, as the daemon publishes it: a dispatch, a turn that streams thinking and text and asks
  // for a call, an auto-approved bash, its result, a second turn, the step's end, a person's message at the start.
  const live: ChatEventLike[] = [
    e("loop.message.posted", { loopId: "l", messageId: 1, author: "human", body: "Make it greet.", source: "user" }),
    e("loop.step.dispatched", { loopId: "l", step: "discovery", stepId: 5, agent: "investigator" }),
    e("step.started", { stepId: 5, agent: "investigator", loopId: "l" }),
    e("model.delta", { stepId: 5, agent: "investigator", iteration: 1, tool: "thinking", text: "Look at " }),
    e("model.delta", { stepId: 5, agent: "investigator", iteration: 1, tool: "thinking", text: "the repo." }),
    e("model.delta", { stepId: 5, agent: "investigator", iteration: 1, text: "I'll start " }),
    e("model.delta", { stepId: 5, agent: "investigator", iteration: 1, text: "by listing." }),
    e("step.turn.completed", { stepId: 5, agent: "investigator", iteration: 1, thinking: "Look at the repo.", text: "I'll start by listing.", stepCost: { state: "known", usd: 0.001 } }),
    e("step.tool.start", { stepId: 5, agent: "investigator", tool: "bash", toolUseId: "t1", summary: "ls", command: "ls -la" }),
    e("step.policy.decided", { stepId: 5, toolUseId: "t1", tool: "bash", verdict: "asked", reason: "asked" }),
    e("step.policy.decided", { stepId: 5, toolUseId: "t1", tool: "bash", verdict: "allowed", reason: "full access", approvedBy: "auto" }),
    e("step.tool.end", { stepId: 5, agent: "investigator", tool: "bash", toolUseId: "t1", result: "ok", output: "a\nb", durationMs: 120 }),
    e("model.delta", { stepId: 5, agent: "investigator", iteration: 2, text: "Done." }),
    e("step.turn.completed", { stepId: 5, agent: "investigator", iteration: 2, text: "Done.", stepCost: { state: "known", usd: 0.002 } }),
    e("step.done", { stepId: 5, agent: "investigator", outcome: "ok" }),
  ];
  // What a reopened loop reads: the journal, which leaves the streamed chunks out.
  const replay = live.filter((x) => x.kind !== "model.delta");

  // The parts a person can see, without the ids and streaming flags that differ by construction.
  const visible = (items: ChatItem[]) => items.map((i) => {
    const { id: _id, at: _at, ...rest } = i as unknown as Record<string, unknown>;
    delete rest.streaming;
    return rest;
  });

  it("draws the same chat from the journal as from the live stream", () => {
    expect(visible(chatItemsFromEvents(replay))).toEqual(visible(chatItemsFromEvents(live)));
  });

  it("keeps the person's message once, the agent's words and the call with its duration and approver", () => {
    const items = chatItemsFromEvents(replay);
    expect(items.filter((i) => i.kind === "message")).toHaveLength(1);
    expect(items.filter((i) => i.kind === "text" && i.agent === "you")).toHaveLength(0);
    expect(items.find((i) => i.kind === "thinking")).toMatchObject({ agent: "investigator", text: "Look at the repo." });
    expect(items.find((i) => i.kind === "tool")).toMatchObject({ tool: "bash", status: "done", durationMs: 120, approvedBy: "auto" });
  });
});

describe("the cost of each model call", () => {
  it("is on screen at the response that spent it, with its state kept", () => {
    const at = (n: number) => new Date(Date.UTC(2026, 10, 15, 10, 0, n)).toISOString();
    const items = chatItemsFromEvents([
      { seq: 1, kind: "step.turn.completed", at: at(1), payload: { stepId: 1, agent: "a", iteration: 1, text: "x", usage: { inputTokens: 59, outputTokens: 50, reasoningTokens: 45 },
        turnCost: { state: "known", usd: 0.000927 }, stepCost: { state: "known", usd: 0.000927 }, wallMs: 2100 } },
      { seq: 2, kind: "step.turn.completed", at: at(2), payload: { stepId: 1, agent: "a", iteration: 2, text: "y", usage: { inputTokens: 80 }, turnCost: { state: "unknown", reason: "no-price-for-route" },
        stepCost: { state: "unknown", usdAtLeast: 0.000927 }, wallMs: 900 } },
    ]);
    const turns = items.filter((i) => i.kind === "turn") as Extract<ChatItem, { kind: "turn" }>[];
    expect(turns).toHaveLength(2);
    expect(turns[0]).toMatchObject({ inputTokens: 59, outputTokens: 50, reasoningTokens: 45, wallMs: 2100, cost: { state: "known", usd: 0.000927 } });
    expect(turns[1]?.cost?.state).toBe("unknown"); // never turned into $0
  });

  it("a finished turn replaces the chunks it streamed with its whole text", () => {
    // The page joined mid-turn (only the tail of the thinking streamed), and the model alternated thinking and
    // text chunk by chunk: the finished turn carries the whole of each, so one row of each is drawn, whole.
    const d = (text: string, tool?: string) => ev("model.delta", { stepId: 7, agent: "investigator", iteration: 3, text, ...(tool ? { tool } : {}) });
    const items = chatItemsFromEvents([
      d("this work, and", "thinking"), d("Both"), d(" more", "thinking"), d(" answers in"),
      ev("step.turn.completed", { stepId: 7, agent: "investigator", iteration: 3, thinking: "Whole thought: this work, and more", text: "Both answers in" }),
    ]);
    expect(of(items, "thinking").map((i) => i.text)).toEqual(["Whole thought: this work, and more"]);
    expect(of(items, "text").map((i) => i.text)).toEqual(["Both answers in"]);
    expect(of(items, "thinking")[0]?.streaming).toBe(false);
  });

  it("an answer that arrives before its question still marks the question answered", () => {
    const items = chatItemsFromEvents([
      ev("question.answered", { stepId: 7, questionId: 32, answer: "B" }),
      ev("question.asked", { id: 32, stepId: 7, agent: "investigator", kind: "gate", body: "Q8", options: ["A", "B"] }),
    ]);
    expect(of(items, "question")[0]?.questions[0]?.answer).toBe("B");
  });

  it("two steps streaming at once each keep one growing row, not a row per chunk", () => {
    const d = (step: number, agent: string, text: string, tool?: string) => ev("model.delta", { stepId: step, agent, iteration: 1, text, ...(tool ? { tool } : {}) });
    const items = chatItemsFromEvents([
      d(1, "loop-titler", "Make", "thinking"), d(2, "intent-clarifier", "Read", "thinking"),
      d(1, "loop-titler", " a", "thinking"), d(2, "intent-clarifier", " the", "thinking"),
      d(1, "loop-titler", " title", "thinking"), d(2, "intent-clarifier", " goal", "thinking"),
    ]);
    const thoughts = of(items, "thinking");
    expect(thoughts.map((t) => [t.agent, t.text])).toEqual([["loop-titler", "Make a title"], ["intent-clarifier", "Read the goal"]]);
  });

  it("says when the engine gives a step that lost its deliverable one more go", () => {
    const items = chatItemsFromEvents([ev("loop.step.recovering", { loopId: "L", missing: "discovery.md", afterOutcome: "blocked" })]);
    const d = of(items, "divider");
    expect(d).toHaveLength(1);
    expect(d[0]!.text).toContain("discovery.md was not filed");
  });
});
