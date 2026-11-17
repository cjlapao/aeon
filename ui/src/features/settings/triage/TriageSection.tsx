import { useEffect, useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, ContentActions } from "../../../components/kit";
import { OverlaySidePanel } from "../../../components/OverlaySidePanel";
import { PageTitle } from "../../../components/PageTitle";
import { useComponentRoots } from "../../projects/useComponentRoots";
import type { LiveController, LiveState } from "../../../lib/live";
import { useTriageVocabulary } from "./triageRpc";
import { boardIsLoadable, emptyFilters, type BoardRow, type FilterDraft } from "./triageModel";
import { BoardNeedsRoot, TriageBoard, TriageBoardRows } from "./TriageBoard";
import { TriageDrawer } from "./TriageDrawer";
import { BlueprintCard } from "./BlueprintCard";

/**
 * TriageSection — the Settings → Triage panel (S51 T7, TM10).
 *
 * One screen, four honest parts: the board (the daemon's filters as
 * params, the daemon's rows, candidates and all), the detail drawer the
 * rows open, the validation-blueprint card, and the vocabulary read ONCE
 * for the section that every dial and both confirms quote.
 *
 * Nothing here is state-shaped by this file's opinion: the closed sets,
 * the legal edges and the authority sentences all arrive from
 * `triage.vocabulary`, and the per-bug sentences arrive from each row's
 * own `describe`. Writes are refused while the connection is not current;
 * a stale connection asks nothing at all (S49's rule, kept); and a write
 * re-reads from the daemon afterwards — the board shows the answer that
 * just changed, never a locally patched copy.
 */
export interface TriageSectionProps {
  controller: LiveController;
  live: LiveState;
  /** @default `live.state === "current"` — writes are refused while not current. */
  canWrite?: boolean;
  /** Fixed component root (the Work page's scope): the board is this component's. */
  componentRoot?: string;
}

export function TriageSection({ controller, live, canWrite = live.state === "current", componentRoot }: TriageSectionProps) {
  const { t } = useI18n();
  const [draft, setDraft] = useState<FilterDraft>({ ...emptyFilters, componentRoot: componentRoot ?? "" });
  const [openId, setOpenId] = useState<number | null>(null);
  const [settingOpen, setSettingOpen] = useState(false);
  // The rows the daemon last answered, mirrored up so the OPEN panel keeps showing fresh daemon
  // bytes across a refresh (its own re-reads cover the bug and its audit; attempts and candidates ride the row).
  const [rows, setRows] = useState<BoardRow[] | null>(null);
  const [refresh, setRefresh] = useState(0);

  const vocabulary = useTriageVocabulary(controller, live);
  const roots = useComponentRoots(controller, live);
  const rootChoices = roots.roots.filter((component) => component.root !== "" && (componentRoot === undefined || component.root === componentRoot)).map((component) => ({ root: component.root, name: component.name }));

  // Open on the first component so the page shows a board, not a prompt.
  useEffect(() => {
    if (draft.componentRoot === "" && rootChoices.length > 0) setDraft((held) => ({ ...held, componentRoot: rootChoices[0]?.root ?? "" }));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rootChoices.length]);

  const title = (
    <PageTitle title={t("settings.triage.title")} purpose={t("settings.triage.subtitle")} help={{ title: t("settings.triage.help.title"), content: t("settings.triage.help.body") }} />
  );

  if (live.state !== "current") {
    // Three states, three shapes: reconnecting asks nothing.
    return (
      <div className="flex min-h-0 flex-1 flex-col gap-4" data-testid="triage-section">
        <ContentActions className="shrink-0" gap="md" align="end">{title}</ContentActions>
        <Alert intent="warning" title={t("settings.triage.reconnecting")} data-testid="triage-reconnecting" />
      </div>
    );
  }

  const openRow = openId === null ? null : rows?.find((row) => row.bug.id === openId) ?? null;

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto" data-testid="triage-section">
      <ContentActions
        className="shrink-0"
        gap="md"
        align="end"
        data-testid="triage-header"
        actions={<Button size="sm" variant="ghost" onClick={() => setSettingOpen(true)} data-testid="triage-open-setting">{t("settings.triage.actions.setting")}</Button>}
      >
        {title}
      </ContentActions>

      <p className="shrink-0 text-sm opacity-70">{t("settings.triage.hint")}</p>

      {vocabulary.error !== undefined && (
        <Alert className="shrink-0" intent="danger" title={t("settings.triage.vocabulary.failed")} description={vocabulary.error} data-testid="triage-vocabulary-failed">
          <Button size="sm" onClick={vocabulary.refetch} data-testid="triage-vocabulary-retry">{t("settings.triage.actions.retry")}</Button>
        </Alert>
      )}

      <TriageBoard draft={draft} vocabulary={vocabulary.data} roots={rootChoices} onDraftChange={setDraft}>
        {boardIsLoadable(draft) ? (
          <TriageBoardRows key={refresh} draft={draft} controller={controller} live={live} onOpen={setOpenId} onRows={setRows} />
        ) : (
          // An unnamed root is not a question — nothing is asked until one is (the roadmap board's gate, kept).
          <BoardNeedsRoot />
        )}
      </TriageBoard>

      {openRow !== null && (
        <OverlaySidePanel title={`#${openRow.bug.id} ${openRow.bug.title}`} titleClassName="text-xl font-semibold" variant="simple" width={640} onClose={() => setOpenId(null)}>
          <TriageDrawer
            row={openRow}
            vocabulary={vocabulary.data}
            controller={controller}
            live={live}
            canWrite={canWrite}
            onChanged={() => setRefresh((n) => n + 1)}
          />
        </OverlaySidePanel>
      )}

      {settingOpen && (
        <OverlaySidePanel title={t("settings.triage.q.setting")} titleClassName="text-xl font-semibold" variant="simple" width={520} onClose={() => setSettingOpen(false)}>
          <BlueprintCard controller={controller} live={live} canWrite={canWrite} />
        </OverlaySidePanel>
      )}
    </div>
  );
}
