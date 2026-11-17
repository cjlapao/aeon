import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { chatItemsFromEvents, chatItemsFromLoopTranscriptPage, deriveSession, mailboxItemsFromRows, mergeMailboxItems, type AgentMessageRowLike, type ChatItem, type ChatSession, type LoopTranscriptMessageLike } from "../../components/agent-chat";
import { onEvent, rpc, rpcTyped, toRpcError } from "../../lib/bridge";
import type { LiveController, LiveState } from "../../lib/live";
import type { AeonEvent } from "../../lib/protocol";
import type { LoopDetail } from "../loops/pages/LoopScreen";
import { scopeToLoop, stepIdsOf } from "./workspaceModel";
import { todosFromEvent, type LoopTodos } from "./todo/todoModel";
import { withLoopRuns } from "./sessionFromLoop";
import { useLoopSteps, type LoopSteps } from "./useLoopSteps";
import { NOTHING_RECOVERED, withRecovered, type Recovered } from "./pendingQuestion";
import { readRecovered } from "./readRecovered";
import { withPastQuestions } from "./replayQuestions";
import { withStepEnds } from "./replayStepEnds";
import { JOURNAL_WINDOW, appendLive, mergeEvents, statusFromEvents, stepsWithTurns, withoutLegacyDetail, type JournalWindow } from "./journal";

/** What the loop page renders, derived from one `loop.show`, one event subscription, and the durable transcript window. */
export interface LoopWorkspace {
  loop: LoopDetail | null;
  /** Every step's summary (paged) and the loop's totals, from the daemon. */
  steps: LoopSteps;
  /** Events scoped to this loop (its own plus every step it ran). */
  events: AeonEvent[];
  items: ChatItem[];
  session: ChatSession;
  steerTexts: Record<number, string>;
  /** S75: the loop's todo list (active, or the last closed one). null until read, or when the daemon has none to answer. */
  todos: LoopTodos | null;
  setTodos(next: LoopTodos): void;
  /** `step.steer.queued` carries no text (the row remembers it); the host that sent it does. */
  rememberSteer(steerId: number, text: string): void;
  connected: boolean;
  error: string | null;
  refresh(): Promise<void>;
  /** Scrolling up past what's loaded: fetch the window immediately before it. */
  loadOlder(): void;
  loadingOlder: boolean;
  /** False once the oldest turn this loop ever wrote has been reached. */
  hasOlder: boolean;
}

// S70 MB6: the exchange rides the SAME stream as everything else — the
// four `agent.message.*` kinds join the subscription so a live loop paints
// its agents' rows as the rows settle (posted, delivered, answered).
// S75: `todo.updated` rides the same stream, so the pinned list moves as the
// agent writes it.
const KINDS = ["loop.*", "step.*", "model.delta", "question.*", "agent.message.*", "todo.*"];
const REFRESH_DEBOUNCE_MS = 150;
/** The daemon's ring is bounded; so is this window's copy. */
const KEEP_EVENTS = 1500;
/** Live events are drawn at most this often. */
const LIVE_FLUSH_MS = 90;
/**
 * The FIFO cap on the loop's OWN history backfill — turns the live event ring
 * has already forgotten, or never held (a loop reopened after it finished).
 * Not a cap on the live tail itself: `chatItemsFromEvents` keeps rendering
 * everything the live stream gives it, exactly as it always has — this only
 * bounds how much of the DURABLE past gets pulled in alongside it, so opening
 * a long-finished loop costs one bounded read, not its whole history at once.
 */
const HISTORY_WINDOW = 50;

interface LoopTranscriptRpcResult {
  messages: LoopTranscriptMessageLike[];
  hasOlder: boolean;
  hasNewer: boolean;
}

/**
 * The loop page's single data owner. Panels take what this returns as props
 * and never call `loop.show` or subscribe themselves, so the page cannot
 * disagree with itself about what the loop is doing.
 */
