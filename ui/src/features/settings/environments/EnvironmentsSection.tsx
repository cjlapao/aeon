import { useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, ContentActions, Tabs } from "../../../components/kit";
import { PageTitle } from "../../../components/PageTitle";
import type { LiveController, LiveState } from "../../../lib/live";
import { useEnvironmentVocabulary } from "./environmentsRpc";
import { EnvironmentsTab, type EnvironmentFormState } from "./EnvironmentsTab";
import { SecretRefsTab } from "./SecretRefsTab";
import { ProvisionsTab } from "./ProvisionsTab";
import { EffectiveTab } from "./EffectiveTab";
import type { Vocabulary } from "./environmentsModel";

/**
 * The Environments section of the real Settings screen (S59 T11, EV1/EV6/EV15/EV18):
 * a person sees what every loop's commands run in, what that environment can
 * and cannot enforce, where it is bound, what a command on a given component
 * would see, and who changed it — and changes a binding with the daemon's own
 * confirmation statement.
 *
 * The panel is built from the daemon's descriptors: the vocabulary read below
 * is the panel's entire field vocabulary (backends, the default allowlist, the
 * deny shapes, every mode set, both honesty lines and the `knownLimits` the
 * daemon says are true today), it is read once for the section, and no tab
 * re-reads it. Nothing here holds a status string, a backend name, a deny word
 * or an egress mode — the guards in `test/environmentsRpc.guard.test.ts` fail
 * the build on one. Writes are refused while the connection is not current and
 * reads keep showing what the daemon last answered, which is the neighbouring
 * sections' idiom.
 */
export interface EnvironmentsSectionProps {
  controller: LiveController;
  live: LiveState;
  /** @default `live.state === "current"` — writes are refused while not current. */
  canWrite?: boolean;
  /** Open on the Effective tab for this component (the Work page's scope). */
  componentId?: string;
}

type TabId = "environments" | "secretrefs" | "provisions" | "effective";

export function EnvironmentsSection({ controller, live, canWrite = live.state === "current", componentId }: EnvironmentsSectionProps) {
  const { t } = useI18n();
  const [tab, setTab] = useState<TabId>(componentId ? "effective" : "environments");
  const [form, setForm] = useState<EnvironmentFormState>(undefined);
  const [creatingRef, setCreatingRef] = useState(false);
  const vocab = useEnvironmentVocabulary(controller, live);
  const vocabulary: Vocabulary | undefined = vocab.data;

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto" data-testid="environments-section">
      <ContentActions
        className="shrink-0"
        gap="md"
        align="end"
        data-testid="environments-header"
        actions={
          tab === "environments" ? (
            <Button size="sm" disabled={!canWrite} onClick={() => setForm({ mode: "add" })} data-testid="environments-create">{t("settings.environments.actions.create")}</Button>
          ) : tab === "secretrefs" ? (
            <Button size="sm" disabled={!canWrite} onClick={() => setCreatingRef(true)} data-testid="environments-secret-create">{t("settings.environments.actions.newRef")}</Button>
          ) : undefined
        }
      >
        <PageTitle title={t("settings.environments.title")} purpose={t("settings.environments.subtitle")} help={{ title: t("settings.environments.help.title"), content: t("settings.environments.help.body") }} />
      </ContentActions>

      {!canWrite && <Alert className="shrink-0" intent="warning" title={t("settings.environments.reconnecting")} data-testid="environments-reconnecting" />}

      {vocab.error && (
        <Alert className="shrink-0" intent="danger" title={t("settings.environments.vocabulary.failed")} description={vocab.error} data-testid="environments-vocabulary-failed">
          <Button size="sm" onClick={vocab.refetch} data-testid="environments-vocabulary-retry">{t("settings.environments.actions.retry")}</Button>
        </Alert>
      )}

      <div className="shrink-0">
        <Tabs
          variant="underline"
          items={[
            { id: "environments", label: t("settings.environments.tabs.environments") },
            { id: "secretrefs", label: t("settings.environments.tabs.secretrefs") },
            { id: "provisions", label: t("settings.environments.tabs.provisions") },
            { id: "effective", label: t("settings.environments.tabs.effective") },
          ]}
          value={tab}
          onChange={(id) => setTab(id as TabId)}
        />
      </div>
      <p className="shrink-0 text-sm opacity-70">{t(`settings.environments.hint.${tab}`)}</p>

      {tab === "environments" && <EnvironmentsTab controller={controller} live={live} canWrite={canWrite} vocabulary={vocabulary} form={form} setForm={setForm} />}
      {tab === "secretrefs" && <SecretRefsTab controller={controller} live={live} canWrite={canWrite} vocabulary={vocabulary} creating={creatingRef} setCreating={setCreatingRef} />}
      {tab === "provisions" && <ProvisionsTab controller={controller} live={live} canWrite={canWrite} />}
      {tab === "effective" && <EffectiveTab controller={controller} live={live} vocabulary={vocabulary} initialComponentId={componentId} />}
    </div>
  );
}
