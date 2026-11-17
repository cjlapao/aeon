import { fireEvent, screen } from "@testing-library/react";
import { setInvoker, setSubscriber, resetBridge, type EventListener } from "../src/lib/bridge";

/**
 * A fake daemon for the web layer.
 *
 * It records **every call and its params**, which is what makes the security
 * assertions possible: a test can prove the page never sent a credential it was
 * not given, and never asked for one back.
 */
export interface FakeDaemon {
  calls: { method: string; params: unknown }[];
  /** Push an event, as the Rust shell would when the daemon notifies. */
  emit: (payload: unknown) => void;
  /** Push a terminal notification (`terminal.output` / `terminal.exit`), as the shell does on `aeon://terminal`. */
  emitTerminal: (payload: unknown) => void;
  /** Push a connection-closed notice. */
  close: (reason: string) => void;
  /** S58 T8: emit on `aeon://navigate`, as the shell does for a tray click or an accepted link. */
  emitNavigate: (target: unknown) => void;
  /** S58 T8: emit on `aeon://navigate-refused` (a fixed reason word, as the shell sends). */
  emitNavigateRefused: (reason: unknown) => void;
  /** Every shell command the page invoked (not protocol methods), with its arguments. */
  shellCalls: { cmd: string; args: Record<string, unknown> | undefined }[];
  /** Every `rpc_call` method name, in order. */
  methods: () => string[];
  /** The params of the last call to a method. */
  paramsOf: (method: string) => unknown;
  /**
   * Every Tauri command the page invoked, in order — `connect` and `disconnect`
   * included. `calls` counts protocol methods; this counts the shell's, which is
   * where a duplicated handshake shows up.
   */
  commands: () => string[];
  /** How many times one command ran. */
  countOf: (cmd: string) => number;
  /**
   * Every event channel the page **asked** to subscribe to, in order.
   *
   * This is the **witness for StrictMode**: an effect that runs, cleans up and
   * runs again leaves two entries here. A test asserting "exactly one connect"
   * is worth nothing unless something proves the double-invoke actually
   * happened, and this is that something.
   */
  subscribes: () => string[];
}

export interface FakeOptions {
  /**
   * Make `listen` fail, as Tauri's ACL does when no capability grants
   * `core:event:default`.
   *
   * This is the shape of the real bug: **every `invoke` succeeds and every
   * stream is silent**, because app commands are not ACL-gated and `listen`
   * is. A fake that always succeeds cannot reproduce it.
   */
  subscribeFails?: string;
  /** Answers per Tauri shell command (`workspace_list`, …) — commands that are not daemon protocol methods. */
  shell?: Record<string, unknown | ((args: Record<string, unknown> | undefined) => unknown)>;
  /** Answers per protocol method. A function may throw to simulate an error. */
  responses?: Record<string, unknown | ((params: unknown) => unknown)>;
  /**
   * What `connect` returns, or an Error to throw.
   *
   * A function is called with the **1-based handshake number**, so a test can
   * reproduce the real shape — the daemon is not answering at first, and is
   * again by the time the person has read the pill and clicked Reconnect.
   */
  negotiated?: object | ((attempt: number) => unknown);
  endpoint?: string;
}