export function useLoopWorkspace(loopId: string, live: LiveState, controller?: LiveController): LoopWorkspace {
  const [loop, setLoop] = useState<LoopDetail | null>(null);
  const [events, setEvents] = useState<AeonEvent[]>([]);
  // The loop's journal: its own stored events, read from the daemon by loop id and merged with the live tail. A
  // reopened loop is drawn from these by the same reducer a live one uses. `journalOn` says the daemon has the
  // method; an older daemon leaves it off and the page keeps the old way (live ring plus transcript).
  const [journal, setJournal] = useState<AeonEvent[]>([]);
  const [journalOn, setJournalOn] = useState(false);
  // Whether the journal holds events older than the oldest one loaded. Set from the FIRST window only: later
  // windows (a reconnect re-reads the latest) must not reopen a gap the person already paged through.
  const [journalOlder, setJournalOlder] = useState(false);
  const journalLoaded = useRef(false);
  // The journal has answered (or the daemon has no such method): only then is it known which steps the events
  // cover, and so which the transcript must supply. Reading the transcript earlier would draw a step twice.
  const [journalSettled, setJournalSettled] = useState(false);
  const [steerTexts, setSteerTexts] = useState<Record<number, string>>({});
  const [todos, setTodos] = useState<LoopTodos | null>(null);
  // S76: the question the loop is parked on, read from the store. The live ring is
  // bounded and a reopened chat never saw the event, so a question that is still
  // owed an answer must come from the rows, not only from the stream.
  const [recovered, setRecovered] = useState<Recovered>(NOTHING_RECOVERED);
  const [error, setError] = useState<string | null>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const pending = useRef<AeonEvent[]>([]);
  const flushTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);

  // --- the durable history window (S42: "see what happened", live or long finished) --------
  //
  // Separate from `events`/`liveItems` on purpose. The live ring forgets by
  // design (EventLog's DefaultRetention, shared across every loop on the
  // daemon) — reopening an old loop, or scrolling back past what the ring
  // still holds, needs `loop.transcript` instead: the durable `step_message`
  // table, windowed so a long loop never loads all at once. `historyItems`
  // holds ONLY turns the live mechanism has no knowledge of; the two are
  // concatenated below, never duplicated, by excluding any step the live
  // events already cover.
  const [historyItems, setHistoryItems] = useState<ChatItem[]>([]);
  // S70: the exchange rows from `agent.messages` — the READ of the same
  // rows the stream announces (S39's reload rule, applied to the mailbox).
  const [mailboxItems, setMailboxItems] = useState<ChatItem[]>([]);
  const [loadingOlder, setLoadingOlder] = useState(false);
  const [hasOlder, setHasOlder] = useState(false);
  const oldestLoaded = useRef<number | undefined>(undefined);
  const historyLoopId = useRef<string>("");

  useEffect(() => {
    setLoop(null); setEvents([]); setJournal([]); setJournalOn(false); setJournalOlder(false); journalLoaded.current = false; setJournalSettled(false); setSteerTexts({}); setError(null);
    setHistoryItems([]); setHasOlder(false); oldestLoaded.current = undefined; historyLoopId.current = "";
    setMailboxItems([]); setTodos(null); setRecovered(NOTHING_RECOVERED);
  }, [loopId]);

  const read = useCallback(async () => {
    if (!loopId) return;
    try {
      const detail = await rpc<LoopDetail>("loop.show", { id: loopId, includeBlueprint: false });
      setLoop(detail);
      // What the loop is still owed (S76): the block of questions its parked
      // step asked (answered and pending), a parked gate (it rides `loop.show`),
      // and any instruction queued but not yet delivered.
      setRecovered(await readRecovered(detail));
      // The mailbox reads from the SAME rows the events announce
      // (MB7/MB6): whatever the live ring has forgotten, this restores
      // verbatim; whatever the ring still holds, the merge refuses to
      // draw twice. An unreadable mailbox is the loop's own silence —
      // the chat keeps working without it and says nothing invented.
      try {
        const box = await rpc<{ messages?: AgentMessageRowLike[] }>("agent.messages", { loopId });
        setMailboxItems(mailboxItemsFromRows(box.messages ?? []));
      } catch { setMailboxItems([]); }
      // S75 TD15: the list is LOADED when the loop opens, not replayed from
      // events — it is on screen before the agent does anything new. A daemon
      // without the method (older build) leaves the strip absent, not empty.
      try {
        const t = await rpc<LoopTodos>("loop.todos", { id: loopId });
        if (Array.isArray(t?.items)) setTodos(t); // anything else is not a list: no strip
      } catch { /* no todo door */ }
      setError(null);
    } catch (cause) { setError(toRpcError(cause).message); }
  }, [loopId]);

  useEffect(() => controller?.registerReader(read), [controller, read]);
  const isCurrent = live.state === "current";
  useEffect(() => { if (isCurrent) void read(); }, [isCurrent, read]);

  const generation = live.state === "current" ? live.generation : -1;
  useEffect(() => {
    if (!isCurrent || !loopId) return;
    // The StrictMode-safe shape ChatPage documents: a cleanup that runs before
    // the async setup resolves must still tear the listener down.
    let disposed = false;
    let stop: (() => void) | undefined;
    let subscriptionId = "";
    void (async () => {
      try {
        stop = await onEvent((raw) => {
          const event = raw as AeonEvent;
          // Chunks arrive many times a second; they are gathered and drawn a few times a second instead, so the page
          // is rebuilt per frame of reading, not per word written.
          pending.current.push(event);
          if (!flushTimer.current) {
            flushTimer.current = setTimeout(() => {
              flushTimer.current = undefined;
              const batch = pending.current;
              pending.current = [];
              setEvents((old) => appendLive(old, batch, KEEP_EVENTS));
            }, LIVE_FLUSH_MS);
          }
          const p = (event.payload ?? {}) as Record<string, unknown>;
          if (event.kind === "todo.updated" && p.loopId === loopId) setTodos(todosFromEvent(p));
          // S76: what the loop is owed changes with these too (a question answered
          // elsewhere, an instruction delivered), so the store is re-read.
          const owedChanged = event.kind.startsWith("question.") || event.kind.startsWith("step.steer.");
          // A finished model turn or step moves the cost (the daemon saves it at each checkpoint), so the loop is
          // re-read then, not only on loop events.
          const costMoved = p.loopId === loopId && (event.kind === "step.turn.completed" || event.kind === "step.done");
          if ((p.loopId === loopId && event.kind.startsWith("loop.")) || owedChanged || costMoved) {
            clearTimeout(timer.current);
            timer.current = setTimeout(() => void read(), REFRESH_DEBOUNCE_MS);
          }
        });
        if (disposed) { stop(); return; }
        // The journal first: this loop's latest window, then a subscription from the daemon's head, so the window and
        // the live tail join with no gap and no overlap. (Replaying from 0 returned the oldest thousand events of the
        // whole daemon, which is why a reopened loop came back empty.) A daemon without the method falls back to 0.
        let after = 0;
        try {
          const win = await rpc<JournalWindow>("loop.events", { id: loopId, limit: JOURNAL_WINDOW });
          if (disposed) return;
          // A daemon that answers with anything but a window is one without the journal.
          if (!Array.isArray(win?.events)) throw new Error("no journal");
          setJournal((cur) => mergeEvents(cur, win.events));
          setJournalOn(true);
          if (!journalLoaded.current) { journalLoaded.current = true; setJournalOlder(win.hasOlder); }
          after = win.head;
        } catch { /* an older daemon: the live ring and the transcript, as before */ }
        if (!disposed) setJournalSettled(true);
        const sub = await rpcTyped("subscribe", { after, filter: { kinds: KINDS } });
        subscriptionId = sub.subscriptionId;
        if (disposed) void rpc("unsubscribe", { subscriptionId });
      } catch (cause) { if (!disposed) setError(toRpcError(cause).message); }
    })();
    return () => {
      disposed = true; stop?.(); clearTimeout(timer.current); clearTimeout(flushTimer.current); flushTimer.current = undefined; pending.current = [];
      if (subscriptionId) void rpc("unsubscribe", { subscriptionId });
    };
  }, [isCurrent, generation, loopId, read]);

  const stepIds = useMemo(() => stepIdsOf(loop), [loop]);
  const scopedStream = useMemo(() => scopeToLoop(events, loopId, stepIds), [events, loopId, stepIds]);
  // Steps the events carry whole turns for. A step the journal knows only partly (it ran before turns were
  // recorded) is drawn from the transcript instead, so its calls are not drawn twice.
  const covered = useMemo(() => stepsWithTurns([...journal, ...scopedStream]), [journal, scopedStream]);
  // Only the JOURNAL's events are thinned for legacy steps; what arrived live is always drawn.
  const scopedLive = useMemo(() => mergeEvents(withoutLegacyDetail(journal, covered), scopedStream), [journal, scopedStream, covered]);
  // The parked question joins the stream as the event the ring forgot — unless
  // the ring still holds it, in which case it is drawn once, from the event.
  const scoped = useMemo(() => withRecovered(scopedLive, recovered, loop), [scopedLive, recovered, loop]);
  const recoveredSteerTexts = useMemo(() => Object.fromEntries(recovered.steers.map((x) => [x.steerId, x.text])), [recovered]);
  const liveItems = useMemo(() => chatItemsFromEvents(scoped, { steerTexts: { ...recoveredSteerTexts, ...steerTexts } }), [scoped, steerTexts, recoveredSteerTexts]);
  const eventSession = useMemo(() => deriveSession(scoped), [scoped]);
  // The daemon's per-step summaries: every step the loop ran, with its totals, however long the loop is. They move
  // when a step's turn, call or end is journalled.
  const stepsMoved = useMemo(() => {
    for (let i = scopedStream.length - 1; i >= 0; i -= 1) {
      const k = scopedStream[i]!.kind;
      if (k === "step.turn.completed" || k === "step.done" || k === "loop.step.dispatched" || k === "step.tool.end") return scopedStream[i]!.seq;
    }
    return 0;
  }, [scopedStream]);
  const steps = useLoopSteps(loopId, isCurrent && loop !== null, `${stepsMoved}:${loop?.status ?? ""}`);
  const session = useMemo(() => withLoopRuns(eventSession, loop, steps.summaries), [eventSession, loop, steps.summaries]);
  // The saved status lags the events (it stays `waiting` until a resumed step ends); the page shows the true one.
  const effectiveLoop = useMemo(() => {
    if (!loop) return loop;
    // The store's own word wins while it still holds a question or a gate owed to the person.
    if (recovered.questions.length > 0 || recovered.gate) return loop;
    const status = statusFromEvents(loop.status, mergeEvents(journal, scopedStream));
    return status === loop.status ? loop : { ...loop, status };
  }, [loop, journal, scopedStream, recovered]);
  const rememberSteer = useCallback((steerId: number, text: string) => setSteerTexts((t) => ({ ...t, [steerId]: text })), []);

  // Steps the live mechanism actually has turns for — never `stepIdsOf(loop)`,
  // which names every step the loop ever ran whether or not the ring still
  // holds anything for it. Only a step NOT in this set is missing from
  // `liveItems`, and only that step's turns are worth reading from the store.
  const coveredStepIds = covered;

  const loadOlder = useCallback(() => {
    if (!loop || loadingOlder) return;
    // The journal first: the window immediately before the oldest event loaded. Nothing is evicted when the person
    // scrolls up (the old transcript window dropped its NEWEST turns here, which is where a blocked reason lives).
    if (journalOn && journalOlder) {
      const oldest = journal[0]?.seq;
      if (oldest === undefined) return;
      setLoadingOlder(true);
      void (async () => {
        try {
          const page = await rpc<JournalWindow>("loop.events", { id: loop.id, before: oldest, limit: JOURNAL_WINDOW });
          if (!Array.isArray(page?.events)) { setJournalOlder(false); return; }
          setJournal((cur) => mergeEvents(cur, page.events));
          setJournalOlder(page.hasOlder);
        } catch (cause) {
          setError(toRpcError(cause).message);
        } finally {
          setLoadingOlder(false);
        }
      })();
      return;
    }
    if (historyLoopId.current === loop.id && !hasOlder && oldestLoaded.current !== undefined) return;
    const { slug, root } = loop;
    setLoadingOlder(true);
    void (async () => {
      try {
        const page = await rpc<LoopTranscriptRpcResult>("loop.transcript", {
          slug, componentRoot: root, before: oldestLoaded.current, limit: HISTORY_WINDOW,
        });
        const fresh = page.messages.filter((m) => !coveredStepIds.has(m.stepId));
        const pageItems = chatItemsFromLoopTranscriptPage(fresh);
        // Never trimmed: dropping the newest turns here lost what the person had just scrolled past.
        setHistoryItems((current) => [...pageItems, ...current]);
        oldestLoaded.current = page.messages[0]?.id ?? oldestLoaded.current;
        setHasOlder(page.hasOlder);
        historyLoopId.current = loop.id;
      } catch (cause) {
        setError(toRpcError(cause).message);
      } finally {
        setLoadingOlder(false);
      }
    })();
  }, [loop, loadingOlder, hasOlder, coveredStepIds, journal, journalOn, journalOlder]);

  // Bootstrap: once a loop is open and the live mechanism's own coverage is
  // known, pull one durable page so the window starts full — "picture
  // perfect," the same last-N-turns view either way, rather than whatever
  // thin slice the live ring happens to still hold for a reopened loop.
  useEffect(() => {
    if (!loop || !journalSettled || historyLoopId.current === loop.id) return;
    if (liveItems.length >= HISTORY_WINDOW) return;
    loadOlder();
    // loadOlder is intentionally not a dependency: it closes over state that
    // changes on every call, and re-running this effect from THAT would
    // refire the bootstrap on every keystroke of typed history — it should
    // fire once per loop, when enough of the window is still visibly empty.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [loop?.id, liveItems.length, journalSettled]);

  // The saved transcript draws the question tool CALL of the parked step as a
  // plain tool row; the recovered question card is the live form of that same
  // call, so the row is dropped rather than shown twice.
  const askedSteps = useMemo(() => new Set(recovered.questions.flatMap((q) => q.stepId === undefined ? [] : [q.stepId])), [recovered]);
  // Every other question the loop asked is put back as its answered card, where
  // the call was, from the store's rows (the transcript only has the call).
  const history = useMemo(() => withStepEnds(withPastQuestions(askedSteps.size === 0 ? historyItems
    : historyItems.filter((i) => !(i.kind === "tool" && i.tool === "question" && i.stepId !== undefined && askedSteps.has(i.stepId))), recovered.past), loop, coveredStepIds),
    [historyItems, askedSteps, recovered.past, loop, coveredStepIds]);
  const items = useMemo(() => mergeMailboxItems([...history, ...liveItems], mailboxItems), [history, liveItems, mailboxItems]);

  return { loop: effectiveLoop, steps, events: scoped, items, session, steerTexts, todos, setTodos, rememberSteer, connected: isCurrent, error, refresh: read, loadOlder, loadingOlder, hasOlder: hasOlder || journalOlder };
}
