import { useMemo, useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, FormField, Input, Pill, Select, Toggle } from "../../../components/kit";
import { refusalBody, toRpcError } from "../../../lib/bridge";
import { notifications } from "../../../lib/notifications";
import type { LiveController, LiveState } from "../../../lib/live";
import { AuthorityConfirm } from "./AuthorityConfirm";
import { WebhookFields } from "./WebhookFields";
import { SchedulePreview } from "./SchedulePreview";
import { useBlueprintOptions, addScheduleTrigger, pauseSchedule, updateScheduleTrigger, useCronPreview } from "./automationRpc";
import {
  canSubmit,
  emptyScheduleDraft,
  envNameOnly,
  type ScheduleDraft,
  type ShowAutomation,
  type Vocabulary,
} from "./automationModel";

/**
 * ScheduleForm — the create/edit form for the SCHEDULE family (and, through
 * `WebhookForm`, the webhook one). Descriptor-driven: zones come from
 * `automation.vocabulary`, blueprints from `blueprint.list` — the forms
 * import nothing of S48's components, only the list METHOD.
 *
 * The cron field carries a LIVE PREVIEW (AN11): every edit debounces a
 * `automation.preview` round trip and renders the daemon's next instants
 * with their DST/window notes verbatim — including the daemon's REFUSAL
 * text, shown under the field as the person types. Client-side cron parsing
 * is not done here at all; the field cannot be more wrong than the answer
 * under it. The timezone is a select from the vocabulary — never free text.
 *
 * A new automation starts DISARMED (AN3) exactly as the CLI composes it:
 * create, then pause, unless the person passes the authority confirmation
 * first — the daemon's own statement on a modal, the same authority the
 * terminal's `--yes` is.
 */
export interface ScheduleFormProps {
  family: "schedule" | "webhook"; // the two cron-shaped families; events have their own form
  /** When set, an EDIT over the row's canonical shape (id fixed). */
  initial?: ShowAutomation;
  vocabulary: Vocabulary | undefined;
  authorityStatement: string;
  controller: LiveController;
  live: LiveState;
  canWrite: boolean;
  onClose: () => void;
  onChanged: () => void;
}

