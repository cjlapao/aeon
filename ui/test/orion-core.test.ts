import { describe, expect, it, vi } from "vitest";
import { confirmation, flushSentences, isStopCommand, matchWakeWord, speakable, splitSentences } from "../src/features/orion/speech";
import { DEFAULT_VAD, vadStep, type VadState } from "../src/features/orion/vad";
import { SpeechQueue } from "../src/features/orion/speechQueue";
import { OperatorSession } from "../src/features/orion/operator";
import { findLoop, ORION_TOOLS } from "../src/features/orion/tools";
import { DEFAULT_ORION, missingForOrion, sanitizeOrion } from "../src/features/orion/orionSettings";

describe("speech text", () => {
  it("cuts whole sentences out of text still arriving, and keeps the unfinished tail", () => {
    const a = splitSentences("Two agents are running. The planner is at the plan gate! It is wait");
    expect(a.sentences).toEqual(["Two agents are running.", "The planner is at the plan gate!"]);
    expect(a.rest).toBe(" It is wait");
    expect(flushSentences("One. Two")).toEqual(["One.", "Two"]);
    expect(splitSentences("Dr. Smith said hi. Bye.").sentences).toEqual(["Dr. Smith said hi."]); // an abbreviation is not an end
  });
  it("strips markdown and code before speech", () => {
    expect(speakable("**Done.** See `loop-1` and [the page](http://x).\n```go\ncode\n```")).toBe("Done. See loop-1 and the page.");
  });
  it("hears the wake word even when it is misheard by a letter, and returns what followed", () => {
    expect(matchWakeWord("Hey Orion, how is the dev loop", "orion")).toEqual({ matched: true, rest: "how is the dev loop" });
    expect(matchWakeWord("hey oryon what's up", "orion").matched).toBe(true);
    expect(matchWakeWord("the onion is ready", "orion").matched).toBe(true); // within one letter: the cost of a forgiving match
    expect(matchWakeWord("nothing to see here", "orion").matched).toBe(false);
  });
  it("takes only a whole-phrase order as 'stop'", () => {
    for (const ok of ["stop", "Stop.", "orion stop", "Stop it!", "that's enough", "be quiet", "shut up"]) expect(isStopCommand(ok)).toBe(true);
    for (const no of ["please do not stop the build", "stop the greeting loop", "bus stop", "what is the stop reason", ""]) expect(isStopCommand(no)).toBe(false);
  });
  it("reads yes, no and neither", () => {
    expect(confirmation("Yes, go ahead")).toBe("yes");
    expect(confirmation("no cancel that")).toBe("no");
    expect(confirmation("maybe later")).toBe("unclear");
  });
});

describe("voice activity", () => {
  const run = (frames: [number, number][]) => {
    let state: VadState = {};
    const events: string[] = [];
    for (const [rms, at] of frames) { const r = vadStep(state, rms, at, DEFAULT_VAD); state = r.state; if (r.event) events.push(`${r.event}@${at}`); }
    return events;
  };
  it("starts on loud, ends after the silence, and discards a click", () => {
    expect(run([[0.001, 0], [0.1, 100], [0.1, 600], [0.001, 700], [0.001, 1700]])).toEqual(["start@100", "end@1700"]);
    expect(run([[0.1, 0], [0.001, 100], [0.001, 1100]])).toEqual(["start@0", "discard@1100"]);
  });
});

describe("the speech queue", () => {
  it("makes the next sentence while the current plays, keeps what it made, and stops on demand", async () => {
    const made: string[] = [];
    const played: string[] = [];
    let release!: () => void;
    const q = new SpeechQueue({
      synth: async (t) => { made.push(t); return `u:${t}`; },
      play: (u) => { played.push(u); return new Promise<void>((r) => { release = r; }); },
      stop: () => release?.(),
    });
    q.say("One."); q.say("Two."); q.say("Three.");
    await vi.waitFor(() => expect(played).toEqual(["u:One."]));
    expect(made).toContain("Two."); // already being made while One plays
    release();
    await vi.waitFor(() => expect(played).toEqual(["u:One.", "u:Two."]));
    q.stop();
    await q.idle();
    expect(played).toHaveLength(2); // Three was forgotten
    q.say("One.");
    await vi.waitFor(() => expect(played).toHaveLength(3));
    expect(made.filter((t) => t === "One.")).toHaveLength(1); // said again from what was kept
    release();
  });
});

