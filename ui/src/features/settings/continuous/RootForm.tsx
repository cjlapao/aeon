import { useState } from "react";
import { createPortal } from "react-dom";
import { useI18n } from "@cjlapao/ui-kit";
import { Button, FormField, Input, Select } from "../../../components/kit";
import { refusalBody, toRpcError } from "../../../lib/bridge";
import { notifications } from "../../../lib/notifications";
import { AuthorityConfirm } from "../automation/AuthorityConfirm";
import { BlueprintPicker } from "../automation/ScheduleForm";
import type { LiveController, LiveState } from "../../../lib/live";
import { setSchedulerRoot } from "./continuousRpc";
import type { SchedulerDescriptor, SchedulerRepo } from "./continuousModel";

/**
 * RootForm — the scheduler row's template, as two questions: **what does it start here?** and
 * **when may it work?** (S27 T10 behaviour 1).
 *
 * Built FROM `scheduler.status.descriptor`, not from a typed guess at the vocabulary: the blueprint
 * choices are `blueprint.list` through the automation folder's own picker (the door stays owned by
 * its folder), the minimum-priority choices are `descriptor.priorityStates`, and the window inputs
 * carry `descriptor.windowGrammar` as their hint — the daemon's own words for what a window may say.
 *
 * Saving a row that is not yet enabled ENABLES it (CN6: a set that turns a row on is the widening),
 * so that save confirms first with the daemon's own statement; saving an already-enabled row only
 * changes its template and needs no confirm. A refusal comes back as a toast in the daemon's own
 * words — and the draft stays exactly as typed (behaviour 2): the person edits against the
 * daemon's complaint, never a wiped form.
 */
export interface RootFormProps {
  repo: SchedulerRepo;
  descriptor: SchedulerDescriptor | undefined;
  /** The daemon's enable statement (`scheduler.status.statement`), for a save that turns the row on. */
  statement: string;
  canWrite: boolean;
  controller: LiveController;
  live: LiveState;
  onClose: () => void;
  onDone: () => void;
}

interface Draft {
  blueprint: string;
  tech: string;
  minPriority: string;
  start: string;
  end: string;
  days: string;
  zone: string;
  briefPath: string;
}

const H = "text-xs font-semibold uppercase tracking-widest opacity-70";

