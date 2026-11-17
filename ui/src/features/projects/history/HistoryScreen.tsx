import { useCallback, useState } from "react";
import { Link } from "react-router-dom";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, FormField, Input, Panel, Select } from "../../../components/kit";
import { toRpcError } from "../../../lib/bridge";
import type { LiveController, LiveState } from "../../../lib/live";
import { useComponentRoots } from "../useComponentRoots";
import { CoverageBanner } from "./CoverageBanner";
import { HistoryRows } from "./HistoryRows";
import { HistoryTranscript } from "./HistoryTranscript";
import { emptyScopeDraft, laneOptions, refusalSentence } from "./historyModel";
import type { ScopeDraft } from "./historyModel";
import { exportHistoryRows, historyExportFilename, saveHistoryExport, useHistoryRows, useHistoryVocabulary } from "./historyRpc";

/**
 * HistoryScreen — Projects → History (S53 T5, HV7; the fifth surface of the
 * S53 slice after the socket, the CLI, the agent's two reads and the
 * records arm).
 *
 * Four parts, in the order a person reads them: the scope and lane pickers,
 * the **coverage banner** (what this view shows, does not show, and cannot
 * say), the **rows** (each rendered from the daemon's own `describe`, with
 * its links), and the **drill-down** (`history.show` for one entry). Plus
 * one control that is not a read-only thing: the person's **export**, which
 * asks `history.export` and writes the payload it answers with.
 *
 * # What this screen never does
 *
 *  • **No write control of any kind.** There is no create, update, delete or
 *    edit affordance here because the slice has none (HV3) — the export is a
 *    read that downloads, gated on `canWrite` (a person, on a current
 *    connection) and disabled whenever the daemon is not answering. An agent
 *    has `history_list`/`history_show` and never this door.
 *  • **No vocabulary of its own.** The lane switch offers
 *    `history.vocabulary`'s `lanes` and nothing else — not one lane word is
 *    typed here (the acceptance criterion, pinned by the folder guard). An
 *    outcome word on a row is that row's own. Until the vocabulary has been
 *    read, the switch offers nothing and the screen says so.
 *  • **No second formatter.** Every daemon sentence — the floor statement, a
 *    row's cost and reason, a notShown reason, the empty-scope `statement`, a
 *    drill-down refusal — renders verbatim.
 *  • **No client-side sweep.** The pickers send filter words and the daemon
 *    groups and filters (HV7); the rows render in the answer's own order.
 *
 * Three connection states, three shapes: not current asks nothing at all,
 * disconnected names its reason, current asks and answers — including the
 * honest empty, which is the daemon's `statement`, never a fabricated row.
 */
export interface HistoryScreenProps {
  controller: LiveController;
  live: LiveState;
  /** @default `live.state === "current"` — the export is a person's act, and a
   *  person on a connection that is not current is not at the socket. */
  canWrite?: boolean;
  /** Start on this component root (the Work page's scope). */
  initialRoot?: string;
}

