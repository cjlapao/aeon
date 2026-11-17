// The bridge to the daemon.
import type { RpcMethods } from "../proto/contract";
import { isKnownEventKind } from "../proto/contract";
//
// # One generic call, not one function per method
//
// S9's U11 asks for this: **the method table lives in `internal/protocol` and
// nowhere else.** A function per method would be a second copy of it, in
// TypeScript, that every later slice has to remember to update — and the one
// somebody forgets is the method that silently cannot be called from the app.
//
// The cost is that callers name methods as strings. That is the right trade: a
// typo is a `-32601` from the daemon, which is a clear failure, where a missing
// wrapper is a feature that quietly does not exist.
//
// # Nothing here opens a socket, and nothing here may
//
// A browser cannot speak a unix socket, which is why the Tauri shell is the UI
// rather than optional packaging. Every byte on the wire is the Rust side's.
// `test/no-socket-in-web-layer.test.ts` asserts this stays true.

export type Invoker = (cmd: string, args?: Record<string, unknown>) => Promise<unknown>;
export type EventListener = (payload: unknown) => void;
export type Unlisten = () => void;
export type Subscriber = (channel: string, handler: EventListener) => Promise<Unlisten>;

/** What one successful handshake established. */
export interface Negotiated {
  /** The DAEMON's build, not this app's. */
  daemonVersion: string;
  /** The NEGOTIATED protocol version for this connection. */
  protocolVersion: number;
  endpoint: string;
}

/**
 * An error from the daemon, with Aeon's three parts where it has them.
 *
 * `data` carries Aeon's explanatory parts when present. `kind` independently
 * distinguishes a daemon refusal, timeout, and broken connection.
 */
export interface RpcErrorData {
  what: string;
  means: string;
  todo: string;
  fatal: boolean;
}

export class RpcError extends Error {
  readonly code: number;
  readonly data?: RpcErrorData;
  readonly kind: "transport" | "timeout" | "rpc";

  constructor(code: number, message: string, data?: RpcErrorData, kind?: "transport" | "timeout" | "rpc") {
    super(message);
    this.name = "RpcError";
    this.code = code;
    this.data = data;
    this.kind = kind ?? (code < 0 ? "rpc" : "transport");
  }

  /** True when the failure never reached the wire. */
  get isTransport(): boolean {
    return this.kind === "transport";
  }
}

/**
 * The sentence to show a person for a failed call. A refusal code ("…refused")
 * carries the domain's own sentence in `data.what` — byte for byte, naming what
 * was refused and usually what would pass — and the code's `message` is only the
 * generic title ("the MCP management door refused"). Everything else keeps its
 * message.
 */
export function errorSentence(error: RpcError): string {
  const what = error.data?.what;
  return error.kind === "rpc" && what && /\brefused$/i.test(error.message) ? what : error.message;
}

/**
 * What to show for any failed call: the daemon's own sentence (`data.what`) whenever it gave one, else the message. A
 * title such as "invalid route" says nothing about which part was wrong; the sentence under it does ("has no price and
 * no noPrice declaration"). Unlike `errorSentence`, this does not wait for a code that ends in "refused".
 */
export function describeError(thrown: unknown): string {
  const error = toRpcError(thrown);
  const what = error.data?.what;
  return error.kind === "rpc" && typeof what === "string" && what !== "" ? what : error.message;
}

/**
 * The whole refusal as one body for a toast: the daemon's sentence, then what it means and what
 * to do about it when the daemon said so — one part per line, nothing paraphrased.
 */
export function refusalBody(error: RpcError): string {
  return [errorSentence(error), error.data?.means, error.data?.todo]
    .filter((part): part is string => typeof part === "string" && part !== "")
    .join("\n");
}

/**
 * Parse what the Rust side threw.
 *
 * A Tauri command's error is a string, so the shell serialises its structured
 * error as JSON. **A string that is not JSON is still an error**, and it is
 * reported rather than swallowed — that path carries the shell's own failures
 * ("not connected to the daemon"), which are the ones a user sees first.
 */
