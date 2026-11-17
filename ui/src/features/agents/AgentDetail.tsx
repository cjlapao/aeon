import { useRef, useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Button, ContentActions, ExpandableButton, IconButton, Pill } from "../../components/kit";
import type { LiveController, LiveState } from "../../lib/live";
import { useAppSettings } from "../../lib/appSettings";
import { rowReadOnly, type AgentDraft } from "./agentsModel";
import { AgentAvatar } from "./AgentFields";
import { AgentRowBody, type AgentEditState } from "./AgentPanel";
import type { RowVerb } from "./AgentsTable";

// AgentDetail.tsx — one agent, on its own page rather than in a drawer: where it is (a
// way back, its name, what kind of row it is), what can be done to it (the verbs, as the
// kit's expandable buttons), and its sections below. It opens to be READ; Edit turns the
// same sections into the form, and Done turns them back. The sections themselves are the
// panel's body (AgentRowBody) — the tabs, the writes, the daemon's refusals are unchanged,
// only the frame around them is a page.
//
// A shipped agent is locked: its Edit is not offered, and the daemon's own sentence says why.

export interface AgentDetailProps {
  slug: string;
  /** Open already editing (the row menu's Edit). */
  startEditing?: boolean;
  controller: LiveController;
  live: LiveState;
  canWrite: boolean;
  /** Back to the list. The screen asks about a draft first. */
  onBack: () => void;
  /** Ask before leaving an unsaved edit, then run `go`. */
  confirmLeave: (go: () => void) => void;
  onDirtyChange: (dirty: boolean) => void;
  onChanged: () => void;
  onVerb: (verb: RowVerb) => void;
}

export function AgentDetail({ slug, startEditing = false, controller, live, canWrite, onBack, confirmLeave, onDirtyChange, onChanged, onVerb }: AgentDetailProps) {
  const { t } = useI18n();
  const { controlTone } = useAppSettings();
  const [editing, setEditing] = useState(startEditing);
  // The header is drawn from the row the body reads: one `agent.view`, asked once, shown twice.
  const [agent, setAgent] = useState<AgentDraft | null>(null);
  const locked = rowReadOnly({ internal: agent?.internal === true });
  const [edit, setEdit] = useState<AgentEditState>({ dirty: false, busy: false, changed: "", canSave: false });
  const saveRef = useRef<() => void>(() => undefined);
  // An action that cannot be done to this row is not offered: a shipped row has no Edit or Delete,
  // and with no daemon there is no write at all (one line says why, instead of a row of dead buttons).
  const canEdit = canWrite && !locked;

  // Nothing changed: one Done. Something changed: Cancel (asks before throwing the edits away) and Save.
  const actions = editing ? (
    edit.dirty && !locked ? (
      <>
        <ExpandableButton icon="Close" label={t("agents.detail.discard")} size="sm" color={controlTone} onClick={() => confirmLeave(() => setEditing(false))} data-testid="agent-cancel" />
        <Button size="sm" onClick={() => saveRef.current()} disabled={!edit.canSave} data-testid="agent-panel-save">
          {edit.busy ? t("agents.panel.saving") : t("agents.panel.save")}
        </Button>
      </>
    ) : (
      <ExpandableButton icon="Check" label={t("agents.detail.done")} size="sm" color={controlTone} onClick={() => confirmLeave(() => setEditing(false))} data-testid="agent-done" />
    )
  ) : (
    <>
      {canEdit && <ExpandableButton icon="Edit" label={t("agents.row.edit")} size="sm" color={controlTone} onClick={() => setEditing(true)} data-testid="agent-edit" />}
      {canWrite && <ExpandableButton icon="Clone" label={t("agents.row.duplicate")} size="sm" color={controlTone} onClick={() => onVerb("duplicate")} data-testid="agent-duplicate" />}
      <ExpandableButton icon="Export" label={t("agents.row.export")} size="sm" color={controlTone} onClick={() => onVerb("export")} data-testid="agent-export" />
      {canEdit && <ExpandableButton icon="Trash" label={t("agents.row.delete")} size="sm" color="rose" onClick={() => onVerb("delete")} data-testid="agent-delete" />}
    </>
  );

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-4" data-testid="agent-detail">
      {/* Back and avatar, then the words; the actions sit on the right and, when they grow, take the width from the words. */}
      <ContentActions className="shrink-0" gap="md" align="start" actions={agent ? actions : undefined} data-testid="agent-header">
        <div className="flex min-w-0 items-start gap-3">
          <IconButton icon="ArrowLeft" variant="ghost" size="sm" srLabel={t("agents.detail.all")} tooltip={t("agents.detail.all")} onClick={() => confirmLeave(onBack)} data-testid="agent-back" />
          {agent && (
            <>
              <span className="flex h-14 w-14 shrink-0 items-center justify-center"><AgentAvatar agent={agent} /></span>
              <div className="min-w-0">
                <div className="flex flex-wrap items-center gap-2">
                  <h1 className="text-2xl font-semibold">{agent.name}</h1>
                  <Pill size="sm" tone={agent.internal ? "violet" : agent.origin === "import" ? "blue" : "green"}>{agent.internal ? t("agents.flag.shipped") : agent.origin === "import" ? t("agents.flag.imported") : t("agents.flag.yours")}</Pill>
                  {!agent.signed && <Pill size="sm" tone="amber">{t("agents.flag.unsigned")}</Pill>}
                  {editing && <Pill size="sm" tone="amber">{t("agents.detail.editing")}</Pill>}
                </div>
                <code className="block text-xs opacity-60">{agent.slug}</code>
                {agent.description && <p className="mt-1 line-clamp-2 max-w-3xl text-sm opacity-80" title={agent.description}>{agent.description}</p>}
                {editing && !locked && (
                  <p className="mt-1 text-xs text-slate-500" data-testid="agent-panel-changed">
                    {edit.dirty ? t("agents.panel.changed", { fields: edit.changed }) : t("agents.panel.unchanged")}
                  </p>
                )}
              </div>
            </>
          )}
        </div>
      </ContentActions>

      {!canWrite && (
        <p className="shrink-0 text-xs text-amber-500" data-testid="agent-offline-note">{t("agents.reason.notConnectedTitle")}: {t("agents.reason.changesOff")}</p>
      )}

      {/* Header and way back stay put; the panel below owns the one scrolling body. */}
      <div className="flex min-h-0 min-w-0 flex-1 flex-col" data-testid={editing ? "agent-panel-edit" : "agent-panel-view"}>
        <AgentRowBody mode={editing ? "edit" : "view"} slug={slug} controller={controller} live={live} canWrite={canWrite}
          onChanged={onChanged} onDirtyChange={onDirtyChange} onRow={setAgent} onEditState={setEdit} saveRef={saveRef} />
      </div>
    </div>
  );
}
