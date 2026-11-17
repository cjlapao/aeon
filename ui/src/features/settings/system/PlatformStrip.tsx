import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Pill, StatCard } from "../../../components/kit";
import { componentsOf, costTone, fmtInt, fmtUsd, rootsOf, stateTone, type StatusView } from "./systemModel";

/**
 * PlatformStrip — the three subsystems at a glance (S54 T6): the code graph, the retrieval index and
 * what it all costs, each a kit `StatCard` with the daemon's own state word beside it.
 *
 * Every figure is the daemon's: the counts are summed from the rows `platform.status` sent, the pill
 * prints the state word it sent (an unwired subsystem is a state, shown as one), and an unrecorded
 * workspace says so instead of printing a zero that would read like an invoice of nothing. The graph
 * and retrieval tiles open their tab; the usage tile has no tab of its own. A block's error sentence
 * rides under the strip, verbatim.
 */
export type StripTab = "graph" | "retrieval";

export interface PlatformStripProps {
  view: StatusView;
  selected: string;
  onOpen: (tab: StripTab) => void;
}

function Tile({ tab, selected, onOpen, children }: { tab?: StripTab; selected: boolean; onOpen: (tab: StripTab) => void; children: React.ReactNode }) {
  if (tab === undefined) return <div className="min-w-0">{children}</div>;
  return (
    <button
      type="button"
      className={`block min-w-0 w-full rounded-xl text-left outline-none focus-visible:ring-2 focus-visible:ring-sky-400 ${selected ? "ring-1 ring-sky-400/60" : ""}`}
      onClick={() => onOpen(tab)}
      data-testid={`strip-open-${tab}`}
    >
      {children}
    </button>
  );
}

export function PlatformStrip({ view: answer, selected, onOpen }: PlatformStripProps) {
  const { t } = useI18n();
  // A half-wired daemon may answer a block-less panel: each block falls back to an honest empty one
  // (no state word, no figures) rather than crashing the page.
  const view: StatusView = { ...answer, graph: answer.graph ?? { state: "" }, retrieval: answer.retrieval ?? { state: "" }, usage: answer.usage ?? { state: "" } };
  const roots = rootsOf(view.graph);
  const nodes = roots.reduce((n, r) => n + (r.nodes ?? 0), 0);
  const edges = roots.reduce((n, r) => n + (r.edges ?? 0), 0);
  const corpora = componentsOf(view.retrieval).flatMap((c) => c.corpora ?? []);
  const files = corpora.reduce((n, c) => n + (c.files ?? 0), 0);
  const stale = corpora.reduce((n, c) => n + (c.staleChunks ?? 0), 0);
  const figures = view.usage.workspace;

  return (
    <div className="shrink-0 space-y-2" data-testid="system-subsystems">
      <div className="grid gap-3" style={{ gridTemplateColumns: "repeat(auto-fit, minmax(min(100%, 260px), 1fr))" }}>
        <Tile tab="graph" selected={selected === "graph"} onOpen={onOpen}>
          <StatCard
            variant="simple"
            size="sm"
            padding="sm"
            icon="Share"
            label={t("settings.system.strip.graph")}
            value={fmtInt(nodes)}
            subtitle={t("settings.system.strip.graphSub", { roots: roots.length, edges: fmtInt(edges) })}
            actions={<Pill size="sm" tone={stateTone(view.graph.state)} data-testid="graph-state">{view.graph.state}</Pill>}
            className="min-w-0"
            data-testid="system-graph"
          />
        </Tile>
        <Tile tab="retrieval" selected={selected === "retrieval"} onOpen={onOpen}>
          <StatCard
            variant="simple"
            size="sm"
            padding="sm"
            icon="Search"
            label={t("settings.system.strip.retrieval")}
            value={corpora.length}
            subtitle={t("settings.system.strip.retrievalSub", { files: fmtInt(files), stale: fmtInt(stale) })}
            actions={<Pill size="sm" tone={stateTone(view.retrieval.state)} data-testid="retrieval-state">{view.retrieval.state}</Pill>}
            className="min-w-0"
            data-testid="system-retrieval"
          />
        </Tile>
        <Tile selected={false} onOpen={onOpen}>
          <StatCard
            variant="simple"
            size="sm"
            padding="sm"
            icon="Wallet"
            label={t("settings.system.strip.usage")}
            value={figures !== undefined && figures.recorded ? <span data-testid="usage-cost">{fmtUsd(figures.costUsdTotal)}</span> : "—"}
            subtitle={
              figures !== undefined && figures.recorded ? (
                <span data-testid="usage-figures">
                  {t("settings.system.usage.loops", { count: fmtInt(figures.loopsRun) })} · {t("settings.system.usage.tokens", { input: fmtInt(figures.inputTokensTotal), output: fmtInt(figures.outputTokensTotal) })}
                </span>
              ) : (
                <span data-testid="usage-unrecorded">{t("settings.system.usage.unrecorded")}</span>
              )
            }
            actions={
              <span className="flex gap-1" data-testid="usage-state">
                <Pill size="sm" tone={stateTone(view.usage.state)}>{view.usage.state}</Pill>
                {figures !== undefined && figures.recorded && (
                  <Pill size="sm" tone={costTone(figures.costState)} data-testid="usage-cost-state">{figures.costState}</Pill>
                )}
              </span>
            }
            className="min-w-0"
            data-testid="system-usage"
          />
        </Tile>
      </div>
      {view.retrieval.availability !== undefined && view.retrieval.availability !== "" && (
        <p className="text-xs opacity-70" data-testid="retrieval-availability">{view.retrieval.availability}</p>
      )}
      {view.graph.error !== undefined && <Alert intent="danger" title={view.graph.error} data-testid="graph-error" />}
      {view.retrieval.error !== undefined && <Alert intent="danger" title={view.retrieval.error} data-testid="retrieval-error" />}
      {view.usage.error !== undefined && <Alert intent="danger" title={view.usage.error} data-testid="usage-error" />}
    </div>
  );
}
