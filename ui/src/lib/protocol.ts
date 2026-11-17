// The protocol's shapes, as the UI needs them.
//
// # These are hand-written, and that is a decision with a trigger
//
// `internal/protocol/conformance/` is the cross-language contract (T9), and
// codegen for TypeScript is deferred with a named trigger: **a third client, or
// the first drift bug.** This file is the second client, so it is hand-written
// against the fixtures — and the day a field here disagrees with one there,
// that is the drift bug and codegen stops being deferred.
//
// Every type below has a fixture in `conformance/`. The comment on each says
// which.

/** `routes.list` — conformance/routes/list.json */
export interface RouteView {
  displayName?: string;
  displayDescription?: string;
  icon?: string;
  capabilities?: string[] | null;
  reasoningEfforts?: string[] | null;

  id: string;
  provider: string;
  model: string;
  baseUrl?: string;
  /** Whether a credential is set. **The key itself is never sent.** */
  hasKey: boolean;
  /** The last four characters, or bullets for a short key. Never the prefix. */
  keyHint?: string;
  source: "environment" | "runtime" | "stored";
  maxOutputTokens: number;
  /** Input budget used by step compaction. Zero means no known budget. */
  contextWindow?: number;
  prices?: PricesView;
  /** Whether a cost can be computed at all. */
  priced: boolean;
  /**
   * Declared UNMETERED on purpose (S18 O8). It separates the two reasons
   * `priced` is false: somebody decided there is no price, versus the price
   * being simply absent. A new route with neither is refused.
   */
  noPrice?: boolean;
  /**
   * `none|minimal|low|medium|high|xhigh|max`, or absent for the provider's own default.
   *
   * **`none` is what lets a STEP use tools.** OpenAI's reasoning models refuse
   * function tools unless reasoning is off, and every Aeon step offers tools —
   * so a route without it can answer Ask and fails every Step.
   */
  reasoningEffort?: string;
  /**
   * Which OpenAI API this route talks to: `chat` (the default) or `responses`.
   *
   * **`responses` is the one that lets a step reason WHILE using tools** —
   * `/v1/chat/completions` refuses that combination outright.
   */
  openaiEndpoint?: string;
  /** How a route that hears or speaks talks to its server: openai (default), synthesize, synthesize-stream, chat. */
  audioApi?: string;
  /** The route's extra request fields as compact JSON; the value of any secret-looking key is hidden. */
  extraBody?: string;

  /**
   * The route's Q8 wire-behaviour declarations (S18 T2). The distinction is
   * the whole design: `null`/absent means **UNDECLARED** (the probe asks
   * anyway — I5, absence of a declaration is not a declaration of absence),
   * `[]` means **DECLARED-NONE** (a step needing those tools is refused
   * before the call), and a list declares exactly those flags present.
   */
  wireCapabilities?: string[] | null;

  /**
   * Cached probe verdicts (S18 T3), one entry per declared flag. `verified`
   * and `contradicted` are what a probe EARNED — with the timestamp and the
   * model the verdict was made against (O2). `declared` is the ABSENCE of a
   * verdict row. A view showing `verified` without a probe row would be the
   * confident wrong answer P31 exists to prevent, so this array comes from
   * the probe cache alone, never from the declaration above.
   */
  capabilityStates?: CapabilityStateView[];
}

/**
 * One flag's three-state answer (M28, P31). A `declared` entry carries no
 * timestamp and no probed model — the probe never earned an opinion about it,
 * and that absence IS the fact.
 */
export interface CapabilityStateView {
  capability: string;
  state: "declared" | "verified" | "contradicted";
  /** The daemon's own sentence for a contradiction; rendered verbatim. */
  detail?: string;
  probedAt?: string;
  probedModel?: string;
}

/**
 * `routes.probe` (S18 T3) — the answer to the ONE minimal request per route.
 * The operator clicks it; nothing else ever does (M32).
 */
export interface RoutesProbeResult {
  route: string;
  /** The verdict per declared capability — including the honest `declared`
   * ones with their reasons, shown but never stored. */
  probes?: CapabilityStateView[];
  /** Whether the earned verdicts reached the cache. An unsaved verdict does
   * not survive the restart, and the surface says so. */
  saved: boolean;
  /** The daemon's honest sentence (no cache; a cache that refused). */
  note?: string;
  /** The THIRD answer (P31): the probe ran and could not check. Data, not
   * an RPC error — and nothing was contradicted. */
  failure?: string;
}

export interface PricesView {
  inputUsdPerMTok?: number;
  outputUsdPerMTok?: number;
  cacheReadUsdPerMTok?: number;
  cacheWriteUsdPerMTok?: number;
}

export interface RoutesListResult {
  routes: RouteView[];
  /** The adapters this daemon has, so a UI offers a choice rather than a box. */
  providers: string[];
  /** False until S4. **The UI must say so.** */
  persisted: boolean;
}