describe("the operator", () => {
  const loops = { loops: [{ id: "L1", slug: "loop-a", title: "Greeting CLI", status: "running", phase: "build", componentRoot: "/r" }, { id: "L2", slug: "loop-b", title: "Docs", status: "done", componentRoot: "/r" }] };
  function session(turns: unknown[]) {
    const calls: { method: string; params: unknown }[] = [];
    const rpc = vi.fn(async (method: string, params: unknown) => {
      calls.push({ method, params });
      if (method === "voice.turn") return turns.shift();
      if (method === "loop.list") return loops;
      if (method === "loop.steps") return { steps: [{ stepId: 7, agent: "planner", status: "running", turns: 3, toolCalls: 5, costState: "known" }], totals: { turns: 3, toolCalls: 5, costUsd: 0.5, costState: "known" } };
      if (method === "loop.show") return { status: "running", position: { lane: "plan" } };
      return {};
    });
    return { s: new OperatorSession({ rpc, route: () => "qwen" }), calls, rpc };
  }

  it("runs a read tool at once and speaks the model's answer", async () => {
    const { s, calls } = session([{ text: "", toolCalls: [{ id: "c1", name: "agents_running", input: {} }] }, { text: "The planner is working in Greeting CLI." }]);
    expect(await s.handle("which agents are running")).toBe("The planner is working in Greeting CLI.");
    expect(calls.filter((c) => c.method === "voice.turn")).toHaveLength(2);
    expect(calls.some((c) => c.method === "step.steer")).toBe(false);
  });

  it("holds a control tool until the person says yes, never runs it on a no, and asks again on an unclear answer", async () => {
    const { s, calls } = session([
      { text: "On it.", toolCalls: [{ id: "c1", name: "agent_steer", input: { loop: "greeting", instruction: "split T3" } }] },
      { text: "Sent." },
    ]);
    const ask = await s.handle("tell the planner to split task three");
    expect(ask).toContain("Tell the agent in greeting: split T3?");
    expect(calls.some((c) => c.method === "step.steer")).toBe(false); // nothing ran yet
    expect(s.awaitingConfirmation).toBe(true);
    expect(await s.handle("hmm")).toBe("Please say yes or no.");
    expect(await s.handle("yes")).toBe("Sent.");
    expect(calls.find((c) => c.method === "step.steer")?.params).toEqual({ stepId: 7, text: "split T3" });
  });

  it("does not carry an earlier turn's tool results into the next, so a stale status is not repeated", async () => {
    const { s, calls } = session([
      { text: "", toolCalls: [{ id: "c1", name: "agents_running", input: {} }] }, { text: "The planner is working." },
      { text: "Nothing now." },
    ]);
    await s.handle("what is running");
    await s.handle("and now?");
    const second = calls.filter((c) => c.method === "voice.turn")[2]!.params as { messages: { role: string }[] };
    expect(second.messages.some((m) => m.role === "tool")).toBe(false);
    expect(second.messages.map((m) => m.role)).toEqual(["user", "assistant", "user"]);
  });

  it("does not run a declined control tool", async () => {
    const { s, calls } = session([{ text: "", toolCalls: [{ id: "c1", name: "loop_stop", input: { loop: "greeting" } }] }, { text: "Okay, left it running." }]);
    await s.handle("stop the greeting loop");
    expect(await s.handle("no")).toBe("Okay, left it running.");
    expect(calls.some((c) => c.method === "loop.stop")).toBe(false);
  });
});

describe("finding a loop by words, and the settings", () => {
  const all = [{ id: "a", slug: "s-a", title: "Greeting CLI", status: "running", componentRoot: "/r" }, { id: "b", slug: "s-b", title: "Greeting docs", status: "done", componentRoot: "/r" }] as never;
  it("names the problem when it is ambiguous or absent, and takes the one running loop for empty words", () => {
    expect(findLoop(all, "greeting").problem).toContain("2 loops match");
    expect(findLoop(all, "nothing").problem).toContain("no loop matches");
    expect(findLoop(all, "").loop?.id).toBe("a");
    expect(findLoop(all, "docs").loop?.id).toBe("b");
  });
  it("offers only the tools it can name, with changes marked as controls", () => {
    expect(ORION_TOOLS.filter((t) => t.kind === "control").map((t) => t.name).sort()).toEqual(["agent_steer", "answer_ask", "loop_start", "loop_stop"]);
  });
  it("starts off, names what is missing, and repairs a damaged saved value", () => {
    expect(DEFAULT_ORION.enabled).toBe(false);
    expect(missingForOrion(DEFAULT_ORION)).toHaveLength(3);
    expect(sanitizeOrion({ enabled: true, preset: "nope", followUpSeconds: 9999, wakeWord: "  " })).toMatchObject({ enabled: true, preset: "solar", followUpSeconds: 20, wakeWord: "orion" });
  });
});
