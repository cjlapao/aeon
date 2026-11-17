import { useCallback, useEffect, useState } from "react";
import { Alert, Button, FormField, Input, Panel, Pill } from "../../../components/kit";
import { rpc, toRpcError, type RpcError } from "../../../lib/bridge";
import { onEvent } from "../../../lib/bridge";
import { registerBenchPage } from "../lib/benchRegistry";
import type { AgentMessageRowLike } from "../../../components/agent-chat";

/**
 * S70 T4 — the agent↔agent mailbox, where it will be consumed.
 *
 * # What this page is for
 *
 * S70 gave the agents a mailbox of their own: one agent asks another and
 * the answer comes back as a ROW, never a waiting process. Rows are the
 * kind of thing whose health is invisible from the loop screen — an ask
 * never delivered, a reply settled twice, a thread that was named and
 * never was. This page reads the ONE door that exists (`agent.messages`,
 * MB7) and shows the rows as the store settled them:
 *
 *   - `from → to · body` — the filing agent's OWN words, verbatim (MB5);
 *     this renderer adds no adjectives and rewrites nothing
 *   - the thread by its header row (MB3: a thread IS its first ask's id),
 *     so one row seen here names the number to ask for next
 *   - the state the row is in — posted, delivered (a boundary folded it
 *     into a turn), answered (a reply settled the ask through its link)
 *   - the honest ABSENCE (P31): naming a thread the loop does not have
 *     answers "the loop holds no thread N", never a blank a reader could
 *     only mistrust — and an empty mailbox says the loop holds no rows,
 *     which is a different sentence from silence.
 *
 * # What this page does NOT do
 *
 * It is a **test bench** (AGENTS §4b), not a designed experience. And it
 * deliberately cannot AUTHOR anything: there is no compose box, because
 * MB7 rules the person's door to a loop is steer and an agent's door is
 * the ask tool behind the policy gate — a write control here would be the
 * second hand the ruling forbids. If this page could send a message, the
 * mailbox would have a door that bypasses every bound on the tool.
 */

interface ReadResult {
  loopId: string;
  thread?: number;
  absent: boolean;
  messages: AgentMessageRowLike[];
}

const STATE_TONE = { posted: "slate", delivered: "blue", answered: "green" } as const;

function stateOf(r: AgentMessageRowLike, asked: Set<number>): keyof typeof STATE_TONE {
  if (asked.has(r.id)) return "answered";
  return r.deliveredAt ? "delivered" : "posted";
}

