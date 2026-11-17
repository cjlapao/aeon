import { useEffect, useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, ContentActions, EmptyState, Select, Tabs } from "../../../components/kit";
import { PageTitle } from "../../../components/PageTitle";
import { OverlaySidePanel } from "../../../components/OverlaySidePanel";
import { useRpcAction } from "../../../lib/useRpcAction";
import { notifications } from "../../../lib/notifications";
import type { LiveController, LiveState } from "../../../lib/live";
import type { RpcMethods } from "../../../proto/contract";
import { GateList } from "./GateList";
import { GateForm, type GateUpdatePatch } from "./GateForm";
import { TemplatePicker } from "./TemplatePicker";
import { MergeRuleEditor } from "./MergeRuleEditor";
import { RunsList } from "./RunsList";
import { RunResults } from "./RunDetail";
import {
  useComponentList,
  useGateList,
  useVerifyVocabulary,
  writeApplyTemplates,
  writeVerifyAdd,
  writeVerifyArchive,
  writeVerifyRestore,
  writeVerifyTry,
  writeVerifyUpdate,
} from "./gatesRpc";
import type { AddGatePayload, GateView } from "./gatesModel";
import { copyPayload, inheritedGates } from "./gatesModel";

/**
 * The Gates panel of the real Settings screen (S57 T11, VG22): a person
 * sees, authors and tries a component's gates, sets and previews its merge
 * rule, and reads what ran — on the same screen the policies, the MCP
 * servers and the tools already live on.
 *
 * Every list here is a list-with-details over daemon reads; every form
 * field and allowed value is generated from `verify.vocabulary` or
 * `mergerule.vocabulary` — the panel types no requirement, state, reason,
 * review mode, bound or sentence of its own (the grep guard in
 * `gatesRpc.guard.test.ts` fails the build on one). Writes update from the
 * RESPONSE, never optimistically, and refusals render verbatim with the
 * draft kept. `Try` asks first with the daemon's own statement
 * (`verify.vocabulary.tryStatement`) and declining sends nothing at all.
 */
export interface GatesSectionProps {
  controller: LiveController;
  live: LiveState;
  /** @default `live.state === "current"` — writes are refused while not current. */
  canWrite?: boolean;
  /** Fixed component (the Work page's scope): the picker goes away and the gates are this component's. */
  componentId?: string;
}

type TabId = "gates" | "merge" | "runs";
type TryResult = RpcMethods["verify.try"]["result"];
type FormState = { mode: "add" } | { mode: "edit"; gate: GateView } | undefined;

export function GatesSection({ controller, live, canWrite = live.state === "current", componentId }: GatesSectionProps) {
  const { t } = useI18n();
  const components = useComponentList(controller, live);
  const [picked, setPicked] = useState<string>(componentId ?? "");
  const [tab, setTab] = useState<TabId>("gates");
  const [form, setForm] = useState<FormState>(undefined);
  const [templatesOpen, setTemplatesOpen] = useState(false);

  const rows = components.data?.components ?? [];
  // Open on the first component so the page shows gates, not an empty prompt.
  useEffect(() => {
    if (componentId === undefined && picked === "" && rows.length > 0) setPicked(rows[0]?.id ?? "");
  }, [picked, rows]);

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto" data-testid="gates-section">
      <ContentActions
        className="shrink-0"
        gap="md"
        align="end"
        data-testid="gates-header"
        actions={
          tab === "gates" && picked !== "" ? (
            <>
              <Button size="sm" variant="ghost" disabled={!canWrite} onClick={() => setTemplatesOpen(true)} data-testid="gates-templates-open">{t("settings.gates.actions.addFromTemplate")}</Button>
              <Button size="sm" disabled={!canWrite} onClick={() => setForm({ mode: "add" })} data-testid="gates-add">{t("settings.gates.actions.add")}</Button>
            </>
          ) : undefined
        }
      >
        <PageTitle title={t("settings.gates.title")} purpose={t("settings.gates.subtitle")} help={{ title: t("settings.gates.help.title"), content: t("settings.gates.help.body") }} />
      </ContentActions>

      {!canWrite && <Alert className="shrink-0" intent="warning" title={t("settings.gates.reconnecting")} data-testid="gates-reconnecting" />}

      {componentId === undefined && <div className="flex shrink-0 flex-wrap items-center gap-3" data-testid="gates-picker-label">
        <span className="text-xs font-semibold uppercase tracking-widest opacity-70">{t("settings.gates.pick.label")}</span>
        <div className="w-64 shrink-0">
          <Select variant="ghost" size="md" aria-label={t("settings.gates.pick.component")} value={picked} onChange={(event) => setPicked(event.target.value)} data-testid="gates-picker">
            <option value="">{t("settings.gates.pick.none")}</option>
            {rows.map((component) => <option key={component.id} value={component.id}>{component.name}</option>)}
          </Select>
        </div>
      </div>}

      {components.error && <Alert className="shrink-0" intent="danger" title={t("settings.gates.components.failed")} description={components.error} data-testid="gates-components-failed" />}

      {picked === "" ? (
        <EmptyState showIcon={false} title={t("settings.gates.pick.emptyTitle")} subtitle={t("settings.gates.pick.emptyBody")} data-testid="gates-pick-empty" />
      ) : (
        <>
          <div className="shrink-0">
            <Tabs
              variant="underline"
              items={[
                { id: "gates", label: t("settings.gates.tabs.gates") },
                { id: "merge", label: t("settings.gates.tabs.merge") },
                { id: "runs", label: t("settings.gates.tabs.runs") },
              ]}
              value={tab}
              onChange={(id) => setTab(id as TabId)}
            />
          </div>
          <p className="shrink-0 text-sm opacity-70">{t(`settings.gates.hint.${tab}`)}</p>
          {tab === "gates" && (
            <GatesTab
              componentId={picked}
              controller={controller}
              live={live}
              canWrite={canWrite}
              form={form}
              setForm={setForm}
              templatesOpen={templatesOpen}
              setTemplatesOpen={setTemplatesOpen}
            />
          )}
          {tab === "merge" && <MergeRuleEditor componentId={picked} controller={controller} live={live} canWrite={canWrite} />}
          {tab === "runs" && <RunsList componentId={picked} controller={controller} live={live} />}
        </>
      )}
    </div>
  );
}

