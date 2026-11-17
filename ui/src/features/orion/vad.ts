/** Deciding when a person starts and stops talking, from the loudness of each moment. Pure: the caller supplies the clock. */

export interface VadConfig {
  /** Loudness (RMS, 0–1) above which a moment counts as speech. */
  threshold: number;
  /** Quiet this long after speech ends the utterance. */
  silenceMs: number;
  /** An utterance shorter than this is a click or a cough, not speech. */
  minSpeechMs: number;
  /** A hard stop, so a stuck microphone cannot record for ever. */
  maxMs: number;
}

export const DEFAULT_VAD: VadConfig = { threshold: 0.015, silenceMs: 900, minSpeechMs: 350, maxMs: 15000 };

export interface VadState { speechStart?: number; lastSpeech?: number }
export type VadEvent = "start" | "end" | "discard" | undefined;

/** One moment: `start` when speech begins, `end` when it has ended, `discard` when what began was too short to be speech. */
export function vadStep(state: VadState, rms: number, now: number, cfg: VadConfig = DEFAULT_VAD): { state: VadState; event: VadEvent } {
  const loud = rms >= cfg.threshold;
  if (state.speechStart === undefined) {
    return loud ? { state: { speechStart: now, lastSpeech: now }, event: "start" } : { state, event: undefined };
  }
  const last = loud ? now : state.lastSpeech ?? now;
  const spoke = last - state.speechStart;
  if (now - state.speechStart >= cfg.maxMs) return { state: {}, event: "end" };
  if (!loud && now - last >= cfg.silenceMs) return { state: {}, event: spoke >= cfg.minSpeechMs ? "end" : "discard" };
  return { state: { speechStart: state.speechStart, lastSpeech: last }, event: undefined };
}
