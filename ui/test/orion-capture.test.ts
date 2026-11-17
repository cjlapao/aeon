import { describe, expect, it } from "vitest";
import { UtteranceCutter } from "../src/features/orion/cutter";
import { resample } from "../src/features/orion/wav";

const RATE = 16000;
const block = (level: number, n = 1600) => new Float32Array(n).fill(level); // 100 ms
function run(cutter: UtteranceCutter, plan: [number, number][]) {
  const out: Float32Array[] = [];
  let t = 0;
  for (const [level, blocks] of plan) for (let i = 0; i < blocks; i += 1) { const u = cutter.push(block(level), t); t += 100; if (u) out.push(u); }
  return out;
}

describe("cutting the microphone into utterances", () => {
  it("returns an utterance with half a second of lead-in, so the start of a word is not clipped", () => {
    const [u] = run(new UtteranceCutter(RATE), [[0.002, 20], [0.1, 6], [0.002, 12]]);
    expect(u).toBeTruthy();
    const lead = u!.findIndex((x) => x > 0.05);
    expect(lead).toBeGreaterThan(RATE * 0.35);   // ~0.5 s of quiet before the voice
    expect(lead).toBeLessThanOrEqual(RATE * 0.55);
  });
  it("ignores a click, and stays quiet through silence", () => {
    expect(run(new UtteranceCutter(RATE), [[0.002, 30], [0.2, 1], [0.002, 20]])).toHaveLength(0);
  });
  it("hears a quiet voice in a quiet room, but not the same level in a noisy one", () => {
    expect(run(new UtteranceCutter(RATE), [[0.001, 20], [0.03, 6], [0.001, 12]])).toHaveLength(1);
    const noisy = new UtteranceCutter(RATE);
    expect(run(noisy, [[0.04, 200]])).toHaveLength(0);   // a constant hum is not speech: dropped, and learned as the floor
    expect(noisy.threshold).toBeGreaterThan(0.1);
    expect(run(noisy, [[0.04, 10]])).toHaveLength(0);    // and the same hum no longer opens the microphone
  });
  it("counts muted blocks as silence, so he never hears himself", () => {
    const c = new UtteranceCutter(RATE);
    let heard = 0;
    for (let i = 0; i < 40; i += 1) if (c.push(block(0.2), i * 100, true)) heard += 1;
    expect(heard).toBe(0);
  });
});

describe("resampling", () => {
  it("changes the length by the rate ratio and keeps the shape", () => {
    const out = resample(new Float32Array([0, 1, 0, -1, 0, 1, 0, -1]), 48000, 16000);
    expect(out.length).toBe(3);
    expect(resample(new Float32Array([1, 2]), 16000, 16000)).toEqual(new Float32Array([1, 2]));
  });
});
