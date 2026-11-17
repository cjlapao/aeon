/**
 * Says sentences one after another, with the next one already being made while the current one plays, and with every
 * sentence kept after it is made so saying it again costs nothing. The audio itself is the caller's: `synth` turns a
 * sentence into something playable, `play` plays it and resolves when it ends, `stop` cuts it off.
 */
export interface SpeechDeps {
  synth(text: string): Promise<string>;
  play(url: string): Promise<void>;
  stop(): void;
  /** Called when a made sentence leaves the cache, so its resource can be freed. */
  release?(url: string): void;
}

export interface SpeechQueueOptions {
  /** How many made sentences are kept. */
  cacheSize?: number;
  onSpeaking?(speaking: boolean): void;
  onError?(error: unknown, sentence: string): void;
}

export class SpeechQueue {
  private readonly queue: string[] = [];
  private readonly made = new Map<string, Promise<string>>();
  private running = false;
  private generation = 0;
  private idleWaiters: (() => void)[] = [];

  constructor(private readonly deps: SpeechDeps, private readonly opts: SpeechQueueOptions = {}) {}

  /** Add a sentence to be said. The one after the current is made at once. */
  say(sentence: string): void {
    const text = sentence.trim();
    if (!text) return;
    this.queue.push(text);
    if (this.running) this.prefetch(this.queue[0]);
    else void this.pump();
  }

  /** Cut off what is playing and forget what is waiting (the person spoke, or asked it to stop). */
  stop(): void {
    this.generation += 1;
    this.queue.length = 0;
    this.deps.stop();
  }

  /** Resolves when nothing is playing or waiting. */
  idle(): Promise<void> {
    return !this.running && this.queue.length === 0 ? Promise.resolve() : new Promise((resolve) => this.idleWaiters.push(resolve));
  }

  get busy(): boolean { return this.running || this.queue.length > 0; }

  private get(text: string): Promise<string> {
    let p = this.made.get(text);
    if (!p) {
      p = this.deps.synth(text);
      p.catch(() => this.made.delete(text)); // a failed sentence is not kept
      this.made.set(text, p);
      this.trim();
    }
    return p;
  }

  private prefetch(text: string | undefined): void {
    if (text) void this.get(text).catch(() => undefined);
  }

  private trim(): void {
    const max = this.opts.cacheSize ?? 40;
    while (this.made.size > max) {
      const oldest = this.made.keys().next().value as string | undefined;
      if (oldest === undefined) break;
      const p = this.made.get(oldest);
      this.made.delete(oldest);
      void p?.then((url) => this.deps.release?.(url), () => undefined);
    }
  }

  private async pump(): Promise<void> {
    this.running = true;
    this.opts.onSpeaking?.(true);
    const mine = this.generation;
    try {
      while (this.queue.length > 0 && this.generation === mine) {
        const text = this.queue.shift()!;
        this.prefetch(this.queue[0]);
        try {
          const url = await this.get(text);
          if (this.generation !== mine) break;
          await this.deps.play(url);
        } catch (error) {
          this.opts.onError?.(error, text);
        }
      }
    } finally {
      this.running = false;
      this.opts.onSpeaking?.(false);
      const waiters = this.idleWaiters;
      this.idleWaiters = [];
      waiters.forEach((w) => w());
      // A sentence that arrived while the loop was finishing must not be left unsaid.
      if (this.queue.length > 0) void this.pump();
    }
  }
}