export function AgentMailboxPage({ connected }: { connected: boolean }) {
  const [loopId, setLoopId] = useState("");
  const [thread, setThread] = useState("");
  const [result, setResult] = useState<ReadResult | null>(null);
  const [error, setError] = useState<RpcError | string | null>(null);
  const [loading, setLoading] = useState(false);
  const [reads, setReads] = useState(0);

  const read = useCallback(async () => {
    if (!loopId.trim()) return;
    setLoading(true);
    try {
      const params: Record<string, unknown> = { loopId: loopId.trim() };
      const t = Number(thread.trim());
      if (thread.trim() !== "" && Number.isInteger(t) && t > 0) params.thread = t;
      setResult(await rpc<ReadResult>("agent.messages", params));
      setReads((n) => n + 1);
      setError(null);
    } catch (cause) {
      setResult(null);
      setError(toRpcError(cause));
    } finally {
      setLoading(false);
    }
  }, [loopId, thread]);

  // Live and reload read the SAME rows (S39's rule, MB6): when the daemon
  // announces any exchange movement for the loop on screen, re-read —
  // the read is the truth, the event is the paint trigger.
  useEffect(() => {
    if (!connected || !loopId.trim()) return;
    let stop: (() => void) | undefined;
    void (async () => {
      try {
        stop = await onEvent((raw) => {
          const ev = raw as { kind?: string; payload?: Record<string, unknown> };
          if (ev.kind?.startsWith("agent.message.") && ev.payload?.loopId === loopId.trim()) void read();
        });
      } catch { /* a bridge that will not listen is what the error panel says */ }
    })();
    return () => stop?.();
  }, [connected, loopId, read]);

  // The scripted conversation (the spec's gallery): the three states —
  // an ask awaiting its reply, a delivered row, an answered thread — and
  // the honest absence, all visible WITHOUT a loop that happens to hold
  // them. The banner says SCRIPTED because a bench that passes off its
  // script as the daemon's answer is the failure §4b exists to prevent.
  const showScript = () => setResult({
    loopId: "scripted — not read from the daemon", absent: false,
    messages: [
      { id: 12, thread: 12, fromAgent: "planner", toAgent: "writer", body: "what framing should the intro take?", expectReply: true, createdAt: "2026-10-31T10:00:00Z" },
      { id: 13, thread: 12, fromAgent: "writer", toAgent: "planner", body: "the framing is this, verbatim", expectReply: false, resultOf: 12, deliveredAt: "2026-10-31T10:02:00Z", createdAt: "2026-10-31T10:01:30Z" },
      { id: 21, thread: 21, fromAgent: "planner", toAgent: "reviewer", body: "is the second chapter too long?", expectReply: true, createdAt: "2026-10-31T10:05:00Z" },
    ],
  });
  const showScriptedAbsence = () => setResult({ loopId: "scripted — not read from the daemon", thread: 99, absent: true, messages: [] });

  const asked = new Set<number>();
  for (const r of result?.messages ?? []) {
    if (typeof r.resultOf === "number" && r.resultOf > 0) asked.add(r.resultOf);
  }
  const threads = new Set((result?.messages ?? []).map((r) => r.thread));

  return <div className="space-y-4">
    <Panel title="Agent mailbox — the one door is a read">
      <p className="mb-3 text-sm text-slate-600 dark:text-slate-300!">
        <code>agent.messages</code> lists a loop&apos;s agent↔agent rows (MB7). There is
        no compose box on purpose: the person&apos;s door to a loop is <em>steer</em>; an
        agent&apos;s door is the <code>ask_agent</code> tool behind the policy gate. A row
        filed here would be a row that bypassed every bound on that tool.
      </p>
      <div className="flex flex-wrap items-end gap-3">
        <FormField label="Loop id" className="min-w-64 flex-1">
          <Input value={loopId} onChange={(e) => setLoopId(e.target.value)} placeholder="l-…" />
        </FormField>
        <FormField label="Thread (optional — a thread is its first ask's id)" className="w-56">
          <Input value={thread} onChange={(e) => setThread(e.target.value)} placeholder="e.g. 12" inputMode="numeric" />
        </FormField>
        <Button onClick={() => void read()} disabled={!connected || loading || !loopId.trim()}>
          {loading ? "Reading…" : "Read mailbox"}
        </Button>
        <Button onClick={showScript} variant="ghost">Show the scripted exchange</Button>
        <Button onClick={showScriptedAbsence} variant="ghost">Show a said absence</Button>
      </div>
      {result?.loopId.startsWith("scripted") && <Alert tone="amber" className="mt-3">Scripted gallery — these rows are this page&apos;s own fixtures, showing the states (awaiting · delivered · answered · absent). Nothing here was read from a daemon, and nothing here can reach one: there is no write door to reach.</Alert>}
      {!connected && <Alert tone="amber" className="mt-3">Not connected — the mailbox lives behind the daemon&apos;s socket; nothing here is invented while the pill is not green.</Alert>}
      {typeof error === "string" ? <Alert tone="red" className="mt-3">{error}</Alert>
        : error ? <Alert tone="red" className="mt-3">{error.message}</Alert> : null}
    </Panel>

    {result && <Panel title={`Rows for ${result.loopId}`}>
      <div className="mb-2 flex items-center gap-2 text-xs text-slate-500">
        <Pill tone={result.messages.length > 0 ? "green" : "neutral"}>
          {result.messages.length} row{result.messages.length === 1 ? "" : "s"}
        </Pill>
        {result.thread !== undefined && <Pill tone={result.absent ? "amber" : "blue"}>
          {result.absent ? `the loop holds no thread ${result.thread} — said, not silenced` : `thread ${result.thread}`}
        </Pill>}
        {result.messages.length > 0 && <Pill tone="blue">{threads.size} thread{threads.size === 1 ? "" : "s"}</Pill>}
        <span className="ml-auto font-mono">reads: {reads}</span>
      </div>
      {result.messages.length === 0
        ? <p className="py-4 text-center text-sm text-slate-500">
          {result.absent ? `Thread ${result.thread} does not exist in this loop. That is the read's answer (P31), not a silence.`
            : "This loop holds no exchange rows. Its agents have not spoken to each other — or have not yet."}
        </p>
        : <ul className="space-y-1.5">
          {result.messages.map((r) => {
            const state = stateOf(r, asked);
            return <li key={r.id} data-testid="mailbox-row"
              className="flex flex-wrap items-baseline gap-2 rounded-md border border-slate-200 bg-white px-2.5 py-1.5 font-mono text-sm dark:border-white/10! dark:bg-white/5!">
              <span className="text-slate-400">#{r.id}</span>
              <span className="font-semibold text-indigo-700 dark:text-indigo-300!">{r.fromAgent}</span>
              <span className="text-slate-400">→</span>
              <span className="font-semibold text-indigo-700 dark:text-indigo-300!">{r.toAgent}</span>
              <span className="text-slate-400">·</span>
              <span className="min-w-0 flex-1 text-slate-700 dark:text-slate-200!">{r.body}</span>
              <Pill tone={STATE_TONE[state]}>{state}{state === "posted" && r.expectReply ? " · awaiting reply" : ""}</Pill>
              <Pill tone="neutral">thread {r.thread}</Pill>
              {r.expectReply && state === "posted" && <Pill tone="amber">awaits a reply</Pill>}
            </li>;
          })}
        </ul>}
    </Panel>}
  </div>;
}

registerBenchPage({
  id: "mailbox",
  label: "Agent mailbox",
  path: "/bench/mailbox",
  order: 4600,
  needsConnection: true,
  render: (ctx) => <AgentMailboxPage connected={ctx.connected} />,
});
