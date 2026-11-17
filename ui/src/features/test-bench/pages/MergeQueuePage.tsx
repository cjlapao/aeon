import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, FormField, Input, Panel, Pill, Select } from "../../../components/kit";
import { toRpcError, type RpcError } from "../../../lib/bridge";
import { registerBenchPage } from "../lib/benchRegistry";
import {
  dequeueRow,
  enqueueRow,
  landRow,
  listComponentRoots,
  listQueue,
  readQueueStatus,
  readVocabulary,
  reorderRow,
  refusalAckDigest,
  retryRow,
  showQueueItem,
  writeSettings,
  type MethodEntry,
  type QueueConfig,
  type QueueRow,
  type ShowResult,
  type StatusResult,
  type Vocabulary,
} from "./mergeQueueRpc";

/**
 * S60 T11 — the merge queue, on the bench, speaking only the daemon's words.
 *
 * # What this page is for
 *
 * S60 gave the queue ten doors (T0) and the CLI learned to knock on all ten
 * (T10). This page is the SAME surface a person can click: it reads
 * `mergequeue.list` / `show` / `status` / `vocabulary` and writes through the
 * five human doors the vocabulary itself enumerates. Nothing here is a
 * product panel (§4b: the bench is the test bench, not S9's designed
 * experience), and nothing here is believed: every word rendered is either a
 * daemon answer verbatim or a `bench.mergeQueue.*` label — the page owns no
 * state word, no reason word, no mode word, no action word. The guard
 * (`mergeQueueRpc.guard.test.ts`) greps this file to hold that line, and the
 * action buttons are RENDERED FROM `mergequeue.vocabulary`'s own method
 * table, so widening the daemon widens the page and this file never learns a
 * new verb.
 *
 * # The refusals are the interface
 *
 * The queue refuses more than it accepts, and every refusal is the point of
 * the design: a land names the row's digest, an auto/resolve settings write
 * widens authority and demands the refusal's own digest echoed back, a retry
 * on a row that did not fail says so. The page shows each refusal verbatim,
 * keeps whatever the person had drafted, and sends the follow-up ONLY after
 * the person presses the button — declining sends nothing at all. Rows move
 * when a response says they moved (P7: never optimistic); an offline bench
 * disables writes and keeps showing the last rows read, labelled as such.
 */

/** A refusal caught on the way out, kept with its parts so the render can
 *  show what/means/todo exactly as the daemon wrote them. */
interface Refusal {
  message: string;
  what: string;
  means: string;
  todo: string;
}

function refusalOf(error: unknown): Refusal {
  const rpc: RpcError = toRpcError(error);
  return { message: rpc.message, what: rpc.data?.what ?? "", means: rpc.data?.means ?? "", todo: rpc.data?.todo ?? "" };
}

/** The staleness, rendered in the three looks the daemon can present: a
 *  state word alone, a state word with its reason, and the read that could
 *  not answer — which shows its reason and NEVER a number. */
function StalenessCell({ row }: { row: QueueRow }) {
  const { t } = useI18n();
  const staleness = (row as { staleness?: { state?: string; reason?: string } }).staleness;
  const state = typeof staleness?.state === "string" ? staleness.state : "";
  const reason = typeof staleness?.reason === "string" ? staleness.reason : "";
  if (state === "") {
    return (
      <span data-testid="mq-staleness" data-shape="unread">
        {t("bench.mergeQueue.staleness.unread")}
        {reason !== "" && <em> — {reason}</em>}
      </span>
    );
  }
  return (
    <span data-testid="mq-staleness" data-shape={reason !== "" ? "state-reason" : "state"}>
      {state}
      {reason !== "" && <em> — {reason}</em>}
    </span>
  );
}

interface Draft {
  mode: string;
  conflictPolicy: string;
  landMethod: string;
  maxCatchUps: string;
}

const draftOf = (config: QueueConfig | undefined, vocabulary: Vocabulary | undefined): Draft => ({
  mode: config?.mode ?? vocabulary?.modes[0] ?? "",
  conflictPolicy: config?.conflictPolicy ?? vocabulary?.conflictPolicies[0] ?? "",
  landMethod: config?.landMethod ?? vocabulary?.landMethods[0] ?? "",
  maxCatchUps: config ? String(config.maxCatchUps) : "",
});

/** A pending daemon-gated confirmation: the words the daemon said, and the
 *  single call that fires ONLY if the person presses the button. */
interface Pending {
  title: string;
  statement: string;
  press: string;
  run: () => Promise<void>;
}

