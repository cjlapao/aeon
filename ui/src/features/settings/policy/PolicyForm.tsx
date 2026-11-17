import { useCallback, useEffect, useMemo, useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, FormField, Input, Panel, Select } from "../../../components/kit";
import { rpcTyped } from "../../../lib/bridge";
import { useRpcAction } from "../../../lib/useRpcAction";
import { useTypeToConfirm } from "../../../lib/useConfirm";
import { AgentTagPicker } from "../../../components/AgentTagPicker";
import type { LiveController, LiveState } from "../../../lib/live";
import { SpecFields } from "./SpecFields";
import { QuickGrantBody } from "./QuickGrantBody";
import {
  buildAddParams,
  canSubmit,
  defaultSource,
  toolPickerInfo,
  describeDiff,
  specFieldsFor,
  suggestedId,
  type AddParamsWire,
  type InForceAnswerView,
  type PolicyDraft,
  type Vocabulary,
} from "./policyModel";

/**
 * The authoring form — generated from `policy.vocabulary` (PM6), not from
 * anything this file remembers.
 *
 * The closed sets are never typed twice here: the type list, target kinds,
 * sources and actions come from the vocabulary arrays, and the spec fields
 * from the type's descriptor. The **authorship rule is rendered from the
 * daemon's matrix**: a `(source, action)` the matrix refuses disables Submit
 * and shows that cell's own `reason` verbatim — the UI holds no copy of PL1.
 * The expiry is empty-for-no-expiry (`notAfter: null`, never a sentinel).
 * The suggested id is the one thing the UI invents, and it stays editable.
 *
 * Submit widens (an `allow`, or a bare-kind target that spans every agent)
 * → the person sees the draft's identity and the target's CURRENT in-force
 * answer first and must confirm; a decline sends nothing. After the door
 * answers, its `before`/`after` (and any warnings) are shown verbatim. A
 * refusal stays on screen word for word and the draft is kept — no silent
 * reset.
 */
export interface PolicyFormProps {
  vocabulary: Vocabulary | undefined;
  canWrite: boolean;
  controller: LiveController;
  live: LiveState;
  busy: boolean;
  onCancel: () => void;
  /** The guided grant: who, what, decision. The full form stays one click away. */
  quick?: boolean;
  /** Called after an accepted add so the section re-reads the list. */
  onAdded: (id: string) => void;
  /**
   * S46 T11 (MG7): the two-step grant flow opens this ONE form PREFILLED
   * (tool_access · allow · operator, target and tool named) — an additive
   * optional seed; absent, the form starts empty exactly as S45 shipped it.
   */
  initial?: Partial<PolicyDraft>;
}

/** A section's title: the same small capitals the management pages use (docs/aeon/UI-DESIGN-LANGUAGE.md §3). */
const SECTION = "text-xs font-semibold uppercase tracking-widest opacity-70";

function emptyDraft(): PolicyDraft {
  return { type: "", targetKind: "", targetId: "", action: "", source: "", id: "", expiry: "", spec: {} };
}

