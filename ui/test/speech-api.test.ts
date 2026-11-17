import { describe, expect, it } from "vitest";
import { modelDraft, modelParams, voiceDraft } from "../src/features/models/modelConfig";

describe("a model's speech API", () => {
  const base = { ...modelDraft(undefined, "openai"), id: "hexgrad-Kokoro-82M", model: "hexgrad/Kokoro-82M", displayName: "Kokoro" };
  it("keeps a model name with a slash in the slug, and sends the speech API when one is chosen", () => {
    const tts = modelParams({ ...base, capabilities: ["tts"], audioApi: "synthesize" });
    expect(tts.model).toBe("hexgrad/Kokoro-82M");
    expect(tts.audioApi).toBe("synthesize");
    expect(modelParams({ ...base, capabilities: ["tts"], audioApi: "" }).audioApi).toBeUndefined();
  });
  it("accepts a slash in the route id, and refuses a traversal", () => {
    expect(modelParams({ ...base, id: "hexgrad/Kokoro-82M", capabilities: ["tts"] }).id).toBe("hexgrad/Kokoro-82M");
    for (const id of ["../x", "a//b", "/a", "a/", "a/.b"]) expect(() => modelParams({ ...base, id })).toThrow(/Route ID/);
  });

  it("builds the draft to test from the form, reusing a saved model's key only by naming it", () => {
    const d = { ...base, capabilities: ["tts"], audioApi: "synthesize", apiKey: "", baseUrl: " http://x/v1 " };
    expect(voiceDraft(d, false)).toMatchObject({ provider: "openai", model: "hexgrad/Kokoro-82M", baseUrl: "http://x/v1", audioApi: "synthesize", capabilities: ["tts"] });
    expect(voiceDraft(d, false).id).toBeUndefined();
    expect(voiceDraft(d, true).id).toBe("hexgrad-Kokoro-82M");
    expect(voiceDraft({ ...d, apiKey: "k", clearKey: true }, true).apiKey).toBeUndefined();
  });
});

import { describeError, RpcError } from "../src/lib/bridge";

describe("an error from the daemon", () => {
  it("shows the daemon's own sentence, not only its generic title", () => {
    const err = new RpcError(-31103, "invalid route", { what: 'route "x" has no price and no noPrice declaration (O8)' } as never, "rpc");
    expect(describeError(err)).toContain("no price");
    expect(describeError(new RpcError(0, "not connected"))).toBe("not connected");
  });
});