export function RootForm({ repo, descriptor, statement, canWrite, controller, live, onClose, onDone }: RootFormProps) {
  const { t } = useI18n();
  // The draft starts at the row's current answer and NEVER resets on a refusal.
  const [draft, setDraft] = useState<Draft>({
    blueprint: repo.template.blueprint,
    tech: repo.template.tech,
    minPriority: repo.minPriority,
    start: repo.window.start,
    end: repo.window.end,
    days: repo.window.days,
    zone: repo.window.zone,
    briefPath: repo.template.with.brief_path,
  });
  const [busy, setBusy] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const set = (patch: Partial<Draft>) => setDraft((d) => ({ ...d, ...patch }));

  const submit = async () => {
    setBusy(true);
    try {
      await setSchedulerRoot({
        componentRoot: repo.componentRoot,
        enabled: true,
        blueprint: draft.blueprint,
        tech: draft.tech === "" ? undefined : draft.tech,
        minPriority: draft.minPriority === "" ? undefined : draft.minPriority,
        window:
          draft.start === "" && draft.end === "" && draft.days === "" && draft.zone === ""
            ? undefined
            : { start: draft.start, end: draft.end, days: draft.days, zone: draft.zone },
        with: { brief_path: draft.briefPath },
      });
      setConfirming(false);
      onDone();
    } catch (cause) {
      // The daemon's sentence, then what it means — a toast; the draft stays in the fields.
      setConfirming(false);
      notifications.push({ title: t("settings.continuous.refused"), body: refusalBody(toRpcError(cause)), tone: "danger", source: "local" });
    } finally {
      setBusy(false);
    }
  };

  const save = () => (repo.enabled ? void submit() : setConfirming(true));

  return (
    <div className="space-y-6 pb-6" data-testid="root-form">
      <section className="space-y-3">
        <h3 className={H}>{t("settings.continuous.q.what")}</h3>
        <BlueprintPicker selected={draft.blueprint} onChange={(blueprint) => set({ blueprint })} controller={controller} live={live} />
        {descriptor !== undefined && <p className="text-xs opacity-70" data-testid="root-form-blueprints-from">{descriptor.blueprintsFrom}</p>}
        <FormField label={t("settings.continuous.form.briefPath")}>
          <Input variant="ghost" aria-label={t("settings.continuous.form.briefPath")} value={draft.briefPath} disabled={busy} onChange={(e) => set({ briefPath: e.target.value })} data-testid="root-form-briefpath" />
        </FormField>
        <FormField label={t("settings.continuous.form.tech")}>
          <Input variant="ghost" aria-label={t("settings.continuous.form.tech")} value={draft.tech} disabled={busy} onChange={(e) => set({ tech: e.target.value })} data-testid="root-form-tech" />
        </FormField>
        <FormField label={t("settings.continuous.form.minPriority")}>
          <Select variant="ghost" aria-label={t("settings.continuous.form.minPriority")} value={draft.minPriority} disabled={busy} onChange={(e) => set({ minPriority: e.target.value })} data-testid="root-form-minpriority">
            <option value="">—</option>
            {(descriptor?.priorityStates ?? []).map((state) => <option key={state} value={state}>{state}</option>)}
          </Select>
        </FormField>
      </section>

      <section className="space-y-3">
        <h3 className={H}>{t("settings.continuous.q.when")}</h3>
        <div className="grid grid-cols-2 gap-3">
          <FormField label={t("settings.continuous.form.windowStart")}>
            <Input variant="ghost" aria-label={t("settings.continuous.form.windowStart")} value={draft.start} placeholder="09:00" disabled={busy} onChange={(e) => set({ start: e.target.value })} data-testid="root-form-start" />
          </FormField>
          <FormField label={t("settings.continuous.form.windowEnd")}>
            <Input variant="ghost" aria-label={t("settings.continuous.form.windowEnd")} value={draft.end} placeholder="18:00" disabled={busy} onChange={(e) => set({ end: e.target.value })} data-testid="root-form-end" />
          </FormField>
          <FormField label={t("settings.continuous.form.windowDays")}>
            <Input variant="ghost" aria-label={t("settings.continuous.form.windowDays")} value={draft.days} placeholder="Mon-Fri" disabled={busy} onChange={(e) => set({ days: e.target.value })} data-testid="root-form-days" />
          </FormField>
          <FormField label={t("settings.continuous.form.windowZone")}>
            <Input variant="ghost" aria-label={t("settings.continuous.form.windowZone")} value={draft.zone} placeholder="UTC" disabled={busy} onChange={(e) => set({ zone: e.target.value })} data-testid="root-form-zone" />
          </FormField>
        </div>
        {descriptor !== undefined && <p className="text-xs opacity-70" data-testid="root-form-window-grammar">{descriptor.windowGrammar}</p>}
        <p className="text-xs opacity-60">{t("settings.continuous.form.windowEmpty")}</p>
      </section>

      <div className="flex justify-end gap-2">
        <Button size="sm" variant="ghost" onClick={onClose} data-testid="root-form-cancel">{t("settings.continuous.actions.cancel")}</Button>
        <Button size="sm" disabled={!canWrite || busy || draft.blueprint === ""} onClick={save} data-testid="root-form-submit">
          {repo.enabled ? t("settings.continuous.actions.save") : t("settings.continuous.actions.saveEnable")}
        </Button>
      </div>

      {/* In a portal on the body: the side panel is its own stacking context, and a modal drawn inside it
          would sit under (or be clipped by) the panel it was opened from. */}
      {createPortal(
        <AuthorityConfirm
          open={confirming}
          authorityStatement={statement}
          confirmLabel={t("settings.continuous.actions.saveEnable")}
          busy={busy}
          onConfirm={() => void submit()}
          onCancel={() => setConfirming(false)}
        />,
        document.body,
      )}
    </div>
  );
}
