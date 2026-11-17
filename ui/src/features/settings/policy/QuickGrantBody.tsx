import { useEffect } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Button, MultiToggle } from "../../../components/kit";
import { AgentTagPicker } from "../../../components/AgentTagPicker";
import type { LiveController, LiveState } from "../../../lib/live";
import { ToolAddressPicker } from "./pickers/ToolAddressPicker";
import { stateFieldOf, toolPickerInfo, type PolicyDraft, type Vocabulary } from "./policyModel";

// The guided grant (S74): three questions in the order a person asks them —
// who, what, and what happens. Everything else a policy needs is derived:
// the type and the spec field come from the daemon's own descriptors, the
// spec's `state` follows the chosen action, and the id follows its suggestion.
// The full form is one click away ("Advanced options") for the targets and
// types this does not cover (a loop, a lane type, a command, an expiry).
//
// The agent is a real roster value (the shared picker refuses free text), and
// the tool is the address picker with its live match preview, so "give the
// brainstormer every system tool" is: pick the agent, pick "All system tools",
// leave Allow, and submit.

export interface QuickGrantBodyProps {
  draft: PolicyDraft;
  set: (patch: Partial<PolicyDraft>) => void;
  vocabulary: Vocabulary;
  controller: LiveController;
  live: LiveState;
  onAdvanced: () => void;
}

/** The target kind a single agent is: the kind the agent picker's value names. */
const AGENT_KIND = "agent";

export function QuickGrantBody({ draft, set, vocabulary, controller, live, onAdvanced }: QuickGrantBodyProps) {
  const { t } = useI18n();
  const info = toolPickerInfo(vocabulary);
  const stateField = stateFieldOf(vocabulary, info?.type ?? "");

  // Derive what the person is not asked about, once the vocabulary is here.
  useEffect(() => {
    if (!info) return;
    const patch: Partial<PolicyDraft> = {};
    if (draft.type !== info.type) patch.type = info.type;
    if (draft.targetKind !== AGENT_KIND && vocabulary.targetKinds.includes(AGENT_KIND)) patch.targetKind = AGENT_KIND;
    if (draft.action === "" && vocabulary.actions[0]) {
      patch.action = vocabulary.actions[0];
    }
    if (Object.keys(patch).length > 0) set(patch);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [info?.type, vocabulary]);

  // The spec's state repeats the action; keep the two equal.
  useEffect(() => {
    if (stateField && draft.action !== "" && draft.spec[stateField] !== draft.action) {
      set({ spec: { ...draft.spec, [stateField]: draft.action } });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [draft.action, stateField]);

  if (!info) {
    return <p className="text-sm opacity-70" data-testid="quick-unavailable">{t("settings.policy.quick.unavailable")}</p>;
  }
  const tool = draft.spec[info.field] ?? "";

  return (
    <div className="space-y-8" data-testid="quick-grant">
      <section className="space-y-3">
        <h3 className="text-xs font-semibold uppercase tracking-widest opacity-70">{t("settings.policy.quick.who")}</h3>
        <p className="text-sm opacity-80">{t("settings.policy.quick.whoHelp")}</p>
        <AgentTagPicker value={draft.targetId} onChange={(targetId) => set({ targetId })} controller={controller} live={live} />
      </section>

      <section className="space-y-3">
        <h3 className="text-xs font-semibold uppercase tracking-widest opacity-70">{t("settings.policy.quick.what")}</h3>
        <p className="text-sm opacity-80">{t("settings.policy.quick.whatHelp")}</p>
        <div className="flex flex-wrap gap-2" data-testid="quick-presets">
          {[
            { value: "*.*.*", label: t("settings.policy.quick.presetSystem") },
            { value: "*.*.read", label: t("settings.policy.quick.presetRead") },
            { value: "*.*.write", label: t("settings.policy.quick.presetWrite") },
          ].map((preset) => (
            <Button
              key={preset.value}
              size="sm"
              variant={tool === preset.value ? "solid" : "outline"}
              onClick={() => set({ spec: { ...draft.spec, [info.field]: preset.value } })}
              data-testid={`quick-preset-${preset.value}`}
            >
              {preset.label}
            </Button>
          ))}
        </div>
        <ToolAddressPicker
          value={tool}
          onChange={(next) => set({ spec: { ...draft.spec, [info.field]: next } })}
          controller={controller}
          live={live}
        />
      </section>

      <section className="space-y-3">
        <h3 className="text-xs font-semibold uppercase tracking-widest opacity-70">{t("settings.policy.quick.decision")}</h3>
        <p className="text-sm opacity-80">{t("settings.policy.quick.decisionHelp")}</p>
        <MultiToggle
          aria-label={t("settings.policy.quick.decision")}
          options={vocabulary.actions.map((a) => ({ value: a, label: a }))}
          value={draft.action}
          onChange={(action) => set({ action })}
          size="sm"
        />
        {draft.targetId !== "" && tool !== "" && draft.action !== "" ? (
          <p className="text-sm" data-testid="quick-sentence">
            {t("settings.policy.quick.sentence", { action: draft.action, agent: draft.targetId, tool })}
          </p>
        ) : null}
      </section>

      <Button size="sm" variant="ghost" onClick={onAdvanced} data-testid="quick-advanced">
        {t("settings.policy.quick.advanced")}
      </Button>
    </div>
  );
}
