import { useI18n } from "@cjlapao/ui-kit";
import type { LiveController, LiveState } from "../../lib/live";
import type { RpcMethods } from "../../proto/contract";
import { AgentCapabilitiesView, AgentFieldsForm, AgentProvenance } from "./AgentFields";
import { AgentIdentityFields } from "./AgentIdentityFields";
import { HandoffBuilder } from "./HandoffBuilder";
import { TechPicker } from "./TechPicker";
import { ToolsPicker } from "./ToolsPicker";
import {
  agentFieldsFor,
  agentFormValues,
  agentMinion,
  agentTech,
  agentToolList,
  toolListJson,
  withAgentMinion,
  withAgentTech,
  type AgentDraft,
  type AgentStringField,
} from "./agentsModel";

// The two tabs of an agent's edit page that are forms, drawn the way the new-agent panel is: who it
// is, what it works with, what it may use — with the minion, the tag picker and the tools picker —
// and, apart, the details most agents never need. The prompt has its own tab (the markdown editor),
// and nothing here writes anything: every change goes into the caller's draft, and the caller's
// Save writes the whole row (AR5).

/** The details most agents never need: the free-text "what it does", the skills a card advertises, the raw metadata. */
const ADVANCED_KEYS: readonly AgentStringField[] = ["functionality", "skills", "metadata"];

type Registry = RpcMethods["handlers.list"]["result"] | undefined;

interface EditTabProps {
  values: AgentDraft;
  /** Put one change into the draft. */
  onField: (key: AgentStringField, value: string) => void;
  disabled: boolean;
}

export function AgentEditOverview({
  values, onField, disabled, registry, controller, live, slug,
}: EditTabProps & { registry: Registry; controller: LiveController; live: LiveState; slug: string }) {
  const { t } = useI18n();
  return (
    <div className="space-y-8" data-testid="agent-edit-overview">
      <section className="space-y-4" aria-label={t("agents.section.who")}>
        <h3 className="text-xs font-semibold uppercase tracking-widest opacity-70">{t("agents.section.who")}</h3>
        <AgentIdentityFields
          idPrefix="agent-edit"
          name={values.name} onName={(next) => onField("name", next)}
          description={values.description} onDescription={(next) => onField("description", next)}
          colour={values.avatarColor} onColour={(next) => onField("avatarColor", next)}
          minion={agentMinion(values.metadata)} onMinion={(next) => onField("metadata", withAgentMinion(values.metadata, next))}
          // AM2: the one rule a name-change tempts you to forget, kept beside the name field.
          nameHint={t("agents.readonly.immutable", { slug })}
          disabled={disabled}
        />
      </section>

      <section className="space-y-2" aria-label={t("agents.section.works")}>
        <h3 className="text-xs font-semibold uppercase tracking-widest opacity-70">{t("agents.section.works")}</h3>
        <p className="text-sm opacity-80">{t("agents.create.worksHelp")}</p>
        <TechPicker tags={registry?.techTags ?? []} value={agentTech(values.metadata)} onChange={(tags) => onField("metadata", withAgentTech(values.metadata, tags))} disabled={disabled} loading={registry === undefined} />
      </section>

      <section className="space-y-2" aria-label={t("agents.section.uses")} data-testid="agent-field-tools">
        <h3 className="text-xs font-semibold uppercase tracking-widest opacity-70">{t("agents.section.uses")}</h3>
        <p className="text-sm opacity-80">{t("agents.field.toolsHint")}</p>
        {registry !== undefined && (
          <ToolsPicker
            value={agentToolList(values.tools)}
            onChange={(next) => onField("tools", toolListJson(next))}
            kits={registry.toolKits ?? []}
            controller={controller}
            live={live}
            disabled={disabled}
            asksEverything={values.origin === "import" && !values.signed}
          />
        )}
      </section>

      <section className="space-y-2" aria-label={t("agents.section.reports")}>
        <h3 className="text-xs font-semibold uppercase tracking-widest opacity-70">{t("agents.section.reports")}</h3>
        <p className="text-sm opacity-80">{t("agents.section.reportsHelp")}</p>
        <HandoffBuilder value={values.handoff} onChange={(json) => onField("handoff", json)} disabled={disabled} />
      </section>
    </div>
  );
}

export function AgentEditAdvanced({ values, onField, disabled, slug }: EditTabProps & { slug: string }) {
  const { t } = useI18n();
  return (
    <div className="space-y-6" data-testid="agent-edit-advanced">
      <p className="text-sm opacity-80">{t("agents.advanced.help")}</p>
      <AgentFieldsForm
        ghost
        fields={agentFieldsFor(ADVANCED_KEYS)}
        values={agentFormValues(values, ADVANCED_KEYS)}
        disabled={disabled}
        slug={slug}
        onChange={onField}
      />
      <AgentCapabilitiesView agent={values} skillsHidden toolsHidden handoffHidden />
      <AgentProvenance agent={values} />
    </div>
  );
}