/** `routes.set` — conformance/routes/set.json */
export interface SetRouteParams {
  displayName?: string;
  displayDescription?: string;
  icon?: string;
  capabilities?: string[];
  reasoningEfforts?: string[];

  id: string;
  provider: string;
  model: string;
  baseUrl?: string;
  /** WRITE-ONLY. Empty means *keep the existing credential*. */
  apiKey?: string;
  /** Remove the credential. Separate from an empty `apiKey`, which keeps it. */
  clearKey?: boolean;
  maxOutputTokens?: number;
  contextWindow?: number;
  prices?: PricesView;
  /**
   * Declare the route unmetered (S18 O8). A NEW persisted route with no
   * prices and no `noPrice: true` is refused, naming the route — a silently
   * missing price is how a typo makes a metered model report `unknown`
   * forever. Absent keeps the saved declaration.
   */
  noPrice?: boolean;
  /** Empty means *keep*, like `apiKey`. */
  reasoningEffort?: string;
  /** Unset it, leaving the provider's default. Separate from an empty value, which keeps it. */
  clearReasoningEffort?: boolean;
  /** `chat` or `responses`; empty means keep. */
  openaiEndpoint?: string;
  /** How a route that hears or speaks talks to its server: openai (default), synthesize, synthesize-stream, chat. */
  audioApi?: string;
  /** A JSON object merged into every request on this route. Absent keeps; an empty string clears. */
  extraBody?: string;
}

/** `routes.trace` — conformance/routes/trace.json */
export interface TraceRouteParams {
  route: string;
  prompt: string;
  system?: string;
  reasoningEffort?: string;
  /** Merged over the route's own extra body for this call only. */
  extraBody?: Record<string, unknown>;
  maxOutputTokens?: number;
  offerTool?: boolean;
}

export interface TraceRouteResult {
  route: string;
  effort?: string;
  extraBody?: string;
  url: string;
  requestBody?: unknown;
  status: number;
  contentType?: string;
  raw?: string;
  truncated?: boolean;
  thinking?: string;
  text?: string;
  thinkingChars: number;
  textChars: number;
  toolCalls: number;
  stopReason?: string;
  usage?: Record<string, number>;
  durationMs: number;
  error?: string;
}

/** `ask` — conformance/ask/streaming.json and ask/buffered.json */
export interface AskParams {
  route: string;
  prompt: string;
  /** With one, the reply streams as events. Without, it comes back buffered. */
  subscriptionId?: string;
  system?: string;
  maxOutputTokens?: number;
  temperature?: number;
  /**
   * This call only; empty uses the route's.
   *
   * A chat offers no tools, so nothing lowers it — unlike a step, which cannot
   * have reasoning on at all on the models that refuse tools while it is.
   */
  reasoningEffort?: string;
}

export interface AskResult {
  askId: string;
  /** Buffered mode only. */
  text?: string;
  usage?: Usage;
  cost?: Cost;
  truncated?: boolean;
  stopReason?: string;
  dropped?: number;
}

/** `subscribe` — conformance/subscribe/filtered.json */
export interface SubscribeResult {
  subscriptionId: string;
  /** Where the subscription actually resumed. */
  from: number;
}

/** Every token count is OPTIONAL, and **absent is not zero** (M9). */
export interface Usage {
  inputTokens?: number;
  outputTokens?: number;
  cacheReadTokens?: number;
  cacheWriteTokens?: number;
  reasoningTokens?: number;
}

/**
 * Cost, in one of three states.
 *
 * **A renderer must handle all three.** Formatting `usd` and hoping is how an
 * unknown cost reaches a person as `$0.00`, which is the specific dishonesty
 * the three states exist to prevent.
 */
export type CostState = "known" | "partial" | "unknown";

export interface Cost {
  state: CostState;
  usd?: number;
  usdAtLeast?: number;
  missing?: string[];
  reason?: string;
}

/** A toast-worthy annotation on an event — delivery-time only, see `protocol.NotifyHint` (Go) */
export interface AeonNotifyHint {
  title: string;
  body?: string;
  tone: "info" | "success" | "warning" | "danger";
  /**
   * S58 SV4/SV7: the inbox subject key this event is about. The consumer
   * DEDUPS by it (`lib/notifications.ts`), so one question that announces
   * itself twice (`loop.waiting` + `question.asked`) toasts once.
   */
  attentionKey?: string;
  /**
   * S58 SV9: a navigation target and nothing else — never an answer, a
   * token, or a capability. The shapes the shell's `parse_link` accepts are
   * the shapes `NavigateListener.parseTarget` accepts; both read
   * `test/fixtures/deeplinks.json`.
   */
  deepLink?: string;
}

