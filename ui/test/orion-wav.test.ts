import { describe, expect, it } from "vitest";
import { encodeWav, toMono, toWav } from "../src/features/orion/wav";

const bytes = async (b: Blob) => new Uint8Array(await new Promise<ArrayBuffer>((res) => { const r = new FileReader(); r.onload = () => res(r.result as ArrayBuffer); r.readAsArrayBuffer(b); }));

describe("WAV", () => {
  it("writes a 16-bit mono PCM file with a correct header and clipped samples", async () => {
    const wav = encodeWav(new Float32Array([0, 1, -1, 2]), 16000);
    expect(wav.type).toBe("audio/wav");
    const b = await bytes(wav);
    const v = new DataView(b.buffer);
    expect(String.fromCharCode(...b.slice(0, 4))).toBe("RIFF");
    expect(String.fromCharCode(...b.slice(8, 16))).toBe("WAVEfmt ");
    expect(v.getUint16(22, true)).toBe(1);           // mono
    expect(v.getUint32(24, true)).toBe(16000);       // rate
    expect(v.getUint16(34, true)).toBe(16);          // bits
    expect(v.getUint32(40, true)).toBe(8);           // data bytes: 4 samples
    expect(v.getUint32(4, true)).toBe(36 + 8);
    expect([v.getInt16(44, true), v.getInt16(46, true), v.getInt16(48, true), v.getInt16(50, true)]).toEqual([0, 32767, -32768, 32767]);
  });
  it("averages channels into one", () => {
    expect([...toMono([new Float32Array([1, 0]), new Float32Array([0, 1])])]).toEqual([0.5, 0.5]);
  });
  it("hands back the recording untouched when it cannot be decoded, and a WAV as it is", async () => {
    const wav = new Blob(["x"], { type: "audio/wav" });
    expect(await toWav(wav)).toBe(wav);
    const odd = new Blob(["not audio"], { type: "audio/webm" });
    expect(await toWav(odd)).toBe(odd); // no audio context here: falls back, never loses the clip
  });
});
