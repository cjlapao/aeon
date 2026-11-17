import { useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Tabs } from "../../components/kit";
import type { LiveController, LiveState } from "../../lib/live";
import { EnvironmentsSection } from "../settings/environments/EnvironmentsSection";
import { GatesSection } from "../settings/gates/GatesPanel";
import { TriageSection } from "../settings/triage/TriageSection";
import type { WorkComponent } from "./workModel";

type View = "gates" | "environments" | "triage";

/**
 * Guardrails — what keeps a component's work honest: its verification gates, the environment its
 * steps run in, and the bugs waiting for validation. One component at a time: these settings belong
 * to a component, so "all projects" asks for a pick rather than showing one component's rules as if
 * they were everyone's.
 */
export function WorkGuardrailsTab({ controller, live, canWrite, component }: { controller: LiveController; live: LiveState; canWrite: boolean; component: WorkComponent | undefined }) {
  const { t } = useI18n();
  const [view, setView] = useState<View>("gates");
  if (!component) {
    return (
      <div className="flex flex-col gap-3" data-testid="work-guardrails-needs-one">
        <p className="text-sm opacity-70">{t("work.guardrails.needsOne")}</p>
      </div>
    );
  }
  return (
    <div className="flex flex-col gap-3" data-testid="work-guardrails">
      <div className="shrink-0">
        <Tabs variant="pill" items={[{ id: "gates", label: t("work.guardrails.gates") }, { id: "environments", label: t("work.guardrails.environments") }, { id: "triage", label: t("work.guardrails.triage") }]} value={view} onChange={(id) => setView(id as View)} />
      </div>
      {view === "gates" && <GatesSection key={component.id} controller={controller} live={live} canWrite={canWrite} componentId={component.id} />}
      {view === "environments" && <EnvironmentsSection key={component.id} controller={controller} live={live} canWrite={canWrite} componentId={component.id} />}
      {view === "triage" && <TriageSection key={component.id} controller={controller} live={live} canWrite={canWrite} componentRoot={component.root} />}
    </div>
  );
}
