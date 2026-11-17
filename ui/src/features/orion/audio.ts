import { UtteranceCutter } from "./cutter";
import { DEFAULT_VAD, type VadConfig } from "./vad";
import { encodeWav, resample, SPEECH_RATE } from "./wav";

/** Everything Orion needs from the browser's audio: the microphone with a voice detector, and one place to play sound. */
export interface AudioEngine {
  /** The microphone's stream, for the orb to move with while he listens. */
  readonly input: MediaStream | null;
  /** The playback's analyser, for the orb to move with while he speaks. */
  readonly output: AnalyserNode | null;
  listen(onUtterance: (audio: Blob) => void): Promise<void>;
  /**
   * While he speaks the microphone stays open but only a voice clearly louder than his own playback, and short (an order
   * to stop, not a sentence), is let through. Off, it hears normally.
   */
  bargeIn(on: boolean): void;
  unlisten(): void;
  play(url: string): Promise<void>;
  stopPlayback(): void;
  dispose(): void;
}

/** While he speaks: how much louder than usual a voice must be, and the longest an order to stop can be. */
const BARGE_BOOST = 3;
const BARGE_MAX_SECONDS = 2.5;

const MIME_CHOICES = ["audio/webm;codecs=opus", "audio/webm", "audio/ogg;codecs=opus", "audio/mp4"];

function rms(buffer: Float32Array): number {
  let sum = 0;
  for (let i = 0; i < buffer.length; i += 1) sum += buffer[i]! * buffer[i]!;
  return Math.sqrt(sum / buffer.length);
}

/**
 * Opens the microphone, the chosen one when there is one. A saved device that is gone (unplugged, a new machine) falls
 * back to the system default rather than failing, so Orion still hears.
 */
export async function openMic(deviceId: string): Promise<MediaStream> {
  const base = { echoCancellation: true, noiseSuppression: true };
  if (deviceId) {
    try { return await navigator.mediaDevices.getUserMedia({ audio: { ...base, deviceId: { exact: deviceId } } }); }
    catch (cause) { if (!(cause instanceof DOMException) || (cause.name !== "OverconstrainedError" && cause.name !== "NotFoundError")) throw cause; }
  }
  return navigator.mediaDevices.getUserMedia({ audio: base });
}

export function createAudioEngine(vad: VadConfig = DEFAULT_VAD, micId: () => string = () => "", onBlocked: (blocked: boolean) => void = () => undefined): AudioEngine {
  let ctx: AudioContext | null = null;
  let stream: MediaStream | null = null;
  let source: MediaStreamAudioSourceNode | null = null;
  let processor: ScriptProcessorNode | null = null;
  let silence: GainNode | null = null;
  let cutter: UtteranceCutter | null = null;
  let opening: Promise<void> | undefined;
  let outAnalyser: AnalyserNode | null = null;
  let micAnalyser: AnalyserNode | null = null;
  let barge = false;
  let element: HTMLAudioElement | null = null;
  let finishPlayback: (() => void) | null = null;
  let stopGesture: (() => void) | undefined;

  // A page that has not been touched yet is not allowed to make sound or listen through an audio context: it stays
  // suspended and reads silence. Resume on the first click or key, and tell the app meanwhile so it can say so.
  const watchState = (c: AudioContext) => {
    const sync = () => {
      const blocked = c.state !== "running";
      onBlocked(blocked);
      if (!blocked) { stopGesture?.(); stopGesture = undefined; }
    };
    c.onstatechange = sync;
    if (c.state !== "running") {
      const resume = () => void c.resume();
      const events = ["pointerdown", "keydown", "touchstart"] as const;
      events.forEach((e) => window.addEventListener(e, resume, { passive: true }));
      stopGesture = () => events.forEach((e) => window.removeEventListener(e, resume));
      void c.resume().catch(() => undefined);
    }
    sync();
  };

  const context = () => {
    if (!ctx) { ctx = new AudioContext(); watchState(ctx); }
    else if (ctx.state === "suspended") void ctx.resume().catch(() => undefined);
    return ctx;
  };

  const open = async (onUtterance: (audio: Blob) => void) => {
    const mic = await openMic(micId());
    stream = mic;
    const c = context();
    source = c.createMediaStreamSource(mic);
    micAnalyser = c.createAnalyser();
    micAnalyser.fftSize = 512;
    source.connect(micAnalyser);
    // Raw samples, cut into utterances here and written as WAV: no compressed container, and the start of a word is kept.
    cutter = new UtteranceCutter(c.sampleRate, vad);
    cutter.boost = barge ? BARGE_BOOST : 1;
    processor = c.createScriptProcessor(4096, 1, 1);
    processor.onaudioprocess = (e) => {
      const heard = cutter?.push(e.inputBuffer.getChannelData(0), performance.now());
      // Speaking over him is short ("stop"); anything longer heard while he talks is his own voice coming back.
      if (heard && barge && heard.length / c.sampleRate > BARGE_MAX_SECONDS) return;
      if (heard && heard.length > 0) onUtterance(encodeWav(resample(heard, c.sampleRate, SPEECH_RATE), SPEECH_RATE));
    };
    // A processor only runs when it is connected onward; a zero-gain node keeps the microphone out of the speakers.
    silence = c.createGain();
    silence.gain.value = 0;
    source.connect(processor);
    processor.connect(silence);
    silence.connect(c.destination);
  };

  return {
    get input() { return stream; },
    get output() { return outAnalyser; },
    listen(onUtterance) {
      // One opening at a time: two callers asking together must not open two microphones.
      if (stream) return Promise.resolve();
      opening ??= open(onUtterance).finally(() => { opening = undefined; });
      return opening;
    },
    bargeIn(on) { barge = on; if (cutter) cutter.boost = on ? BARGE_BOOST : 1; },
    unlisten() {
      if (processor) processor.onaudioprocess = null;
      processor?.disconnect();
      silence?.disconnect();
      source?.disconnect();
      processor = null; silence = null; source = null; micAnalyser = null;
      cutter?.reset();
      cutter = null;
      stream?.getTracks().forEach((t) => t.stop());
      stream = null;
    },
    play(url) {
      const c = context();
      element ??= new Audio();
      const audio = element;
      if (!outAnalyser) {
        outAnalyser = c.createAnalyser();
        outAnalyser.fftSize = 512;
        c.createMediaElementSource(audio).connect(outAnalyser);
        outAnalyser.connect(c.destination);
      }
      return new Promise<void>((resolve, reject) => {
        finishPlayback = resolve;
        audio.onended = () => { finishPlayback = null; resolve(); };
        audio.onerror = () => { finishPlayback = null; reject(new Error("the audio could not be played")); };
        audio.src = url;
        audio.play().catch((e) => { finishPlayback = null; reject(e); });
      });
    },
    stopPlayback() {
      element?.pause();
      const done = finishPlayback;
      finishPlayback = null;
      done?.();
    },
    dispose() {
      this.unlisten();
      this.stopPlayback();
      stopGesture?.();
      void ctx?.close();
      ctx = null;
      outAnalyser = null;
    },
  };
}

