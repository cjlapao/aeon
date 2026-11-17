import { useEffect, useMemo, useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { ActionPanel, Alert, Button, ConfirmModal, FormField, Input, Pill, Select, Toggle } from "../../../components/kit";
import { useRpcAction } from "../../../lib/useRpcAction";
import { notifications } from "../../../lib/notifications";
import type { LiveController, LiveState } from "../../../lib/live";
import type { MergeRuleVocabulary, RuleDiffRow } from "./gatesModel";
import { isOwnSource, parseList, ruleDiff } from "./gatesModel";
import { useMergeRuleGet, useMergeRuleVocabulary, writeMergeRuleClear, writeMergeRuleSet } from "./gatesRpc";
import { MergePreview } from "./MergePreview";

/**
 * The merge-rule editor (S57 T11, VG22). The effective rule — source,
 * revision, the last change's audit row and the AUTONOMY FLOOR checklist —
 * is what `mergerule.get` answered; every select lists the daemon's own
 * arrays from `mergerule.vocabulary` (none of the closed values appears in
 * this file). A save is CONFIRMED with the field-by-field diff of the draft
 * against the rule the daemon holds, and after the write the panel
 * re-reads and shows the DAEMON's own diff sentence (`lastChange.summary`)
 * as the authoritative record — the client diff is for the confirm dialog
 * only; the audit sentence is never re-derived here. Clearing is confirmed
 * too: after it, WHICH rule applies is whatever the next effective read
 * answers, and the panel says exactly that.
 */
export interface MergeRuleEditorProps {
  componentId: string;
  controller: LiveController;
  live: LiveState;
  canWrite: boolean;
}

/** The ten rule members this editor drafts — exactly the wire's fields. */
interface RuleDraft {
  enabled: boolean;
  requireLocal: boolean;
  requireDod: string;
  requiredChecks: string;
  checksPolicy: string;
  noChecks: string;
  reviewMode: string;
  aeonReviewAgents: string;
  requireThreadsResolved: boolean;
  method: string;
}

// Both rule shapes in the vocabulary are valid seeds: `mergerule.get`
// answers the full row, while `mergerule.vocabulary.baseline` is the bare
// rule without the row's identity or revision. The seed type is the fields
// the draft reads — nothing else may leak into an editable field.
type RuleSeed = {
  enabled?: boolean;
  requireLocal?: boolean;
  requireDod?: string;
  requiredChecks?: readonly unknown[];
  checksPolicy?: string;
  noChecks?: string;
  reviewMode?: string;
  aeonReviewAgents?: readonly unknown[];
  requireThreadsResolved?: boolean;
  method?: string;
};

function draftFrom(rule: RuleSeed | undefined): RuleDraft | undefined {
  if (!rule) return undefined;
  return {
    enabled: rule.enabled ?? false,
    requireLocal: rule.requireLocal ?? false,
    requireDod: rule.requireDod ?? "",
    requiredChecks: (rule.requiredChecks ?? []).map((word) => String(word)).join(", "),
    checksPolicy: rule.checksPolicy ?? "",
    noChecks: rule.noChecks ?? "",
    reviewMode: rule.reviewMode ?? "",
    aeonReviewAgents: (rule.aeonReviewAgents ?? []).map((word) => String(word)).join(", "),
    requireThreadsResolved: rule.requireThreadsResolved ?? false,
    method: rule.method ?? "",
  };
}

function wireFrom(draft: RuleDraft): Record<string, unknown> {
  return {
    enabled: draft.enabled,
    requireLocal: draft.requireLocal,
    requireDod: draft.requireDod,
    requiredChecks: parseList(draft.requiredChecks),
    checksPolicy: draft.checksPolicy,
    noChecks: draft.noChecks,
    reviewMode: draft.reviewMode,
    aeonReviewAgents: parseList(draft.aeonReviewAgents),
    requireThreadsResolved: draft.requireThreadsResolved,
    method: draft.method,
  };
}

const H = "text-xs font-semibold uppercase tracking-widest opacity-70";

export function MergeRuleEditor({ componentId, controller, live, canWrite }: MergeRuleEditorProps) {
  const { t } = useI18n();
  const action = useRpcAction();
  const vocabularyQuery = useMergeRuleVocabulary(controller, live);
  const getQuery = useMergeRuleGet(componentId, controller, live);

  const [vocabulary, setVocabulary] = useState<MergeRuleVocabulary | undefined>(undefined);
  useEffect(() => {
    if (vocabularyQuery.data) setVocabulary(vocabularyQuery.data);
  }, [vocabularyQuery.data]);

  const rule = getQuery.data?.rule;
  const [draft, setDraft] = useState<RuleDraft | undefined>(undefined);
  useEffect(() => {
    // Seed from the daemon's answer (its own row, or its baseline row when
    // the component has none) — never from a locally-built default rule.
    const seeded = draftFrom(rule) ?? draftFrom(vocabulary?.baseline);
    setDraft((current) => (current === undefined || seeded === undefined ? seeded : current));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rule, vocabulary]);

  const [confirm, setConfirm] = useState<{ kind: "set"; diff: RuleDiffRow[] } | { kind: "clear" } | undefined>(undefined);

  const wire = useMemo(() => (draft ? wireFrom(draft) : undefined), [draft]);
  const diff = useMemo(() => (wire ? ruleDiff(rule, wire) : []), [rule, wire]);

  const applySaved = (revision: number | undefined) => {
    // The panel re-reads `mergerule.get` after every write, and THAT answer
    // carries `lastChange` — the daemon's own audit sentence for the change.
    // The client-side diff above the confirm modal is for the confirm only;
    // the audit sentence is never re-derived here.
    notifications.push({
      title: t("settings.gates.merge.saved", { revision: revision ?? "?" }),
      tone: "success",
      source: "local",
    });
  };

  const save = async () => {
    if (!wire) return;
    const result = await action.run(() => writeMergeRuleSet(componentId, wire), {
      errorTitle: t("settings.gates.merge.saveFailed"),
    });
    if (result) {
      getQuery.refetch(); // the lastChange row shown below is the daemon's next answer
      applySaved(result.rule?.revision);
    }
    // A refusal lands in action.error verbatim; the draft stays as typed.
  };

  const clear = async () => {
    const result = await action.run(() => writeMergeRuleClear(componentId), {
      errorTitle: t("settings.gates.merge.clearFailed"),
    });
    if (result) {
      getQuery.refetch();
      applySaved(undefined);
    }
  };

  const floor = getQuery.data?.floor;
  const off = !canWrite || action.busy;

  return (
    <div className="space-y-8 pb-6" data-testid="gates-merge-panel">
      {getQuery.error && (
        <Alert intent="danger" title={t("settings.gates.merge.failed")} description={getQuery.error} data-testid="gates-merge-failed">
          <Button size="sm" onClick={getQuery.refetch}>{t("settings.gates.actions.retry")}</Button>
        </Alert>
      )}

      {draft && vocabulary && (
        <div className="space-y-8" data-testid="gates-merge-form">
          <section className="space-y-3">
            <h3 className={H}>{t("settings.gates.q.on")}</h3>
            {getQuery.data && (
              <div className="flex flex-wrap items-center gap-2 text-sm" data-testid="gates-merge-source">
                <Pill size="sm" tone={isOwnSource(getQuery.data.source) ? "sky" : "slate"} data-testid="gates-merge-source-word">{t(`settings.gates.merge.source.${getQuery.data.source}`)}</Pill>
                {typeof rule?.revision === "number" && <span className="text-xs opacity-80" data-testid="gates-merge-revision">{t("settings.gates.merge.revision", { revision: rule.revision })}</span>}
                {getQuery.data.lastChange?.summary && <span className="text-xs italic opacity-80" data-testid="gates-merge-lastchange">{t("settings.gates.merge.lastChange", { summary: getQuery.data.lastChange.summary })}</span>}
              </div>
            )}
            <div data-testid="merge-enabled">
              <Toggle size="sm" aria-label={t("settings.gates.merge.fields.enabled")} label={t("settings.gates.merge.fields.enabled")} checked={draft.enabled} disabled={off} onChange={(event) => setDraft({ ...draft, enabled: event.target.checked })} />
            </div>
          </section>

          {floor && (
            <ActionPanel
              tone={floor.ok ? "green" : "amber"}
              size="sm"
              data-testid="gates-merge-floor"
              title={t("settings.gates.q.floor")}
              description={
                floor.ok ? (
                  <span data-testid="gates-merge-floor-ok">{t("settings.gates.merge.floorOk")}</span>
                ) : (
                  <ul className="list-disc space-y-1 pl-5">
                    {(floor.missing ?? []).map((item) => <li key={item.code} data-testid={`gates-merge-floor-${item.code}`}>{item.sentence}</li>)}
                  </ul>
                )
              }
            />
          )}

          <section className="space-y-3">
            <h3 className={H}>{t("settings.gates.q.pass")}</h3>
            <div data-testid="merge-requireLocal">
              <Toggle size="sm" aria-label={t("settings.gates.merge.fields.requireLocal")} label={t("settings.gates.merge.fields.requireLocal")} checked={draft.requireLocal} disabled={off} onChange={(event) => setDraft({ ...draft, requireLocal: event.target.checked })} />
            </div>
            <FieldSelect label={t("settings.gates.merge.fields.requireDod")} value={draft.requireDod} options={vocabulary.requireDod} disabled={off} onChange={(value) => setDraft({ ...draft, requireDod: value })} testId="merge-requireDod" />
            <FieldSelect label={t("settings.gates.merge.fields.checksPolicy")} value={draft.checksPolicy} options={vocabulary.checksPolicy} disabled={off} onChange={(value) => setDraft({ ...draft, checksPolicy: value })} testId="merge-checksPolicy" />
            <FieldSelect label={t("settings.gates.merge.fields.noChecks")} value={draft.noChecks} options={vocabulary.noChecks} disabled={off} onChange={(value) => setDraft({ ...draft, noChecks: value })} testId="merge-noChecks" />
            <FormField label={t("settings.gates.merge.fields.requiredChecks")}>
              <div data-testid="gates-merge-requiredChecks-label">
                <Input variant="ghost" className="font-mono" aria-label={t("settings.gates.merge.fields.requiredChecks")} value={draft.requiredChecks} disabled={off} onChange={(event) => setDraft({ ...draft, requiredChecks: event.target.value })} data-testid="merge-requiredChecks" />
              </div>
            </FormField>
          </section>

          <section className="space-y-3">
            <h3 className={H}>{t("settings.gates.q.review")}</h3>
            <FieldSelect label={t("settings.gates.merge.fields.reviewMode")} value={draft.reviewMode} options={vocabulary.reviewMode} disabled={off} onChange={(value) => setDraft({ ...draft, reviewMode: value })} testId="merge-reviewMode" />
            <FormField label={t("settings.gates.merge.fields.aeonReviewAgents")}>
              <div data-testid="gates-merge-aeonReviewAgents-label">
                <Input variant="ghost" className="font-mono" aria-label={t("settings.gates.merge.fields.aeonReviewAgents")} value={draft.aeonReviewAgents} disabled={off} onChange={(event) => setDraft({ ...draft, aeonReviewAgents: event.target.value })} data-testid="merge-aeonReviewAgents" />
              </div>
            </FormField>
            <div data-testid="merge-requireThreadsResolved">
              <Toggle size="sm" aria-label={t("settings.gates.merge.fields.requireThreadsResolved")} label={t("settings.gates.merge.fields.requireThreadsResolved")} checked={draft.requireThreadsResolved} disabled={off} onChange={(event) => setDraft({ ...draft, requireThreadsResolved: event.target.checked })} />
            </div>
          </section>

          <section className="space-y-3">
            <h3 className={H}>{t("settings.gates.q.how")}</h3>
            <FieldSelect label={t("settings.gates.merge.fields.method")} value={draft.method} options={vocabulary.method} disabled={off} onChange={(value) => setDraft({ ...draft, method: value })} testId="merge-method" />
          </section>

          <div className="flex gap-2">
            <Button size="sm" onClick={() => setConfirm({ kind: "set", diff })} disabled={off || diff.length === 0} data-testid="gates-merge-save">{t("settings.gates.merge.save")}</Button>
            {isOwnSource(getQuery.data?.source) && (
              <Button size="sm" variant="ghost" onClick={() => setConfirm({ kind: "clear" })} disabled={off} data-testid="gates-merge-clear">{t("settings.gates.merge.clear")}</Button>
            )}
          </div>
        </div>
      )}

      <MergePreview componentId={componentId} />

      {confirm && (
        <ConfirmModal
          isOpen
          title={t(confirm.kind === "set" ? "settings.gates.merge.confirmSetTitle" : "settings.gates.merge.confirmClearTitle")}
          description={t(confirm.kind === "set" ? "settings.gates.merge.confirmSetBody" : "settings.gates.merge.confirmClearBody")}
          confirmLabel={t(confirm.kind === "set" ? "settings.gates.merge.confirmGo" : "settings.gates.merge.clear")}
          onClose={() => setConfirm(undefined)}
          onConfirm={() => {
            const kind = confirm.kind;
            setConfirm(undefined);
            void (kind === "set" ? save() : clear());
          }}
        >
          {confirm.kind === "set" && (
            // The fields that change, computed field-by-field from the rule the daemon answered with —
            // shown before the write, never after (the daemon's lastChange.summary is only the receipt).
            <table className="w-full text-left text-xs" data-testid="gates-merge-confirm-diff">
              <tbody>
                {confirm.diff.map((row) => (
                  <tr key={row.field}>
                    <td className="py-1 pr-2 font-mono opacity-80">{row.field}</td>
                    <td className="py-1 pr-2">{row.before}</td>
                    <td className="py-1">→ {row.after}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </ConfirmModal>
      )}
    </div>
  );
}

function FieldSelect({ label, value, options, disabled, onChange, testId }: {
  label: string;
  value: string;
  options: readonly string[] | undefined;
  disabled: boolean;
  onChange: (value: string) => void;
  testId: string;
}) {
  return (
    <FormField label={label}>
      <div data-testid={`${testId}-label`}>
        <Select variant="ghost" aria-label={label} value={value} disabled={disabled} onChange={(event) => onChange(event.target.value)} data-testid={testId}>
          {(options ?? []).map((word) => <option key={word} value={word}>{word}</option>)}
        </Select>
      </div>
    </FormField>
  );
}
