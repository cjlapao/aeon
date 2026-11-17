import { useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, ConfirmModal, EmptyState, Pill, Select, Table, Toggle, type TableColumn } from "../../../components/kit";
import { OverlaySidePanel } from "../../../components/OverlaySidePanel";
import { RelativeTime } from "../../../components/RelativeTime";
import type { GateView } from "./gatesModel";
import { hasMemoryBound, ownedRow, pillTone } from "./gatesModel";

/**
 * The gates of one component, as the Agents page shows its rows: a quiet table, and one gate
 * opens in a side panel with its actions. Rows are what `verify.list` answered — nothing is
 * sorted, filtered or rewritten locally beyond the toggles the DAEMON's own params answer
 * (requirement, includeArchived). The source note is the effective read's `source` /
 * `sourceComponent` pair, the policy preview is the daemon's sentence verbatim, and a last run
 * is the daemon's state word in a pill whose tone comes from the word's POSITION in
 * `verify.vocabulary.states` — the word is never compared to a string typed here.
 *
 * Try is the one action that asks first: the dialog shows the daemon's own `tryStatement` and
 * the command about to run; declining closes the dialog and sends NOTHING.
 */
export interface GateListProps {
  gates: readonly GateView[];
  loading: boolean;
  error?: string;
  /** `verify.vocabulary.requirements` — the filter's options, in the daemon's order. */
  requirements: readonly string[];
  /** `verify.vocabulary.states` — pill tones by position. */
  states: readonly string[];
  /** `verify.vocabulary.tryStatement` — shown verbatim before a Try runs. */
  tryStatement: string;
  canWrite: boolean;
  showArchived: boolean;
  requirement: string;
  onShowArchived: (value: boolean) => void;
  onRequirement: (value: string) => void;
  onAdd: () => void;
  onEdit: (gate: GateView) => void;
  onArchive: (gate: GateView) => void;
  onRestore: (gate: GateView) => void;
  /** Called only when the person confirmed the daemon's statement. */
  onTry: (gate: GateView) => void;
  onCopyFromRoot?: () => void;
  onRetry: () => void;
}

const STATE_TONES = ["emerald", "rose", "amber", "slate"] as const;
const REQUIREMENT_TONES = ["sky", "slate"] as const;
const H = "text-xs font-semibold uppercase tracking-widest opacity-70";