/** A recording as base64, for the daemon. */
export function blobToBase64(blob: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onerror = () => reject(reader.error ?? new Error("the recording could not be read"));
    reader.onload = () => resolve(String(reader.result).replace(/^data:[^,]*,/, ""));
    reader.readAsDataURL(blob);
  });
}

/** Audio from the daemon as something the player can open. */
export function base64ToUrl(base64: string, mime: string): string {
  const raw = atob(base64);
  const bytes = new Uint8Array(raw.length);
  for (let i = 0; i < raw.length; i += 1) bytes[i] = raw.charCodeAt(i);
  return URL.createObjectURL(new Blob([bytes], { type: mime }));
}

export interface Clip { blob: Blob; ms: number; /** Loudest moment heard, 0–1 (RMS). Near zero means the microphone captured silence. */ peak: number }

/** Tracks the loudest moment of a stream. Best effort: a browser without audio analysis reports 0 and the clip is still returned. */
function trackPeak(stream: MediaStream): { peak: () => number; stop: () => void } {
  let peak = 0;
  try {
    const ctx = new AudioContext();
    const analyser = ctx.createAnalyser();
    analyser.fftSize = 1024;
    ctx.createMediaStreamSource(stream).connect(analyser);
    const frame = new Float32Array(analyser.fftSize);
    const timer = setInterval(() => { analyser.getFloatTimeDomainData(frame); peak = Math.max(peak, rms(frame)); }, 40);
    return { peak: () => peak, stop: () => { clearInterval(timer); void ctx.close(); } };
  } catch { return { peak: () => 0, stop: () => undefined }; }
}

/** Records the microphone for a fixed time and hands back the clip with how loud it got. For a one-off check, not for listening. */
export async function recordClip(ms: number, onStart?: () => void, deviceId = ""): Promise<Clip> {
  const stream = await openMic(deviceId);
  const meter = trackPeak(stream);
  const began = performance.now();
  try {
    const mimeType = MIME_CHOICES.find((m) => typeof MediaRecorder !== "undefined" && MediaRecorder.isTypeSupported(m));
    const recorder = new MediaRecorder(stream, mimeType ? { mimeType } : undefined);
    const chunks: Blob[] = [];
    recorder.ondataavailable = (e) => { if (e.data.size > 0) chunks.push(e.data); };
    const done = new Promise<Blob>((resolve) => { recorder.onstop = () => resolve(new Blob(chunks, { type: recorder.mimeType || "audio/webm" })); });
    recorder.start();
    onStart?.();
    setTimeout(() => { if (recorder.state !== "inactive") recorder.stop(); }, ms);
    const blob = await done;
    return { blob, ms: Math.round(performance.now() - began), peak: meter.peak() };
  } finally {
    meter.stop();
    stream.getTracks().forEach((t) => t.stop());
  }
}
