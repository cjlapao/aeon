import { connect, endpointPath, onClosed, type Negotiated, type Unlisten } from "./bridge";
import { registerResync } from "./eventGap";

export type LiveState =
  | { state: "connecting"; endpoint: string }
  | { state: "current"; endpoint: string; daemonVersion: string; protocolVersion: number; generation: number }
  | { state: "stale"; endpoint: string; since: number; cause: string; retry: number; daemonVersion?: string; protocolVersion?: number }
  | { state: "disconnected"; endpoint: string; reason: string };

export interface LiveTransport {
  connect: () => Promise<Negotiated>;
  endpointPath: () => Promise<string>;
  onClosed: (handler: (reason: string) => void) => Promise<Unlisten>;
}

const productionTransport: LiveTransport = {
  connect,
  endpointPath,
  onClosed: (handler) => onClosed((reason) => handler(typeof reason === "string" ? reason : "socket closed")),
};

const delays = [1000, 2000, 4000, 8000, 15000];

/**
 * One liveness owner per window. The shell probes the socket after 20 seconds
 * of silence and calls it dead after two unanswered probes (about 40 seconds).
 * This layer never infers death from an idle event stream and never polls data.
 */
export class LiveController {
  private stateValue: LiveState = { state: "connecting", endpoint: "unknown" };
  private readonly listeners = new Set<() => void>();
  private readonly readers = new Set<() => Promise<void>>();
  private unlisten?: Unlisten;
  private opening?: Promise<Unlisten>;
  private timer?: ReturnType<typeof setTimeout>;
  private run = 0;
  private generation = 0;
  private closed = false;

  private unregisterResync?: () => void;
  private lastResync = 0;
  private resyncing = false;

  constructor(private readonly transport: LiveTransport = productionTransport, private readonly now = Date.now) {}

  /**
   * Events were dropped on the way to this window. Re-read everything the way a reconnect does: every displayed reader runs,
   * then the generation moves so queries refetch and subscriptions start fresh. At most once a second, and never while one is
   * running, so a burst of reports is one re-read.
   */
  resync = async (): Promise<void> => {
    const state = this.stateValue;
    if (this.closed || state.state !== "current" || this.resyncing || this.now() - this.lastResync < 1000) return;
    this.resyncing = true;
    this.lastResync = this.now();
    try {
      await Promise.all([...this.readers].map((read) => read().catch(() => undefined)));
      const current = this.stateValue;
      if (!this.closed && current.state === "current") this.set({ ...current, generation: ++this.generation });
    } finally {
      this.resyncing = false;
    }
  };

  getState = (): LiveState => this.stateValue;
  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  };
  registerReader(reader: () => Promise<void>): () => void {
    this.readers.add(reader);
    return () => this.readers.delete(reader);
  }

  async start(): Promise<void> {
    this.closed = false;
    this.unregisterResync?.();
    this.unregisterResync = registerResync(() => void this.resync());
    const run = this.run;
    if (!this.unlisten) {
      // StrictMode may stop and restart while the first async listener is
      // opening. Serialize registration so an old cleanup cannot remove the
      // newer listener.
      if (this.opening) await this.opening;
      if (!this.unlisten) {
        const opening = this.transport.onClosed((reason) => this.lost(reason));
        this.opening = opening;
        const unlisten = await opening;
        if (this.opening === opening) this.opening = undefined;
        if (this.closed || run !== this.run) { unlisten(); return; }
        this.unlisten = unlisten;
      }
    }
    await this.reconnect();
  }

  async reconnect(): Promise<void> {
    const run = ++this.run;
    if (this.timer) clearTimeout(this.timer);
    const endpoint = await this.transport.endpointPath();
    if (run !== this.run || this.closed) return;
    this.set({ state: "connecting", endpoint });
    try {
      const negotiated = await this.transport.connect();
      if (run !== this.run || this.closed) return;
      // Every displayed snapshot reader runs again BEFORE this session is
      // labelled current. A filtered event seq gap cannot substitute for it.
      await Promise.all([...this.readers].map((read) => read()));
      if (run !== this.run || this.closed) return;
      this.set({ state: "current", endpoint: negotiated.endpoint || endpoint,
        daemonVersion: negotiated.daemonVersion, protocolVersion: negotiated.protocolVersion,
        generation: ++this.generation });
    } catch (error) {
      if (run !== this.run || this.closed) return;
      this.set({ state: "disconnected", endpoint, reason: String(error) });
    }
  }

  lost(reason: string): void {
    if (this.closed) return;
    const run = ++this.run;
    const previous = this.stateValue;
    const endpoint = previous.endpoint;
    const since = this.now();
    this.set({ state: "stale", endpoint, since, cause: reason, retry: 0,
      daemonVersion: previous.state === "current" ? previous.daemonVersion : previous.state === "stale" ? previous.daemonVersion : undefined,
      protocolVersion: previous.state === "current" ? previous.protocolVersion : previous.state === "stale" ? previous.protocolVersion : undefined });
    const attempt = async (index: number) => {
      if (run !== this.run || this.closed) return;
      try {
        const negotiated = await this.transport.connect();
        if (run !== this.run || this.closed) return;
        await Promise.all([...this.readers].map((read) => read()));
        if (run !== this.run || this.closed) return;
        this.set({ state: "current", endpoint: negotiated.endpoint || endpoint,
          daemonVersion: negotiated.daemonVersion, protocolVersion: negotiated.protocolVersion,
          generation: ++this.generation });
      } catch (error) {
        if (run !== this.run || this.closed) return;
        if (index + 1 >= delays.length) {
          this.set({ state: "disconnected", endpoint, reason: String(error) });
        } else {
          this.set({ state: "stale", endpoint, since, cause: String(error), retry: index + 1,
            daemonVersion: previous.state === "current" ? previous.daemonVersion : previous.state === "stale" ? previous.daemonVersion : undefined,
            protocolVersion: previous.state === "current" ? previous.protocolVersion : previous.state === "stale" ? previous.protocolVersion : undefined });
          this.timer = setTimeout(() => void attempt(index + 1), delays[index + 1]);
        }
      }
    };
    this.timer = setTimeout(() => void attempt(0), delays[0]);
  }

  ageSeconds(): number {
    return this.stateValue.state === "stale" ? Math.max(0, Math.floor((this.now() - this.stateValue.since) / 1000)) : 0;
  }

  stop(): void {
    this.unregisterResync?.();
    this.unregisterResync = undefined;
    this.closed = true;
    ++this.run;
    if (this.timer) clearTimeout(this.timer);
    this.unlisten?.();
    this.unlisten = undefined;
  }

  private set(state: LiveState): void {
    this.stateValue = state;
    for (const listener of this.listeners) listener();
  }
}
