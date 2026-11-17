import { useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, FormField, Input, Panel, Pill } from "../../components/kit";
import { toRpcError } from "../../lib/bridge";
import { pathsEditableFor, type ItemView, type UpdateResult, type Vocabulary } from "./roadmapModel";
import { updateRoadmapItem } from "./roadmapRpc";

/**
 * ItemEditForm — the item's editable fields (S50 T7, RM7).
 *
 * What this form may edit is what the DAEMON says is editable, read from
 * `roadmap.vocabulary`: the path fields appear only while the item's status
 * sits in `pathEditableStatuses`, and the byte limits displayed are
 * `titleMaxBytes`/`epicMaxBytes` — the numbers the validator on the other
 * side of the socket checks with, not remembered copies. The form does not
 * re-derive the window from the status name; the array is the window.
 *
 * Only Touched fields travel ("leave the field alone" and "write an empty
 * string" are different intentions, and one submit must not confuse them;
 * clearing the epic is its own checkbox because `epic: ""` cannot say
 * both). A refusal keeps the draft exactly as typed — the person's words
 * survive the daemon's no, which said its piece in its own sentence,
 * rendered here verbatim. A `changed: false` answer is answered honestly:
 * nothing happened, and the line says so.
 */
export interface ItemEditFormProps {
  item: ItemView;
  vocabulary: Vocabulary | undefined;
  canWrite: boolean;
  onSaved: (result: UpdateResult) => void;
}

interface EditDraft {
  title: string;
  epic: string;
  briefPath: string;
  planPath: string;
  clearEpic: boolean;
}

function utf8Bytes(value: string): number {
  return new TextEncoder().encode(value).length;
}

export function ItemEditForm({ item, vocabulary, canWrite, onSaved }: ItemEditFormProps) {
  const { t } = useI18n();
  const [draft, setDraft] = useState<EditDraft>({
    title: item.title,
    epic: item.epic ?? "",
    briefPath: item.briefPath ?? "",
    planPath: item.planPath ?? "",
    clearEpic: false,
  });
  const [busy, setBusy] = useState(false);
  const [refusal, setRefusal] = useState<string | undefined>(undefined);
  const [answered, setAnswered] = useState<string | undefined>(undefined);

  const editable = pathsEditableFor(vocabulary, item.status);
  const titleMax = vocabulary?.titleMaxBytes;
  const epicMax = vocabulary?.epicMaxBytes;

  async function save() {
    setBusy(true);
    setRefusal(undefined);
    setAnswered(undefined);
    const params: Parameters<typeof updateRoadmapItem>[0] = { id: item.id };
    if (draft.title !== item.title) params.title = draft.title;
    if (draft.clearEpic) {
      params.clearEpic = true;
    } else if (draft.epic !== (item.epic ?? "")) {
      params.epic = draft.epic;
    }
    // Paths travel only when the vocabulary's window admits this status —
    // and the daemon still holds the line even if a status raced ahead.
    if (editable === true) {
      if (draft.briefPath !== (item.briefPath ?? "")) params.briefPath = draft.briefPath;
      if (draft.planPath !== (item.planPath ?? "")) params.planPath = draft.planPath;
    }
    try {
      const result = await updateRoadmapItem(params);
      if (result.changed === false) {
        // The honest no-change answer — the daemon saw nothing to write.
        setAnswered(t("roadmap.manage.edit.noChange"));
      } else {
        onSaved(result);
      }
    } catch (cause) {
      // The draft stays exactly as typed; the daemon's sentence goes up
      // verbatim — the refusal IS the answer, not raw material for ours.
      setRefusal(toRpcError(cause).message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <Panel tone="slate" data-testid="item-edit-form">
      <div className="space-y-3">
        <FormField
          label={t("roadmap.manage.edit.title")}
          hint={titleMax === undefined ? undefined : t("roadmap.manage.edit.titleLimit", { max: titleMax })}
        >
          <Input
            value={draft.title}
            disabled={busy || !canWrite}
            aria-label={t("roadmap.manage.edit.title")}
            data-testid="edit-title"
            onChange={(e) => setDraft({ ...draft, title: e.target.value })}
          />
          {titleMax !== undefined && utf8Bytes(draft.title) > titleMax && (
            <p className="pt-1 text-xs text-red-600 dark:text-red-400" data-testid="edit-title-over">
              {t("roadmap.manage.edit.overLimit", { used: utf8Bytes(draft.title), max: titleMax })}
            </p>
          )}
        </FormField>
        <FormField
          label={t("roadmap.manage.edit.epic")}
          hint={epicMax === undefined ? undefined : t("roadmap.manage.edit.epicLimit", { max: epicMax })}
        >
          <Input
            value={draft.epic}
            disabled={busy || !canWrite || draft.clearEpic}
            placeholder={item.epic === "" ? t("roadmap.manage.edit.noEpic") : undefined}
            aria-label={t("roadmap.manage.edit.epic")}
            data-testid="edit-epic"
            onChange={(e) => setDraft({ ...draft, epic: e.target.value })}
          />
          <label className="flex items-center gap-2 pt-1 text-xs" data-testid="edit-clear-epic-row">
            <input
              type="checkbox"
              checked={draft.clearEpic}
              disabled={busy || !canWrite}
              data-testid="edit-clear-epic"
              onChange={(e) => setDraft({ ...draft, clearEpic: e.target.checked })}
            />
            {t("roadmap.manage.edit.clearEpic")}
          </label>
          {epicMax !== undefined && !draft.clearEpic && utf8Bytes(draft.epic) > epicMax && (
            <p className="pt-1 text-xs text-red-600 dark:text-red-400" data-testid="edit-epic-over">
              {t("roadmap.manage.edit.overLimit", { used: utf8Bytes(draft.epic), max: epicMax })}
            </p>
          )}
        </FormField>
        {editable === true ? (
          <>
            <FormField label={t("roadmap.manage.edit.briefPath")} hint={vocabulary?.pathRule}>
              <Input
                value={draft.briefPath}
                disabled={busy || !canWrite}
                aria-label={t("roadmap.manage.edit.briefPath")}
                data-testid="edit-brief-path"
                onChange={(e) => setDraft({ ...draft, briefPath: e.target.value })}
              />
            </FormField>
            <FormField label={t("roadmap.manage.edit.planPath")}>
              <Input
                value={draft.planPath}
                disabled={busy || !canWrite}
                aria-label={t("roadmap.manage.edit.planPath")}
                data-testid="edit-plan-path"
                onChange={(e) => setDraft({ ...draft, planPath: e.target.value })}
              />
            </FormField>
          </>
        ) : editable === false ? (
          // Hidden because the vocabulary's array says this status is
          // outside the window — the rule quoted so the WHY is visible.
          <p className="text-xs opacity-70" data-testid="edit-paths-hidden">
            {t("roadmap.manage.edit.pathsHidden")}
          </p>
        ) : null}
        <div className="flex items-center gap-2">
          <Button size="sm" disabled={busy || !canWrite} onClick={() => void save()} data-testid="edit-save">
            {busy ? t("roadmap.manage.edit.saving") : t("roadmap.manage.edit.save")}
          </Button>
          <Pill tone="slate" data-testid="edit-status-anchor">{item.status}</Pill>
        </div>
        {refusal !== undefined && (
          <Alert intent="danger" title={refusal} data-testid="edit-refusal" />
        )}
        {answered !== undefined && <p className="text-xs opacity-70" data-testid="edit-nochange">{answered}</p>}
      </div>
    </Panel>
  );
}
