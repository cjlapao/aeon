import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, EmptyState, Panel, Pill, Stepper, type StepperStep } from "../../components/kit";
import { RelativeTime } from "../../components/RelativeTime";
import type { LiveController, LiveState } from "../../lib/live";
import { useAgentLedger, useLoadAll } from "./agentsLedger";
import { actorKindTone, actorLabel, AGENT_CHANGE_OPS, changeProposalId, type AgentChangeRow } from "./agentsModel";

// The agent's history, as a timeline (CU4 + AR6). `agent_change` is append-only: who, when,
// and the daemon's own sentence — not a diff and not a way back. This tab is about ONE agent:
// it always asks with the row's slug. The whole ledger (the entries that name no single agent,
// like a migrate run) lives on the Agents page, under Activity.
//
// Every entry is loaded for now — an agent's own history is short. The kit's stepper is about
// to learn to show only the latest few, with a button for the rest; this is the one place to
// change then.

export interface AgentChangesProps {
  slug: string;
  /** Kept so the panel's call stays what it was; the ledger reads through the bridge. */
  controller?: LiveController;
  live: LiveState;
}

/** The icon a ledger op carries on its node. */
const OP_ICON: Record<string, string> = {
  create: "Add",
  edit: "Edit",
  duplicate: "Clone",
  delete: "Trash",
  import: "Save",
  migrate: "Cog",
  seed: "Verified",
};

/** The op in words: the seven the ledger admits are said plainly, anything else is the daemon's own word. */
export function useOpLabel(): (op: string) => string {
  const { t } = useI18n();
  return (op) => ((AGENT_CHANGE_OPS as readonly string[]).includes(op) ? t(`agents.history.op.${op}`) : op);
}

/** The ledger's "slate" reads as nothing on a dark surface; the kit's neutral pill is the same idea made to be seen. */
export function pillTone(tone: string): "neutral" | "blue" | "green" | "amber" | "red" | "violet" {
  return tone === "slate" ? "neutral" : (tone as "blue" | "green" | "amber" | "red" | "violet");
}

/** The actor as a short tag: the kind, then who — both the daemon's. */
export function ActorTag({ row }: { row: AgentChangeRow }) {
  return (
    <span className="inline-flex flex-wrap items-center gap-2 text-xs">
      <Pill tone={pillTone(actorKindTone(row.actorKind))} size="xs">{row.actorKind}</Pill>
      <span className="font-mono opacity-80">{actorLabel(row.actorKind, row.actorId)}</span>
    </span>
  );
}

export function AgentChanges({ slug, live }: AgentChangesProps) {
  const { t } = useI18n();
  const opLabel = useOpLabel();
  const ledger = useAgentLedger(live, slug);
  useLoadAll(ledger);
  const failed = ledger.error !== undefined;

  const steps: StepperStep[] = ledger.rows.map((row) => {
    const proposal = changeProposalId(row.summary);
    return {
      id: String(row.id),
      title: <span data-testid={`agent-change-${row.id}-op`}>{opLabel(row.op)}</span>,
      subtitle: (
        <span data-testid={`agent-change-${row.id}-at`}>
          <RelativeTime iso={row.occurredAt} /> · <span className="font-mono">{row.occurredAt}</span>
        </span>
      ),
      // The stepper shows `content` for the current step only, so the actor rides with the sentence.
      description: (
        <span className="block space-y-1">
          <span className="block" data-testid={`agent-change-${row.id}-summary`}>{row.summary}</span>
          {proposal !== null && <span className="block text-xs opacity-60">{t("agents.history.proposal", { id: proposal })}</span>}
          <span className="block"><ActorTag row={row} /></span>
        </span>
      ),
      // The stepper repeats the active step's description in a box under the list unless `content` is set;
      // an empty one, with the box hidden below, leaves the timeline alone.
      content: <span />,
      icon: OP_ICON[row.op] ?? "Edit",
      status: "completed",
    };
  });

  const latest = ledger.rows[0];
  return (
    <div className="grid items-start gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(260px,340px)]" data-testid="agent-history">
      <div className="min-w-0 space-y-3">
        {failed && (
          // The refusal whole, beside the rows still on screen: an error adds a line rather than blanking a history.
          <div className="space-y-2" data-testid="agent-history-error">
            <Alert intent="danger" title={t("agents.history.failed")} description={ledger.error} />
            <Button size="sm" variant="ghost" onClick={ledger.reload} data-testid="agent-history-retry">{t("agents.action.retry")}</Button>
          </div>
        )}
        {ledger.loading && !failed ? (
          <EmptyState title={t("agents.history.loading")} data-testid="agent-history-loading" />
        ) : !failed && ledger.rows.length === 0 ? (
          <EmptyState title={t("agents.history.empty")} data-testid="agent-history-empty" />
        ) : (
          <div data-testid="agent-history-rows">
            <Stepper
              orientation="vertical"
              variant="simple"
              padding="md"
              steps={steps}
              completedStepIds={steps.map((step) => step.id ?? "")}
              interactive={false}
              connector="progress"
              contentClassName="hidden"
              showProgressBar={false}
              showProgressSummary={false}
              animated={false}
            />
          </div>
        )}
        {ledger.total > 0 && (
          <p className="text-xs opacity-70" data-testid="agent-history-count">{t("agents.history.count", { shown: ledger.rows.length, total: ledger.total })}</p>
        )}
      </div>

      <Panel variant="simple" padding="md" clickable={false}>
        {latest !== undefined && (
          <div className="space-y-1" data-testid="agent-history-latest">
            <h2 className="text-xs font-semibold uppercase tracking-widest opacity-70">{t("agents.history.latest")}</h2>
            <p className="text-sm">{opLabel(latest.op)}</p>
            <p className="text-xs opacity-70"><RelativeTime iso={latest.occurredAt} /></p>
          </div>
        )}
        <p className={`text-xs leading-relaxed opacity-70 ${latest === undefined ? "" : "mt-4"}`}>{t("agents.history.note")}</p>
        <p className="mt-2 text-xs leading-relaxed opacity-70">{t("agents.history.elsewhere")}</p>
      </Panel>
    </div>
  );
}