export function GateList({
  gates, loading, error, requirements, states, tryStatement, canWrite, showArchived, requirement,
  onShowArchived, onRequirement, onEdit, onArchive, onRestore, onTry, onCopyFromRoot, onRetry,
}: GateListProps) {
  const { t } = useI18n();
  const [openId, setOpenId] = useState<string>("");
  const [tryTarget, setTryTarget] = useState<GateView | undefined>(undefined);
  const opened = gates.find((gate) => gate.id === openId);

  const confirmTry = () => {
    if (!tryTarget) return;
    const gate = tryTarget;
    setTryTarget(undefined);
    onTry(gate); // the ONLY path that sends a verify.try
  };

  const columns: TableColumn<GateView>[] = [
    {
      id: "name",
      groupable: false,
      header: t("settings.gates.col.gate"),
      render: (gate) => (
        <div className="w-0 min-w-full py-1" data-testid={`gate-row-toggle-${gate.id}`}>
          <span className="block truncate text-sm font-semibold" data-testid={`gate-row-${gate.id}`}>{gate.name}</span>
          <code className="block truncate text-xs opacity-60">{gate.command}</code>
        </div>
      ),
    },
    {
      id: "requirement",
      groupable: false,
      header: t("settings.gates.col.requirement"),
      width: 130,
      render: (gate) => (
        <Pill size="sm" tone={pillTone(requirements, gate.requirement, REQUIREMENT_TONES)} data-testid={`gate-requirement-${gate.id}`}>
          {t(`settings.gates.word.${gate.requirement}`)}
        </Pill>
      ),
    },
    {
      id: "lastRun",
      groupable: false,
      header: t("settings.gates.col.lastRun"),
      width: 190,
      render: (gate) =>
        gate.lastRun ? (
          <span className="flex items-center gap-2 text-xs" data-testid={`gate-lastrun-${gate.id}`}>
            <Pill size="sm" tone={pillTone(states, gate.lastRun.state, STATE_TONES)}>{t(`settings.gates.word.${gate.lastRun.state}`)}</Pill>
            <span className="opacity-70"><RelativeTime iso={gate.lastRun.endedAt} /></span>
          </span>
        ) : (
          <span className="text-xs opacity-60" data-testid={`gate-lastrun-none-${gate.id}`}>{t("settings.gates.row.neverRun")}</span>
        ),
    },
    {
      id: "status",
      groupable: false,
      header: t("settings.gates.col.status"),
      width: 240,
      render: (gate) => (
        <span className="flex flex-wrap gap-1">
          {!ownedRow(gate) && <Pill size="sm" tone="slate" data-testid={`gate-source-${gate.id}`}>{t("settings.gates.row.inheritedFrom", { component: gate.sourceComponent ?? gate.componentId })}</Pill>}
          {!gate.enabled && <Pill size="sm" tone="slate" data-testid={`gate-disabled-${gate.id}`}>{t("settings.gates.row.disabled")}</Pill>}
          {typeof gate.archivedAt === "string" && <Pill size="sm" tone="slate" data-testid={`gate-archived-${gate.id}`}>{t("settings.gates.row.archived")}</Pill>}
        </span>
      ),
    },
  ];

  const detail = opened && (() => {
    const gate = opened;
    const archived = typeof gate.archivedAt === "string";
    const owned = ownedRow(gate);
    return (
      <OverlaySidePanel title={gate.name} titleClassName="text-xl font-semibold" variant="simple" width={560} onClose={() => setOpenId("")}>
        <div className="space-y-8" data-testid={`gate-detail-${gate.id}`}>
          <div className="flex flex-wrap gap-2">
            {canWrite && owned && !archived && (
              <>
                <Button size="sm" onClick={() => { setOpenId(""); onEdit(gate); }} data-testid={`gate-edit-${gate.id}`}>{t("settings.gates.actions.edit")}</Button>
                <Button size="sm" variant="ghost" onClick={() => { setOpenId(""); onArchive(gate); }} data-testid={`gate-archive-${gate.id}`}>{t("settings.gates.actions.archive")}</Button>
              </>
            )}
            {canWrite && owned && archived && (
              <Button size="sm" onClick={() => { setOpenId(""); onRestore(gate); }} data-testid={`gate-restore-${gate.id}`}>{t("settings.gates.actions.restore")}</Button>
            )}
            {canWrite && <Button size="sm" variant="ghost" onClick={() => { setOpenId(""); setTryTarget(gate); }} data-testid={`gate-try-${gate.id}`}>{t("settings.gates.actions.try")}</Button>}
          </div>

          <section className="space-y-3">
            <h3 className={H}>{t("settings.gates.q.runs")}</h3>
            <p className="break-all font-mono text-xs" data-testid={`gate-command-${gate.id}`}>{gate.command}</p>
            <p className="text-xs opacity-80" data-testid={`gate-timeout-${gate.id}`}>
              {t("settings.gates.row.timeout", { seconds: gate.timeoutS })}
              {hasMemoryBound(gate) && <> · <span data-testid={`gate-memory-${gate.id}`}>{t("settings.gates.row.memory", { mb: gate.memoryMb ?? 0 })}</span></>}
            </p>
            {gate.policyPreview && <p className="text-xs italic opacity-80" data-testid={`gate-policy-${gate.id}`}>{gate.policyPreview}</p>}
          </section>

          <section className="space-y-3">
            <h3 className={H}>{t("settings.gates.q.last")}</h3>
            {gate.lastRun ? (
              <p className="flex items-center gap-2 text-sm">
                <Pill size="sm" tone={pillTone(states, gate.lastRun.state, STATE_TONES)}>{t(`settings.gates.word.${gate.lastRun.state}`)}</Pill>
                <span className="opacity-70"><RelativeTime iso={gate.lastRun.endedAt} /></span>
              </p>
            ) : (
              <p className="text-sm opacity-60">{t("settings.gates.row.neverRun")}</p>
            )}
            {gate.lastRun?.reason && <p className="text-xs opacity-80" data-testid={`gate-lastrun-reason-${gate.id}`}>{t("settings.gates.row.lastRunReason", { reason: gate.lastRun.reason })}</p>}
          </section>
        </div>
      </OverlaySidePanel>
    );
  })();

  return (
    <div className="flex min-h-[20rem] flex-1 flex-col gap-3" data-testid="gates-list-wrap">
      <div className="flex shrink-0 flex-wrap items-center gap-3" data-testid="gates-list-bar">
        <div className="w-52 shrink-0" data-testid="gates-filter-requirement-label">
          <Select variant="ghost" size="md" aria-label={t("settings.gates.filter.requirement")} value={requirement} onChange={(event) => onRequirement(event.target.value)}>
            <option value="">{t("settings.gates.filter.requirement")}: {t("settings.gates.filter.any")}</option>
            {requirements.map((word) => <option key={word} value={word}>{t(`settings.gates.word.${word}`)}</option>)}
          </Select>
        </div>
        <div data-testid="gates-filter-archived">
          <Toggle size="sm" aria-label={t("settings.gates.filter.includeArchived")} label={t("settings.gates.filter.includeArchived")} checked={showArchived} onChange={(event) => onShowArchived(event.target.checked)} />
        </div>
        {onCopyFromRoot && <Button size="sm" variant="ghost" onClick={onCopyFromRoot} data-testid="gates-copy-from-root">{t("settings.gates.actions.copyFromRoot")}</Button>}
      </div>

      {error && (
        <Alert className="shrink-0" intent="danger" title={t("settings.gates.list.failed")} description={<span className="whitespace-pre-line">{error}</span>} data-testid="gates-list-failed">
          <Button size="sm" onClick={onRetry}>{t("settings.gates.actions.retry")}</Button>
        </Alert>
      )}

      <div className="min-h-[18rem] flex-1" data-testid="gates-list">
        <Table<GateView>
          variant="outlined"
          data={[...gates]}
          columns={columns}
          rowKey={(gate) => gate.id}
          corner="rounded-sm"
          loading={loading}
          onRowClick={(gate) => setOpenId(gate.id)}
          emptyState={error ? <span /> : <div data-testid="gates-list-empty"><EmptyState title={t("settings.gates.list.emptyTitle")} subtitle={t("settings.gates.list.emptyBody")} /></div>}
          fullHeight
        />
      </div>

      {detail}

      {tryTarget && (
        <ConfirmModal
          isOpen
          title={t("settings.gates.try.confirmTitle", { name: tryTarget.name })}
          confirmLabel={t("settings.gates.try.confirm")}
          cancelLabel={t("settings.gates.try.decline")}
          onClose={() => setTryTarget(undefined)} // declining sends nothing
          onConfirm={confirmTry}
        >
          {/* The daemon's statement and the command about to run are both its own words. */}
          <p data-testid="gates-try-statement">{tryStatement}</p>
          <p className="font-mono text-xs" data-testid="gates-try-command">{tryTarget.command}</p>
        </ConfirmModal>
      )}
    </div>
  );
}
