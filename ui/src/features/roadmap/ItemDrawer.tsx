import { DetachedNote } from "../../components/DetachedNote";
import { useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, FormField, Input, Pill } from "../../components/kit";
import { BlueprintPicker } from "../settings/automation/ScheduleForm";
import { toRpcError } from "../../lib/bridge";
import type { LiveController, LiveState } from "../../lib/live";
import { ideateRoadmapItem, materializeRoadmapItem, planRoadmapItem, useRoadmapShow } from "./roadmapRpc";
import { filledDispatchStatement, type Vocabulary } from "./roadmapModel";
import { ItemEditForm } from "./ItemEditForm";
import { HistoryPanel } from "./HistoryPanel";

/**
 * ItemDrawer — one item opened: the daemon's describe sentence, an Edit tab
 * and a History tab, and the three dispatch verbs behind ONE authority
 * confirm each (S50 T7, RM6/RM9).
 *
 * The confirm quotes the daemon's `dispatchStatement` from the vocabulary
 * with its `<item>` slot filled by the daemon's OWN describe sentence for
 * this item — the statement's words and the item's words, none of them
 * ours. Declining sends NOTHING: no verb call, no draft, no partial write.
 * A dispatch that comes back refused is shown as the daemon wrote it.
 *
 * The workspace root is asked for here (not carried over from the Gantt's
 * filter) because a dispatch runs a loop in a REAL tree — the same
 * discipline the test-bench roadmap page models: name the tree or do not
 * run; an empty root is not a guess, it is a refusal to answer.
 */
export interface ItemDrawerProps {
  itemId: number;
  vocabulary: Vocabulary | undefined;
  controller: LiveController;
  live: LiveState;
  canWrite: boolean;
  onClose: () => void;
  onChanged: () => void;
}

type Verb = "ideate" | "plan" | "materialize";
type Tab = "edit" | "history";

/** Dispatch answers are small facts, and facts keep their own words: the
 *  fields the daemon returned are listed as they arrived, values verbatim,
 *  booleans kept as the daemon's true/false — never folded into prose.
 *  Exported for the proposals strip, which answers in the same shape. */
export function AnswerFacts({ answer }: { answer: unknown }) {
  if (typeof answer !== "object" || answer === null) return null;
  const entries = Object.entries(answer as Record<string, unknown>).filter(
    ([, v]) => typeof v === "string" || typeof v === "number" || typeof v === "boolean",
  );
  if (entries.length === 0) return null;
  return (
    <ul className="space-y-0.5 text-xs" data-testid="dispatch-answer">
      {entries.map(([key, value]) => (
        <li key={key} className="font-mono opacity-80">
          {key}: {String(value)}
        </li>
      ))}
    </ul>
  );
}