export function PolicyForm({ vocabulary, canWrite, controller, live, busy, onCancel, onAdded, initial, quick = false }: PolicyFormProps) {
  const { t } = useI18n();
  const [draft, setDraft] = useState<PolicyDraft>({ ...emptyDraft(), ...initial });
  const [touchedId, setTouchedId] = useState(false);
  const [advanced, setAdvanced] = useState(!quick);
  // A daemon that does not name a tool-address field (an older build) leaves the
  // guided grant nothing to stand on: open the full form instead of a dead end.
  const guidedPossible = toolPickerInfo(vocabulary) !== undefined;
  useEffect(() => {
    if (quick && vocabulary && !guidedPossible) setAdvanced(true);
  }, [quick, vocabulary, guidedPossible]);
  const [result, setResult] = useState<{ describe?: string; before?: InForceAnswerView; after?: InForceAnswerView; warnings?: string[] } | null>(null);
  const action = useRpcAction();
  const { confirm, modal } = useTypeToConfirm();

  // Source defaults to the matrix's own first row — the daemon orders the
  // closed set (operator first); the UI never names it.
  useEffect(() => {
    if (vocabulary && draft.source === "") {
      setDraft((current) => (current.source === "" ? { ...current, source: defaultSource(vocabulary) } : current));
    }
  }, [vocabulary, draft.source]);

  const idSuggestion = useMemo(
    () => suggestedId(draft.type, draft.targetKind, draft.targetId),
    [draft.type, draft.targetKind, draft.targetId],
  );
  // Until the person edits the id, it follows the suggestion.
  useEffect(() => {
    if (!touchedId) setDraft((current) => (current.id === idSuggestion ? current : { ...current, id: idSuggestion }));
  }, [idSuggestion, touchedId]);

  const decision = vocabulary ? canSubmit(draft, vocabulary) : { ok: false, matrixBlocked: false, missing: [] };
  const specFields = vocabulary ? specFieldsFor(vocabulary, draft.type) : [];
  const set = (patch: Partial<PolicyDraft>): void => setDraft((current) => ({ ...current, ...patch }));

  const submit = useCallback(async () => {
    if (!decision.ok) return;
    setResult(null);
    const params: AddParamsWire = buildAddParams(draft);

    // Widening is a confirm-first act: an `allow`, or a target that names no
    // id (the bare kind the daemon reads as spanning every agent). The
    // confirmation carries the draft's identity and the target's CURRENT
    // in-force answer — fetched, not guessed. (The new row's `describe`
    // sentence exists only after the boundary writes it, so it arrives with
    // the result below, verbatim — the UI never pre-authors it, D9.)
    const widens = draft.action === "allow" || (draft.targetKind !== "" && draft.targetId === "");
    if (widens && canWrite) {
      let inForceLine = "";
      if (draft.targetKind === "agent" && draft.targetId !== "") {
        const answer = await action.run(() => rpcTyped("policy.inForce", { agent: draft.targetId }), {
          errorTitle: t("settings.policy.form.inForceFailed"),
        });
        if (answer) inForceLine = (answer.answers ?? []).map((a) => a.render ?? a.state ?? "").join(" | ");
      }
      const confirmed = await confirm({
        title: t("settings.policy.form.confirmTitle", { id: params.id }),
        description: [
          t("settings.policy.form.confirmIdentity", {
            type: params.type,
            target: draft.targetId === "" ? params.target.kind : `${params.target.kind}:${draft.targetId}`,
            action: params.action,
          }),
          inForceLine === "" ? t("settings.policy.form.confirmNoInForce") : inForceLine,
        ].join("\n"),
        confirmValue: params.id,
        confirmValueLabel: t("settings.policy.form.confirmValueLabel"),
        confirmLabel: t("settings.policy.form.submit"),
      });
      if (!confirmed) return; // decline sends NOTHING
    }

    const added = await action.run(() => rpcTyped("policy.add", params), {
      errorTitle: t("settings.policy.form.addFailed"),
    });
    if (added) {
      setResult({
        describe: added.policy.describe,
        before: added.before as InForceAnswerView,
        after: added.after as InForceAnswerView,
        warnings: added.warnings,
      });
      onAdded(added.policy.id);
    }
    // A refusal: action.error renders below and the DRAFT STAYS — the person
    // fixes what the daemon named, it does not start over (S35 map #6 parity).
  }, [action, canWrite, confirm, decision.ok, draft, onAdded, t]);

  const resultDiff = result ? describeDiff(result.before, result.after) : undefined;

  const body = (
    <>
      {!advanced && vocabulary && (
        <QuickGrantBody
          draft={draft}
          set={set}
          vocabulary={vocabulary}
          controller={controller}
          live={live}
          onAdvanced={() => setAdvanced(true)}
        />
      )}
      {advanced && (
      <div className="space-y-8" data-testid="policy-form-advanced">
        <section className="space-y-4" aria-label={t("settings.policy.form.aboutTitle")}>
          <h3 className={SECTION}>{t("settings.policy.form.aboutTitle")}</h3>
          <p className="text-sm opacity-80">{t("settings.policy.form.aboutHelp")}</p>
          <div className="grid gap-4 md:grid-cols-2">
            <FormField label={t("settings.policy.form.type")} labelFor="policy-form-type">
              <Select id="policy-form-type" aria-label={t("settings.policy.form.type")} variant="ghost" size="md" value={draft.type} onChange={(event) => set({ type: event.target.value })}>
                <option value="">—</option>
                {(vocabulary?.types ?? []).map((option) => (
                  <option key={option} value={option}>{option}</option>
                ))}
              </Select>
            </FormField>
            <FormField label={t("settings.policy.form.targetKind")} labelFor="policy-form-target-kind">
              <Select id="policy-form-target-kind" aria-label={t("settings.policy.form.targetKind")} variant="ghost" size="md" value={draft.targetKind} onChange={(event) => set({ targetKind: event.target.value, targetId: "" })}>
                <option value="">—</option>
                {(vocabulary?.targetKinds ?? []).map((option) => (
                  <option key={option} value={option}>{option}</option>
                ))}
              </Select>
            </FormField>
          </div>
          {draft.targetKind !== "" && (
            <FormField label={t("settings.policy.form.targetId")} hint={t("settings.policy.form.targetIdHint")}>
              {/* A real agent identity or nothing — the picker cannot emit a
                  free-typed slug. For the kinds the roster does not cover the
                  person types the identifier and the boundary owns its rules
                  (a wrong one comes back as the boundary's refusal, verbatim). */}
              <AgentTagPicker value={draft.targetId} onChange={(targetId) => set({ targetId })} controller={controller} live={live} />
            </FormField>
          )}
        </section>

        <section className="space-y-4" aria-label={t("settings.policy.form.decisionTitle")}>
          <h3 className={SECTION}>{t("settings.policy.form.decisionTitle")}</h3>
          <p className="text-sm opacity-80">{t("settings.policy.form.decisionHelp")}</p>
          <div className="grid gap-4 md:grid-cols-2">
            <FormField label={t("settings.policy.form.action")} labelFor="policy-form-action">
              <Select id="policy-form-action" aria-label={t("settings.policy.form.action")} variant="ghost" size="md" value={draft.action} onChange={(event) => set({ action: event.target.value })}>
                <option value="">—</option>
                {(vocabulary?.actions ?? []).map((option) => (
                  <option key={option} value={option}>{option}</option>
                ))}
              </Select>
            </FormField>
            <FormField label={t("settings.policy.form.source")} labelFor="policy-form-source">
              <Select id="policy-form-source" aria-label={t("settings.policy.form.source")} variant="ghost" size="md" value={draft.source} onChange={(event) => set({ source: event.target.value })}>
                {(vocabulary?.sources ?? []).map((option) => (
                  <option key={option} value={option}>{option}</option>
                ))}
              </Select>
            </FormField>
          </div>
        </section>

        <section className="space-y-4" aria-label={t("settings.policy.form.spec")}>
          <h3 className={SECTION}>{t("settings.policy.form.spec")}</h3>
          <SpecFields
            fields={specFields}
            values={draft.spec}
            controller={controller}
            live={live}
            onChange={(name, value) => set({ spec: { ...draft.spec, [name]: value } })}
          />
        </section>

        <section className="space-y-4" aria-label={t("settings.policy.form.lifeTitle")}>
          <h3 className={SECTION}>{t("settings.policy.form.lifeTitle")}</h3>
          <p className="text-sm opacity-80">{t("settings.policy.form.lifeHelp")}</p>
          <div className="grid gap-4 md:grid-cols-2">
            <FormField label={t("settings.policy.form.expiry")} labelFor="policy-form-expiry" hint={t("settings.policy.form.expiryHint")}>
              <Input
                id="policy-form-expiry"
                variant="ghost"
                size="md"
                aria-label={t("settings.policy.form.expiry")}
                value={draft.expiry}
                placeholder={t("settings.policy.form.expiryPlaceholder")}
                onChange={(event) => set({ expiry: event.target.value })}
              />
            </FormField>
            <FormField label={t("settings.policy.form.id")} labelFor="policy-form-id" hint={t("settings.policy.form.idSuggested")}>
              <Input
                id="policy-form-id"
                variant="ghost"
                size="md"
                aria-label={t("settings.policy.form.id")}
                value={draft.id}
                placeholder={idSuggestion}
                onChange={(event) => {
                  setTouchedId(true);
                  set({ id: event.target.value });
                }}
              />
            </FormField>
          </div>
        </section>
      </div>
      )}

      {/* The daemon's own authorship statement — rendered from the matrix
          cell for this (source, action), word for word, never re-authored. */}
      {decision.matrixBlocked && (
        <Alert
          intent="warning"
          title={decision.refusal ?? t("settings.policy.form.matrixUnknown")}
          className="mt-3"
          data-testid="matrix-refusal"
        />
      )}

      {action.error && (
        // The boundary's refusal, verbatim, under the form — with the draft
        // kept, so the next submit is a correction and not a reroll.
        <Alert intent="danger" title={action.error} className="mt-3" data-testid="add-refusal" />
      )}

      {result && (
        <div className="mt-3 space-y-2" data-testid="add-result">
          <Alert
            intent="success"
            title={t("settings.policy.form.added", { id: draft.id })}
            description={result.describe} // the changed row's sentence — the daemon's, verbatim
          />
          {resultDiff === undefined ? null : resultDiff.changed ? (
            <div className="grid gap-3 md:grid-cols-2" data-testid="add-before-after">
              <div>
                <p className="text-xs uppercase tracking-wide opacity-70">{t("settings.policy.before")}</p>
                {(resultDiff.before?.answers ?? []).map((a, i) => (
                  <p key={i} className="text-sm">{a.render ?? a.state ?? ""}</p>
                ))}
              </div>
              <div>
                <p className="text-xs uppercase tracking-wide opacity-70">{t("settings.policy.after")}</p>
                {(resultDiff.after?.answers ?? []).map((a, i) => (
                  <p key={i} className="text-sm">{a.render ?? a.state ?? ""}</p>
                ))}
              </div>
            </div>
          ) : (
            <p className="text-sm opacity-70" data-testid="add-no-change">{t("settings.policy.noChange")}</p>
          )}
          {(result.warnings ?? []).map((warning, index) => (
            <Alert key={index} intent="warning" title={String(warning)} />
          ))}
        </div>
      )}

      <div className="mt-4 flex justify-end gap-2">
        <Button variant="ghost" size="sm" onClick={onCancel}>
          {t("settings.policy.form.cancel")}
        </Button>
        <Button
          size="sm"
          disabled={!canWrite || busy || !decision.ok}
          onClick={() => void submit()}
          title={!canWrite ? t("settings.policy.reconnecting") : undefined}
        >
          {t("settings.policy.form.submit")}
        </Button>
      </div>
      {modal}
    </>
  );
  return quick ? (
    <div data-testid="policy-form">{body}</div>
  ) : (
    <Panel title={t("settings.policy.form.title")} subtitle={t("settings.policy.form.subtitle")} data-testid="policy-form">
      {body}
    </Panel>
  );
}
