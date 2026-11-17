import { useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, FormField, Input, Pill } from "../../../components/kit";
import { refusalBody, toRpcError } from "../../../lib/bridge";
import { notifications } from "../../../lib/notifications";
import type { LiveController, LiveState } from "../../../lib/live";
import { restoreLearning, retractLearning, useLearningsHistory, useLearningsShow } from "./memoryRpc";
import { actorTone, provenanceTone, retractionLabel, strengthLabel, type MemoryRow, type MemoryView, type Vocabulary } from "./memoryModel";

/**
 * MemoryDrawer — one memory opened (S52 T6, LM7/LM8/LM9).
 *
 * The retract confirm quotes the daemon's OWN law — `retractLaw` from
 * `learnings.vocabulary`, the same string the CLI prints before `--yes`
 * (RM9's one-string law) — beside the row itself: the row is where the
 * confirm "names whom it affects" (its scope is who carries this memory —
 * the person, a project root, or the one agent slug). Declining sends
 * NOTHING: no verb call, no partial write, not even an audit line. A
 * refusal comes back exactly as the plane wrote it (the reason bound is
 * the plane's law, never re-checked here), and the reason draft SURVIVES
 * it — a refusal that eats the person's typing is a second punishment for
 * the same act.
 *
 * A replayed retraction answers `changed:false` and the panel says so
 * plainly: the stamp already stands, nothing was written, LM2's
 * idempotence has a voice. The history block is the audit career — op,
 * actor (kind + id, the actor-kind colours are ours, the words are the
 * daemon's), the daemon's own summary sentence verbatim, and when.
 */
export interface MemoryDrawerProps {
  row: MemoryRow;
  vocabulary: Vocabulary | undefined;
  controller: LiveController;
  live: LiveState;
  canWrite: boolean;
  onClose?: () => void;
  onChanged: () => void;
}

type Act = "retract" | "restore";

/** Write answers are small facts, and facts keep their own words: the
 *  fields the daemon returned are listed as they arrived — never folded
 *  into prose (the roadmap drawer's AnswerFacts shape). */
function AnswerFacts({ answer }: { answer: unknown }) {
  if (typeof answer !== "object" || answer === null) return null;
  const entries = Object.entries(answer as Record<string, unknown>).filter(
    ([, v]) => typeof v === "boolean" || typeof v === "number" || typeof v === "string",
  );
  if (entries.length === 0) return null;
  return (
    <ul className="space-y-0.5 text-xs" data-testid="memory-answer">
      {entries.map(([key, value]) => (
        <li key={key} className="font-mono opacity-80">
          {key}: {String(value)}
        </li>
      ))}
    </ul>
  );
}

