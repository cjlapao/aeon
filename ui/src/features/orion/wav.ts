/** Uncompressed audio, which every speech-to-text server accepts. Browsers record compressed (WebM/Opus), which many servers do not. */

/** 16-bit mono PCM in a WAV container. Pure: samples in, bytes out. */
export function encodeWav(samples: Float32Array, sampleRate: number): Blob {
  const bytes = samples.length * 2;
  const buf = new ArrayBuffer(44 + bytes);
  const v = new DataView(buf);
  const text = (at: number, s: string) => { for (let i = 0; i < s.length; i += 1) v.setUint8(at + i, s.charCodeAt(i)); };
  text(0, "RIFF"); v.setUint32(4, 36 + bytes, true); text(8, "WAVE");
  text(12, "fmt "); v.setUint32(16, 16, true); v.setUint16(20, 1, true); v.setUint16(22, 1, true);
  v.setUint32(24, sampleRate, true); v.setUint32(28, sampleRate * 2, true); v.setUint16(32, 2, true); v.setUint16(34, 16, true);
  text(36, "data"); v.setUint32(40, bytes, true);
  for (let i = 0; i < samples.length; i += 1) {
    const x = Math.max(-1, Math.min(1, samples[i]!));
    v.setInt16(44 + i * 2, x < 0 ? x * 0x8000 : x * 0x7fff, true);
  }
  return new Blob([buf], { type: "audio/wav" });
}

/** Several channels averaged into one. */
export function toMono(channels: readonly Float32Array[]): Float32Array {
  if (channels.length === 1) return channels[0]!;
  const out = new Float32Array(channels[0]!.length);
  for (const ch of channels) for (let i = 0; i < out.length; i += 1) out[i]! += ch[i]! / channels.length;
  return out;
}

/** Samples at another rate, by linear interpolation: enough for speech, with no audio context needed. */
export function resample(samples: Float32Array, from: number, to: number): Float32Array {
  if (from === to || samples.length === 0) return samples;
  const out = new Float32Array(Math.max(1, Math.round((samples.length * to) / from)));
  const ratio = from / to;
  for (let i = 0; i < out.length; i += 1) {
    const at = i * ratio;
    const lo = Math.floor(at);
    const hi = Math.min(samples.length - 1, lo + 1);
    out[i] = samples[lo]! + (samples[hi]! - samples[lo]!) * (at - lo);
  }
  return out;
}

/** Speech models work at 16 kHz; a recording is 44.1 or 48 kHz. */
export const SPEECH_RATE = 16000;

/**
 * A recording as 16 kHz mono WAV. When the browser cannot decode it the original is returned untouched, so a failure
 * here never loses the recording, it only sends it in the format it was made in.
 */
export async function toWav(blob: Blob): Promise<Blob> {
  if (blob.type.includes("wav")) return blob;
  try {
    const ctx = new AudioContext();
    const decoded = await ctx.decodeAudioData(await blobBytes(blob));
    void ctx.close();
    const frames = Math.max(1, Math.ceil(decoded.duration * SPEECH_RATE));
    const offline = new OfflineAudioContext(1, frames, SPEECH_RATE);
    const source = offline.createBufferSource();
    source.buffer = decoded;
    source.connect(offline.destination);
    source.start();
    const rendered = await offline.startRendering();
    return encodeWav(rendered.getChannelData(0), SPEECH_RATE);
  } catch {
    return blob;
  }
}

function blobBytes(blob: Blob): Promise<ArrayBuffer> {
  if (typeof blob.arrayBuffer === "function") return blob.arrayBuffer();
  return new Promise((resolve, reject) => {
    const r = new FileReader();
    r.onerror = () => reject(r.error);
    r.onload = () => resolve(r.result as ArrayBuffer);
    r.readAsArrayBuffer(blob);
  });
}
