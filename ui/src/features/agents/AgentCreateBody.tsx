import { useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Button, FormField, Textarea } from "../../components/kit";
import type { LiveController, LiveState } from "../../lib/live";
import { useAppToast } from "../../lib/useAppToast";
import { useRpcQuery } from "../../lib/useRpcQuery";
import { agentCreateParams, handoffMap, handoffRows } from "./agentsModel";
import { AgentIdentityFields } from "./AgentIdentityFields";
import { HandoffBuilder } from "./HandoffBuilder";
import { TechPicker } from "./TechPicker";
import { ToolsPicker } from "./ToolsPicker";
import { usePanelWrites } from "./usePanelWrites";

// The new-agent panel, in the order a person thinks about an agent: who it is, what it should do,
// what it works with, what it may use. Everything the daemon decides — the slug, the version, the
// starting mood (always idle: the chat sets it from what the agent is doing) — is left to it.
// The prompt starts empty with a skeleton one click away, so nothing half-written is ever saved
// by accident. On success the agent's own page opens, where the rest (the model it runs on, its
// history) lives.

/** The skeleton, line by line: the translations hold one line each (a message formatter folds line breaks), and the blank lines are here. */
const SKELETON_LINES = [0, 1, 2, 3, 4, 5, 6, 7] as const;

export interface AgentCreateBodyProps {
  canWrite: boolean;
  controller: LiveController;
  live: LiveState;
  onChanged: () => void;
  onCancel: () => void;
  /** Open the agent that was just made. */
  onOpenAgent?: (slug: string) => void;
}

function skeleton(t: (key: string) => string): string {
  const out: string[] = [];
  for (const i of SKELETON_LINES) out.push(i === 1 || i === 5 ? "" : t(`agents.create.skeleton${i}`));
  return out.join("\n");
}

export function AgentCreateBody({ canWrite, controller, live, onChanged, onCancel, onOpenAgent }: AgentCreateBodyProps) {
  const { t } = useI18n();
  const notify = useAppToast();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [prompt, setPrompt] = useState("");
  const [tech, setTech] = useState<string[]>([]);
  const [tools, setTools] = useState<string[]>([]);
  const [handoff, setHandoff] = useState("{}");
  const [colour, setColour] = useState("");
  const [minion, setMinion] = useState("");
  const { busy, writeDisabled, rpcWrite, onChangedRef } = usePanelWrites({ canWrite, guard: "create", onChanged });
  // The closed lists the daemon offers: the tech vocabulary and the tool kits.
  const registry = useRpcQuery("handlers.list", {}, controller, live);
  // Read defensively: a daemon that predates the kits answers without them, and the panel still opens.
  const techTags = registry.data?.techTags ?? [];
  const toolKits = registry.data?.toolKits ?? [];

  const create = async () => {
    if (!canWrite) return;
    // The button stays live on an empty name on purpose: `agent.create` refuses it, and that
    // sentence teaches the rule better than a dead button does.
    const answer = await rpcWrite("agent.create", agentCreateParams({ name, description, prompt, tech, tools, handoff: handoffMap(handoffRows(handoff)), avatarColor: colour, avatarMinion: minion }));
    if (answer === undefined) return;
    notify.success(t("agents.create.done", { name: answer.name }));
    onChangedRef.current();
    onOpenAgent?.(answer.slug);
  };

  return (
    <div className="space-y-8" data-testid="agent-create">
      <section className="space-y-4" aria-label={t("agents.create.who")}>
        <h3 className="text-xs font-semibold uppercase tracking-widest opacity-70">{t("agents.create.who")}</h3>
        <AgentIdentityFields
          name={name} onName={setName} description={description} onDescription={setDescription}
          colour={colour} onColour={setColour} minion={minion} onMinion={setMinion}
          nameHint={t("agents.create.nameHint")} disabled={writeDisabled}
        />
      </section>

      <section className="space-y-2" aria-label={t("agents.create.does")}>
        <h3 className="text-xs font-semibold uppercase tracking-widest opacity-70">{t("agents.create.does")}</h3>
        <FormField label={t("agents.create.prompt")} labelFor="agent-create-prompt" hint={t("agents.create.promptHint")}>
          <Textarea
            id="agent-create-prompt"
            variant="ghost"
            rows={9}
            spellCheck={false}
            className="font-mono text-xs"
            value={prompt}
            disabled={writeDisabled}
            placeholder={t("agents.create.promptPlaceholder")}
            onChange={(event) => setPrompt(event.target.value)}
            data-testid="agent-create-prompt"
          />
        </FormField>
        {prompt.trim() === "" && (
          <Button size="sm" variant="ghost" disabled={writeDisabled} onClick={() => setPrompt(skeleton(t))} data-testid="agent-create-skeleton">
            {t("agents.create.useSkeleton")}
          </Button>
        )}
      </section>

      <section className="space-y-2" aria-label={t("agents.create.works")}>
        <h3 className="text-xs font-semibold uppercase tracking-widest opacity-70">{t("agents.create.works")}</h3>
        <p className="text-sm opacity-80">{t("agents.create.worksHelp")}</p>
        {techTags.length > 0 || registry.data !== undefined ? (
          <TechPicker tags={techTags} value={tech} onChange={setTech} disabled={writeDisabled} />
        ) : (
          <p className="text-sm opacity-60" data-testid="agent-create-tech-waiting">{registry.error ?? t("agents.create.techLoading")}</p>
        )}
      </section>

      <section className="space-y-2" aria-label={t("agents.create.uses")}>
        <h3 className="text-xs font-semibold uppercase tracking-widest opacity-70">{t("agents.create.uses")}</h3>
        <p className="text-sm opacity-80">{t("agents.create.usesHelp")}</p>
        {registry.data !== undefined && (
          <ToolsPicker value={tools} onChange={setTools} kits={toolKits} controller={controller} live={live} disabled={writeDisabled} />
        )}
      </section>

      <section className="space-y-2" aria-label={t("agents.section.reports")}>
        <h3 className="text-xs font-semibold uppercase tracking-widest opacity-70">{t("agents.section.reports")}</h3>
        <p className="text-sm opacity-80">{t("agents.section.reportsHelp")}</p>
        <HandoffBuilder value={handoff} onChange={setHandoff} disabled={writeDisabled} testId="agent-create-handoff" />
      </section>

      <footer className="flex items-center justify-between gap-3 border-t border-white/10 pt-4">
        <p className="max-w-sm text-xs opacity-70">{t("agents.create.later")}</p>
        <div className="flex gap-2">
          <Button variant="ghost" onClick={onCancel} data-testid="agent-create-cancel">{t("agents.create.cancel")}</Button>
          {/* `-run` on the submit control: the wrapper carries `agent-create`, and a test that
              found both with one query could not tell the panel from its own button. */}
          <Button onClick={() => void create()} disabled={writeDisabled} data-testid="agent-panel-create-run">
            {busy ? t("agents.panel.saving") : t("agents.create.submit")}
          </Button>
        </div>
      </footer>
    </div>
  );
}