export function toRpcError(thrown: unknown): RpcError {
  if (thrown instanceof RpcError) return thrown;

  const text = typeof thrown === "string" ? thrown : String(thrown);
  try {
    const parsed = JSON.parse(text) as { code?: unknown; message?: unknown; data?: unknown; kind?: unknown };
    if (typeof parsed.code === "number" && typeof parsed.message === "string") {
      const data = parsed.data as RpcErrorData | undefined;
      const kind = parsed.kind === "transport" || parsed.kind === "timeout" || parsed.kind === "rpc" ? parsed.kind : undefined;
      return new RpcError(parsed.code, parsed.message, data, kind);
    }
  } catch {
    // Not JSON. Fall through.
  }
  return new RpcError(0, text);
}

// --- the injected runtime ----------------------------------------------------

async function tauriInvoke(cmd: string, args?: Record<string, unknown>): Promise<unknown> {
  const { invoke } = await import("@tauri-apps/api/core");
  return invoke(cmd, args);
}

async function tauriSubscribe(channel: string, handler: EventListener): Promise<Unlisten> {
  const { listen } = await import("@tauri-apps/api/event");
  const unlisten = await listen(channel, (event) => handler(event.payload));
  return unlisten;
}

let invoker: Invoker = tauriInvoke;
let subscriber: Subscriber = tauriSubscribe;

/** Replace the runtime. Tests use this; nothing else should. */
export function setInvoker(next: Invoker): void {
  invoker = next;
}

export function setSubscriber(next: Subscriber): void {
  subscriber = next;
}

export function resetBridge(): void {
  invoker = tauriInvoke;
  subscriber = tauriSubscribe;
}

/** Invoke a local shell feature that does not belong to the daemon protocol. */
export async function invokeShell<T>(cmd: string, args: Record<string, unknown>): Promise<T> {
  return invoker(cmd, args) as Promise<T>;
}

// --- the surface -------------------------------------------------------------

/** The Tauri event channels the shell emits on. */
export const EVENT_CHANNEL = "aeon://event";
export const CLOSED_CHANNEL = "aeon://closed";
/**
 * Where the shell forwards `terminal.output` and `terminal.exit` params
 * (S31 T8, D10). Separate from `EVENT_CHANNEL` because these are **not**
 * events: they carry no `kind`, they never entered the `EventLog` (LW7), and
 * a screen listens only while a terminal is open.
 */
export const TERMINAL_CHANNEL = "aeon://terminal";
/**
 * S58 T7/T8: the two words the shell says about a deep link or a tray
 * Inbox click — an accepted target (the canonical string, never the raw
 * URL) and a refusal (a fixed reason word, never the raw URL either).
 * Listening is already inside `core:event:default`; these channels are why
 * the capabilities file stayed at four permissions.
 */
export const NAVIGATE_CHANNEL = "aeon://navigate";
export const NAVIGATE_REFUSED_CHANNEL = "aeon://navigate-refused";

/** Connect and negotiate. Performs `initialize` first, always (T7). */
export async function connect(): Promise<Negotiated> {
  try {
    const raw = await invoker("connect");
    return validateNegotiated(raw);
  } catch (e) {
    throw toRpcError(e);
  }
}

export async function disconnect(): Promise<void> {
  try {
    await invoker("disconnect");
  } catch {
    // Disconnecting cannot meaningfully fail, and a UI tearing down should not
    // be stopped by it.
  }
}

/**
 * Call any protocol method.
 *
 * `params` is passed through untouched — this layer does not know the method
 * table, deliberately.
 */
export async function rpc<T>(method: string, params?: unknown): Promise<T> {
  try {
    return (await invoker("rpc_call", { method, params: params ?? null })) as T;
  } catch (e) {
    throw toRpcError(e);
  }
}

/** Fixture-derived contract for product screens; the generic bench call stays available. */
export function rpcTyped<K extends keyof RpcMethods>(
  method: K,
  params: RpcMethods[K]["params"],
): Promise<RpcMethods[K]["result"]> {
  return rpc<RpcMethods[K]["result"]>(method, params);
}

/** Where the shell looked, so a failure can name the path it tried. */
export async function endpointPath(): Promise<string> {
  try {
    const raw = await invoker("endpoint_path");
    return typeof raw === "string" && raw !== "" ? raw : "unknown";
  } catch {
    return "unknown";
  }
}