/** An event envelope — conformance/events/*.json */
export interface AeonEvent {
  subscriptionId: string;
  /**
   * Aeon's sequence number, scoped to the stream.
   *
   * **This is the cursor**, not the one on the notification envelope — that is
   * the transport's per-connection delivery counter and it restarts at
   * reconnection, which is exactly when a cursor is needed.
   */
  seq: number;
  /**
   * This subscription's own delivery counter: 1, 2, 3. A jump means a notification was dropped on the way (the daemon's
   * per-connection queue was full), which `seq` cannot show because a filtered subscription skips log numbers on purpose.
   */
  dseq?: number;
  kind: string;
  componentRoot?: string;
  at: string;
  payload?: unknown;
  /**
   * Present only on the live delivery that earned it — never on a cursor
   * replay (`subscribe {after}`), since the daemon attaches it after the
   * event is already appended to the durable log. A reconnect's catch-up
   * must never re-surface a toast for something already shown live.
   */
  notify?: AeonNotifyHint;
}

/** `ask.delta` — conformance/events/ask-delta.json */
export interface AskDeltaPayload {
  askId: string;
  kind: "text" | "thinking" | "tool_use" | "stop";
  text?: string;
  block?: { kind: string; id?: string; name?: string; input?: unknown };
}

/** `ask.done` — conformance/events/ask-done.json */
export interface AskDonePayload {
  askId: string;
  usage: Usage;
  cost: Cost;
  stopReason?: string;
  dropped?: number;
  owedToolErrors?: string[];
  /** Set when the call failed. The same structured error a method returns. */
  error?: { code: number; message: string; data?: { what: string; means: string; todo: string; fatal: boolean } };
}

export const EVENT_ASK_DELTA = "ask.delta";
export const EVENT_ASK_DONE = "ask.done";

// --- S4, the store -----------------------------------------------------------

/** `store.info` — conformance/store/info.json */
export interface StoreInfoResult {
  /** FIRST, because every debugging session starts by finding the file. */
  path: string;
  bytes: number;
  /**
   * The write-ahead log beside it.
   *
   * Separate because in WAL mode committed data lives there until a
   * checkpoint: a busy store's main file can read as 4 KiB, which looks broken
   * unless the log is shown.
   */
  walBytes: number;
  journalMode: string;
  /**
   * Read from the CONNECTION, not inferred from the DSN.
   *
   * The wrong DSN spelling produces a store that looks configured and runs
   * without cascades (measured), so this is the answer rather than the
   * intention — and the app shows it for that reason.
   */
  foreignKeys: boolean;
  applied: MigrationView[];
  pending: MigrationView[];
  tables: string[];
  /**
   * The statically linked extension, per pool.
   *
   * There is no extension-missing state to render: it is compiled in, so a
   * failure means the binary was built without cgo. Both pools are reported
   * because an extension registered for one would be a retrieval slice that
   * works in tests and fails under load.
   */
  vecWriterVersion: string;
  vecReaderVersion: string;
  readPoolSize: number;
  /** Always 1. WAL allows one writer, and that is arranged rather than hoped. */
  writerPoolSize: number;
  busyTimeoutMs: number;
}

export interface MigrationView {
  name: string;
  description?: string;
  status?: string;
  appliedAt?: string;
}

/** `loop.create` / `loop.list` — conformance/store/loop-create.json */
export interface LoopView {
  id: string;
  slug: string;
  /** With the slug, the loop's whole identity. The same slug in two roots is two loops. */
  componentRoot: string;
  title: string;
  goal?: string;
  mode: string;
  phase: string;
  status: string;
  startedAt: string;
  updatedAt: string;
  /** DERIVED from the question table, never a stored flag. */
  awaiting: boolean;
}

export interface LoopListResult {
  loops: LoopView[];
  componentRoot?: string;
  all: boolean;
}

/** `loop.write` — conformance/store/loop-write.json */
export interface DocumentView {
  file: string;
  rev: number;
  bytes: number;
  sha256: string;
  writtenBy?: string;
  writtenAt: string;
  /** Present on a read, absent in a listing. */
  content?: string;
}

export interface DocumentWriteResult {
  document: DocumentView;
  /** A write whose content matches the head creates NO revision. */
  unchanged: boolean;
}

export interface DocumentListResult {
  documents: DocumentView[];
}

/** `step.show` — conformance/store/step-show.json */
export interface StepView {
  id: number;
  loopSlug: string;
  componentRoot: string;
  taskid?: string;
  phase: string;
  agent: string;
  status: "running" | "suspended" | "orphaned" | "done" | string;
  outcome?: string;
  startedAt: string;
  endedAt?: string;
  cost: Cost;
  /** The canonical transcript, present on `step.show`. */
  messages?: TranscriptMessage[];
  contextManifest?: unknown;
  /** Present while this step is suspended and waiting for an answer. */
  question?: PendingQuestion;
  /**
   * Set only when `status` is `suspended` because a PERSON stopped the step
   * — never when the model asked a real question (then `question` is set
   * instead, and this is absent). Both share the status (no process,
   * resumable either way); this is the one field that tells them apart.
   * conformance/store/step-show-paused.json
   */
  pauseReason?: string;
  recovery?: RecoveryView;
  receipts?: ReceiptView[];
}

