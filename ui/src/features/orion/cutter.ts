import { DEFAULT_VAD, vadStep, type VadConfig, type VadState } from "./vad";

/** Loudness (RMS) of a block of samples. */
export function blockRms(buffer: Float32Array): number {
  let sum = 0;
  for (let i = 0; i < buffer.length; i += 1) sum += buffer[i]! * buffer[i]!;
  return Math.sqrt(sum / (buffer.length || 1));
}

/**
 * Cuts a continuous stream of microphone blocks into utterances. It keeps the last half second before speech is noticed
 * (the start of a word is quiet and would be clipped, and a clipped name is not recognised), follows the room's noise
 * floor so a noisy room does not hold the microphone open and a quiet one is still heard, and hands back each finished
 * utterance as raw samples. Pure: the caller supplies blocks and the clock.
 */
export class UtteranceCutter {
  private state: VadState = {};
  private floor = 0;
  private preroll: Float32Array[] = [];
  private prerollSamples = 0;
  private current: Float32Array[] | undefined;
  /** Multiplies the speech threshold: raised while he talks, so only a voice clearly louder than his own playback counts. */
  boost = 1;

  constructor(private readonly sampleRate: number, private readonly cfg: VadConfig = DEFAULT_VAD, private readonly prerollMs = 500) {}

  /** The loudness above which a block counts as speech right now: the configured floor, or well above the room's noise. */
  get threshold(): number { return Math.max(this.cfg.threshold, this.floor * 3) * this.boost; }
  get speaking(): boolean { return this.current !== undefined; }

  /** One block in; a finished utterance out, or nothing yet. `muted` blocks count as silence (he is speaking). */
  push(block: Float32Array, now: number, muted = false): Float32Array | undefined {
    const level = muted ? 0 : blockRms(block);
    const step = vadStep(this.state, level, now, { ...this.cfg, threshold: this.threshold });
    this.state = step.state;
    if (this.current === undefined && step.event !== "start") {
      // Quiet: learn the noise floor (slowly rising, quickly falling) and remember the block as pre-roll.
      if (!muted) this.floor = level > this.floor ? this.floor * 0.95 + level * 0.05 : this.floor * 0.8 + level * 0.2;
      this.remember(block);
      return undefined;
    }
    if (step.event === "start") { this.current = [...this.preroll]; this.preroll = []; this.prerollSamples = 0; }
    this.current!.push(block.slice());
    if (step.event === "end") {
      const heard = this.finish();
      // Speech that never paused until the hard stop is not speech: it is a fan, a hum, a room. Learn it as the floor so the
      // threshold rises above it, and throw the stretch away rather than send a minute of noise to be transcribed.
      if ((heard.length / this.sampleRate) * 1000 >= this.cfg.maxMs * 0.9) { this.floor = Math.max(this.floor, blockRms(heard)); return undefined; }
      return heard;
    }
    if (step.event === "discard") { this.current = undefined; }
    return undefined;
  }

  private remember(block: Float32Array): void {
    this.preroll.push(block.slice());
    this.prerollSamples += block.length;
    const max = (this.sampleRate * this.prerollMs) / 1000;
    while (this.prerollSamples > max && this.preroll.length > 1) this.prerollSamples -= this.preroll.shift()!.length;
  }

  private finish(): Float32Array {
    const parts = this.current ?? [];
    this.current = undefined;
    const out = new Float32Array(parts.reduce((n, p) => n + p.length, 0));
    let at = 0;
    for (const p of parts) { out.set(p, at); at += p.length; }
    return out;
  }

  /** Drop what is in progress (the microphone was closed). */
  reset(): void { this.state = {}; this.current = undefined; this.preroll = []; this.prerollSamples = 0; }
}