export function HistoryScreen({ controller, live, canWrite = live.state === "current", initialRoot = "" }: HistoryScreenProps) {
  const { t } = useI18n();
  const [draft, setDraft] = useState<ScopeDraft>({ ...emptyScopeDraft(), root: initialRoot });
  const [openId, setOpenId] = useState<string | null>(null);
  const [exportBusy, setExportBusy] = useState(false);
  const [exportRefusal, setExportRefusal] = useState<string | undefined>(undefined);
  const [exportedRows, setExportedRows] = useState<number | undefined>(undefined);

  const vocabulary = useHistoryVocabulary(controller, live);
  const roots = useComponentRoots(controller, live);
  const rows = useHistoryRows(draft, controller, live);

  const change = useCallback((next: ScopeDraft) => {
    setDraft(next);
    // A row opened under a different scope is a different entry's story.
    setOpenId(null);
    setExportedRows(undefined);
    setExportRefusal(undefined);
  }, []);

  // Three states, three shapes — a screen that asks while the connection is
  // not current shows an answer it never got (S49's rule, kept by triage).
  // The export is shown DISABLED rather than vanished: an affordance that
  // disappears teaches nothing about why it is not there.
  if (live.state !== "current") {
    const offline = live.state === "disconnected";
    return (
      <div className="space-y-6" data-testid="history-screen">
        <HistoryHeader t={t} />
        <Panel title={t("history.manage.title")}>
          <div className="space-y-3">
            <Alert
              intent={offline ? "danger" : "warning"}
              title={offline ? t("history.manage.offline") : t("history.manage.reconnecting")}
              description={offline ? live.reason : t("history.manage.reconnectingHint")}
              data-testid={offline ? "history-offline" : "history-reconnecting"}
            />
            <div className="flex flex-wrap items-center gap-3">
              <Button size="sm" variant="soft" disabled data-testid="history-export">
                {t("history.manage.export.label")}
              </Button>
              <span className="text-xs text-slate-500" data-testid="history-export-person-only">
                {t("history.manage.export.personOnly")}
              </span>
            </div>
          </div>
        </Panel>
      </div>
    );
  }

  const canExport = canWrite && live.state === "current";

  const runExport = async () => {
    // Belt and door: the control is disabled otherwise, and the handler
    // refuses again — an export is never anyone else's act (HV3).
    if (!canExport || exportBusy) return;
    setExportBusy(true);
    setExportRefusal(undefined);
    try {
      const result = await exportHistoryRows(draft);
      saveHistoryExport(result, historyExportFilename(draft));
      setExportedRows(result.rows.length);
    } catch (cause) {
      const refusal = toRpcError(cause);
      setExportRefusal(refusalSentence(refusal.message, refusal.data));
    } finally {
      setExportBusy(false);
    }
  };

  const lanes = laneOptions(vocabulary.data);

  return (
    <div className="space-y-6" data-testid="history-screen">
      <HistoryHeader t={t} />

      {vocabulary.error !== undefined && (
        // The retry is a SIBLING, not an Alert child: `Alert` renders a title
        // and a description and discards children (ui/README, and the note in
        // components/ErrorPanel.tsx). Until the vocabulary is read the lane
        // switch offers nothing, and this says why rather than falling back to
        // a list of words this screen invented.
        <div className="space-y-2" data-testid="history-vocabulary-failed">
          <Alert intent="danger" title={t("history.manage.vocabulary.failed")} description={vocabulary.error} />
          <button type="button" onClick={vocabulary.refetch} data-testid="history-vocabulary-retry">
            {t("history.manage.actions.retry")}
          </button>
        </div>
      )}

      <Panel title={t("history.manage.scope.title")} subtitle={t("history.manage.scope.subtitle")} data-testid="history-scope">
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
          <FormField label={t("history.manage.scope.rootLabel")} hint={roots.roots.length > 0 ? t("history.manage.scope.rootPicked") : t("history.manage.scope.rootHint")}>
            <Select
              value={draft.root}
              aria-label={t("history.manage.scope.rootLabel")}
              data-testid="history-scope-root"
              onChange={(e) => change({ ...draft, root: e.target.value })}
            >
              {/* "" is not a silence: it is the every-component span, the
                  merge's own reading, and the banner says which one is on. */}
              <option value="">{t("history.manage.scope.rootAll")}</option>
              {roots.roots.map((root) => (
                <option key={root.id} value={root.root}>
                  {root.name || root.slug || root.id}
                </option>
              ))}
            </Select>
          </FormField>

          <FormField label={t("history.manage.scope.laneLabel")} hint={lanes.length === 0 ? t("history.manage.scope.laneWaiting") : t("history.manage.scope.laneHint")}>
            <Select
              value={draft.lane}
              aria-label={t("history.manage.scope.laneLabel")}
              data-testid="history-scope-lane"
              onChange={(e) => change({ ...draft, lane: e.target.value })}
            >
              {/* The default lane is the daemon's, so it is the one option
                  that sends NOTHING rather than a word for it. */}
              <option value="">{t("history.manage.scope.laneDefault")}</option>
              {lanes.map((lane) => (
                // Served words only — the switch has no list of its own.
                <option key={lane} value={lane}>
                  {lane}
                </option>
              ))}
            </Select>
          </FormField>

          <FormField label={t("history.manage.scope.sinceLabel")} hint={t("history.manage.scope.sinceHint")}>
            <Input
              value={draft.since}
              placeholder="2026-11-01T00:00:00Z"
              aria-label={t("history.manage.scope.sinceLabel")}
              data-testid="history-scope-since"
              onChange={(e) => change({ ...draft, since: e.target.value })}
            />
          </FormField>
        </div>
      </Panel>

      <CoverageBanner draft={draft} controller={controller} live={live} />

      <Panel title={t("history.manage.rows.title")} subtitle={t("history.manage.rows.subtitle")} data-testid="history-rows-panel">
        <div className="space-y-3">
          <div className="flex flex-wrap items-center gap-3">
            {/* The person's door. Disabled, not hidden, when it cannot be used:
                the affordance is honest about why it is inert. */}
            <Button
              size="sm"
              variant="soft"
              disabled={!canExport || exportBusy}
              loading={exportBusy}
              data-testid="history-export"
              onClick={() => void runExport()}
            >
              {exportBusy ? t("history.manage.export.working") : t("history.manage.export.label")}
            </Button>
            {!canWrite && (
              <span className="text-xs text-slate-500" data-testid="history-export-person-only">
                {t("history.manage.export.personOnly")}
              </span>
            )}
          </div>

          {rows.error !== undefined && (
            <Alert intent="danger" title={t("history.manage.rows.failed")} description={rows.error} data-testid="history-rows-failed" />
          )}

          {rows.loading && <p className="text-sm text-slate-500" data-testid="history-rows-loading">{t("history.manage.rows.loading")}</p>}

          {rows.data !== undefined && rows.error === undefined && rows.data.rows.length === 0 && (
            // P31's honest empty: the daemon's OWN statement when it has one
            // ("no history — no live or archived loops in scope"), and an
            // explicit line when it did not send one. Never silence, never a
            // row that was not answered.
            <p className="text-sm text-slate-500" data-testid="history-rows-empty">
              {rows.data.statement ?? t("history.manage.rows.noStatement")}
            </p>
          )}

          {rows.data !== undefined && rows.data.rows.length > 0 && (
            <HistoryRows rows={rows.data.rows} openId={openId} onOpen={setOpenId} />
          )}

          {exportedRows !== undefined && (
            <p className="text-xs text-slate-500" data-testid="history-exported">
              {t("history.manage.export.done", { count: exportedRows })}
            </p>
          )}

          {exportRefusal !== undefined && <Alert intent="danger" title={t("history.manage.export.refused")} description={exportRefusal} data-testid="history-export-refusal" />}
        </div>
      </Panel>

      {openId !== null && (
        <HistoryTranscript id={openId} draft={draft} controller={controller} live={live} onClose={() => setOpenId(null)} />
      )}
    </div>
  );
}

function HistoryHeader({ t }: { t: (key: string) => string }) {
  return (
    <header className="flex items-end justify-between gap-4">
      <div>
        <h1 className="text-2xl font-semibold">{t("history.manage.title")}</h1>
        <p className="mt-1 text-sm text-slate-600 dark:text-slate-400">{t("history.manage.subtitle")}</p>
      </div>
      <Link to="/projects" className="text-sm aeon-accent-text underline" data-testid="history-back">
        {t("history.manage.back")}
      </Link>
    </header>
  );
}
