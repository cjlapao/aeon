import { useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Button, FormField, Input, Select, Toggle } from "../../../components/kit";
import type { RpcMethods } from "../../../proto/contract";
import type { GateView } from "./gatesModel";

/**
 * The add/edit form for one gate, as the content of a side panel. Every choice the form offers
 * comes from `verify.vocabulary` — the requirement select lists exactly the daemon's words; none
 * is typed into this file. The timeout is a plain number field because the wire carries no bounds
 * to generate from: the store owns the closed range, and its refusal renders with the DRAFT kept.
 * Memory is DECLARED, not enforced — the field says so; the value stored is the daemon's. On edit
 * the memory field is absent: `verify.update` touches only the members it is sent.
 */
export interface GateFormProps {
  mode: "add" | "edit";
  gate?: GateView;
  componentId: string;
  /** `verify.vocabulary.requirements`, in the daemon's order. */
  requirements: readonly string[];
  busy: boolean;
  canWrite: boolean;
  onSubmitAdd: (payload: RpcMethods["verify.add"]["params"]) => void;
  onSubmitUpdate?: (patch: GateUpdatePatch) => void;
  onClose: () => void;
}

export type GateUpdatePatch = Omit<RpcMethods["verify.update"]["params"], "id">;

const H = "text-xs font-semibold uppercase tracking-widest opacity-70";

export function GateForm({ mode, gate, componentId, requirements, busy, canWrite, onSubmitAdd, onSubmitUpdate, onClose }: GateFormProps) {
  const { t } = useI18n();
  const [name, setName] = useState(gate?.name ?? "");
  const [command, setCommand] = useState(gate?.command ?? "");
  const [requirement, setRequirement] = useState(gate?.requirement ?? requirements[0] ?? "");
  const [timeoutS, setTimeoutS] = useState(String(gate?.timeoutS ?? 600));
  const [memoryMb, setMemoryMb] = useState(gate?.memoryMb != null ? String(gate.memoryMb) : "");
  const [enabled, setEnabled] = useState(gate?.enabled ?? true);

  const disabled = busy || !canWrite;

  const submit = () => {
    const seconds = Number(timeoutS);
    if (mode === "add") {
      const payload: RpcMethods["verify.add"]["params"] = { componentId, name, command, requirement, timeoutS: seconds, enabled };
      if (memoryMb.trim() !== "") {
        const mb = Number(memoryMb);
        if (Number.isFinite(mb)) payload.memoryMb = mb;
      }
      onSubmitAdd(payload);
      return;
    }
    onSubmitUpdate?.({ command, requirement, timeoutS: seconds, enabled });
  };

  return (
    <div className="space-y-8" data-testid="gates-form">
      <section className="space-y-3">
        <h3 className={H}>{t("settings.gates.q.what")}</h3>
        <FormField label={t("settings.gates.form.name")}>
          <Input variant="ghost" aria-label={t("settings.gates.form.name")} value={name} disabled={disabled || mode === "edit"} onChange={(event) => setName(event.target.value)} data-testid="gates-form-name" />
        </FormField>
        <FormField label={t("settings.gates.form.command")}>
          <Input variant="ghost" className="font-mono" aria-label={t("settings.gates.form.command")} value={command} disabled={disabled} onChange={(event) => setCommand(event.target.value)} data-testid="gates-form-command" />
        </FormField>
      </section>

      <section className="space-y-3">
        <h3 className={H}>{t("settings.gates.q.must")}</h3>
        <FormField label={t("settings.gates.form.requirement")}>
          <Select variant="ghost" aria-label={t("settings.gates.form.requirement")} value={requirement} disabled={disabled} onChange={(event) => setRequirement(event.target.value)} data-testid="gates-form-requirement">
            {requirements.map((word) => <option key={word} value={word}>{t(`settings.gates.word.${word}`)}</option>)}
          </Select>
        </FormField>
        <div data-testid="gates-form-enabled">
          <Toggle size="sm" aria-label={t("settings.gates.form.enabled")} label={t("settings.gates.form.enabled")} checked={enabled} disabled={disabled} onChange={(event) => setEnabled(event.target.checked)} />
        </div>
      </section>

      <section className="space-y-3">
        <h3 className={H}>{t("settings.gates.q.limits")}</h3>
        <FormField label={t("settings.gates.form.timeout")}>
          <Input variant="ghost" aria-label={t("settings.gates.form.timeout")} inputMode="numeric" value={timeoutS} disabled={disabled} onChange={(event) => setTimeoutS(event.target.value)} data-testid="gates-form-timeout" />
        </FormField>
        {mode === "add" && (
          <FormField label={t("settings.gates.form.memory")}>
            <Input variant="ghost" aria-label={t("settings.gates.form.memory")} inputMode="numeric" value={memoryMb} disabled={disabled} onChange={(event) => setMemoryMb(event.target.value)} data-testid="gates-form-memory" />
            <span className="text-xs opacity-70" data-testid="gates-form-memory-hint">{t("settings.gates.form.memoryHint")}</span>
          </FormField>
        )}
      </section>

      <div className="flex gap-2">
        <Button size="sm" onClick={submit} disabled={disabled || name.trim() === "" || command.trim() === ""} data-testid="gates-form-save">{t("settings.gates.form.save")}</Button>
        <Button size="sm" variant="ghost" onClick={onClose} disabled={busy} data-testid="gates-form-cancel">{t("settings.gates.form.cancel")}</Button>
      </div>
    </div>
  );
}
