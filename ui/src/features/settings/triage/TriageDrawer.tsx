import { useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, FormField, Input, Pill } from "../../../components/kit";
import { refusalBody, toRpcError } from "../../../lib/bridge";
import { notifications } from "../../../lib/notifications";
import type { LiveController, LiveState } from "../../../lib/live";
import { confirmTriageDuplicate, handoffTriageBug, updateTriageBug, useTriageHistory, useTriageShow, validateTriageBug } from "./triageRpc";
import { priorityTone, validationTone, type BoardRow, type Vocabulary } from "./triageModel";

/**
 * TriageDrawer — one bug opened (S51 T7, TM10): its attempts and history,
 * an edit form that can only send title and body (TM1), the scored
 * duplicate candidates with the human-confirmed link, and validate /
 * hand-off behind ONE authority confirm each.
 *
 * The confirm quotes the daemon's OWN statement — `validateStatement` or
 * `handoffStatement` from `triage.vocabulary`, the same string the CLI
 * prints (RM9's one-string law) — beside the bug's daemon sentences from
 * its board row. The bug rides its own line; the sentence has no slot to
 * fill. Declining sends NOTHING: no verb call, no partial write. A refusal
 * comes back exactly as the daemon wrote it (TM4's shape), and the edit
 * draft SURVIVES it — a refusal that eats the person's typing is a second
 * punishment for the same act.
 *
 * Confirm-duplicate has no statement (there is no attempt to start): its
 * confirm names the pair — this bug, that candidate, the daemon's own
 * distance — and demands the person's name (D5: a machine confirmer is
 * refused by the daemon itself, and that refusal is shown verbatim).
 */
export interface TriageDrawerProps {
  row: BoardRow;
  vocabulary: Vocabulary | undefined;
  controller: LiveController;
  live: LiveState;
  canWrite: boolean;
  onClose?: () => void;
  onChanged: () => void;
}

type Verb = "validate" | "handoff";

/** Verb answers are small facts, and facts keep their own words: the
 *  fields the daemon returned are listed as they arrived, values verbatim
 *  — never folded into prose (the roadmap drawer's AnswerFacts shape). */
function AnswerFacts({ answer }: { answer: unknown }) {
  if (typeof answer !== "object" || answer === null) return null;
  const entries = Object.entries(answer as Record<string, unknown>).filter(
    ([, v]) => typeof v === "string" || typeof v === "number" || typeof v === "boolean",
  );
  if (entries.length === 0) return null;
  return (
    <ul className="space-y-0.5 text-xs" data-testid="triage-answer">
      {entries.map(([key, value]) => (
        <li key={key} className="font-mono opacity-80">
          {key}: {String(value)}
        </li>
      ))}
    </ul>
  );
}