export interface RecoveryView {
  case: "A" | "B" | "C" | "unclassifiable" | string;
  reissued: boolean;
  reason?: string;
  updatedAt: string;
  failure?: RecoveryFailureView;
}

export interface RecoveryFailureView {
  kind: string;
  tool?: string;
  toolUseId?: string;
  detail: string;
}

export interface ReceiptView {
  toolUseId: string;
  tool: string;
  argDigest: string;
  startedAt: string;
  completedAt?: string;
  resultCaptured: boolean;
}

export interface TranscriptMessage {
  role: "system" | "user" | "assistant" | string;
  content: TranscriptBlock[];
}

export interface TranscriptBlock {
  kind: "text" | "thinking" | "tool_use" | "tool_result" | string;
  text?: string;
  /** Present on a signed thinking block. **S7's replay depends on it.** */
  signature?: string;
  id?: string;
  name?: string;
  input?: unknown;
  toolUseId?: string;
  content?: string;
  isError?: boolean;
}

export interface StepListResult {
  steps: StepView[];
}

/** `step.verify` — conformance/store/step-verify.json */
export interface StepVerifyResult {
  ok: boolean;
  messages: number;
  /** Reported because "present" and "the right length" are different claims. */
  signatureLength: number;
  digest: string;
  mismatch?: string;
}

/** `store.awaiting` — conformance/store/awaiting.json */
export interface AwaitingResult {
  /** THE QUERY. Shown, because there is no `awaiting_user` column to find. */
  sql: string;
  loops: { slug: string; componentRoot: string }[];
}

export const METHOD_STORE_INFO = "store.info";
export const METHOD_STORE_AWAITING = "store.awaiting";
export const METHOD_LOOP_CREATE = "loop.create";
export const METHOD_LOOP_LIST = "loop.list";
export const METHOD_LOOP_WRITE = "loop.write";
export const METHOD_LOOP_READ = "loop.read";
export const METHOD_STEP_SHOW = "step.show";
export const METHOD_STEP_VERIFY = "step.verify";
export const METHOD_STEP_LIST = "step.list";
export const METHOD_STEP_STOP = "step.stop";
export const METHOD_STEP_CONTINUE = "step.continue";

// --- S5 · the step ------------------------------------------------------------

/** `step` — conformance/step/run.json */
export interface StepParams {
  prompt: string;
  /**
   * The grant list — **the only thing that decides what the step can call**.
   * Omitted takes the default, which is `read` and nothing else.
   *
   * `bash` is never granted by default. A caller asks for it explicitly.
   */
  tools?: string[];
  subscriptionId?: string;
  maxIterations?: number;
  /**
   * Required, with no default. The daemon multiplexes projects, so its own
   * working directory is not the client's — a step defaulting to it would read
   * the daemon's files and look like it had worked.
   */
  workspaceRoot: string;
  /** Workspace-relative files frozen into the opening context. */
  contextFiles?: string[];
  route?: string;
  loopSlug?: string;
  /**
   * This step only. **Empty is not "the route's value"** — it is "let the step
   * decide", and the step lowers a reasoning route to `none` because the models
   * with that dial refuse tools while it is on.
   */
  reasoningEffort?: string;
}

export interface StepRunResult {
  stepId: number;
  kind: "complete" | "suspended" | string;
  /** One of the four outcomes. `reject` is not a failure. */
  outcome?: "ok" | "reject" | "blocked" | "failed" | string;
  reason?: string;
  handoff?: unknown;
  usage: Usage;
  cost: Cost;
  iterations: number;
  question?: PendingQuestion;
  /** Mirrors StepView's — set only for a stop, never a real question.
   * conformance/step/stopped-result.json */
  pauseReason?: string;
  /** Non-zero when the event stream could not be delivered to this client. */
  sinkFailures?: number;
}

/** `step.stop` — conformance/step/stop.json */
export interface StepStopParams {
  stepId: number;
}

export interface StepStopResult {
  stepId: number;
  /** False means there was nothing live to interrupt — not an error: it
   * already finished, was never started, or was stopped by an earlier call. */
  stopped: boolean;
}

/**
 * `step.continue` — conformance/step/continue.json
 *
 * Resumes a step a person stopped. Only meaningful once nothing is
 * mid-flight, which is the whole reason the provider and reasoning effort
 * can only change HERE and not while the step is running.
 */
export interface StepContinueParams {
  stepId: number;
  /** Empty keeps the step's own route, re-resolved by id through the
   * CURRENT config — the same convention `answer` uses on resume. */
  route?: string;
  /** Empty keeps the step's own reasoning effort. */
  reasoningEffort?: string;
}

export interface PendingQuestion {
  id: number;
  stepId?: number;
  kind: string;
  body: string;
  options?: string[];
  toolUseId?: string;
}