export function MemoryDrawer({ row, vocabulary, controller, live, canWrite, onChanged }: MemoryDrawerProps) {
  const { t } = useI18n();
  const show = useLearningsShow(row.id, controller, live);
  const history = useLearningsHistory(row.id, controller, live);
  // The full view only when it is THIS row's: a stale or mismatched answer must never speak for another row.
  const full = show.data?.learning !== undefined && show.data.learning.id === row.id ? show.data.learning : undefined;
  const shown = { ...row, ...(full ?? {}) } as MemoryView;

  const [reason, setReason] = useState("");
  const [act, setAct] = useState<Act | null>(null);
  const [busy, setBusy] = useState(false);
  const [answer, setAnswer] = useState<unknown>(undefined);
  const [replayed, setReplayed] = useState(false);

  // The bound is QUOTED from the vocabulary's own numbers — a limit
  // re-typed here would be a second truth about it; while the vocabulary
  // is not loaded the label says so instead of inventing the numbers.
  const reasonLabel =
    vocabulary === undefined
      ? t("settings.memory.confirm.reasonUnknown")
      : t("settings.memory.confirm.reason", {
          min: vocabulary.limits.reasonMin,
          max: vocabulary.limits.reasonMax,
        });

  async function runAct() {
    if (act === null) return;
    setBusy(true);
    setAnswer(undefined);
    setReplayed(false);
    try {
      const answered =
        act === "retract" ? await retractLearning(row.id, reason) : await restoreLearning(row.id);
      setAnswer(answered);
      const changed =
        typeof answered === "object" && answered !== null && (answered as { changed?: unknown }).changed === true;
      setReplayed(!changed);
      if (changed) {
        setReason(""); // the act landed; the draft's job is done.
        onChanged();
        show.refetch();
        history.refetch();
      }
    } catch (cause) {
      // Verbatim, and the draft STAYS (the refusal is the plane's line;
      // the typing is the person's — it is not ours to discard).
      notifications.push({ title: t("settings.memory.act.refusedTitle"), body: refusalBody(toRpcError(cause)), tone: "danger", source: "local" });
    } finally {
      setBusy(false);
    }
  }

  const H = "text-xs font-semibold uppercase tracking-widest opacity-70";

  return (
    <div className="space-y-6" data-testid="memory-drawer">
      <section className="space-y-2">
        <div className="flex flex-wrap items-center gap-2">
          <span className="font-mono text-xs opacity-70">#{row.id}</span>
          <Pill size="sm" tone="slate">{`${shown.scope}/${shown.scopeKey}`}</Pill>
          <Pill size="sm" tone={provenanceTone(shown.provenance)} data-testid="memory-drawer-provenance">{shown.provenance}</Pill>
          {strengthLabel(shown.observedCount) !== "" && <Pill size="sm" tone="sky">{strengthLabel(shown.observedCount)}</Pill>}
          <span className="ml-auto text-xs opacity-70">{shown.createdAt}</span>
        </div>
        <p className="sr-only" data-testid="memory-drawer-body">{shown.body}</p>
        {(shown.tags ?? []).length > 0 && (
          <div className="flex flex-wrap gap-1">{(shown.tags ?? []).map((tag) => <Pill key={tag} size="sm" tone="slate">{tag}</Pill>)}</div>
        )}
        {retractionLabel(shown) !== undefined && (
          <Alert intent="warning" title={t("settings.memory.row.retracted")} description={retractionLabel(shown)} data-testid="memory-drawer-retraction" />
        )}
      </section>

      {answer !== undefined && (
        <div>
          {replayed ? (
            // LM2's idempotence spoken plainly: the row already stood (or already sat) exactly so;
            // nothing was written, audit line included.
            <Alert intent="info" title={t("settings.memory.act.nothingChanged")} data-testid="memory-act-replay" />
          ) : (
            <AnswerFacts answer={answer} />
          )}
        </div>
      )}

      {/* The acts. An act is CONFIRMED before it runs: the confirm prints the daemon's retract law
          verbatim, the row it will stamp, and who carries that memory — then and only then the button
          acts. A decline is a whole sentence meaning NOTHING happens (LM7/LM8). */}
      <section className="space-y-3">
        <h3 className={H}>{t("settings.memory.q.acts")}</h3>
        {act === null ? (
          <div className="flex gap-2" data-testid="memory-acts">
            {canWrite ? (
              <>
                <Button size="sm" color="rose" onClick={() => { setAnswer(undefined); setReplayed(false); setAct("retract"); }} data-testid="memory-ask-retract">{t("settings.memory.actions.retract")}</Button>
                <Button size="sm" variant="ghost" onClick={() => { setAnswer(undefined); setReplayed(false); setAct("restore"); }} data-testid="memory-ask-restore">{t("settings.memory.actions.restore")}</Button>
              </>
            ) : (
              <p className="text-xs opacity-70" data-testid="memory-acts-locked">{t("settings.memory.act.locked")}</p>
            )}
          </div>
        ) : (
          <div className="space-y-2" data-testid="memory-confirm">
            {/* The law FIRST, byte for byte from the vocabulary — the same string the CLI gate prints. */}
            <p className="text-sm" data-testid="memory-confirm-law">{vocabulary?.retractLaw ?? t("settings.memory.confirm.lawMissing")}</p>
            <p className="text-xs opacity-70" data-testid="memory-confirm-row">{`#${row.id} · ${shown.scope}/${shown.scopeKey} · ${shown.provenance} · ${shown.body}`}</p>
            {act === "retract" ? (
              <FormField label={reasonLabel} hint={t("settings.memory.confirm.reasonHint")}>
                <Input variant="ghost" value={reason} aria-label={reasonLabel} data-testid="memory-confirm-reason" onChange={(e) => setReason(e.target.value)} />
              </FormField>
            ) : null}
            <div className="flex gap-2">
              <Button size="sm" color={act === "retract" ? "rose" : "green"} loading={busy} onClick={runAct} data-testid={`memory-confirm-${act}`}>
                {act === "retract" ? t("settings.memory.actions.confirmRetract") : t("settings.memory.actions.confirmRestore")}
              </Button>
              <Button size="sm" variant="ghost" onClick={() => setAct(null)} data-testid="memory-confirm-decline">{t("settings.memory.actions.decline")}</Button>
            </div>
          </div>
        )}
      </section>

      {/* The career. An unknown id or a failed read shows the daemon's refusal verbatim — an empty
          career means the row predates the audit table, and this panel says THAT, never a silence. */}
      <section className="space-y-3">
        <h3 className={H} data-testid="memory-history-title">{t("settings.memory.history.title")}</h3>
        {history.error !== undefined ? (
          <Alert intent="danger" title={t("settings.memory.history.failed")} description={history.error} data-testid="memory-history-failed" />
        ) : (history.data?.changes.length ?? 0) === 0 ? (
          <p className="text-xs opacity-70" data-testid="memory-history-empty">{t("settings.memory.history.empty")}</p>
        ) : (
          <ul className="space-y-1">
            {(history.data?.changes ?? []).map((change) => (
              <li key={change.id} className="flex flex-wrap items-center gap-2 text-xs" data-testid={`memory-history-${change.id}`}>
                <Pill size="sm" tone={actorTone(change.actorKind)}>{`${change.actorKind}:${change.actorId}`}</Pill>
                <span className="font-mono">{change.op}</span>
                {/* The summary is the plane's sentence, quoted whole. */}
                <span className="italic opacity-80">{change.summary}</span>
                <span className="ml-auto opacity-60">{change.occurredAt}</span>
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  );
}