export function ItemDrawer({ itemId, vocabulary, controller, live, canWrite, onClose, onChanged }: ItemDrawerProps) {
  const { t } = useI18n();
  const show = useRoadmapShow(itemId, controller, live);
  const [tab, setTab] = useState<Tab>("edit");
  const [verb, setVerb] = useState<Verb | null>(null);
  const [workspaceRoot, setWorkspaceRoot] = useState("");
  const [materializeBlueprint, setMaterializeBlueprint] = useState("");
  const [materializeGoal, setMaterializeGoal] = useState("");
  const [busy, setBusy] = useState(false);
  const [refusal, setRefusal] = useState<string | undefined>(undefined);
  const [answer, setAnswer] = useState<unknown>(undefined);

  const item = show.data?.item;
  const describe = show.data?.describe;
  const statement = filledDispatchStatement(vocabulary?.dispatchStatement, describe);
  const rootReady = workspaceRoot.trim() !== "";
  const verbReady =
    rootReady && (verb !== "materialize" || (materializeBlueprint.trim() !== "" && materializeGoal.trim() !== ""));

  async function runVerb() {
    if (verb === null || !verbReady) return;
    setBusy(true);
    setRefusal(undefined);
    setAnswer(undefined);
    const root = workspaceRoot.trim();
    try {
      // One module holds the verbs (roadmapRpc); this branch runs only
      // from a confirm the person pressed — declining above it sent
      // nothing at all.
      const result =
        verb === "ideate"
          ? await ideateRoadmapItem(itemId, root)
          : verb === "plan"
            ? await planRoadmapItem(itemId, root)
            : await materializeRoadmapItem(itemId, materializeBlueprint.trim(), materializeGoal.trim(), root);
      setAnswer(result);
      onChanged();
    } catch (cause) {
      // The daemon named why, in its own sentence, which renders whole.
      setRefusal(toRpcError(cause).message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="fixed inset-0 z-40 flex justify-end bg-black/30" role="dialog" aria-modal="true" data-testid="item-drawer">
      <div className="h-full w-full max-w-xl overflow-y-auto space-y-3 bg-white p-4 shadow-xl dark:bg-slate-900">
        <div className="flex items-start justify-between gap-3">
          <div>
            <h3 className="text-sm font-semibold" data-testid="drawer-title">
              {item ? `${item.id} · ${item.title}` : `#${itemId}`}
            </h3>
            {/* The describe sentence, byte for byte from `roadmap.show`. */}
            <p className="text-xs opacity-80 pt-0.5" data-testid="drawer-describe">{describe ?? ""}</p>
            {item && (
              <div className="flex flex-wrap gap-1.5 pt-1.5">
                <Pill tone="slate" data-testid="drawer-status">{item.status}</Pill>
                <Pill tone="slate">{t("roadmap.manage.drawer.priority")}: {item.priorityState}</Pill>
                {item.loopSlug && <Pill tone="purple">{item.loopSlug}</Pill>}
                {<DetachedNote kind="roadmap" id={String(item.id)} className="basis-full" />}
              </div>
            )}
          </div>
          <Button size="sm" variant="ghost" onClick={onClose} data-testid="drawer-close">
            {t("roadmap.manage.drawer.close")}
          </Button>
        </div>

        <div className="flex gap-1 border-b border-slate-200 pb-1 dark:border-slate-700">
          {(["edit", "history"] as const).map((name) => (
            <Button
              key={name}
              size="sm"
              variant={tab === name ? "solid" : "ghost"}
              data-testid={`drawer-tab-${name}`}
              onClick={() => setTab(name)}
            >
              {t(`roadmap.manage.drawer.tab.${name}`)}
            </Button>
          ))}
        </div>

        {show.error !== undefined && <Alert intent="danger" title={show.error} data-testid="drawer-error" />}
        {show.data === undefined && show.error === undefined && (
          <p className="text-sm opacity-60">{show.loading ? t("roadmap.manage.drawer.loading") : ""}</p>
        )}

        {item && tab === "edit" && <ItemEditForm item={item} vocabulary={vocabulary} canWrite={canWrite} onSaved={() => onChanged()} />}
        {item && tab === "history" && <HistoryPanel itemId={item.id} controller={controller} live={live} />}

        <div className="space-y-2 pt-2" data-testid="drawer-dispatch">
          <p className="text-xs opacity-70">{t("roadmap.manage.drawer.dispatchNote")}</p>
          <div className="flex flex-wrap gap-2">
            <Button size="sm" variant="soft" disabled={!canWrite || busy} data-testid="dispatch-open-ideate" onClick={() => openConfirm("ideate")}>
              {t("roadmap.manage.drawer.ideate")}
            </Button>
            <Button size="sm" variant="soft" disabled={!canWrite || busy} data-testid="dispatch-open-plan" onClick={() => openConfirm("plan")}>
              {t("roadmap.manage.drawer.plan")}
            </Button>
            <Button size="sm" variant="soft" disabled={!canWrite || busy} data-testid="dispatch-open-materialize" onClick={() => openConfirm("materialize")}>
              {t("roadmap.manage.drawer.materialize")}
            </Button>
          </div>
          {refusal !== undefined && <Alert intent="danger" title={refusal} data-testid="dispatch-refusal" />}
          {answer !== undefined && <AnswerFacts answer={answer} />}
        </div>

        {verb !== null && (
          <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4" role="dialog" aria-modal="true" data-testid="dispatch-confirm">
            <div className="w-full max-w-lg space-y-3 rounded-xl bg-white p-5 shadow-xl dark:bg-slate-900">
              {/* The daemon's authority statement, slot filled by the
                  daemon's describe — the two sentences the plane owns,
                  quoted and nothing else. */}
              <Alert intent="warning" title={statement} data-testid="dispatch-statement" />
              {describe !== undefined && (
                <p className="text-xs opacity-80" data-testid="dispatch-item-line">{describe}</p>
              )}
              <FormField label={t("roadmap.manage.drawer.workspaceRoot")} hint={t("roadmap.manage.drawer.workspaceRootHint")}>
                <Input
                  value={workspaceRoot}
                  placeholder="/srv/repos/ledger"
                  aria-label={t("roadmap.manage.drawer.workspaceRoot")}
                  data-testid="dispatch-workspace-root"
                  onChange={(e) => setWorkspaceRoot(e.target.value)}
                />
              </FormField>
              {!rootReady && (
                <p className="text-xs opacity-70" data-testid="dispatch-root-missing">
                  {t("roadmap.manage.drawer.workspaceRootMissing")}
                </p>
              )}
              {verb === "materialize" && (
                <>
                  <FormField label={t("roadmap.manage.drawer.materializeBlueprint")}>
                    <BlueprintPicker selected={materializeBlueprint} onChange={setMaterializeBlueprint} controller={controller} live={live} />
                  </FormField>
                  <FormField label={t("roadmap.manage.drawer.materializeGoal")}>
                    <Input
                      value={materializeGoal}
                      aria-label={t("roadmap.manage.drawer.materializeGoal")}
                      data-testid="dispatch-goal"
                      onChange={(e) => setMaterializeGoal(e.target.value)}
                    />
                  </FormField>
                </>
              )}
              <div className="flex justify-end gap-2">
                <Button size="sm" variant="ghost" data-testid="dispatch-cancel" onClick={() => setVerb(null)}>
                  {t("roadmap.manage.drawer.cancel")}
                </Button>
                <Button size="sm" disabled={busy || !verbReady} data-testid={`dispatch-go-${verb}`} onClick={() => void runVerb()}>
                  {t(`roadmap.manage.drawer.go.${verb}`)}
                </Button>
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  );

  function openConfirm(next: Verb) {
    setRefusal(undefined);
    setAnswer(undefined);
    setVerb(next);
  }
}
