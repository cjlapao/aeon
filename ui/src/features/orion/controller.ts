import { OperatorSession } from "./operator";
import { notificationPrompt, shouldReact } from "./notify";
import type { AppNotification } from "../../lib/notifications";
import { isStopCommand, matchWakeWord, flushSentences, speakable } from "./speech";
import { SpeechQueue } from "./speechQueue";
import { missingForOrion, type OrionSettings } from "./orionSettings";
import type { Rpc, Ui } from "./tools";

/** The parts of the browser's audio the controller uses, so it can be run against a fake. */
export interface OrionAudio {
  listen(onUtterance: (audio: Blob) => void): Promise<void>;
  unlisten(): void;
  bargeIn(on: boolean): void;
  play(url: string): Promise<void>;
  stopPlayback(): void;
}
export interface OrionCodec {
  /** Turns a recording into the form the speech model takes (WAV). Optional; absent sends it as recorded. */
  prepare?(blob: Blob): Promise<Blob>;
  toBase64(blob: Blob): Promise<string>;
  toUrl(base64: string, mime: string): string;
  release(url: string): void;
}

export type OrionPhase = "off" | "idle" | "awake" | "thinking" | "speaking" | "error";
export interface OrionSnapshot { phase: OrionPhase; heard?: string; said?: string; error?: string; missing: string[] }

/**
 * Orion's whole behaviour, apart from how it looks. Asleep he listens for the wake word (or, in click mode, not at all);
 * awake he hears what is said, hands it to the operator, and says the answer a sentence at a time, then stays awake for
 * a while so a follow-up needs no wake word. While he talks the microphone only lets a loud, short voice through, so he does not hear himself but can be told to stop.
 */
export class OrionController {
  private snap: OrionSnapshot = { phase: "off", missing: [] };
  private readonly listeners = new Set<(s: OrionSnapshot) => void>();
  private readonly session: OperatorSession;
  private readonly queue: SpeechQueue;
  private awakeTimer: ReturnType<typeof setTimeout> | undefined;
  private running = false;
  private busy = false;

  constructor(private readonly deps: { rpc: Rpc; audio: OrionAudio; codec: OrionCodec; settings: () => OrionSettings; ui?: Ui }) {
    this.session = new OperatorSession({ rpc: deps.rpc, route: () => deps.settings().operatorRoute, ui: deps.ui });
    this.queue = new SpeechQueue({
      synth: async (text) => {
        const s = deps.settings();
        const res = (await deps.rpc("voice.speak", { route: s.ttsRoute, text, voice: s.voice })) as { audio: string; mime: string };
        return deps.codec.toUrl(res.audio, res.mime);
      },
      play: (url) => deps.audio.play(url),
      stop: () => deps.audio.stopPlayback(),
      release: (url) => deps.codec.release(url),
    }, { onSpeaking: (on) => deps.audio.bargeIn(on), onError: (e) => this.fail(e) });
  }

  get snapshot(): OrionSnapshot { return this.snap; }
  subscribe(fn: (s: OrionSnapshot) => void): () => void { this.listeners.add(fn); return () => this.listeners.delete(fn); }
  private set(patch: Partial<OrionSnapshot>) { this.snap = { ...this.snap, ...patch }; this.listeners.forEach((l) => l(this.snap)); }
  private errorTimer: ReturnType<typeof setTimeout> | undefined;
  private fail(error: unknown) {
    this.set({ phase: "error", error: error instanceof Error ? error.message : String(error) });
    // The message stays long enough to read, then he goes back to waiting (and, by name, out of sight).
    clearTimeout(this.errorTimer);
    this.errorTimer = setTimeout(() => { if (this.running && this.snap.phase === "error") this.set({ phase: "idle", error: undefined }); }, 8000);
  }

  /** Turn Orion on. Refuses, naming what is missing, when a model is not chosen. */
  async start(): Promise<void> {
    const missing = missingForOrion(this.deps.settings());
    if (missing.length) { this.set({ phase: "off", missing, error: undefined }); return; }
    if (this.running) return; // already running: a second start must not open a second microphone
    this.running = true;
    this.set({ phase: "idle", missing: [], error: undefined });
    if (this.deps.settings().wakeMode === "word") await this.openMic();
  }

  /** The wake mode changed while he runs: open the microphone for the word, or close it for the click. Never starts him. */
  async applyWakeMode(): Promise<void> {
    if (!this.running) return;
    if (this.deps.settings().wakeMode === "word") await this.openMic();
    else if (this.snap.phase === "idle" || this.snap.phase === "error") this.deps.audio.unlisten();
  }

  stop(): void {
    this.running = false;
    clearTimeout(this.awakeTimer);
    clearTimeout(this.errorTimer);
    this.queue.stop();
    this.deps.audio.unlisten();
    this.session.reset();
    this.set({ phase: "off", heard: undefined, said: undefined, error: undefined });
  }

  private async openMic(): Promise<void> {
    try { await this.deps.audio.listen((blob) => void this.onUtterance(blob)); }
    catch (cause) { this.fail(cause instanceof Error ? new Error(`the microphone is not available: ${cause.message}`) : cause); }
  }

  /** The orb was clicked: wake him now, or put him back to sleep, or cut him off while he talks. */
  async toggle(): Promise<void> {
    if (!this.running) return;
    if (this.snap.phase === "speaking") { this.queue.stop(); this.rearm(); return; }
    if (this.snap.phase === "awake") { this.sleep(); return; }
    if (this.snap.phase === "idle" || this.snap.phase === "error") { await this.wake(); }
  }