export function MergeQueuePage({ connected }: { connected: boolean }) {
  const { t } = useI18n();
  const [roots, setRoots] = useState<Awaited<ReturnType<typeof listComponentRoots>>>([]);
  const [root, setRoot] = useState("");
  const [rows, setRows] = useState<QueueRow[]>([]);
  const [includeTerminal, setIncludeTerminal] = useState(false);
  const [status, setStatus] = useState<StatusResult | null>(null);
  const [vocabulary, setVocabulary] = useState<Vocabulary | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const [detail, setDetail] = useState<ShowResult | null>(null);
  const [draft, setDraft] = useState<Draft | null>(null);
  const [enqueueDraft, setEnqueueDraft] = useState({ baseBranch: "", featureBranch: "", headSha: "", attended: false });
  const [refusal, setRefusal] = useState<Refusal | null>(null);
  /** A settings refusal that named an acknowledgement digest: the write
   *  widens authority, so the retry carries the digest back — but only from
   *  the button, never from the first refusal alone. */
  const [pendingAck, setPendingAck] = useState<{ digest: string; refusal: Refusal } | null>(null);
  const [pending, setPending] = useState<Pending | null>(null);
  const [busy, setBusy] = useState(false);
  const connectedRef = useRef(connected);
  connectedRef.current = connected;

  // The reads. `connected` flipping true re-reads; going offline keeps the
  // rows already on screen (the last thing the daemon actually said).
  const refresh = useCallback(async () => {
    if (!connected) return;
    try {
      const [queue, vocab] = await Promise.all([listQueue(includeTerminal), vocabulary === null ? readVocabulary() : Promise.resolve(vocabulary)]);
      setRows(queue);
      setVocabulary(vocab);
      if (root !== "") setStatus(await readQueueStatus(root));
    } catch (error) {
      setRefusal(refusalOf(error));
    }
  }, [connected, includeTerminal, root, vocabulary]);

  useEffect(() => {
    if (!connected) return;
    void (async () => {
      try {
        const list = await listComponentRoots();
        setRoots(list);
        const first = list[0];
        if (first !== undefined) setRoot((current) => (current === "" ? first.root : current));
      } catch {
        setRoots([]);
      }
    })();
  }, [connected]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const pick = useCallback(async (id: string) => {
    setSelected(id);
    setDetail(null);
    try {
      setDetail(await showQueueItem(id));
    } catch (error) {
      setRefusal(refusalOf(error));
    }
  }, []);

  /** Fold one write's ItemView answer back into the rows it moved (P7). */
  const fold = useCallback((updated: QueueRow) => {
    setRows((current) => current.map((row) => (row.id === updated.id ? updated : row)));
    setSelected(updated.id);
    setSelected((id) => {
      if (id === updated.id) void (async () => {
        try {
          setDetail(await showQueueItem(updated.id));
        } catch {
          /* the row moved out from under the pane; the next refresh settles it */
        }
      })();
      return id;
    });
  }, []);

  const act = useCallback(async (run: () => Promise<QueueRow>) => {
    setBusy(true);
    setRefusal(null);
    try {
      fold(await run());
    } catch (error) {
      setRefusal(refusalOf(error));
    } finally {
      setBusy(false);
    }
  }, [fold]);

  const askPending = useCallback((pending: Pending) => setPending(pending), []);
  const declinePending = useCallback(() => setPending(null), []);

  const pressPending = useCallback(async () => {
    const fire = pending?.run;
    setPending(null);
    if (!fire) return;
    setBusy(true);
    setRefusal(null);
    try {
      await fire();
      await refresh();
    } catch (error) {
      setRefusal(refusalOf(error));
    } finally {
      setBusy(false);
    }
  }, [pending, refresh]);

  // ── the settings draft, aimed from the daemon's own status answer ──────
  const config = status?.config;
  useEffect(() => {
    if (config) setDraft(draftOf(config, vocabulary ?? undefined));
  }, [config, vocabulary]);

  const sendSettings = useCallback(async (acknowledged?: string) => {
    if (draft === null || root === "") return;
    setBusy(true);
    setRefusal(null);
    setPendingAck(null);
    try {
      const answer = await writeSettings({
        componentRoot: root,
        mode: draft.mode,
        conflictPolicy: draft.conflictPolicy,
        landMethod: draft.landMethod,
        maxCatchUps: Number(draft.maxCatchUps),
        ...(acknowledged !== undefined ? { acknowledged } : {}),
      });
      setStatus((current) => (current === null ? current : { ...current, config: answer.config }));
      setRefusal({ message: answer.statement, what: answer.statement, means: "", todo: "" });
    } catch (error) {
      const caught = refusalOf(error);
      setRefusal(caught);
      const digest = refusalAckDigest({ what: caught.what, means: caught.means, todo: caught.todo }, caught.message);
      if (digest !== null) setPendingAck({ digest, refusal: caught });
    } finally {
      setBusy(false);
    }
  }, [draft, root]);

  const humanMethods = useMemo<MethodEntry[]>(
    () => (vocabulary?.methods ?? []).filter((method) => method.door === "human"),
    [vocabulary],
  );
  const hasDoor = useCallback((suffix: string) => humanMethods.some((method) => method.name.endsWith(`.${suffix}`)), [humanMethods]);

  const visibleRows = useMemo(() => (root === "" ? rows : rows.filter((row) => row.componentRoot === root)), [rows, root]);
  const laneBefore = useCallback(
    (row: QueueRow) => visibleRows.filter((other) => other.base === row.base && other.position < row.position).sort((a, b) => b.position - a.position)[0],
    [visibleRows],
  );

  const writesDisabled = busy || !connected;

  // ── render ─────────────────────────────────────────────────────────────
  return <div className="flex flex-col gap-4">
    <Panel title={t("bench.mergeQueue.title")} subtitle={t("bench.mergeQueue.subtitle")} data-testid="merge-queue-page">
      {!connected && <Alert intent="warning" title={t("bench.mergeQueue.offline.title")} description={t("bench.mergeQueue.offline.body")} />}
      {refusal !== null && <Alert intent="danger" title={t("bench.mergeQueue.refusal")} description={`${refusal.what}${refusal.means ? ` — ${refusal.means}` : ""}${refusal.todo ? ` — ${refusal.todo}` : ""}`} data-testid="mq-refusal" />}
      <div className="flex flex-wrap items-end gap-3">
        <FormField label={t("bench.mergeQueue.root.label")}>
          <Select value={root} onChange={(e) => setRoot(e.target.value)} data-testid="mq-root">
            {roots.length === 0 && <option value="">{t("bench.mergeQueue.root.none")}</option>}
            {roots.map((entry) => <option key={entry.id} value={entry.root}>{entry.name} · {entry.slug}</option>)}
          </Select>
        </FormField>
        <Button
         
          onClick={() => setIncludeTerminal((current) => !current)}
          disabled={writesDisabled}
          data-testid="mq-terminal-toggle"
        >{`${t("bench.mergeQueue.terminal")}: ${includeTerminal ? t("bench.mergeQueue.yes") : t("bench.mergeQueue.no")}`}</Button>
        <Button onClick={() => void refresh()} disabled={!connected} data-testid="mq-refresh">{t("bench.mergeQueue.refresh")}</Button>
      </div>
      {status !== null && <p className="text-xs text-slate-500" data-testid="mq-config-summary">
        {t("bench.mergeQueue.summary", { mode: status.config.mode, policy: status.config.conflictPolicy, method: status.config.landMethod, catchUps: String(status.config.maxCatchUps), by: status.config.updatedBy })}
      </p>}
    </Panel>

    <Panel title={t("bench.mergeQueue.queue")} data-testid="mq-list">
      {visibleRows.length === 0 && <p className="text-sm text-slate-500" data-testid="mq-empty">{t("bench.mergeQueue.empty")}</p>}
      <ul className="flex flex-col gap-1">
        {visibleRows.map((row) => <li key={row.id}>
          <button
            type="button"
            onClick={() => void pick(row.id)}
            className={`flex w-full flex-wrap items-center gap-2 rounded border px-2 py-1 text-left text-sm ${selected === row.id ? "border-indigo-400" : "border-slate-200 dark:border-slate-700"}`}
            data-testid={`mq-row-${row.id}`}
          >
            <Pill tone="slate">{row.position}</Pill>
            <Pill tone="blue" data-testid={`mq-status-${row.id}`}>{row.status}</Pill>
            <span className="font-semibold">{row.branch}</span>
            <span className="text-slate-400">→ {row.base}</span>
            {typeof row.prNumber === "number" && <Pill tone="neutral">PR #{row.prNumber}</Pill>}
            {typeof row.loop === "string" && <Pill tone="neutral">{row.loop}</Pill>}
            <StalenessCell row={row} />
            {row.attended && <Pill tone="amber" >{t("bench.mergeQueue.attended")}</Pill>}
            <span className="ml-auto text-xs text-slate-500">{row.enqueuedBy.kind}/{row.enqueuedBy.id}</span>
          </button>
        </li>)}
      </ul>
      <div className="mt-2 flex flex-wrap gap-2" data-testid="mq-row-actions">
        {hasDoor("retry") && <Button disabled={writesDisabled || selected === null} onClick={() => void act(() => retryRow(selected as string))} data-testid="mq-retry">{t("bench.mergeQueue.retry")}</Button>}
        {hasDoor("dequeue") && <Button disabled={writesDisabled || selected === null} onClick={() => void act(() => dequeueRow(selected as string))} data-testid="mq-dequeue">{t("bench.mergeQueue.dequeue")}</Button>}
        {hasDoor("reorder") && (() => {
          const row = visibleRows.find((entry) => entry.id === selected);
          const before = row !== undefined ? laneBefore(row) : undefined;
          return <Button
           
            disabled={writesDisabled || row === undefined || before === undefined}
            onClick={() => {
              if (row === undefined || before === undefined) return;
              void act(() => reorderRow(row.id, before.id));
            }}
            data-testid="mq-reorder"
          >{t("bench.mergeQueue.reorder")}</Button>;
        })()}
        {hasDoor("land") && (() => {
          const row = visibleRows.find((entry) => entry.id === selected);
          return <Button
           
            disabled={writesDisabled || row === undefined}
            onClick={() => {
              if (row === undefined) return;
              void (async () => {
                try {
                  const shown = await showQueueItem(row.id);
                  askPending({
                    title: t("bench.mergeQueue.land.title"),
                    statement: `${shown.item.branch} → ${shown.item.base} · ${shown.item.status} · ${shown.item.nextStep}`,
                    press: t("bench.mergeQueue.land.press"),
                    run: () => act(() => landRow(shown.item.id, shown.item.digest)),
                  });
                } catch (error) {
                  setRefusal(refusalOf(error));
                }
              })();
            }}
            data-testid="mq-land"
          >{t("bench.mergeQueue.land.submit")}</Button>;
        })()}
      </div>
    </Panel>

    {detail !== null && <Panel title={`${t("bench.mergeQueue.detail")} ${detail.item.branch}`} data-testid="mq-detail">
      <p className="text-sm" data-testid="mq-next-step">{detail.item.nextStep}</p>
      <ul className="mt-1 text-xs text-slate-600 dark:text-slate-300" data-testid="mq-transitions">
        {detail.transitions.map((step, index) => <li key={index}>
          {step.at} · {step.actorKind}/{step.actorId} · {step.fromStatus ?? "—"} → {step.toStatus}{step.detail !== undefined ? ` · ${step.detail}` : ""}
        </li>)}
      </ul>
      {detail.worktree !== undefined && <p className="mt-1 text-xs text-slate-500">{detail.worktree}</p>}
    </Panel>}

    <Panel title={t("bench.mergeQueue.enqueue.panel")} data-testid="mq-enqueue-panel">
      <div className="flex flex-wrap items-end gap-2">
        <FormField label={t("bench.mergeQueue.enqueue.base")}><Input data-testid="mq-enqueue-base" value={enqueueDraft.baseBranch} onChange={(e) => setEnqueueDraft({ ...enqueueDraft, baseBranch: e.target.value })} /></FormField>
        <FormField label={t("bench.mergeQueue.enqueue.branch")}><Input data-testid="mq-enqueue-branch" value={enqueueDraft.featureBranch} onChange={(e) => setEnqueueDraft({ ...enqueueDraft, featureBranch: e.target.value })} /></FormField>
        <FormField label={t("bench.mergeQueue.enqueue.head")}><Input data-testid="mq-enqueue-head" value={enqueueDraft.headSha} onChange={(e) => setEnqueueDraft({ ...enqueueDraft, headSha: e.target.value })} /></FormField>
        <FormField label={t("bench.mergeQueue.enqueue.attended")}><Select data-testid="mq-enqueue-attended" value={enqueueDraft.attended ? "y" : "n"} onChange={(e) => setEnqueueDraft({ ...enqueueDraft, attended: e.target.value === "y" })}>
          <option value="n">{t("bench.mergeQueue.no")}</option><option value="y">{t("bench.mergeQueue.yes")}</option>
        </Select></FormField>
        <Button disabled={writesDisabled || root === "" || !hasDoor("enqueue")} data-testid="mq-enqueue"
          onClick={() => void (async () => {
            const branch = enqueueDraft.featureBranch.trim();
            const base = enqueueDraft.baseBranch.trim();
            const head = enqueueDraft.headSha.trim();
            if (branch === "" || base === "" || head === "") {
              setRefusal({ message: t("bench.mergeQueue.enqueue.incomplete"), what: t("bench.mergeQueue.enqueue.incomplete"), means: "", todo: "" });
              return;
            }
            setBusy(true); setRefusal(null);
            try {
              fold(await enqueueRow({ componentRoot: root, baseBranch: base, featureBranch: branch, headSha: head, attended: enqueueDraft.attended }));
            } catch (error) {
              setRefusal(refusalOf(error));
            } finally {
              setBusy(false);
            }
          })()}>{t("bench.mergeQueue.enqueue.submit")}</Button>
      </div>
    </Panel>

    {draft !== null && vocabulary !== null && <Panel title={t("bench.mergeQueue.settings.panel")} data-testid="mq-settings">
      <div className="flex flex-wrap items-end gap-2">
        <FormField label={t("bench.mergeQueue.settings.mode")}><Select data-testid="mq-set-mode" value={draft.mode} onChange={(e) => setDraft({ ...draft, mode: e.target.value })}>{vocabulary.modes.map((word) => <option key={word} value={word}>{word}</option>)}</Select></FormField>
        <FormField label={t("bench.mergeQueue.settings.policy")}><Select data-testid="mq-set-policy" value={draft.conflictPolicy} onChange={(e) => setDraft({ ...draft, conflictPolicy: e.target.value })}>{vocabulary.conflictPolicies.map((word) => <option key={word} value={word}>{word}</option>)}</Select></FormField>
        <FormField label={t("bench.mergeQueue.settings.method")}><Select data-testid="mq-set-method" value={draft.landMethod} onChange={(e) => setDraft({ ...draft, landMethod: e.target.value })}>{vocabulary.landMethods.map((word) => <option key={word} value={word}>{word}</option>)}</Select></FormField>
        <FormField label={t("bench.mergeQueue.settings.catchUps")}><Input data-testid="mq-set-catchups" value={draft.maxCatchUps} onChange={(e) => setDraft({ ...draft, maxCatchUps: e.target.value })} /></FormField>
        <Button disabled={writesDisabled || !hasDoor("settings")} data-testid="mq-settings-apply" onClick={() => void sendSettings()}>{t("bench.mergeQueue.settings.apply")}</Button>
      </div>
      {pendingAck !== null && <div className="mt-2 rounded border border-amber-400 bg-amber-50 p-2 dark:border-amber-500 dark:bg-amber-950" role="dialog" aria-modal="true" data-testid="mq-ack-gate">
        <Alert intent="warning" title={t("bench.mergeQueue.settings.widening")} description={pendingAck.refusal.what} />
        <div className="mt-1 flex gap-2">
          <Button disabled={busy} data-testid="mq-ack-press" onClick={() => void sendSettings(pendingAck.digest)}>{t("bench.mergeQueue.settings.ackPress")}</Button>
          <Button disabled={busy} data-testid="mq-ack-decline" onClick={() => setPendingAck(null)}>{t("bench.mergeQueue.decline")}</Button>
        </div>
      </div>}
    </Panel>}

    {vocabulary !== null && <Panel title={t("bench.mergeQueue.vocabulary")} data-testid="mq-vocabulary">
      <p className="text-xs text-slate-500">{humanMethods.map((entry) => entry.name).join(" · ")}</p>
      <ul className="mt-1 text-xs text-slate-600 dark:text-slate-300">
        {vocabulary.states.map((state) => <li key={state.name} data-testid={`mq-vocab-${state.name}`}>
          <strong>{state.name}</strong> — {state.meaning} <em>({state.legalNext.join(" · ")})</em>
        </li>)}
      </ul>
    </Panel>}

    {pending !== null && <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4" role="dialog" aria-modal="true" data-testid="mq-pending">
      <div className="w-full max-w-lg rounded bg-white p-3 dark:bg-slate-900">
        <p className="text-sm font-semibold">{pending.title}</p>
        <p className="mt-1 text-xs text-slate-600 dark:text-slate-300" data-testid="mq-pending-statement">{pending.statement}</p>
        <div className="mt-2 flex gap-2">
          <Button disabled={busy} data-testid="mq-pending-press" onClick={() => void pressPending()}>{pending.press}</Button>
          <Button disabled={busy} data-testid="mq-pending-decline" onClick={declinePending}>{t("bench.mergeQueue.decline")}</Button>
        </div>
      </div>
    </div>}
  </div>;
}

registerBenchPage({
  id: "merge-queue",
  label: "Merge queue",
  path: "/bench/merge-queue",
  order: 4650,
  needsConnection: true,
  render: (ctx) => <MergeQueuePage connected={ctx.connected} />,
});
