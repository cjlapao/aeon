import { useMemo, useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, FormField, Input, Pill, Select, Toggle } from "../../../components/kit";
import { refusalBody, toRpcError } from "../../../lib/bridge";
import { notifications } from "../../../lib/notifications";
import type { LiveController, LiveState } from "../../../lib/live";
import { AuthorityConfirm } from "./AuthorityConfirm";
import { DryRunBlock, MatchBuilder, MatchTester } from "./MatchBuilder";
import { BlueprintPicker } from "./ScheduleForm";
import { createEventTrigger, disableEvent, updateEventTrigger, useComponentOptions } from "./automationRpc";
import {
  canSubmit,
  buildMatch,
  DISARM_ON_CREATE_REASON,
  emptyEventDraft,
  parseLimit,
  type EventDraft,
  type ShowAutomation,
  type Vocabulary,
} from "./automationModel";

/**
 * EventForm — the event family's create/edit. Everything descriptor-driven:
 * the event select and the match keys come from `automation.vocabulary`,
 * and the MatchBuilder offers **only the keys applicable to the chosen
 * event**. The sample tester answers through `automation.test`; the
 * labelled dry-run ("would have matched in the last N events") through
 * `automation.dryrun` — no client-side matching anywhere.
 *
 * Edit is narrowing-only (AN9): the door itself refuses widenings, so the
 * form sends the edited shape and lets the ONE validator say the word —
 * verbatim, in the refusal line under the fields.
 *
 * Created disarmed unless the authority confirmation is passed first (AN3),
 * composed the way the CLI composes `add-event`: create, then disable.
 */
export interface EventFormProps {
  initial?: ShowAutomation;
  vocabulary: Vocabulary | undefined;
  authorityStatement: string;
  controller: LiveController;
  live: LiveState;
  canWrite: boolean;
  onClose: () => void;
  onChanged: () => void;
}

