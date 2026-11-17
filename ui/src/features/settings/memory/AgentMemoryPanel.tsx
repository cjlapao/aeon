import { useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Input, Pill, Table, type TableColumn } from "../../../components/kit";
import { AgentTagsPicker } from "../../../components/AgentTagPicker";
import type { LiveController, LiveState } from "../../../lib/live";
import { useLearningsForAgent } from "./memoryRpc";
import { provenanceTone, strengthLabel, type Vocabulary } from "./memoryModel";

/**
 * AgentMemoryPanel — "what would this agent see?" (S52 T6, LM5's window).
 *
 * The same question the agent answers silently every time it wakes: the
 * readable set for one agent slug, in the daemon's own order, with the
 * WHY each row earned its place. The panel asks `learnings.forAgent` and
 * renders what comes back — it composes nothing, unions nothing, and
 * re-ranks nothing: LM5's fidelity (this list equals what the prompt
 * builder would inject) is the daemon's differential test, so the honest
 * panel is the one that adds only column headings.
 *
 * The relayed rows are labelled because the daemon says so: `relayed`
 * rides the pill as its own word (the M1 ordering sentence lives in the
 * page help, not repeated here). An agent slug that is no agent answers its own refusal,
 * naming the slug, shown verbatim.
 */
export interface AgentMemoryPanelProps {
  vocabulary: Vocabulary | undefined;
  controller: LiveController;
  live: LiveState;
}

export function AgentMemoryPanel({ controller, live }: AgentMemoryPanelProps) {
  const { t } = useI18n();
  const [agents, setAgents] = useState<string[]>([]);
  const [query, setQuery] = useState("");

  return (
    <div className="flex flex-col gap-3 pb-6" data-testid="memory-agent-panel">
      <p className="text-sm opacity-80">{t("settings.memory.agent.subtitle")}</p>
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <div>
          <p className="mb-1 text-xs uppercase tracking-wide opacity-70">{t("settings.memory.agent.picker")}</p>
          <AgentTagsPicker value={agents} onChange={setAgents} controller={controller} live={live} />
        </div>
        <Input
          variant="ghost"
          value={query}
          placeholder={t("settings.memory.agent.queryHint")}
          aria-label={t("settings.memory.agent.query")}
          data-testid="memory-agent-query"
          onChange={(e) => setQuery(e.target.value)}
        />
      </div>

      {agents.length === 0 ? (
        <p className="mt-2 text-xs opacity-70" data-testid="memory-agent-need">{t("settings.memory.agent.needAgent")}</p>
      ) : (
        agents.map((agent) => (
          <AgentSeen key={agent} agent={agent} query={query} showName={agents.length > 1} controller={controller} live={live} />
        ))
      )}
    </div>
  );
}

type Hit = NonNullable<ReturnType<typeof useLearningsForAgent>["data"]>[number];

/** One agent's readable set — the daemon's answer for that slug alone, never merged with another's. */
function AgentSeen({ agent, query, showName, controller, live }: { agent: string; query: string; showName: boolean } & Omit<AgentMemoryPanelProps, "vocabulary">) {
  const { t } = useI18n();
  const result = useLearningsForAgent(agent, query, controller, live);
  const columns: TableColumn<Hit>[] = [
    {
      id: "rank",
      groupable: false,
      header: t("settings.memory.agent.col.rank"),
      width: 110,
      render: (hit) => (
        <span className="font-mono text-xs opacity-70" data-testid={`memory-agent-hit-${hit.learning.id}`}>#{hit.why.rank} · #{hit.learning.id}</span>
      ),
    },
    {
      id: "provenance",
      groupable: false,
      header: t("settings.memory.agent.col.provenance"),
      width: 170,
      render: (hit) => (
        <span className="flex flex-wrap gap-1">
          <Pill size="sm" tone={provenanceTone(hit.learning.provenance)} data-testid={`memory-agent-hit-${hit.learning.id}-provenance`}>{hit.learning.provenance}</Pill>
          {strengthLabel(hit.learning.observedCount) !== "" && <Pill size="sm" tone="sky">{strengthLabel(hit.learning.observedCount)}</Pill>}
        </span>
      ),
    },
    {
      id: "learning",
      groupable: false,
      header: t("settings.memory.agent.col.learning"),
      render: (hit) => <span className="py-1 text-sm">{hit.learning.body}</span>,
    },
    {
      id: "why",
      groupable: false,
      header: t("settings.memory.agent.col.why"),
      width: 260,
      render: (hit) => (
        <span className="text-xs italic opacity-70" data-testid={`memory-agent-why-${hit.learning.id}`}>
          {t("settings.memory.agent.why", { basis: hit.why.basis })}
        </span>
      ),
    },
  ];

  return (
    <section className="flex flex-col" data-testid={`memory-agent-section-${agent}`}>
      {showName && <h3 className="mt-2 font-mono text-sm font-semibold">{agent}</h3>}
      {result.loading ? (
        <p className="mt-2 text-sm opacity-70" data-testid="memory-agent-loading">{t("settings.memory.board.loading")}</p>
      ) : result.error !== undefined ? (
        // Verbatim. An unknown slug is refused BY NAME by the plane (LM1's
        // authority); the panel repeats the refusal, never rewrites it.
        <Alert intent="danger" title={t("settings.memory.agent.refused")} description={result.error} className="mt-2" data-testid="memory-agent-refusal" />
      ) : (result.data?.length ?? 0) === 0 ? (
        <p className="mt-2 text-xs opacity-70" data-testid="memory-agent-empty">{t("settings.memory.agent.empty")}</p>
      ) : (
        <>
          <div className="mt-2 h-[18rem]" data-testid={`memory-agent-table-${agent}`}>
            <Table<Hit>
              variant="outlined"
              data={result.data ?? []}
              columns={columns}
              rowKey={(hit) => String(hit.learning.id)}
              corner="rounded-sm"
              fullHeight
            />
          </div>
        </>
      )}
    </section>
  );
}