export function ScheduleForm({ family, initial, vocabulary, authorityStatement, controller, live, canWrite, onClose, onChanged }: ScheduleFormProps) {
  const { t } = useI18n();
  const editing = initial !== undefined;
  const draft0 = useMemo<ScheduleDraft>(() => (initial !== undefined
    ? {
        id: initial.id,
        name: initial.name,
        blueprint: initial.blueprint,
        expr: initial.expr ?? "",
        zone: initial.zone ?? "UTC",
        workspace: initial.workspace ?? "",
        goal: initial.goal ?? "",
        windowStart: initial.window?.start ?? "",
        windowEnd: initial.window?.end ?? "",
        windowDays: initial.window?.days ?? "",
        secretEnv: initial.secretEnv ?? "",
        eventFilter: (initial as { eventFilter?: string }).eventFilter ?? "",
      }
    : emptyScheduleDraft(vocabulary?.zones[0] ?? "UTC")), [initial, vocabulary]);
  const [draft, setDraft] = useState<ScheduleDraft>(draft0);
  const [armed, setArmed] = useState(false);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<string | undefined>(undefined);
  const [disarmedNote, setDisarmedNote] = useState(false);

  const isWebhook = family === "webhook";
  const preview = useCronPreview(isWebhook ? "" : draft.expr, draft.zone);
  const secretOk = !isWebhook || envNameOnly(draft.secretEnv).ok;
  const gate = canSubmit(family, { ...draft, name: draft.name !== "" ? draft.name : draft.id }, vocabulary) && secretOk;

  const set = (patch: Partial<ScheduleDraft>) => setDraft((d) => ({ ...d, ...patch }));

  // A refusal is a toast and nothing else; every field stays as typed.
  const refuse = (cause: unknown) => {
    const body = typeof cause === "string" ? cause : cause instanceof Error && !(cause as { data?: unknown }).data ? cause.message : refusalBody(toRpcError(cause));
    notifications.push({ title: t("settings.automation.form.refused"), body, tone: "danger", source: "local" });
  };

  const submit = async () => {
    setBusy(true);
    try {
      if (editing) {
        const res = await updateScheduleTrigger({
          id: draft.id,
          blueprint: draft.blueprint,
          ...(isWebhook || draft.expr === initial?.expr ? {} : { cron: draft.expr }),
          // A webhook has no clock: no zone, no window (the daemon refuses a webhook that carries either).
          ...(isWebhook ? { eventFilter: draft.eventFilter } : { timezone: draft.zone }),
          workspace: draft.workspace,
          goal: draft.goal,
          ...(isWebhook ? {} : draft.windowStart === "" && initial?.window?.start !== undefined ? { clearWindow: true } : {}),
          ...(!isWebhook && draft.windowStart !== "" ? { windowStart: draft.windowStart } : {}),
          ...(!isWebhook && draft.windowEnd !== "" ? { windowEnd: draft.windowEnd } : {}),
          ...(!isWebhook && draft.windowDays !== "" ? { windowDays: draft.windowDays } : {}),
          ...(isWebhook && draft.secretEnv !== "" ? { secretEnv: draft.secretEnv } : {}),
        });
        // before/after are the door's own words (it refuses mid-draft, and
        // the refusal arrives verbatim in the thrown error instead).
        setResult(`${res.changed ? "updated" : "unchanged"} — ${draft.id}`);
        onChanged();
      } else {
        const res = await addScheduleTrigger({
          kind: family,
          id: draft.id,
          blueprint: draft.blueprint,
          expr: isWebhook ? "" : draft.expr,
          zone: isWebhook ? "" : draft.zone,
          workspace: draft.workspace,
          goal: draft.goal,
          windowStart: isWebhook ? "" : draft.windowStart,
          windowEnd: isWebhook ? "" : draft.windowEnd,
          windowDays: isWebhook ? "" : draft.windowDays,
          secretEnv: isWebhook ? draft.secretEnv : "",
          eventFilter: isWebhook ? draft.eventFilter : "",
        });
        if (!res.found) {
          // The add door's own refusal sentence (duplicate id, bad shape) —
          // shown, never rewritten, and nothing after it runs.
          refuse(res.reason);
          return;
        }
        if (!armed) {
          // AN3 as the CLI composes it: created, then immediately disarmed —
          // enabling is a separate deliberate act.
          await pauseSchedule(draft.id);
          setDisarmedNote(true);
        }
        setResult(res.reason !== "" ? res.reason : `created — ${draft.id}`);
        onChanged();
      }
    } catch (err) {
      // One validator (TD3): whatever the daemon refused with, that sentence
      // lands under the fields — the form never re-explains it.
      refuse(err);
    } finally {
      setBusy(false);
    }
  };

  const save = async () => {
    // Creating ARMED is the deliberate exception: it passes the daemon's own
    // authority statement as a confirmation first. Default is disarmed and
    // needs no confirmation — there is nothing armed to confirm.
    if (!editing && armed) {
      setConfirmOpen(true);
      return;
    }
    await submit();
  };

  const H = "text-xs font-semibold uppercase tracking-widest opacity-70";
  const off = !canWrite || busy;

  return (
    <div className="space-y-8" data-testid={`automation-form-${family}`}>
      {!canWrite && <Alert intent="warning" title={t("settings.automation.reconnecting")} />}

      <section className="space-y-3">
        <h3 className={H}>{t("settings.automation.q.who")}</h3>
        <FormField label={t("settings.automation.form.id")}>
          <Input variant="ghost" aria-label={t("settings.automation.form.id")} value={draft.id} disabled={editing} onChange={(e) => set({ id: e.target.value })} />
        </FormField>
        <BlueprintPicker selected={draft.blueprint} onChange={(blueprint) => set({ blueprint })} controller={controller} live={live} />
        <FormField label={t("settings.automation.form.workspace")}>
          <Input variant="ghost" aria-label={t("settings.automation.form.workspace")} value={draft.workspace} onChange={(e) => set({ workspace: e.target.value })} />
        </FormField>
        <FormField label={t("settings.automation.form.goal")}>
          <Input variant="ghost" aria-label={t("settings.automation.form.goal")} value={draft.goal} onChange={(e) => set({ goal: e.target.value })} />
        </FormField>
        {isWebhook && <p className="text-xs opacity-60" data-testid="webhook-goal-hint">{t("settings.automation.webhook.goalHint")} <code>{"{issue.title}"}</code></p>}
      </section>

      {!isWebhook && (
        <section className="space-y-3">
          <h3 className={H}>{t("settings.automation.q.when")}</h3>
          <FormField label={t("settings.automation.form.cron")}>
            <Input variant="ghost" aria-label={t("settings.automation.form.cron")} value={draft.expr} placeholder="0 7 * * *" onChange={(e) => set({ expr: e.target.value })} />
          </FormField>
          {/* The daemon's live answer — instants or refusal — under the field, verbatim. This line is the form's whole cron logic. */}
          <SchedulePreview preview={preview} />
          {/* A select from the vocabulary: a free-text timezone is a typo that only fails at 3am on the second Sunday — never offered. */}
          <FormField label={t("settings.automation.form.zone")}>
            <Select variant="ghost" aria-label={t("settings.automation.form.zone")} value={draft.zone} onChange={(e) => set({ zone: e.target.value })}>
              {(vocabulary?.zones ?? ["UTC"]).map((zone) => <option key={zone} value={zone}>{zone}</option>)}
            </Select>
          </FormField>
          <p className="text-xs opacity-70">{t("settings.automation.form.windowTitle")}{vocabulary?.windowGrammar ? ` — ${vocabulary.windowGrammar}` : ""}</p>
          <div className="grid grid-cols-3 gap-2">
            <Input variant="ghost" aria-label={t("settings.automation.form.windowStart")} value={draft.windowStart} placeholder="09:00" onChange={(e) => set({ windowStart: e.target.value })} />
            <Input variant="ghost" aria-label={t("settings.automation.form.windowEnd")} value={draft.windowEnd} placeholder="18:00" onChange={(e) => set({ windowEnd: e.target.value })} />
            <Input variant="ghost" aria-label={t("settings.automation.form.windowDays")} value={draft.windowDays} placeholder="mon-fri" onChange={(e) => set({ windowDays: e.target.value })} />
          </div>
        </section>
      )}

      {isWebhook && <WebhookFields draft={draft} set={set} secretOk={secretOk} headingClass={H} />}

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
        <p className="font-mono text-xs opacity-80">{draft.id} — {draft.expr} ({draft.zone})</p>
      </AuthorityConfirm>
    </div>
  );
}

/** The blueprint picker — S48's LIST METHOD, not its components: every option is a name the daemon
 *  answered with, newest first. Health per blueprint is not part of `blueprint.list`; nothing is
 *  shown about it rather than a guess (health dots live on the rows, from `automation.health`). */
export function BlueprintPicker({ selected, onChange, controller, live }: {
  selected: string;
  onChange: (blueprint: string) => void;
  controller: LiveController;
  live: LiveState;
}) {
  const { t } = useI18n();
  const blueprints = useBlueprintOptions(controller, live);
  const options = blueprints.data?.blueprints ?? [];
  return (
    <div data-testid="blueprint-picker">
      <FormField label={t("settings.automation.form.blueprint")}>
        <Select variant="ghost" aria-label={t("settings.automation.form.blueprint")} value={selected} onChange={(e) => onChange(e.target.value)}>
          <option value="">—</option>
          {options.map((row) => <option key={`${row.name}@${row.version}`} value={row.name}>{row.name} · v{row.version} · {row.origin}</option>)}
        </Select>
      </FormField>
    </div>
  );
}