export function EventForm({ initial, vocabulary, authorityStatement, controller, live, canWrite, onClose, onChanged }: EventFormProps) {
  const { t } = useI18n();
  const editing = initial !== undefined;
  const draft0 = useMemo<EventDraft>(() => (initial !== undefined
    ? {
        id: initial.id,
        name: initial.name,
        blueprint: initial.blueprint,
        event: (initial.match as { event?: string } | undefined)?.event ?? "",
        values: Object.fromEntries(
          Object.entries((initial.match as Record<string, unknown> | undefined) ?? {})
            .filter(([key, value]) => key !== "event" && Array.isArray(value))
            .map(([key, value]) => [key, value as string[]]),
        ),
        maxRunsPerHour: String(initial.maxRunsPerHour ?? ""),
        kickoff: initial.kickoff ?? "",
        componentId: (initial as { componentId?: string }).componentId ?? "",
      }
    : emptyEventDraft()), [initial]);
  const [draft, setDraft] = useState<EventDraft>(draft0);
  const [armed, setArmed] = useState(false);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<string | undefined>(undefined);
  const [disarmedNote, setDisarmedNote] = useState(false);
  const components = useComponentOptions(controller, live);

  const set = (patch: Partial<EventDraft>) => setDraft((d) => ({ ...d, ...patch }));
  const gate = canSubmit("event", draft, vocabulary);

  // A refusal is a toast and nothing else; every field stays as typed.
  const refuse = (cause: unknown) => {
    const body = cause instanceof Error && !(cause as { data?: unknown }).data ? cause.message : refusalBody(toRpcError(cause));
    notifications.push({ title: t("settings.automation.form.refused"), body, tone: "danger", source: "local" });
  };

  const submit = async () => {
    setBusy(true);
    try {
      const match = buildMatch(draft.event, draft.values);
      const wire = {
        id: draft.id,
        name: draft.name,
        blueprint: draft.blueprint,
        match,
        kickoff: draft.kickoff,
        maxRunsPerHour: parseLimit(draft.maxRunsPerHour),
      };
      if (editing) {
        await updateEventTrigger(wire);
        setResult(`updated — ${draft.id}`);
      } else {
        const res = await createEventTrigger({ ...wire, componentId: draft.componentId });
        if (!armed) {
          // AN3 verbatim as the CLI door words it: creating an event trigger
          // leaves it disabled until a person enables it deliberately. The
          // reason is the CLI's own constant — audit rows read the same in
          // every UI language, because audit text is not translated.
          await disableEvent(res.id, DISARM_ON_CREATE_REASON);
          setDisarmedNote(true);
        }
        setResult(`created — ${res.id}`);
      }
      onChanged();
    } catch (err) {
      refuse(err);
    } finally {
      setBusy(false);
    }
  };

  const save = async () => {
    if (!editing && armed) {
      setConfirmOpen(true);
      return;
    }
    await submit();
  };

  const H = "text-xs font-semibold uppercase tracking-widest opacity-70";
  const off = !canWrite || busy;

  return (
    <div className="space-y-8" data-testid="automation-form-event">
      {!canWrite && <Alert intent="warning" title={t("settings.automation.reconnecting")} />}

      <section className="space-y-3">
        <h3 className={H}>{t("settings.automation.q.who")}</h3>
        <FormField label={t("settings.automation.form.id")}>
          <Input variant="ghost" aria-label={t("settings.automation.form.id")} value={draft.id} disabled={editing} onChange={(e) => set({ id: e.target.value })} />
        </FormField>
        <FormField label={t("settings.automation.form.name")}>
          <Input variant="ghost" aria-label={t("settings.automation.form.name")} value={draft.name} onChange={(e) => set({ name: e.target.value })} />
        </FormField>
        <BlueprintPicker selected={draft.blueprint} onChange={(blueprint) => set({ blueprint })} controller={controller} live={live} />
        {/* S38's list method for the scope picker — the trigger fires work under a component's root; the daemon validates the id. */}
        <FormField label={t("settings.automation.form.component")}>
          <Select variant="ghost" aria-label={t("settings.automation.form.component")} value={draft.componentId} onChange={(e) => set({ componentId: e.target.value })}>
            <option value="">—</option>
            {(components.data?.components ?? []).map((row) => <option key={row.id} value={row.id}>{row.id}</option>)}
          </Select>
        </FormField>
        <FormField label={t("settings.automation.form.kickoff")}>
          <textarea
            aria-label={t("settings.automation.form.kickoff")}
            className="aeon-form-input min-h-16 w-full rounded-sm border p-2 font-mono text-xs"
            value={draft.kickoff}
            onChange={(e) => set({ kickoff: e.target.value })}
          />
        </FormField>
        <p className="text-xs opacity-60">{t("settings.automation.form.kickoffHint")}</p>
      </section>

      <section className="space-y-3">
        <h3 className={H}>{t("settings.automation.q.match")}</h3>
        <MatchBuilder event={draft.event} values={draft.values} vocabulary={vocabulary} onEvent={(event) => set({ event })} onValues={(values) => set({ values })} />
        {draft.event !== "" && <MatchTester event={draft.event} values={draft.values} vocabulary={vocabulary} />}
        {editing && <DryRunBlock id={draft.id} />}
      </section>

      <section className="space-y-3">
        <h3 className={H}>{t("settings.automation.q.limitsForm")}</h3>
        <FormField label={t("settings.automation.form.maxRuns")}>
          <Input variant="ghost" aria-label={t("settings.automation.form.maxRuns")} value={draft.maxRunsPerHour} placeholder="6" onChange={(e) => set({ maxRunsPerHour: e.target.value })} />
        </FormField>
      </section>

      {!editing && (
        <section className="space-y-3">
          <h3 className={H}>{t("settings.automation.q.arm")}</h3>
          <div data-testid="armed-choice" className="flex flex-wrap items-center gap-2">
            <Toggle size="sm" aria-label={t("settings.automation.form.createArmed")} label={t("settings.automation.form.createArmed")} checked={armed} onChange={(e) => setArmed(e.target.checked)} />
            <Pill size="sm" tone="amber">{t("settings.automation.form.disarmedByDefault")}</Pill>
          </div>
        </section>
      )}

      {result !== undefined && <Alert intent="success" title={result} data-testid="form-result" />}
      {disarmedNote && <Alert intent="info" title={t("settings.automation.form.createdDisarmed")} data-testid="form-disarmed-note" />}

      <div className="flex gap-2">
        <Button size="sm" disabled={off || !gate} onClick={() => void save()} data-testid="automation-form-save">{t(editing ? "settings.automation.form.save" : "settings.automation.form.create")}</Button>
        <Button size="sm" variant="ghost" onClick={onClose} data-testid="automation-form-cancel">{t("settings.automation.form.cancel")}</Button>
      </div>

      <AuthorityConfirm
        open={confirmOpen}
        authorityStatement={authorityStatement}
        confirmLabel={t("settings.automation.form.confirmArm")}
        busy={busy}
        onConfirm={() => {
          setConfirmOpen(false);
          void submit();
        }}
        onCancel={() => setConfirmOpen(false)}
      >
        <p className="font-mono text-xs opacity-80">{draft.id} — {JSON.stringify(buildMatch(draft.event, draft.values))}</p>
      </AuthorityConfirm>
    </div>
  );
}