export interface AnswerResult {
  stepId: number;
  questionId: number;
  answeredAt: string;
  status: "running" | string;
}

/** What a `step.*` event carries — conformance/events/step-tool.json */
export interface StepEventPayload {
  stepId?: number;
  tool?: string;
  text?: string;
  summary?: string;
  result?: string;
  isError?: boolean;
  iteration?: number;
  outcome?: string;
  reason?: string;
  start?: number;
  end?: number;
  estimateBefore?: number;
  estimateAfter?: number;
}

export const METHOD_STEP = "step";
export const METHOD_ANSWER = "answer";
export const EVENT_QUESTION_ASKED = "question.asked";
export const EVENT_STEP_SAVED = "step.saved";
export const EVENT_STEP_COMPACTED = "step.compacted";

/** The values `routes.set` accepts for `reasoningEffort`. */
export const REASONING_EFFORTS = ["none", "minimal", "low", "medium", "high", "xhigh", "max"] as const;

/**
 * The OpenAI API surfaces a route can name.
 *
 * `chat` refuses function tools while reasoning is on; `responses` accepts
 * both, which is the only way to have a step that reasons.
 */
export const OPENAI_ENDPOINTS = ["chat", "responses"] as const;
/** The speech APIs a route can speak (see the daemon's model.AudioAPIs). */
export const AUDIO_APIS = ["openai", "synthesize", "synthesize-stream", "chat"] as const;

export const EVENT_STEP_STARTED = "step.started";
export const EVENT_STEP_MODEL_DELTA = "model.delta";
export const EVENT_STEP_TOOL_START = "step.tool.start";
export const EVENT_STEP_TOOL_END = "step.tool.end";
export const EVENT_STEP_DONE = "step.done";

/**
 * The newest daemon protocol this app was built to speak. The name is a
 * ceiling, not a floor: a daemon negotiating anything AT OR BELOW this
 * number is understood; anything above it means the daemon has moved on
 * without this app. See `checkProtocolCompat` in `lib/protocolCompat.ts`,
 * the one place that reads this constant.
 *
 * Was 1 until this was caught stale: S38 T2 bumped the wire protocol to 2
 * (the project→component rename), the Rust shell already asks for
 * 2 (`src-tauri/src/session.rs`'s `PROTOCOL_VERSION`), and every call site in
 * this app already speaks the v2 names (`component.list`, `componentRoot`) —
 * this was the one place nobody updated, so a fully-compatible app told
 * itself it wasn't.
 */
export const MIN_COMPATIBLE_APP_PROTOCOL_VERSION = 2;

// ---------------------------------------------------------------------------
// S40 — the automation desk. Hand-written against the conformance fixtures
// like everything above; each type names its fixture. The five outcomes are
// a CLOSED list (AU12) — the same five the daemon validates, the same five
// the fire log always shows, zero included.

/** `automation.trigger.list` — conformance/automation/trigger-list.json */
export const METHOD_AUTOMATION_TRIGGER_LIST = "automation.trigger.list";
/** `automation.trigger.show` — conformance/automation/trigger-show.json */
export const METHOD_AUTOMATION_TRIGGER_SHOW = "automation.trigger.show";
/** `automation.trigger.fires` — conformance/automation/trigger-fires.json */
export const METHOD_AUTOMATION_TRIGGER_FIRES = "automation.trigger.fires";
/** `automation.trigger.create` — conformance/automation/trigger-create.json */
export const METHOD_AUTOMATION_TRIGGER_CREATE = "automation.trigger.create";
/** `automation.trigger.enable` — conformance/automation/trigger-enable.json */
export const METHOD_AUTOMATION_TRIGGER_ENABLE = "automation.trigger.enable";
/** `automation.trigger.disable` — conformance/automation/trigger-disable.json */
export const METHOD_AUTOMATION_TRIGGER_DISABLE = "automation.trigger.disable";

/** The AU12 five. `""` is the claimed-not-yet-decided state — the crash window AU13 answers, shown never hidden (P31). */
export type FireOutcome = "started" | "refused" | "limited" | "skipped" | "could-not-say";
export const FIRE_OUTCOMES: readonly FireOutcome[] = ["started", "refused", "limited", "skipped", "could-not-say"];

/** conformance/automation/trigger-fires.json — one row of the fire log. */
export interface EventFireView {
  id: number;
  triggerId: string;
  outboxId: number;
  subjectKind: string;
  subjectId: string;
  depth: number;
  outcome: FireOutcome | "";
  reason: string;
  loopId: string;
  loopSlug: string;
  costNote: "" | "cost-unknown" | "cost-floor";
  claimedAt: string;
  decidedAt: string;
}

