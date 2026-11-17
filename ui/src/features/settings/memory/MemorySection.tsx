import { useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Tabs, ContentActions } from "../../../components/kit";
import { PageTitle } from "../../../components/PageTitle";
import type { LiveController, LiveState } from "../../../lib/live";
import { ProposalsDesk } from "../policy/ProposalsDesk";
import { MEMORY_PROPOSAL_KINDS } from "./memoryModel";
import { useLearningsVocabulary } from "./memoryRpc";
import { emptyMemoryFilters, type MemoryFilterDraft, type MemoryRow } from "./memoryModel";
import { MemoryBoard, MemoryBoardRows } from "./MemoryBoard";
import { AgentMemoryPanel } from "./AgentMemoryPanel";

/**
 * MemorySection — the Settings → Memory panel (S52 T6, LM10).
 *
 * One screen, four honest parts: the board (the daemon's filters as
 * params, the daemon's rows, retracted ones only when the person turns
 * the dial), the detail drawer a row opens — where retract and restore
 * live behind a confirm that quotes the daemon's own law —, the
 * "what would this agent see?" panel, and the `learning_retract`
 * proposals riding S45's desk (the agent's half of this plane: it may
 * propose, a person decides — LM8's door).
 *
 * Nothing here is state-shaped by this file's opinion: the closed sets,
 * the limits and the retract law arrive from `learnings.vocabulary`, and
 * the per-row sentences ride the rows themselves. Writes are refused
 * while the connection is not current, and a write re-reads from the
 * daemon afterwards — the board shows the answer that just changed,
 * never a locally patched copy (S49/S51's rule, kept).
 */
export interface MemorySectionProps {
  controller: LiveController;
  live: LiveState;
  /** @default `live.state === "current"` — writes are refused while not current. */
  canWrite?: boolean;
  /** Start with the board filtered to this project root (the Work page's scope). A person may still clear it. */
  initialProject?: string;
}

type TabId = "knowledge" | "agent" | "proposals";

export function MemorySection({ controller, live, canWrite = live.state === "current", initialProject = "" }: MemorySectionProps) {
  const { t } = useI18n();
  const [draft, setDraft] = useState<MemoryFilterDraft>({ ...emptyMemoryFilters, project: initialProject });
  const [tab, setTab] = useState<TabId>("knowledge");
  const [openId, setOpenId] = useState<number | null>(null);
  // The rows the daemon last answered, mirrored up so the OPEN memory keeps showing fresh daemon
  // bytes across a refresh.
  const [, setRows] = useState<MemoryRow[] | null>(null);

  const vocabulary = useLearningsVocabulary(controller, live);

  const title = (
    <PageTitle title={t("settings.memory.title")} purpose={t("settings.memory.subtitle")} help={{ title: t("settings.memory.help.title"), content: t("settings.memory.help.body") }} />
  );

  if (live.state !== "current") {
    // Three states, three shapes: reconnecting asks nothing.
    return (
      <div className="flex min-h-0 flex-1 flex-col gap-4" data-testid="memory-section">
        <ContentActions className="shrink-0" gap="md" align="end">{title}</ContentActions>
        <Alert intent="warning" title={t("settings.memory.reconnecting")} data-testid="memory-reconnecting" />
      </div>
    );
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto" data-testid="memory-section">
      <ContentActions className="shrink-0" gap="md" align="end" data-testid="memory-header">{title}</ContentActions>

      {vocabulary.error !== undefined && (
        <Alert className="shrink-0" intent="danger" title={t("settings.memory.vocabulary.failed")} description={vocabulary.error} data-testid="memory-vocabulary-failed">
          <button type="button" onClick={vocabulary.refetch} data-testid="memory-vocabulary-retry">{t("settings.memory.actions.retry")}</button>
        </Alert>
      )}

      <div className="shrink-0">
        <Tabs
          variant="underline"
          items={[
            { id: "knowledge", label: t("settings.memory.tabs.knowledge") },
            { id: "agent", label: t("settings.memory.tabs.agent") },
            { id: "proposals", label: t("settings.memory.tabs.proposals") },
          ]}
          value={tab}
          onChange={(id) => setTab(id as TabId)}
        />
      </div>
      <p className="shrink-0 text-sm opacity-70">{t(`settings.memory.hint.${tab}`)}</p>

      {tab === "knowledge" && (
        <MemoryBoard draft={draft} vocabulary={vocabulary.data} onDraftChange={setDraft}>
          <MemoryBoardRows
            draft={draft}
            vocabulary={vocabulary.data}
            controller={controller}
            live={live}
            canWrite={canWrite}
            selectedId={openId}
            onSelect={setOpenId}
            onRows={setRows}
            onChanged={() => {}}
          />
        </MemoryBoard>
      )}

      {tab === "agent" && <AgentMemoryPanel vocabulary={vocabulary.data} controller={controller} live={live} />}

      {/* The agent's half of this plane: `learning_retract` proposals, decided by a person through S45's
          desk — the same component, the same doors, scoped here to this family's kinds (MCP's precedent).
          Until a person commits, an agent's proposal is ink on paper. */}
      {tab === "proposals" && (
        <div className="pb-6">
          <ProposalsDesk controller={controller} live={live} canWrite={canWrite} kinds={MEMORY_PROPOSAL_KINDS} />
        </div>
      )}
    </div>
  );
}