export function installFakeDaemon(opts: FakeOptions = {}): FakeDaemon {
  const calls: { method: string; params: unknown }[] = [];
  const commands: string[] = [];
  const subs: string[] = [];
  const shellCalls: { cmd: string; args: Record<string, unknown> | undefined }[] = [];
  // Sets, not single variables: the real Tauri `listen` lets any number of
  // independent callers subscribe to the same channel, each getting every
  // event — exactly what the always-mounted global notification listener
  // and a screen's own narrower listener both do at once (S41). A single
  // slot per channel would make the second subscriber silently overwrite
  // the first's handler, which is a bug this fake must not have even though
  // most tests only ever register one.
  const eventHandlers = new Set<EventListener>();
  const navigateHandlers = new Set<EventListener>();
  const refusedHandlers = new Set<EventListener>();
  const closedHandlers = new Set<EventListener>();
  const terminalHandlers = new Set<EventListener>();
  let handshakes = 0;

  setInvoker(async (cmd: string, args?: Record<string, unknown>) => {
    commands.push(cmd);
    if (cmd === "endpoint_path") return opts.endpoint ?? "/run/user/1000/aeon/rpc.sock";
    if (cmd === "disconnect") return null;
    if (cmd === "connect") {
      handshakes += 1;
      const answer =
        typeof opts.negotiated === "function"
          ? (opts.negotiated as (attempt: number) => unknown)(handshakes)
          : opts.negotiated;
      if (answer instanceof Error) throw answer.message;
      return (
        answer ?? {
          daemonVersion: "9.9.9-fake",
          protocolVersion: 7,
          endpoint: opts.endpoint ?? "/run/user/1000/aeon/rpc.sock",
        }
      );
    }
    if (cmd === "rpc_call") {
      const method = String(args?.method);
      const params = args?.params;
      calls.push({ method, params });

      const answer = opts.responses?.[method];
      if (typeof answer === "function") {
        return (answer as (p: unknown) => unknown)(params);
      }
      if (answer !== undefined) return answer;
      return {};
    }
    if (opts.shell && cmd in opts.shell) {
      shellCalls.push({ cmd, args });
      const answer = opts.shell[cmd];
      return typeof answer === "function" ? (answer as (a: Record<string, unknown> | undefined) => unknown)(args) : answer;
    }
    throw new Error(`the page called an unexpected Tauri command: ${cmd}`);
  });

  setSubscriber(async (channel: string, handler: EventListener) => {
    // Recorded **before** the refusal: a page that asked and was refused still
    // asked, and the count is the point here.
    subs.push(channel);
    if (opts.subscribeFails) {
      throw opts.subscribeFails;
    }
    if (channel === "aeon://event") eventHandlers.add(handler);
    if (channel === "aeon://closed") closedHandlers.add(handler);
    if (channel === "aeon://terminal") terminalHandlers.add(handler);
    if (channel === "aeon://navigate") navigateHandlers.add(handler);
    if (channel === "aeon://navigate-refused") refusedHandlers.add(handler);
    return () => {
      if (channel === "aeon://event") eventHandlers.delete(handler);
      if (channel === "aeon://closed") closedHandlers.delete(handler);
      if (channel === "aeon://terminal") terminalHandlers.delete(handler);
      if (channel === "aeon://navigate") navigateHandlers.delete(handler);
      if (channel === "aeon://navigate-refused") refusedHandlers.delete(handler);
    };
  });

  return {
    calls,
    shellCalls,
    emit: (payload) => eventHandlers.forEach((handler) => handler(payload)),
    emitTerminal: (payload) => terminalHandlers.forEach((handler) => handler(payload)),
    close: (reason) => closedHandlers.forEach((handler) => handler(reason)),
    emitNavigate: (target) => navigateHandlers.forEach((handler) => handler(target)),
    emitNavigateRefused: (reason) => refusedHandlers.forEach((handler) => handler(reason)),
    methods: () => calls.map((c) => c.method),
    paramsOf: (method) => calls.filter((c) => c.method === method).at(-1)?.params,
    commands: () => [...commands],
    countOf: (cmd) => commands.filter((c) => c === cmd).length,
    subscribes: () => [...subs],
  };
}

export function resetFakeDaemon(): void {
  resetBridge();
}

/**
 * A `backup.status` answer in the daemon's `never` state — no backup has
 * ever run, so no file, age or verify fact is invented.
 *
 * Why the helper carries this: the fake's generic fallback for an unconfigured
 * method is `{}`, which is truthy. A screen that treats any truthy answer as a
 * valid status — the backups header, whose `!status` branch is its honest
 * third state — would then render the body of an answer it was never given
 * and crash on the contract-required `db` struct. A real daemon cannot send
 * `{}` for `backup.status`: the contract demands the full struct, or an error.
 * A test that mounts the settings screen therefore answers this method for
 * real, with this fixture, instead of leaning on the fallback.
 */
export function backupStatus(overrides: Record<string, unknown> = {}) {
  return {
    ageSeconds: 0,
    db: { applied: 43, bytes: 0, journalMode: "wal", pending: 0, walBytes: 0 },
    dir: "",
    enabled: false,
    lastSuccess: "",
    lastVerified: "",
    nextDueAt: "",
    running: false,
    settings: [],
    state: "never",
    ...overrides,
  };
}

/** A route as the daemon reports it — **never with a key**. */
export function routeView(overrides: Record<string, unknown> = {}) {
  return {
    id: "anthropic",
    provider: "anthropic",
    model: "claude-sonnet-5",
    hasKey: true,
    keyHint: "…wxyz",
    source: "environment",
    maxOutputTokens: 4096,
    prices: { inputUsdPerMTok: 3, outputUsdPerMTok: 15 },
    priced: true,
    ...overrides,
  };
}

export function routesList(routes: unknown[] = [routeView()], persisted = false) {
  return { routes, providers: ["anthropic", "openai"], persisted };
}

// The app chrome's app-menu navigation, shared by the split product-surface
// test files: Settings rides the user menu (the joined rail retired the
// header that carried it), the other destinations are rail links.
export function chooseAppMenu(label: "System" | "Settings" | "Test bench") {
  if (label === "Settings") {
    fireEvent.click(screen.getByRole("button", { name: "User menu" }));
    fireEvent.click(screen.getByRole("menuitem", { name: "Settings" }));
    return;
  }
  fireEvent.click(screen.getByRole("link", { name: label }));
}