/** conformance/automation/trigger-list.json — one trigger as the desk sees it. `maxCostPerDayUsd: null` is NO CAP, not zero; `lastFire: null` is NEVER FIRED, not broken. */
export interface EventTriggerView {
  id: string;
  name: string;
  source: string;
  rootId: string;
  componentId: string;
  match: { event: string; [key: string]: unknown };
  blueprint: string;
  kickoff: string;
  maxRunsPerHour: number;
  maxCostPerDayUsd: number | null;
  enabled: boolean;
  disabledReason: string;
  createdByKind: string;
  createdById: string;
  createdAt: string;
  updatedAt: string;
  lastFire: EventFireView | null;
}

/** `automation.trigger.list` result. */
export interface EventTriggerListResult {
  triggers: EventTriggerView[];
}

/** `automation.trigger.fires` result — counts ALWAYS carry the five keys, zero shown as zero. */
export interface EventTriggerFiresResult {
  fires: EventFireView[];
  counts: Record<FireOutcome, number>;
}

// ---------------------------------------------------------------------------
// S24 — the loop queue. Hand-written against the conformance fixtures like
// everything above; each type names its fixture. The seven statuses are
// CG1's CLOSED set — the same seven the store's CHECK confines and the CLI
// renders — and the bench gives each its own treatment (T8's gate). This
// section is hand-written because protogen reads only `method.go`'s
// registration lines and the queue's registrations live with their handlers
// in `protocol/queue.go` (T7's one-file rule, recorded in its Notes).

/** `queue.append` — conformance/queue/append.json */
export const METHOD_QUEUE_APPEND = "queue.append";
/** `queue.list` — conformance/queue/list.json */
export const METHOD_QUEUE_LIST = "queue.list";
/** `queue.reorder` — conformance/queue/reorder.json */
export const METHOD_QUEUE_REORDER = "queue.reorder";
/** `queue.remove` — conformance/queue/remove.json */
export const METHOD_QUEUE_REMOVE = "queue.remove";
/** `queue.set-size` — conformance/queue/set-size.json */
export const METHOD_QUEUE_SET_SIZE = "queue.set-size";
/** `queue.settings` — conformance/queue/settings-read.json */
export const METHOD_QUEUE_SETTINGS = "queue.settings";
/** `queue.advance` — conformance/queue/advance.json */
export const METHOD_QUEUE_ADVANCE = "queue.advance";

/** CG1's seven, the whole machine — a status outside this list is a bug, not a sixth color. */
export const QUEUE_STATUSES = ["pending", "sizing", "ready", "running", "done", "blocked", "cancelled"] as const;
export type QueueStatus = (typeof QUEUE_STATUSES)[number];

/**
 * conformance/queue/list.json — one row on the board.
 *
 * Size is D8's triple flattened by the wire: min, max and basis are
 * all-present-or-all-absent, so `sizeMin: null` means **no size has been
 * reported**, which renders "not sized yet" — never blank, which would read
 * as zero. `attended` is D5's three states: true, false, or null for
 * "the project's declared default decides".
 */
export interface QueueItemView {
  id: number;
  position: number;
  status: QueueStatus;
  blueprint: string;
  goal: string;
  workspaceRoot: string;
  tech?: string;
  with?: Record<string, string>;
  attended: boolean | null;
  sizeMin: number | null;
  sizeMax: number | null;
  sizeBasis: string | null;
  loopSlug?: string;
  createdAt: string;
  updatedAt: string;
}

/** `queue.list` result. */
export interface QueueListResult {
  componentRoot: string;
  items: QueueItemView[];
}

/** `queue.append`/`reorder`/`remove`/`set-size` results — the row as it now stands. */
export interface QueueItemResult {
  item: QueueItemView;
}

/** `queue.settings` result. `known: false` is P31's third state: the values shown are S24's declared defaults, not stored facts. */
export interface QueueSettingsResult {
  componentRoot: string;
  known: boolean;
  queueAdvance: string;
  sizingMode: string;
}

/** `queue.advance` result (CG4 — the call IS the decision; `started: false` says honestly why not). */
export interface QueueAdvanceResult {
  started: boolean;
}

// ---------------------------------------------------------------------------
// S26 — the forward roadmap. Hand-written against the conformance fixtures
// like everything above; each type names its fixture. The eight statuses are
// R1's CLOSED set (no ninth), the priority three states are D2's, the
// estimate trio is D3's all-present-or-all-absent, and provenance is D5's
// with NO default — the writer answers. Like the queue's, this section is
// hand-written because protogen reads only `method.go`'s registration lines
// and the roadmap's registrations live with their handlers in
// `protocol/roadmap.go`; PROTOCOL_VERSION is additive here (T5's Notes).

