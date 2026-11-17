import { describe, expect, it, vi } from "vitest";
import { OrionController, type OrionAudio } from "../src/features/orion/controller";
import { DEFAULT_ORION, type OrionSettings } from "../src/features/orion/orionSettings";

const settings: OrionSettings = { ...DEFAULT_ORION, enabled: true, asrRoute: "asr", ttsRoute: "tts", operatorRoute: "op", followUpSeconds: 20 };

function rig(over: Partial<OrionSettings> = {}, heard: string[] = []) {
  const cfg = { ...settings, ...over };
  const spoken: string[] = [];
  let onUtterance: (b: Blob) => void = () => undefined;
  const audio: OrionAudio & { mic: boolean; muted: boolean[] } = {
    mic: false, muted: [],
    listen: async (cb) => { audio.mic = true; onUtterance = cb; },
    unlisten: () => { audio.mic = false; },
    bargeIn: (m) => { audio.muted.push(m); },
    play: async (url) => { spoken.push(url); },
    stopPlayback: () => undefined,
  };
  const turns: unknown[] = [];
  const rpc = vi.fn(async (method: string, params: unknown) => {
    if (method === "voice.transcribe") return { text: heard.shift() ?? "" };
    if (method === "voice.speak") return { audio: (params as { text: string }).text, mime: "audio/mpeg" };
    if (method === "voice.turn") return turns.shift() ?? { text: "Okay." };
    return {};
  });
  const c = new OrionController({ rpc, audio, codec: { toBase64: async () => "AAA", toUrl: (b) => `u:${b}`, release: () => undefined }, settings: () => cfg });
  return { c, audio, rpc, spoken, turns, hear: async () => { onUtterance(new Blob(["x"], { type: "audio/webm" })); await vi.waitFor(() => expect(c.snapshot.phase).not.toBe("thinking")); } };
}