/**
 * The Gates tab: the list-with-details, the add/edit form, the template
 * picker and the try flow, all over the component's EFFECTIVE set (the
 * daemon's `effective: true` read — own gates plus the root's, each with
 * its source). Mounted only while its tab is open: the doors open when the
 * person looks at them.
 */
function GatesTab({
  componentId,
  controller,
  live,
  canWrite,
  form,
  setForm,
  templatesOpen,
  setTemplatesOpen,
}: {
  componentId: string;
  controller: LiveController;
  live: LiveState;
  canWrite: boolean;
  form: FormState;
  setForm: (next: FormState) => void;
  templatesOpen: boolean;
  setTemplatesOpen: (open: boolean) => void;
}) {
  const { t } = useI18n();
  const vocab = useVerifyVocabulary(controller, live);
  const action = useRpcAction();

  const [showArchived, setShowArchived] = useState(false);
  const [requirement, setRequirement] = useState("");
  const list = useGateList({ componentId, effective: true, includeArchived: showArchived, requirement }, controller, live);

  const [tryResult, setTryResult] = useState<TryResult | undefined>(undefined);
  const [copied, setCopied] = useState<{ copied: number; refused: string[] } | undefined>(undefined);

  const gates = list.data?.gates ?? [];
  const inherited = inheritedGates(gates);
  const requirements = vocab.data?.requirements ?? [];
  const states = vocab.data?.states ?? [];

  const refresh = () => list.refetch();

  const note = (text: string | undefined) => {
    // The scrubbed-environment note the daemon attaches when the written
    // command carries a token shape — its sentence, pushed as-is.
    if (text) notifications.push({ title: t("settings.gates.form.noteTitle"), body: text, tone: "warning", source: "local" });
  };

  const createGate = async (payload: AddGatePayload) => {
    const result = await action.run(() => writeVerifyAdd(payload), { errorTitle: t("settings.gates.form.addFailed") });
    if (result) {
      refresh(); // the list row comes from the daemon's answer, never from the draft
      setForm(undefined);
      note(result.note);
    }
  };

  const updateGate = async (gate: GateView, patch: GateUpdatePatch) => {
    const result = await action.run(() => writeVerifyUpdate({ id: gate.id, ...patch }), {
      errorTitle: t("settings.gates.form.updateFailed"),
    });
    if (result) {
      refresh();
      setForm(undefined);
      note(result.note);
    }
  };

  const archive = async (gate: GateView) => {
    const result = await action.run(() => writeVerifyArchive(gate.id), { errorTitle: t("settings.gates.row.archiveFailed") });
    if (result) refresh();
  };

  const restore = async (gate: GateView) => {
    const result = await action.run(() => writeVerifyRestore(gate.id), { errorTitle: t("settings.gates.row.restoreFailed") });
    if (result) refresh();
  };

  // The person confirmed in the row's dialog (which shows the daemon's
  // tryStatement); declining never reaches here and sends nothing.
  const runTry = async (gate: GateView) => {
    setTryResult(undefined);
    const result = await action.run(() => writeVerifyTry(componentId, gate.id), { errorTitle: t("settings.gates.try.failed") });
    if (result) setTryResult(result);
  };

  const applyTemplates = async (templateIds: string[]) => {
    const result = await action.run(() => writeApplyTemplates(componentId, templateIds), {
      errorTitle: t("settings.gates.templates.applyFailed"),
    });
    if (result) {
      refresh();
      setTemplatesOpen(false);
      notifications.push({ title: t("settings.gates.templates.applied", { count: result.gates.length }), tone: "success", source: "local" });
    }
    // A refusal (a name collision, all-or-nothing): action.error renders
    // verbatim below and NOTHING was applied — the list stays as the
    // daemon last answered it.
  };

  // One `verify.add` per inherited gate — the spec's no-new-method copy.
  // Partial failure is LISTED, never smoothed: the sentence names how many
  // arrived and the names below say which did not.
  const copyFromRoot = async () => {
    const refused: string[] = [];
    let count = 0;
    for (const gate of inherited) {
      try {
        await writeVerifyAdd(copyPayload(componentId, gate));
        count += 1;
      } catch {
        refused.push(gate.name);
      }
    }
    setCopied({ copied: count, refused });
    refresh();
  };

  return (
    <div className="flex min-h-[26rem] flex-1 flex-col gap-3">
      {copied && (
        <Alert
          className="shrink-0"
          intent={copied.refused.length > 0 ? "warning" : "success"}
          title={t("settings.gates.copy.summary", { copied: copied.copied, failed: copied.refused.length })}
          description={copied.refused.length > 0 ? copied.refused.join(", ") : undefined}
          data-testid="gates-copy-summary"
        />
      )}

      <GateList
        gates={gates}
        loading={list.loading}
        error={list.error}
        requirements={requirements}
        states={states}
        tryStatement={vocab.data?.tryStatement ?? ""}
        canWrite={canWrite}
        showArchived={showArchived}
        requirement={requirement}
        onShowArchived={setShowArchived}
        onRequirement={setRequirement}
        onAdd={() => setForm({ mode: "add" })}
        onEdit={(gate) => setForm({ mode: "edit", gate })}
        onArchive={(gate) => void archive(gate)}
        onRestore={(gate) => void restore(gate)}
        onTry={(gate) => void runTry(gate)}
        onCopyFromRoot={inherited.length > 0 && canWrite ? () => void copyFromRoot() : undefined}
        onRetry={refresh}
      />

      {tryResult && (
        <OverlaySidePanel title={t("settings.gates.actions.try")} titleClassName="text-xl font-semibold" variant="simple" width={640} onClose={() => setTryResult(undefined)}>
          <div className="space-y-4" data-testid="gates-try-result">
            <p className="text-sm font-medium" data-testid="gates-try-statement">{t("settings.gates.try.ranIn", { path: tryResult.ranIn })}</p>
            <p className="text-xs opacity-70" data-testid="gates-try-loops">
              {tryResult.activeLoops.length > 0 ? t("settings.gates.try.activeLoops", { slugs: tryResult.activeLoops.join(", ") }) : t("settings.gates.try.rootQuiet")}
            </p>
            <RunResults results={tryResult.results} states={states} />
          </div>
        </OverlaySidePanel>
      )}

      {form && (
        <OverlaySidePanel
          title={t(form.mode === "add" ? "settings.gates.form.addTitle" : "settings.gates.form.editTitle", { name: form.mode === "edit" ? form.gate.name : "" })}
          titleClassName="text-xl font-semibold"
          variant="simple"
          width={520}
          onClose={() => setForm(undefined)}
        >
          <GateForm
            mode={form.mode}
            gate={form.mode === "edit" ? form.gate : undefined}
            componentId={componentId}
            requirements={requirements}
            busy={action.busy}
            canWrite={canWrite}
            onSubmitAdd={(payload: RpcMethods["verify.add"]["params"]) => createGate(payload)}
            onSubmitUpdate={form.mode === "edit" ? (patch) => updateGate(form.gate, patch) : undefined}
            onClose={() => setForm(undefined)}
          />
        </OverlaySidePanel>
      )}

      {templatesOpen && (
        <OverlaySidePanel title={t("settings.gates.templates.title")} titleClassName="text-xl font-semibold" variant="simple" width={560} onClose={() => setTemplatesOpen(false)}>
          <TemplatePicker controller={controller} live={live} busy={action.busy} canWrite={canWrite} onApply={(ids) => void applyTemplates(ids)} onClose={() => setTemplatesOpen(false)} />
        </OverlaySidePanel>
      )}
    </div>
  );
}