/** `roadmap.add` — conformance/roadmap/add.json */
export const METHOD_ROADMAP_ADD = "roadmap.add";
/** `roadmap.list` — conformance/roadmap/list.json */
export const METHOD_ROADMAP_LIST = "roadmap.list";
/** `roadmap.show` — conformance/roadmap/show.json */
export const METHOD_ROADMAP_SHOW = "roadmap.show";
/** `roadmap.cancel` — conformance/roadmap/cancel.json */
export const METHOD_ROADMAP_CANCEL = "roadmap.cancel";
/** `roadmap.estimate` — conformance/roadmap/estimate.json */
export const METHOD_ROADMAP_ESTIMATE = "roadmap.estimate";
/** `roadmap.priority` — conformance/roadmap/priority.json */
export const METHOD_ROADMAP_PRIORITY = "roadmap.priority";
/** `roadmap.ideate` — dispatches the ideation loop (T3); the answer is the daemon's seam result. */
export const METHOD_ROADMAP_IDEATE = "roadmap.ideate";
/** `roadmap.plan` — dispatches the project-planner (T3); the answer is the daemon's seam result. */
export const METHOD_ROADMAP_PLAN = "roadmap.plan";
/** `roadmap.materialize` — the one item's loop.start, queued or direct (T4); the answer is the daemon's seam result. */
export const METHOD_ROADMAP_MATERIALIZE = "roadmap.materialize";
/** `roadmap.depend` (S27 T3) — one item waits for another: an edge of intent, same repository only, cycles refused naming the closure; readiness is read at the attempt, never stored (D4). */
export const METHOD_ROADMAP_DEPEND = "roadmap.depend";
/** `roadmap.undepend` (S27 T3) — removes exactly one edge and nothing else: no cascade, no loop touched, no status moved. */
export const METHOD_ROADMAP_UNDEPEND = "roadmap.undepend";

/** R1's eight, the whole machine — a status outside this list is a bug, not a ninth color. */
export const ROADMAP_STATUSES = [
  "idea",
  "ideating",
  "brief-agreed",
  "planned",
  "ready",
  "running",
  "done",
  "cancelled",
] as const;
export type RoadmapStatus = (typeof ROADMAP_STATUSES)[number];

/** D2's three — `unknown` is the honest absence, never a blank and never an implied-low. */
export const ROADMAP_PRIORITY_STATES = ["known", "assumed", "unknown"] as const;
export type RoadmapPriorityState = (typeof ROADMAP_PRIORITY_STATES)[number];

/** D5's three, with NO default anywhere — whoever writes the row answers. */
export const ROADMAP_PROVENANCES = ["authored", "triggered", "triaged"] as const;
export type RoadmapProvenance = (typeof ROADMAP_PROVENANCES)[number];

/**
 * conformance/roadmap/list.json — one row on the forward Gantt.
 *
 * The estimate trio is all-present-or-all-absent (D3): `estimateMin: null`
 * means **no estimate exists** and renders "not estimated", never a blank
 * (a blank reads as zero) and never a bare number when present.
 * `priorityLabel` is meaningful only when `priorityState` is not `unknown`
 * (D2's door enforces the pairing); `loopSlug` is absent until
 * materialization — the column is simply unset (D6).
 */
export interface RoadmapItemView {
  id: number;
  componentRoot: string;
  title: string;
  epic?: string;
  status: RoadmapStatus;
  priorityState: RoadmapPriorityState;
  priorityLabel: string | null;
  estimateMin: number | null;
  estimateMax: number | null;
  estimateBasis: string | null;
  provenance: RoadmapProvenance;
  briefPath?: string;
  planPath?: string;
  loopSlug?: string;
  createdAt: string;
  updatedAt: string;
}

/** `roadmap.list` result — the door's own Gantt order (status rung, then created), filters applied underneath. */
export interface RoadmapListResult {
  componentRoot: string;
  items: RoadmapItemView[];
}

/** `roadmap.add`/`cancel`/`estimate`/`priority` results — the row as it now stands. */
export interface RoadmapItemResult {
  item: RoadmapItemView;
}

/** `roadmap.ideate`/`plan`/`materialize` results — the daemon's seam answer, rendered honestly as it arrives. */
export type RoadmapDispatchResult = Record<string, unknown>;

// --- S29 T7/T8 — the `triage.*` surface ------------------------------------

export const METHOD_TRIAGE_FILE = "triage.file";
export const METHOD_TRIAGE_LIST = "triage.list";
export const METHOD_TRIAGE_SHOW = "triage.show";
export const METHOD_TRIAGE_SET_PRIORITY = "triage.set-priority";
export const METHOD_TRIAGE_CONFIRM_DUPLICATE = "triage.confirm-duplicate";
/** `triage.validate` — dispatches the validation-attempt loop (T3); the answer is the daemon's seam result. */
export const METHOD_TRIAGE_VALIDATE = "triage.validate";
/** `triage.handoff` — hands a validated, prioritized bug to the roadmap (T6/D8); the answer is the daemon's seam result. */
export const METHOD_TRIAGE_HANDOFF = "triage.handoff";
/** `triage.suggest-duplicates` — nearest-neighbour candidates within the bug's own project (T4, a read); no side effect. */
export const METHOD_TRIAGE_SUGGEST = "triage.suggest-duplicates";

