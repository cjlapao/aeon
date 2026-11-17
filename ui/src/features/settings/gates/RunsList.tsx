import { useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, EmptyState, Input, Pill, Select, Table, type TableColumn } from "../../../components/kit";
import { OverlaySidePanel } from "../../../components/OverlaySidePanel";
import { RelativeTime } from "../../../components/RelativeTime";
import type { LiveController, LiveState } from "../../../lib/live";
import { useVerifyVocabulary, useRuns } from "./gatesRpc";
import { RunDetail } from "./RunDetail";
import { pillTone, type RunView } from "./gatesModel";

/**
 * The run ledger: `verify.runs` per component, loop and state filters sent as DAEMON params
 * (never filtered client-side); a run opens in a side panel with `verify.runShow` for the
 * per-gate results. A run row is the ledger the daemon wrote — head, trigger, state and, when the
 * runner had something to say, its summary sentence verbatim. There is no `stale` marker on the
 * wire to render: the head a run was made against is shown, and the panel never claims more.
 */
export interface RunsListProps {
  componentId: string;
  controller: LiveController;
  live: LiveState;
}

const STATE_TONES = ["emerald", "rose", "amber", "slate"] as const;
const RUNS_LIMIT = 50;

export function RunsList({ componentId, controller, live }: RunsListProps) {
  const { t } = useI18n();
  const vocab = useVerifyVocabulary(controller, live);
  const [loop, setLoop] = useState("");
  const [state, setState] = useState("");
  const [selected, setSelected] = useState<string>("");

  const runs = useRuns({ componentId, loop, state, limit: RUNS_LIMIT }, controller, live);
  const rows = runs.data?.runs ?? [];
  const states = vocab.data?.states ?? [];

  const columns: TableColumn<RunView>[] = [
    {
      id: "state",
      groupable: false,
      header: t("settings.gates.col.state"),
      width: 120,
      render: (run) => (
        <span data-testid={`run-row-${run.id}`}>
          <Pill size="sm" tone={pillTone(states, run.state, STATE_TONES)} data-testid={`run-state-${run.id}`}>{t(`settings.gates.word.${run.state}`)}</Pill>
        </span>
      ),
    },
    {
      id: "loop",
      groupable: false,
      header: t("settings.gates.col.loop"),
      render: (run) => (
        <div className="w-0 min-w-full py-1" data-testid={`run-row-toggle-${run.id}`}>
          <span className="block truncate font-mono text-xs">{run.loopSlug !== "" ? run.loopSlug : t("settings.gates.runs.noLoop")}</span>
          {run.summary && <span className="block truncate text-xs italic opacity-70" title={run.summary} data-testid={`run-summary-${run.id}`}>{run.summary}</span>}
        </div>
      ),
    },
    {
      id: "attempt",
      groupable: false,
      header: t("settings.gates.col.attempt"),
      width: 190,
      render: (run) => <span className="text-xs opacity-70">{t("settings.gates.runs.attempt", { position: run.position, attempt: run.attempt })}</span>,
    },
    {
      id: "head",
      groupable: false,
      header: t("settings.gates.col.head"),
      width: 170,
      render: (run) => <span className="font-mono text-xs opacity-70" data-testid={`run-head-${run.id}`}>{t("settings.gates.runs.head", { sha: run.headSha.slice(0, 12) })}</span>,
    },
    {
      id: "when",
      groupable: false,
      header: t("settings.gates.col.when"),
      width: 130,
      render: (run) => <span className="text-xs opacity-70"><RelativeTime iso={run.endedAt} /></span>,
    },
  ];

  return (
    <div className="flex min-h-[22rem] flex-1 flex-col gap-3" data-testid="gates-runs-panel">
      <div className="flex shrink-0 flex-wrap items-center gap-3">
        <div className="w-56 shrink-0" data-testid="gates-runs-loop-label">
          <Input variant="ghost" aria-label={t("settings.gates.runs.filterLoop")} placeholder={t("settings.gates.runs.filterLoop")} value={loop} onChange={(event) => setLoop(event.target.value)} data-testid="gates-runs-loop" />
        </div>
        <div className="w-52 shrink-0" data-testid="gates-runs-state-label">
          <Select variant="ghost" size="md" aria-label={t("settings.gates.runs.filterState")} value={state} onChange={(event) => setState(event.target.value)}>
            <option value="">{t("settings.gates.runs.filterState")}: {t("settings.gates.filter.any")}</option>
            {states.map((word) => <option key={word} value={word}>{t(`settings.gates.word.${word}`)}</option>)}
          </Select>
        </div>
      </div>

      {runs.error && (
        <Alert className="shrink-0" intent="danger" title={t("settings.gates.runs.failed")} description={runs.error} data-testid="gates-runs-failed">
          <Button size="sm" onClick={runs.refetch}>{t("settings.gates.actions.retry")}</Button>
        </Alert>
      )}

      <div className="min-h-[18rem] flex-1" data-testid="gates-runs-list">
        <Table<RunView>
          variant="outlined"
          data={rows}
          columns={columns}
          rowKey={(run) => run.id}
          corner="rounded-sm"
          loading={runs.loading}
          onRowClick={(run) => setSelected(run.id)}
          emptyState={runs.error ? <span /> : <div data-testid="gates-runs-empty"><EmptyState title={t("settings.gates.runs.emptyTitle")} subtitle={t("settings.gates.runs.emptyBody")} /></div>}
          fullHeight
        />
      </div>

      {selected !== "" && (
        <OverlaySidePanel title={t("settings.gates.runs.gateResults")} titleClassName="text-xl font-semibold" variant="simple" width={640} onClose={() => setSelected("")}>
          <RunDetail runId={selected} states={states} controller={controller} live={live} />
        </OverlaySidePanel>
      )}
    </div>
  );
}