export function TriageDrawer({ row, vocabulary, controller, live, canWrite, onChanged }: TriageDrawerProps) {
  const { t } = useI18n();
  const bug = row.bug;
  const show = useTriageShow(bug.id, controller, live);
  const history = useTriageHistory(bug.id, controller, live);

  // The edit draft is the PERSON's once typed and the daemon's until then:
  // an untouched draft follows `triage.show`; typing detaches it; a saved
  // edit clears it back onto the daemon's fresh bytes. A refusal NEVER
  // clears it — the typing survives the no.
  const [draft, setDraft] = useState<{ title: string; body: string } | null>(null);
  const shown = show.data;
  const title = draft?.title ?? shown?.title ?? bug.title;
  const body = draft?.body ?? shown?.body ?? bug.body;
  const [busy, setBusy] = useState(false);
  const [editAnswer, setEditAnswer] = useState<boolean | undefined>(undefined);
  const [verb, setVerb] = useState<Verb | null>(null);
  const [verbAnswer, setVerbAnswer] = useState<unknown>(undefined);
  const [duplicateOf, setDuplicateOf] = useState<number | null>(null);
  const [confirmedBy, setConfirmedBy] = useState("");

  // A refusal is a toast and nothing else: the daemon's sentence, verbatim, whatever it refused.
  // The edit draft SURVIVES it — the typing is the person's, not ours to discard.
  function refuse(cause: unknown) {
    notifications.push({ title: t("settings.triage.drawer.refused"), body: refusalBody(toRpcError(cause)), tone: "danger", source: "local" });
  }

  async function saveEdit() {
    setBusy(true);
    setEditAnswer(undefined);
    try {
      const answer = await updateTriageBug(bug.id, title, body);
      const changed = (answer as { changed?: boolean }).changed === true;
      setEditAnswer(changed);
      if (changed) {
        setDraft(null); // the draft's job is done; show's refetch carries the daemon's bytes back
        onChanged();
        show.refetch();
        history.refetch();
      }
    } catch (cause) {
      // Verbatim, and the draft STAYS (the refusal is the daemon's line;
      // the typing is the person's — it is not ours to discard).
      refuse(cause);
    } finally {
      setBusy(false);
    }
  }

  async function runVerb() {
    if (verb === null) return;
    setBusy(true);
    setVerbAnswer(undefined);
    try {
      const answer = verb === "validate" ? await validateTriageBug(bug.id) : await handoffTriageBug(bug.id);
      setVerbAnswer(answer);
      onChanged();
      show.refetch();
      history.refetch();
    } catch (cause) {
      refuse(cause);
    } finally {
      setBusy(false);
    }
  }

  async function runConfirmDuplicate() {
    if (duplicateOf === null) return;
    setBusy(true);
    setVerbAnswer(undefined);
    try {
      const answer = await confirmTriageDuplicate(bug.componentRoot, bug.id, duplicateOf, confirmedBy.trim());
      setVerbAnswer(answer);
      onChanged();
      show.refetch();
      history.refetch();
    } catch (cause) {
      refuse(cause);
    } finally {
      setBusy(false);
    }
  }

  const statement = verb === "validate" ? vocabulary?.validateStatement : vocabulary?.handoffStatement;

  const H = "text-xs font-semibold uppercase tracking-widest opacity-70";

  return (
    <div className="space-y-8" data-testid={`triage-drawer-${bug.id}`}>
      <div className="flex flex-wrap gap-2">
        <Button size="sm" disabled={!canWrite || busy} data-testid="triage-ask-validate" onClick={() => { setVerb("validate"); setVerbAnswer(undefined); }}>{t("settings.triage.actions.validate")}</Button>
        <Button size="sm" variant="ghost" disabled={!canWrite || busy} data-testid="triage-ask-handoff" onClick={() => { setVerb("handoff"); setVerbAnswer(undefined); }}>{t("settings.triage.actions.handoff")}</Button>
      </div>

      {verb !== null && (
        <section className="space-y-3" data-testid={`triage-confirm-${verb}`}>
          <h3 className={H}>{t("settings.triage.q.run")}</h3>
          {/* The daemon's statement, byte for byte (RM9: the CLI prints the same string). The bug rides its own line — no slot to fill. */}
          <p className="font-mono text-xs opacity-90" data-testid={`triage-statement-${verb}`}>{statement ?? t("settings.triage.statement.pending")}</p>
          <p className="text-xs opacity-80" data-testid="triage-confirm-bug">#{bug.id} {bug.title} · {row.describe.validationSentence}</p>
          <div className="flex gap-2">
            <Button size="sm" disabled={busy || statement === undefined || statement === ""} data-testid={`triage-go-${verb}`} onClick={() => void runVerb()}>{t(`settings.triage.actions.go.${verb}`)}</Button>
            <Button size="sm" variant="ghost" data-testid="triage-confirm-cancel" onClick={() => setVerb(null)}>{t("settings.triage.drawer.cancel")}</Button>
          </div>
        </section>
      )}

      {verbAnswer !== undefined && (
        <div className="space-y-2">
          <AnswerFacts answer={verbAnswer} />
          {(verbAnswer as { roadmapItem?: unknown }).roadmapItem !== undefined && (
            // TM10: the hand-off reaches INTO the roadmap screen it fed — the validated bug's next door is the plan that takes it.
            <p className="text-xs" data-testid="triage-handoff-link">
              <a className="underline" href={`/roadmap#item-${String((verbAnswer as { roadmapItem: unknown }).roadmapItem)}`}>/roadmap #{String((verbAnswer as { roadmapItem: unknown }).roadmapItem)}</a>
            </p>
          )}
        </div>
      )}

      <section className="space-y-3">
        <h3 className={H}>{t("settings.triage.q.know")}</h3>
        <div className="flex flex-wrap items-center gap-2">
          <Pill size="sm" tone={validationTone(bug.validation)} data-testid="triage-drawer-validation">{bug.validation}</Pill>
          <Pill size="sm" tone={priorityTone(bug.priorityState)} data-testid="triage-drawer-priority">{bug.priorityState}</Pill>
        </div>
        <p className="text-sm" data-testid="triage-drawer-validation-sentence">{row.describe.validationSentence}</p>
        <p className="text-sm" data-testid="triage-drawer-priority-sentence">{row.describe.prioritySentence}</p>
        {row.describe.duplicate !== undefined && row.describe.duplicate !== "" && <p className="text-sm" data-testid="triage-drawer-duplicate">{row.describe.duplicate}</p>}
        {row.describe.handoff !== undefined && row.describe.handoff !== "" && (
          <p className="text-sm" data-testid="triage-drawer-handoff">
            {row.describe.handoff}
            {bug.roadmapItemId !== undefined && <> · <a className="underline" href={`/roadmap#item-${bug.roadmapItemId}`} data-testid="triage-roadmap-link">/roadmap #{bug.roadmapItemId}</a></>}
          </p>
        )}
        {/* Attempts, counted by the daemon; the history below is its audit. */}
        <p className="text-xs opacity-70" data-testid="triage-drawer-attempts">
          {t("settings.triage.drawer.attempts")} {row.attempts}
          {row.lastAttemptAt !== undefined && row.lastAttemptAt !== "" ? ` · ${row.lastAttemptAt}` : ` · ${t("settings.triage.row.neverAttempted")}`}
        </p>
      </section>

      {row.candidates.length > 0 && (
        <section className="space-y-3" data-testid="triage-duplicates">
          <h3 className={H}>{t("settings.triage.q.dup")}</h3>
          <p className="text-xs opacity-70">{t("settings.triage.duplicates.intro")}</p>
          {row.candidates.map((candidate) => (
            <div key={candidate.bug.id} className="flex flex-wrap items-center gap-2 text-xs" data-testid={`triage-duplicate-candidate-${candidate.bug.id}`}>
              <span>#{candidate.bug.id} {candidate.bug.title} · d={candidate.distance.toFixed(3)}</span>
              <Button size="xs" variant="ghost" disabled={!canWrite} data-testid={`triage-mark-duplicate-${candidate.bug.id}`} onClick={() => { setDuplicateOf(candidate.bug.id); setVerbAnswer(undefined); }}>{t("settings.triage.duplicates.mark")}</Button>
            </div>
          ))}
          {duplicateOf !== null && (
            <div className="space-y-2" data-testid="triage-duplicate-confirm">
              <p className="text-xs opacity-80" data-testid="triage-duplicate-confirm-line">{t("settings.triage.duplicates.confirmLine", { id: String(bug.id), of: String(duplicateOf) })}</p>
              <FormField label={t("settings.triage.duplicates.confirmedBy")} hint={t("settings.triage.duplicates.confirmedByHint")}>
                <Input variant="ghost" value={confirmedBy} data-testid="triage-confirmed-by" onChange={(e) => setConfirmedBy(e.target.value)} />
              </FormField>
              <div className="flex gap-2">
                <Button size="sm" disabled={busy || confirmedBy.trim() === ""} data-testid="triage-duplicate-go" onClick={() => void runConfirmDuplicate()}>{t("settings.triage.duplicates.confirm")}</Button>
                <Button size="sm" variant="ghost" data-testid="triage-duplicate-cancel" onClick={() => setDuplicateOf(null)}>{t("settings.triage.drawer.cancel")}</Button>
              </div>
            </div>
          )}
        </section>
      )}

      {/* Edit: title and body, and NOTHING else can be sent (TM1). */}
      <section className="space-y-3" data-testid="triage-edit">
        <h3 className={H}>{t("settings.triage.q.edit")}</h3>
        <p className="text-xs opacity-70">{t("settings.triage.edit.intro")}</p>
        <FormField label={t("settings.triage.edit.title")}>
          <Input variant="ghost" value={title} disabled={!canWrite} data-testid="triage-edit-title" onChange={(e) => setDraft({ title: e.target.value, body })} />
        </FormField>
        <FormField label={t("settings.triage.edit.body")}>
          <textarea className="aeon-form-input w-full rounded-sm border p-2 text-sm" rows={4} value={body} disabled={!canWrite} data-testid="triage-edit-body" onChange={(e) => setDraft({ title, body: e.target.value })} />
        </FormField>
        <div className="flex items-center gap-2">
          <Button size="sm" disabled={busy || !canWrite} data-testid="triage-edit-save" onClick={() => void saveEdit()}>{busy ? t("settings.triage.edit.saving") : t("settings.triage.edit.save")}</Button>
          {editAnswer === true && <p className="text-xs opacity-70" data-testid="triage-edit-saved">{t("settings.triage.edit.saved")}</p>}
          {editAnswer === false && <p className="text-xs opacity-70" data-testid="triage-edit-nochange">{t("settings.triage.edit.noChange")}</p>}
        </div>
      </section>

      <section className="space-y-3">
        <h3 className={H}>{t("settings.triage.q.history")}</h3>
        {history.error !== undefined && <Alert intent="danger" title={history.error} data-testid="triage-history-error" />}
        {history.data !== undefined && (
          <div data-testid="triage-drawer-history">
            <p className="pb-1 text-xs opacity-70">{t("settings.triage.drawer.history")}</p>
            {history.data.length === 0 ? (
              <p className="text-xs opacity-70">{t("settings.triage.drawer.historyEmpty")}</p>
            ) : (
              <ul className="space-y-0.5 text-xs">
                {history.data.map((line, index) => (
                  <li key={`${line.occurredAt}-${index}`} className="font-mono opacity-80" data-testid={`triage-history-${index}`}>
                    {line.occurredAt} · {line.op} · {line.summary} · {line.actorKind}:{line.actorId}
                  </li>
                ))}
              </ul>
            )}
          </div>
        )}
      </section>
    </div>
  );
}