describe("Orion", () => {
  it("will not start without the three models, and says which are missing", async () => {
    const { c, audio } = rig({ asrRoute: "", operatorRoute: "" });
    await c.start();
    expect(c.snapshot.phase).toBe("off");
    expect(c.snapshot.missing).toEqual(["a speech-to-text model", "an operator model"]);
    expect(audio.mic).toBe(false);
  });

  it("listens for the wake word, ignores speech that is not for him, and answers what follows the word a sentence at a time", async () => {
    const r = rig({}, ["what a nice day", "hey Orion how is the dev loop"]);
    r.turns.push({ text: "It is at the plan gate. Waiting for you." });
    await r.c.start();
    expect(r.c.snapshot.phase).toBe("idle");
    expect(r.audio.mic).toBe(true);
    await r.hear();
    expect(r.c.snapshot.phase).toBe("idle");                       // not for him
    expect(r.rpc.mock.calls.some((c) => c[0] === "voice.turn")).toBe(false);
    await r.hear();
    await vi.waitFor(() => expect(r.c.snapshot.phase).toBe("awake"));
    expect(r.c.snapshot.heard).toBe("how is the dev loop");
    expect(r.spoken).toEqual(["u:It is at the plan gate.", "u:Waiting for you."]);
    expect(r.audio.muted).toContain(true);                          // he did not listen to himself
    expect(r.audio.muted.at(-1)).toBe(false);
  });

  it("answers a bare wake word with a prompt, then takes the next phrase without the word", async () => {
    const r = rig({}, ["Orion", "how many agents are running"]);
    r.turns.push({ text: "Two." });
    await r.c.start();
    await r.hear();
    await vi.waitFor(() => expect(r.spoken).toEqual(["u:Yes?"]));
    await r.hear();
    await vi.waitFor(() => expect(r.spoken).toContain("u:Two."));
  });

  it("in click mode keeps the microphone closed until the orb is clicked", async () => {
    const r = rig({ wakeMode: "click" });
    await r.c.start();
    expect(r.audio.mic).toBe(false);
    await r.c.toggle();
    expect(r.audio.mic).toBe(true);
    expect(r.c.snapshot.phase).toBe("awake");
    await r.c.toggle();
    expect(r.audio.mic).toBe(false);
    expect(r.c.snapshot.phase).toBe("idle");
  });

  it("opens one microphone however often he is started, and follows a change of wake mode", async () => {
    const cfg = { ...settings };
    let opened = 0;
    const audio: OrionAudio = { listen: async () => { opened += 1; }, unlisten: () => { opened = 0; }, bargeIn: () => undefined, play: async () => undefined, stopPlayback: () => undefined };
    const c = new OrionController({ rpc: async () => ({}), audio, codec: { toBase64: async () => "", toUrl: () => "", release: () => undefined }, settings: () => cfg });
    await c.start();
    await c.start();
    expect(opened).toBe(1);                    // a second start does not open a second one
    cfg.wakeMode = "click";
    await c.applyWakeMode();
    expect(opened).toBe(0);                    // click mode closes it while he sleeps
    cfg.wakeMode = "word";
    await c.applyWakeMode();
    expect(opened).toBe(1);
  });

  it("shows a failure instead of going silent", async () => {
    const r = rig({}, ["orion hello"]);
    r.rpc.mockImplementationOnce(async () => { throw new Error("route asr is not configured"); });
    await r.c.start();
    await r.hear();
    await vi.waitFor(() => expect(r.c.snapshot.phase).toBe("error"));
    expect(r.c.snapshot.error).toContain("not configured");
  });

  it("stops talking when told to stop, goes back to waiting for his name, and ignores 'stop' as part of a sentence", async () => {
    let utter: (b: Blob) => void = () => undefined;
    let release: () => void = () => undefined;
    const bargeStates: boolean[] = [];
    const heard = ["orion what is running", "please do not stop the build", "stop"];
    const audio: OrionAudio = {
      listen: async (cb) => { utter = cb; }, unlisten: () => undefined, bargeIn: (on) => { bargeStates.push(on); },
      play: () => new Promise<void>((resolve) => { release = resolve; }), stopPlayback: () => release(),
    };
    const rpc = vi.fn(async (method: string, params: unknown) => {
      if (method === "voice.transcribe") return { text: heard.shift() ?? "" };
      if (method === "voice.speak") return { audio: (params as { text: string }).text, mime: "audio/wav" };
      if (method === "voice.turn") return { text: "Two agents are running. The planner is at the gate." };
      return {};
    });
    const c = new OrionController({ rpc, audio, codec: { toBase64: async () => "", toUrl: (b) => b, release: () => undefined }, settings: () => ({ ...settings }) });
    await c.start();
    utter(new Blob(["a"]));
    await vi.waitFor(() => expect(c.snapshot.phase).toBe("speaking"));
    utter(new Blob(["b"]));                          // a sentence that only contains the word: not an order
    await new Promise((r) => setTimeout(r, 20));
    expect(c.snapshot.phase).toBe("speaking");
    utter(new Blob(["c"]));                          // "stop"
    await vi.waitFor(() => expect(c.snapshot.phase).toBe("idle"));
    expect(bargeStates.at(-1)).toBe(false);          // speaking ended
    expect(c.snapshot.said).toBeUndefined();
  });

  it("drops an answer still being worked out when told to stop", async () => {
    let utter: (b: Blob) => void = () => undefined;
    let answer: (v: unknown) => void = () => undefined;
    const heard = ["orion list the loops", "stop"];
    const audio: OrionAudio = { listen: async (cb) => { utter = cb; }, unlisten: () => undefined, bargeIn: () => undefined, play: async () => undefined, stopPlayback: () => undefined };
    const spoke: string[] = [];
    const rpc = vi.fn(async (method: string, params: unknown) => {
      if (method === "voice.transcribe") return { text: heard.shift() ?? "" };
      if (method === "voice.speak") { spoke.push((params as { text: string }).text); return { audio: "", mime: "audio/wav" }; }
      if (method === "voice.turn") return new Promise((resolve) => { answer = resolve; });
      return {};
    });
    const c = new OrionController({ rpc, audio, codec: { toBase64: async () => "", toUrl: (b) => b, release: () => undefined }, settings: () => ({ ...settings }) });
    await c.start();
    utter(new Blob(["a"]));
    await vi.waitFor(() => expect(c.snapshot.phase).toBe("thinking"));
    utter(new Blob(["b"]));
    await vi.waitFor(() => expect(c.snapshot.phase).toBe("idle"));
    answer({ text: "Nothing is running." });         // the model finally answers
    await new Promise((r) => setTimeout(r, 20));
    expect(spoke).toEqual([]);                        // and it is never spoken
    expect(c.snapshot.phase).toBe("idle");
  });
});