  private async wake(): Promise<void> {
    if (this.deps.settings().wakeMode === "click") await this.openMic();
    this.set({ phase: "awake", error: undefined });
    this.rearm();
  }

  private sleep(): void {
    clearTimeout(this.awakeTimer);
    if (this.deps.settings().wakeMode === "click") this.deps.audio.unlisten();
    this.set({ phase: "idle" });
  }

  private rearm(): void {
    clearTimeout(this.awakeTimer);
    const seconds = this.deps.settings().followUpSeconds;
    if (this.snap.phase === "awake" && seconds > 0) this.awakeTimer = setTimeout(() => this.sleep(), seconds * 1000);
  }

  /** "Stop": cut off what he is saying (or the answer he is working out), and go back to waiting for his name. */
  private stopNow(): void {
    this.turn += 1; // an answer still being worked out is dropped when it arrives
    this.queue.stop();
    this.set({ said: undefined, heard: undefined });
    this.sleep();
  }

  private turn = 0;
  private bargeBusy = false;

  /** While he speaks or thinks, the only thing worth hearing is an order to stop. */
  private async onBarge(blob: Blob): Promise<void> {
    if (this.bargeBusy) return;
    this.bargeBusy = true;
    try {
      const s = this.deps.settings();
      const sent = this.deps.codec.prepare ? await this.deps.codec.prepare(blob) : blob;
      const res = (await this.deps.rpc("voice.transcribe", { route: s.asrRoute, audio: await this.deps.codec.toBase64(sent), mime: sent.type, language: s.language || undefined })) as { text: string };
      if (this.running && (this.snap.phase === "speaking" || this.snap.phase === "thinking") && isStopCommand(res.text)) this.stopNow();
    } catch { /* a failed check on a stray sound is not worth interrupting him for */ }
    finally { this.bargeBusy = false; }
  }

  private async onUtterance(blob: Blob): Promise<void> {
    if (!this.running) return;
    if (this.snap.phase === "speaking" || this.snap.phase === "thinking") { await this.onBarge(blob); return; }
    if (this.busy) return;
    this.busy = true;
    try {
      const s = this.deps.settings();
      const asleep = this.snap.phase === "idle" || this.snap.phase === "error";
      const sent = this.deps.codec.prepare ? await this.deps.codec.prepare(blob) : blob;
      const res = (await this.deps.rpc("voice.transcribe", { route: s.asrRoute, audio: await this.deps.codec.toBase64(sent), mime: sent.type, language: s.language || undefined })) as { text: string };
      const heard = res.text.trim();
      if (!heard) return;
      if (isStopCommand(heard)) { if (this.snap.phase === "awake") this.stopNow(); return; } // nothing to stop asleep; awake, it sends him back to waiting
      let request = heard;
      if (asleep) {
        const wake = matchWakeWord(heard, s.wakeWord);
        if (!wake.matched) return; // not for him; nothing is kept
        this.set({ phase: "awake", error: undefined });
        request = wake.rest.trim();
        if (!request) { this.set({ heard }); this.say("Yes?"); this.rearm(); return; }
      }
      await this.respond(request);
    } catch (cause) { this.fail(cause); }
    finally { this.busy = false; }
  }

  /** Hand what was said to the operator and say what comes back. */
  async respond(request: string, shown = request, fromPerson = true): Promise<void> {
    clearTimeout(this.awakeTimer);
    this.set({ phase: "thinking", heard: shown });
    const mine = this.turn;
    const reply = await this.session.handle(request, fromPerson);
    if (mine !== this.turn) return; // he was told to stop while working it out
    this.set({ said: reply });
    this.say(reply);
  }

  private inbox: AppNotification[] = [];

  /**
   * The app raised a notification. If he is enabled to react to it, he is woken (the orb shows) and told, in his own
   * conversation, with no recording and no transcription: the text goes straight to the operator. If he is already
   * talking or working something out, it waits its turn, so he never talks over himself.
   */
  async react(n: AppNotification): Promise<void> {
    if (!this.running || !shouldReact(this.deps.settings().reactTo, n)) return;
    if (this.inbox.length >= 5) this.inbox.shift(); // a flood keeps the newest few
    this.inbox.push(n);
    await this.drain();
  }

  private async drain(): Promise<void> {
    if (!this.running || this.reacting || this.inbox.length === 0) return;
    // While he waits for the person's yes or no, a notification waits too: it must not cut in on a confirmation.
    if (this.snap.phase === "speaking" || this.snap.phase === "thinking" || this.busy || this.session.awaitingConfirmation) return;
    const next = this.inbox.shift()!;
    this.reacting = true;
    try {
      await this.wake();
      await this.respond(notificationPrompt(next), next.title, false);
    } catch (cause) { this.fail(cause); }
    finally { this.reacting = false; }
  }

  private reacting = false;

  private say(text: string): void {
    const sentences = flushSentences(speakable(text));
    if (sentences.length === 0) { this.set({ phase: "awake" }); this.rearm(); return; }
    this.set({ phase: "speaking" });
    sentences.forEach((x) => this.queue.say(x));
    void this.queue.idle().then(() => {
      if (!this.running) return;
      if (this.snap.phase === "speaking") { this.set({ phase: "awake" }); this.rearm(); }
      void this.drain(); // the next notification that waited, if any
    });
  }
}