/** D1's four — the validation machine. `unvalidated` is the default and never collapses into `could-not-tell`. */
export const TRIAGE_VALIDATION_STATES = [
  "unvalidated",
  "reproduced",
  "not-reproduced",
  "could-not-tell",
] as const;
export type TriageValidationState = (typeof TRIAGE_VALIDATION_STATES)[number];

/** D2's three — a SEPARATE set from validation; `unknown` is the honest absence, never a blank. */
export const TRIAGE_PRIORITY_STATES = ["known", "assumed", "unknown"] as const;
export type TriagePriorityState = (typeof TRIAGE_PRIORITY_STATES)[number];

/**
 * conformance/triage/... — one row on the bug backlog.
 *
 * `validation` and `priorityState` are TWO separate closed sets (D2), rendered
 * apart — never one combined badge (R4's guard the renderer obeys by having
 * two fields to render). `priorityLabel` is meaningful only when
 * `priorityState` is not `unknown` (D2's CHECK enforces the pairing).
 * `duplicateOf`/`roadmapItemId` render the honest absence, not a zero.
 */
export interface TriageView {
  id: number;
  componentRoot: string;
  title: string;
  body: string;
  validation: TriageValidationState;
  priorityState: TriagePriorityState;
  priorityLabel: string | null;
  filedBy: string;
  duplicateOf: number | null;
  roadmapItemId: number | null;
  validated: boolean;
  createdAt: string;
  updatedAt: string;
}

/** `triage.list` result — the door's own order, filters applied underneath. */
export interface TriageListResult {
  items: TriageView[];
}

/** `triage.file`/`show`/`set-priority`/`confirm-duplicate` result — the row as it now stands. */
export interface TriageItemResult {
  bug: TriageView;
}

/** `triage.validate`/`handoff`/`suggest` results — the daemon's seam answer, rendered honestly as it arrives. */
export type TriageDispatchResult = Record<string, unknown>;

// S28 — the learnings. Hand-written against the conformance fixtures like
// everything above; each type names its fixture. The scope is D1's closed
// three, the provenance is M1's four (`relaxed` is the board's answer,
// refused at this door), and the observed count rides only with an observed
// learning (M3: the strength is the count, never a collapsed score).

/** `learnings.add` — conformance/learnings/add.json */
export const METHOD_LEARNINGS_ADD = "learnings.add";
/** `learnings.list` — conformance/learnings/list.json */
export const METHOD_LEARNINGS_LIST = "learnings.list";
/** `learnings.show` — conformance/learnings/show.json */
export const METHOD_LEARNINGS_SHOW = "learnings.show";
/** `learnings.query` — conformance/learnings/query.json */
export const METHOD_LEARNINGS_QUERY = "learnings.query";

/** D1's three — a learning names its owner: a person, a project or an agent. */
export const LEARNINGS_SCOPES = ["person", "project", "agent"] as const;
export type LearningsScope = (typeof LEARNINGS_SCOPES)[number];

/** M1's four — `relaxed` is the board's answer, refused at this door (never hand-authored). */
export const LEARNINGS_PROVENANCES = ["told", "observed", "assumed", "relaxed"] as const;
export type LearningsProvenance = (typeof LEARNINGS_PROVENANCES)[number];

/** A learning rendered the honest way: provenance always shown, the count only with `observed` (M3). */
export interface LearningsView {
  id: number;
  scope: LearningsScope;
  scopeKey: string;
  body: string;
  provenance: LearningsProvenance;
  observedCount?: number;
  tags?: string[];
  sourceLoopSlug?: string;
  sourceStepId?: number;
  createdAt: string;
}

/** `learnings.add` params — provenance required, no default; `relaxed` is refused. */
export interface LearningsAddParams {
  scope: LearningsScope;
  scopeKey: string;
  body: string;
  provenance: LearningsProvenance;
  observedCount?: number;
  tags?: string[];
  sourceLoopSlug?: string;
  sourceStepId?: number;
}

/** `learnings.list` params — one owner, named. */
export interface LearningsListParams {
  scope: LearningsScope;
  scopeKey: string;
}

/** `learnings.show` params — one id. */
export interface LearningsShowParams {
  id: number;
}

/** One tag the query asks for — matched by prefix (LIKE 'pattern%') or exact (M5). */
export interface LearningsTag {
  pattern: string;
  prefix?: boolean;
}

/** `learnings.query` params — the tag-filtered read (D5) with an optional embedding rank (T3). */
export interface LearningsQueryParams {
  scope?: LearningsScope;
  scopeKey?: string;
  tags?: LearningsTag[];
  queryText?: string;
}

/** `learnings.add`/`show` results — the row as it now stands. */
export interface LearningsItemResult {
  learning: LearningsView;
}

/** `learnings.list` results — the scope's record, ordered by created_at. */
export interface LearningsListResult {
  learnings: LearningsView[];
}

/** `learnings.query` results — the tag-filtered (and, where a route is configured, ranked) set. */
export interface LearningsQueryResult {
  learnings: LearningsView[];
}
