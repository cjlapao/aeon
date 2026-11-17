import { useEffect, useMemo, useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import { Alert, Button, EmptyState, FormField, Input, MultiToggle, NetworkGraph, Pill, Select } from "../../../components/kit";
import { refusalBody, toRpcError } from "../../../lib/bridge";
import { notifications } from "../../../lib/notifications";
import { buildGraphNetwork, buildPathNetwork, type ExplainResult, type NodeRef, type PathResult } from "./graphNetwork";
import { explainGraphSymbol, pathBetweenSymbols, queryGraphSymbols } from "./systemRpc";

/**
 * GraphExplorer — read the code graph (S30 T7; Platform status). Pick a symbol and see who calls it and
 * what it calls, or pick two and see how they connect. Every answer is the daemon's: the explain call
 * answers one of four states and each renders on its own (D8/CG7) — `found` draws the symbol with its
 * callers and callees, `ambiguous` lists EVERY candidate and picks none, `not-found` and `not-built`
 * say so honestly. Nothing is drawn that the daemon did not send, and the graph shows one symbol's
 * neighbourhood, never the whole repository. Coordinates (file:lines) only — never file content (CG8).
 *
 * The suggestions under the box are `graph.query` by prefix; they only ever offer symbols that exist.
 * A refusal is a toast.
 */
type Mode = "explore" | "path";

const KINDS = ["", "function", "method", "type", "interface", "class", "file", "package"];
const SUGGESTIONS = 8;

export interface GraphExplorerProps {
  root: string;
  built: boolean;
  onMaintain: () => void;
}

export function GraphExplorer({ root, built, onMaintain }: GraphExplorerProps) {
  const { t } = useI18n();
  const [mode, setMode] = useState<Mode>("explore");
  const [symbol, setSymbol] = useState("");
  const [kind, setKind] = useState("");
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const [explain, setExplain] = useState<ExplainResult | null>(null);
  const [path, setPath] = useState<PathResult | null>(null);
  const [selected, setSelected] = useState<NodeRef | null>(null);
  const [suggestions, setSuggestions] = useState<NodeRef[]>([]);
  const [busy, setBusy] = useState(false);

  // Suggestions: symbols that start with what is typed (debounced; a failed lookup offers nothing).
  useEffect(() => {
    const prefix = (mode === "explore" ? symbol : "").trim();
    if (!built || prefix.length < 2) {
      setSuggestions([]);
      return;
    }
    let lived = true;
    const timer = setTimeout(() => {
      queryGraphSymbols(root, kind, prefix, SUGGESTIONS)
        .then((answer) => lived && setSuggestions(Array.isArray(answer?.nodes) ? answer.nodes : []))
        .catch(() => lived && setSuggestions([]));
    }, 250);
    return () => {
      lived = false;
      clearTimeout(timer);
    };
  }, [root, kind, symbol, mode, built]);

  const refuse = (cause: unknown) =>
    notifications.push({ title: t("settings.system.explore.refused"), body: refusalBody(toRpcError(cause)), tone: "danger", source: "local" });

  const explore = async (name: string) => {
    if (name.trim() === "") return;
    setBusy(true);
    try {
      const answer = await explainGraphSymbol(root, name.trim());
      setExplain(answer);
      setSelected(answer.node ?? null);
      setSuggestions([]);
    } catch (cause) {
      refuse(cause);
    } finally {
      setBusy(false);
    }
  };

  const findPath = async () => {
    setBusy(true);
    try {
      const answer = await pathBetweenSymbols(root, from.trim(), to.trim());
      setPath(answer);
      setSelected(answer.start ?? null);
    } catch (cause) {
      refuse(cause);
    } finally {
      setBusy(false);
    }
  };

  const net = useMemo(() => buildGraphNetwork(explain), [explain]);
  const route = useMemo(() => buildPathNetwork(path), [path]);
  const drawn = mode === "explore" ? net : route;
  const byId = drawn?.byId;

  const canvas = !built ? (
    <div data-testid="explore-not-built-root">
      <EmptyState title={t("settings.system.explore.rootNotBuilt")} subtitle={t("settings.system.explore.rootNotBuiltBody")} />
      <div className="mt-3 flex justify-center"><Button size="sm" onClick={onMaintain} data-testid="explore-build">{t("settings.system.explore.build")}</Button></div>
    </div>
  ) : drawn !== null ? (
    <div data-testid={mode === "explore" ? "graph-network-found" : "graph-path-found"} className="min-w-0">
      <NetworkGraph
        nodes={drawn.nodes}
        edges={drawn.edges}
        communities={drawn.communities}
        highlightedEdgeIds={route !== null && mode === "path" ? route.highlighted : undefined}
        height={400}
        variant="outlined"
        showLabels
        ariaLabel={t("settings.system.explore.ariaLabel")}
        onNodeSelect={(n) => setSelected(n ? byId?.get(n.id) ?? null : null)}
      />
    </div>
  ) : mode === "explore" ? (
    <ExploreState res={explain} onPick={(name) => { setSymbol(name); void explore(name); }} onMaintain={onMaintain} />
  ) : (
    <PathState res={path} />
  );

  return (
    <div className="flex min-h-0 min-w-0 flex-1 flex-col gap-3" data-testid="graph-explorer">
      <div className="flex shrink-0 flex-wrap items-end gap-3">
        <MultiToggle
          size="sm"
          aria-label={t("settings.system.explore.mode")}
          value={mode}
          onChange={(value) => setMode(value as Mode)}
          options={[
            { value: "explore", label: t("settings.system.explore.modeExplore") },
            { value: "path", label: t("settings.system.explore.modePath") },
          ]}
        />
        {mode === "explore" ? (
          <>
            <div className="w-80 max-w-full">
              <FormField label={t("settings.system.explore.symbol")}>
                <Input
                  variant="ghost"
                  aria-label={t("settings.system.explore.symbol")}
                  value={symbol}
                  placeholder={t("settings.system.explore.symbolHint")}
                  onChange={(e) => setSymbol(e.target.value)}
                  onKeyDown={(e) => { if (e.key === "Enter" && !busy) void explore(symbol); }}
                  data-testid="explore-symbol"
                />
              </FormField>
            </div>
            <div className="w-40 shrink-0">
              <FormField label={t("settings.system.explore.kind")}>
                <Select variant="ghost" size="md" aria-label={t("settings.system.explore.kind")} value={kind} onChange={(e) => setKind(e.target.value)} data-testid="explore-kind">
                  {KINDS.map((k) => <option key={k} value={k}>{k === "" ? t("settings.system.explore.anyKind") : k}</option>)}
                </Select>
              </FormField>
            </div>
            <Button size="sm" disabled={busy || !built || symbol.trim() === ""} onClick={() => void explore(symbol)} data-testid="explore-go">{t("settings.system.explore.go")}</Button>
          </>
        ) : (
          <>
            <div className="w-60 max-w-full">
              <FormField label={t("settings.system.explore.from")}>
                <Input variant="ghost" aria-label={t("settings.system.explore.from")} value={from} onChange={(e) => setFrom(e.target.value)} data-testid="path-from" />
              </FormField>
            </div>
            <div className="w-60 max-w-full">
              <FormField label={t("settings.system.explore.to")}>
                <Input variant="ghost" aria-label={t("settings.system.explore.to")} value={to} onChange={(e) => setTo(e.target.value)} data-testid="path-to" />
              </FormField>
            </div>
            <Button size="sm" disabled={busy || !built || from.trim() === "" || to.trim() === ""} onClick={() => void findPath()} data-testid="path-go">{t("settings.system.explore.findPath")}</Button>
          </>
        )}
      </div>

      {suggestions.length > 0 && mode === "explore" && (
        <div className="flex shrink-0 flex-wrap items-center gap-2 text-xs" data-testid="explore-suggestions">
          <span className="opacity-70">{t("settings.system.explore.matches")}</span>
          {suggestions.map((node) => (
            <button
              key={node.id}
              type="button"
              className="rounded-full border border-white/15 px-2.5 py-0.5 hover:bg-white/10"
              onClick={() => { setSymbol(node.symbol); void explore(node.symbol); }}
              data-testid={`explore-suggestion-${node.id}`}
            >
              {node.symbol} <span className="opacity-60">· {node.kind}</span>
            </button>
          ))}
        </div>
      )}

      <div className="grid min-h-[22rem] min-w-0 flex-1 gap-3 lg:grid-cols-[minmax(0,1fr)_300px]">
        <div className="min-w-0">{canvas}</div>
        <SymbolCard
          node={selected}
          callers={explain?.incoming?.length ?? 0}
          callees={explain?.outgoing?.length ?? 0}
          showCounts={mode === "explore" && net !== null}
          onCentre={(node) => { setMode("explore"); setSymbol(node.symbol); void explore(node.symbol); }}
          onPathStart={(node) => { setMode("path"); setFrom(node.symbol); }}
        />
      </div>
    </div>
  );
}

/** One symbol as coordinates — symbol, kind, language, file:lines. There is no slot here for content (CG8). */
function SymbolCard({ node, callers, callees, showCounts, onCentre, onPathStart }: {
  node: NodeRef | null;
  callers: number;
  callees: number;
  showCounts: boolean;
  onCentre: (node: NodeRef) => void;
  onPathStart: (node: NodeRef) => void;
}) {
  const { t } = useI18n();
  return (
    <aside className="flex min-w-0 flex-col gap-3 rounded-xl border border-white/10 bg-white/[0.04] p-4" data-testid="symbol-card">
      <h3 className="text-xs font-semibold uppercase tracking-widest opacity-70">{t("settings.system.explore.selected")}</h3>
      {node === null ? (
        <p className="text-sm opacity-70">{t("settings.system.explore.nothingSelected")}</p>
      ) : (
        <>
          <p className="break-all text-lg font-semibold" data-testid="symbol-name">{node.symbol}</p>
          <div className="flex gap-2"><Pill size="sm" tone="blue">{node.kind}</Pill><Pill size="sm" tone="slate">{node.language}</Pill></div>
          <code className="block break-all text-xs opacity-80" data-testid="symbol-coords">{node.filePath}<br />{t("settings.system.explore.lines", { start: node.startLine, end: node.endLine })}</code>
          {showCounts && <p className="text-sm opacity-80" data-testid="symbol-counts">{t("settings.system.explore.counts", { callers, callees })}</p>}
          <p className="text-xs opacity-60">{t("settings.system.explore.cardHint")}</p>
          <div className="flex flex-wrap gap-2">
            <Button size="sm" variant="ghost" onClick={() => onCentre(node)} data-testid="symbol-centre">{t("settings.system.explore.centre")}</Button>
            <Button size="sm" variant="ghost" onClick={() => onPathStart(node)} data-testid="symbol-path-start">{t("settings.system.explore.pathStart")}</Button>
          </div>
        </>
      )}
    </aside>
  );
}

/** The explain answer's non-drawing states — one rendering each, so none shares a branch with another. */
function ExploreState({ res, onPick, onMaintain }: { res: ExplainResult | null; onPick: (name: string) => void; onMaintain: () => void }) {
  const { t } = useI18n();
  if (res === null) return <div data-testid="explore-hint"><EmptyState title={t("settings.system.explore.hint")} subtitle={t("settings.system.explore.hintBody")} /></div>;
  switch (res.state) {
    case "ambiguous":
      return (
        <div className="space-y-2" data-testid="graph-state-ambiguous">
          <Alert intent="warning" title={t("settings.system.explore.ambiguous")} description={t("settings.system.explore.ambiguousBody")} />
          <ul className="divide-y divide-[var(--border)] rounded-lg border border-white/10" data-testid="graph-candidates">
            {(res.candidates ?? []).map((c) => (
              <li key={c.id} className="flex items-center justify-between gap-3 px-3 py-2 text-sm" data-testid="graph-candidate">
                <span className="min-w-0"><code>{c.symbol}</code> <Pill size="sm" tone="blue">{c.kind}</Pill></span>
                <span className="flex shrink-0 items-center gap-2">
                  <code className="truncate text-xs opacity-70">{c.filePath}:{c.startLine}</code>
                  <Button size="sm" variant="ghost" onClick={() => onPick(c.symbol)} data-testid={`graph-candidate-pick-${c.id}`}>{t("settings.system.explore.pick")}</Button>
                </span>
              </li>
            ))}
          </ul>
        </div>
      );
    case "not-found":
      return <div data-testid="graph-state-not-found"><EmptyState title={t("settings.system.explore.notFound")} subtitle={t("settings.system.explore.notFoundBody")} /></div>;
    case "not-built":
      return (
        <div data-testid="graph-state-not-built">
          <EmptyState title={t("settings.system.explore.notBuilt")} subtitle={t("settings.system.explore.notBuiltBody")} />
          <div className="mt-3 flex justify-center"><Button size="sm" onClick={onMaintain}>{t("settings.system.explore.build")}</Button></div>
        </div>
      );
    case "found":
      // found with no node to draw: the daemon's contract says this cannot happen; reported, not rendered.
      return <Alert intent="warning" title={t("settings.system.explore.noNode")} data-testid="graph-state-found-empty" />;
    default:
      // Three-state honesty: a state this page has never seen is REPORTED, never rendered as one it knows.
      return <Alert intent="warning" title={t("settings.system.explore.unknownState", { state: String((res as { state?: unknown }).state) })} data-testid="graph-state-other" />;
  }
}

/** The path answer when there is nothing to draw: the daemon's own state word, verbatim. */
function PathState({ res }: { res: PathResult | null }) {
  const { t } = useI18n();
  if (res === null) return <div data-testid="path-hint"><EmptyState title={t("settings.system.explore.pathHint")} subtitle={t("settings.system.explore.pathHintBody")} /></div>;
  return <Alert intent="warning" title={t("settings.system.explore.pathState", { state: res.state })} description={t("settings.system.explore.pathStateBody")} data-testid="path-state" />;
}