// --- workspaces (S41) ---------------------------------------------------

/**
 * A daemon this app can talk to. `Local` always exists; any number of
 * `Remote` entries can be added. Never carries a credential — see
 * `aeon/ui/src-tauri/src/workspace.rs`'s own doc comment for why that's
 * structural rather than a redaction applied on the way out.
 */
export interface Workspace {
  id: string;
  name: string;
  kind: "local" | "remote";
  host?: string;
  port?: number;
}

/** `Local` first, then every saved remote workspace. */
export async function workspaceList(): Promise<Workspace[]> {
  return invokeShell<Workspace[]>("workspace_list", {});
}

/** The workspace the app is currently pointed at. */
export async function workspaceActive(): Promise<Workspace> {
  return invokeShell<Workspace>("workspace_active", {});
}

/** Save a new remote workspace. Does not switch to it. */
export async function workspaceAdd(name: string, host: string, port: number): Promise<Workspace> {
  return invokeShell<Workspace>("workspace_add", { name, host, port });
}

/** Remove a saved remote workspace. Refused for `local`. */
export async function workspaceRemove(id: string): Promise<void> {
  await invokeShell<null>("workspace_remove", { id });
}

/**
 * Mark a workspace active. Does not reconnect on its own — the caller is
 * expected to follow this with a hard reload, so the "whole app context
 * reloads" behaviour a workspace switch means is exact, not approximated by
 * tearing down React state in place.
 */
export async function workspaceSelect(id: string): Promise<Workspace> {
  return invokeShell<Workspace>("workspace_select", { id });
}

/**
 * The failure of installing an event listener, in three parts.
 *
 * # Why this has its own wording
 *
 * Tauri's own message is accurate and useless to a person:
 * *"event.listen not allowed. Permission core:event:allow-listen is missing"*.
 * It says what the runtime refused and nothing about what that means for them
 * — which is that **every call will work and every stream will be silent**,
 * the most confusing half-working state this app has.
 *
 * One wording, shared by the shell and every page, so the remedy is stated the
 * same way wherever the failure surfaces.
 */
export function listenerUnavailable(cause: unknown): RpcError {
  const detail = cause instanceof Error ? cause.message : String(cause);
  return new RpcError(CODE_LISTENER_UNAVAILABLE, "the app could not install its event listener", {
    what: `the app could not install its event listener: ${detail}`,
    means:
      "streamed replies and lifecycle events reach this window as Tauri events. Without a " +
      "listener, every call still works and every stream is silent — so a prompt would run, " +
      "cost money, and show nothing",
    todo:
      "check src-tauri/capabilities/ — the window needs `core:event:default`, which grants " +
      "listen. `ui/test/capability.test.ts` guards it",
    fatal: false,
  });
}

/**
 * A code for a failure that is this app's, not the daemon's and not the
 * transport's. Positive, so it cannot be confused with a JSON-RPC or Aeon code.
 */
export const CODE_LISTENER_UNAVAILABLE = 2;

/** Subscribe to the daemon's event stream. Returns an unlisten function. */
export function onEvent(handler: EventListener): Promise<Unlisten> {
  return subscriber(EVENT_CHANNEL, (payload) => {
    if (payload === null || typeof payload !== "object") return;
    if (!isKnownEventKind((payload as { kind?: unknown }).kind)) return;
    handler(payload);
  });
}

/** Subscribe to the connection being lost, so the UI can go honest. */
export function onClosed(handler: EventListener): Promise<Unlisten> {
  return subscriber(CLOSED_CHANNEL, handler);
}

/**
 * Listen for an accepted navigation target (S58 T8). The payload is the
 * shell's canonical string — the web layer parses it again, because two
 * parsers over one fixture is the drift guard, not redundancy
 * (`NavigateListener.parseTarget` mirrors the Rust `parse_link`).
 */
export function onNavigate(handler: EventListener): Promise<Unlisten> {
  return subscriber(NAVIGATE_CHANNEL, handler);
}

/** Listen for a refused link: the payload is one fixed reason word. */
export function onNavigateRefused(handler: EventListener): Promise<Unlisten> {
  return subscriber(NAVIGATE_REFUSED_CHANNEL, handler);
}

// --- terminal notifications (S31 T8, D10) ------------------------------------

/**
 * One `terminal.output`, as the daemon publishes it (brief §5), params
 * verbatim off the socket.
 *
 * Hand-written until T9's hygiene pass folds the notification table into the
 * generated contract. Keep it deliberately small and in step with the Rust
 * allow-list in `session.rs`: a method is forwarded by the shell **and**
 * admitted here, or it reaches nothing.
 */
export interface TerminalOutput {
  terminalId: string;
  /** Per-terminal and monotonic — **not** the EventLog's durable seq (LW7). */
  seq: number;
  /** Base64 of the coalesced bytes. Decoding belongs to the screen. */
  data: string;
}

/** One `terminal.exit`. A normal exit carries `exitCode`; a signalled one carries `signal`. */
export interface TerminalExit {
  terminalId: string;
  exitCode?: number | null;
  signal?: string | null;
}

export type TerminalNotification = TerminalOutput | TerminalExit;

const TERMINAL_METHODS = ["terminal.output", "terminal.exit"] as const;
export type TerminalMethod = (typeof TERMINAL_METHODS)[number];

/**
 * Which of the two allow-listed terminal notifications this payload is — or
 * `null` when it is neither.
 *
 * Hand-written, and deliberately **not** `isKnownEventKind`: these payloads
 * carry no `kind` (they are not events), so the event gate would swallow every
 * one. The shapes are brief §5's; the `terminalId` they share is what makes a
 * wrong-channel payload — an event envelope, a stray string — recognisable
 * as not-a-terminal-frame rather than forwarding it to a screen that would
 * render nonsense.
 */
export function terminalMethodOf(payload: unknown): TerminalMethod | null {
  if (payload === null || typeof payload !== "object") return null;
  const p = payload as Record<string, unknown>;
  if (typeof p.terminalId !== "string" || p.terminalId === "") return null;
  if (typeof p.seq === "number" && typeof p.data === "string") return "terminal.output";
  // The exit arm admits whichever half of the outcome the daemon reported.
  // An exit carrying *neither* is not the published shape, so it is not
  // rendered rather than being guessed at.
  if ("exitCode" in p || "signal" in p) return "terminal.exit";
  return null;
}

/**
 * Subscribe to terminal notifications. Returns an unlisten function.
 *
 * The typed counterpart of `onEvent`, beside it and same-seam: the payload
 * reaches the listener verbatim — the bridge does not decode the base64 or
 * reinterpret the seq.
 *
 * Unlike `onEvent`, where each page maps a failed subscription through
 * `listenerUnavailable` itself, this function speaks that wording on its own:
 * there is no consumer to remember it (N1 — the terminal screen is the next
 * slice's work), and a stream that silently installs nothing is the exact
 * half-working state the three parts exist to name.
 */
export function onTerminal(
  handler: (notification: TerminalNotification) => void,
): Promise<Unlisten> {
  return subscriber(TERMINAL_CHANNEL, (payload) => {
    if (terminalMethodOf(payload) === null) return;
    handler(payload as TerminalNotification);
  }).catch((cause: unknown) => {
    throw listenerUnavailable(cause);
  });
}

/**
 * Validate what the shell returned.
 *
 * **Refused rather than defaulted.** A missing `protocolVersion` defaulted to 1
 * would be exactly the constant this whole design exists to avoid displaying.
 */
function validateNegotiated(raw: unknown): Negotiated {
  if (typeof raw !== "object" || raw === null) {
    throw new RpcError(0, `connect returned ${typeof raw}, expected an object`);
  }
  const v = raw as Partial<Negotiated>;
  if (typeof v.daemonVersion !== "string" || v.daemonVersion === "") {
    throw new RpcError(0, "connect returned no daemonVersion");
  }
  if (typeof v.protocolVersion !== "number" || !Number.isInteger(v.protocolVersion)) {
    throw new RpcError(0, "connect returned no negotiated protocolVersion");
  }
  return {
    daemonVersion: v.daemonVersion,
    protocolVersion: v.protocolVersion,
    endpoint: typeof v.endpoint === "string" ? v.endpoint : "unknown",
  };
}
