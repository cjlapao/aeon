import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Alert, Button, FormField, Input, Panel, Pill, Select } from "../../../components/kit";
import { NetworkGraph } from "@cjlapao/ui-kit";
import { rpcTyped, toRpcError, RpcError } from "../../../lib/bridge";
import { ErrorPanel } from "../../../components/ErrorPanel";
import { cancelWorker, isLiveState, useAsyncWorkers } from "../../../lib/asyncworkers";
import { registerBenchPage } from "../lib/benchRegistry";
import { buildGraphNetwork } from "../../settings/system/graphNetwork";

// The ego-network mapping moved to settings/system/graphNetwork.ts when Platform status started
// drawing the graph too; the bench keeps the name it always exported.
export { buildGraphNetwork };

/**
 * The code graph, where it will be read (S30, T7).
 *
 * # What this page is
 *
 * A window onto `internal/graph` through the daemon's OWN eight doors —
 * `graph.explain · graph.path · graph.query` (the structural reads) and
 * `graph.build · graph.reparse · graph.delete · graph.status · graph.roots`
 * (the index maintenance the pre-flight named as the CRUD gap: an index
 * with no visible way to fill, fix, or empty it is an index nobody trusts).
 * Everything arrives through the generic `rpc()` bridge; this slice adds
 * no Rust command (AGENTS §4b: a command per method is a second method
 * table somebody forgets to update). The three write verbs go through the
 * daemon like everything else — P34's daemon-first, made visible with a
 * label rather than hidden: the buttons say so because the honest shape
 * of this page is "an operator asking the daemon to maintain its index",
 * not "a browser editing a database".
 *
 * # The four states are four renderings, not two renderings and a guess
 *
 * D8/CG7 is the whole product here. `found` renders the node with its
 * edges in BOTH directions; `ambiguous` lists EVERY candidate with file
 * and line and picks none — the disambiguation is the person re-asking
 * with the qualified name, never this page choosing for them;
 * `not-found` says the root is indexed and nothing matched; `not-built`
 * says this root has no rows and points at the Build button — because
 * "empty index" and "no index" are different instructions, and a page
 * that renders them alike turns a first run into a shrug. `test/bench-graph.test.tsx`
 * asserts the four stay four in the DOM and that no answer box exists to
 * type a reply into: a query surface that could display a hand-written
 * answer could display a hand-written fact.
 *
 * # References only — CG8, in the DOM
 *
 * Nothing here has a place to put file content. A node renders as
 * symbol · kind · file:lines — coordinates, not bytes. Fetching a node's
 * CONTENT is a `read` call the caller makes next with those coordinates;
 * collapsing that step is D11's boundary this page exists to keep
 * visible. There is no textarea, no `<pre>` fed by any graph method, and
 * no graph result field this page could print that could hold a sentence
 * the daemon did not send as data.
 *
 * # What this page does NOT do
 *
 * It does not watch the filesystem (no fsnotify in the product, T4's
 * boundary) — writes update the graph through the daemon's write-signal
 * hook, and the buttons here are the DELIBERATE maintenance doors, not a
 * substitute for that automation. It does not parse anything in the
 * browser; the tree-sitter walk is daemon-side and this page never sees
 * source. It does not persist a root in any browser storage — the root
 * list comes from `graph.roots`, which is data, not memory.
 */

type NodeRef = NonNullable<NonNullable<Awaited<ReturnType<typeof explainGraph>>["node"]>>;
export type ExplainResult = Awaited<ReturnType<typeof explainGraph>>;
type PathResult = Awaited<ReturnType<typeof pathGraph>>;
type QueryResult = Awaited<ReturnType<typeof queryGraph>>;
type StatusResult = Awaited<ReturnType<typeof statusGraph>>;
/** S64: graph.build answers with a HANDLE — the report rides the worker
 * row now (asyncworkers.get), so this page follows the ledger instead of
 * holding a ten-minute call open. */
type BuildAccepted = Awaited<ReturnType<typeof buildGraph>>;
type ReparseResult = Awaited<ReturnType<typeof reparseGraph>>;

function explainGraph(componentRoot: string, symbol: string) {
  return rpcTyped("graph.explain", { componentRoot, symbol });
}
function pathGraph(componentRoot: string, from: string, to: string) {
  return rpcTyped("graph.path", { componentRoot, from, to });
}
function queryGraph(componentRoot: string, kind: string, prefix: string, limit: number) {
  return rpcTyped("graph.query", { componentRoot, kind, prefix, limit });
}
function statusGraph(componentRoot: string) {
  return rpcTyped("graph.status", { componentRoot });
}
function buildGraph(componentRoot: string) {
  return rpcTyped("graph.build", { componentRoot });
}
function reparseGraph(componentRoot: string, path: string) {
  return rpcTyped("graph.reparse", { componentRoot, path });
}
function deleteGraph(componentRoot: string) {
  return rpcTyped("graph.delete", { componentRoot });
}
function rootsGraph() {
  return rpcTyped("graph.roots", {});
}

/** The kinds the walkers emit today (graph.go's open set, named so a
 * person browses what exists rather than free-typing what might). */
const NODE_KINDS = ["", "function", "method", "type", "interface", "class", "file", "package"];

const STATE_TONES: Record<string, "green" | "amber" | "slate" | "red"> = {
  found: "green",
  ambiguous: "amber",
  "not-found": "slate",
  "not-built": "red",
};

/** One node, rendered as COORDINATES — symbol, kind, file:lines. The
 * CG8 shape: there is simply no slot here for content. */
function NodeLine({ node, testid }: { node: NodeRef; testid?: string }) {
  return (
    <div className="flex flex-wrap items-baseline gap-2" data-testid={testid}>
      <code className="text-sm">{node.symbol}</code>
      <Pill tone="slate" size="sm">{node.kind}</Pill>
      <span className="text-xs opacity-70">
        {node.filePath}:{node.startLine}–{node.endLine} · {node.language}
      </span>
    </div>
  );
}

/** Every candidate, none picked (CG5): the ambiguous answer's body. */
function CandidateList({ candidates }: { candidates: NonNullable<ExplainResult["candidates"]> }) {
  return (
    <div className="space-y-1" data-testid="graph-candidates">
      {candidates.map((c) => (
        <NodeLine key={`${c.id}`} node={c} testid="graph-candidate" />
      ))}
    </div>
  );
}

/** The four states, one rendering each — the switch has a branch per
 * state precisely so none of them can share a branch with another. */
function StateBody({ res }: { res: ExplainResult }) {
  switch (res.state) {
    case "found":
      return (
        <div data-testid="graph-state-found" className="space-y-3">
          {res.node && <NodeLine node={res.node} testid="graph-node" />}
          <div>
            <div className="text-xs uppercase opacity-60">outgoing</div>
            {(res.outgoing ?? []).length === 0 ? (
              <div className="text-sm opacity-70" data-testid="graph-no-outgoing">
                nothing points out from this node — the graph says it stands alone
              </div>
            ) : (
              (res.outgoing ?? []).map((e, i) => (
                <div key={i} className="flex flex-wrap items-baseline gap-2" data-testid="graph-edge-out">
                  <Pill tone="violet" size="sm">{e.kind}</Pill>
                  <Pill tone={e.resolution === "extracted" ? "green" : "amber"} size="sm">{e.resolution}</Pill>
                  <NodeLine node={e.to} />
                </div>
              ))
            )}
          </div>
          <div>
            <div className="text-xs uppercase opacity-60">incoming</div>
            {(res.incoming ?? []).length === 0 ? (
              <div className="text-sm opacity-70" data-testid="graph-no-incoming">
                nothing points into this node — callers may live in files that were never indexed
              </div>
            ) : (
              (res.incoming ?? []).map((e, i) => (
                <div key={i} className="flex flex-wrap items-baseline gap-2" data-testid="graph-edge-in">
                  <Pill tone="violet" size="sm">{e.kind}</Pill>
                  <Pill tone={e.resolution === "extracted" ? "green" : "amber"} size="sm">{e.resolution}</Pill>
                  <NodeLine node={e.from} />
                </div>
              ))
            )}
          </div>
        </div>
      );
    case "ambiguous":
      return (
        <div data-testid="graph-state-ambiguous" className="space-y-2">
          <Alert
            tone="amber"
            title="more than one match"
            description="every match is listed; none is picked. Re-ask with the qualified name (for example order.Validate) to resolve one."
          />
          {res.candidates && <CandidateList candidates={res.candidates} />}
        </div>
      );
    case "not-found":
      return (
        <div data-testid="graph-state-not-found">
          <Alert
            tone="slate"
            title="indexed, no match"
            description="this root HAS a graph and nothing in it matches the name. The symbol may live in a file the build skipped, or it may not exist."
          />
        </div>
      );
    case "not-built":
      return (
        <div data-testid="graph-state-not-built">
          <Alert
            tone="red"
            title="never indexed"
            description="this root has no graph rows at all — it was never built, which is a different fact from empty. Use Build (the maintenance panel above) to fill it."
          />
        </div>
      );
    default:
      // Three-state honesty (P31) applied to the state field itself: a
      // state this page has never seen is REPORTED, not rendered as one
      // of the four it knows.
      return <Alert tone="amber" title="unknown state" description={`the daemon answered "${String((res as { state?: unknown }).state)}" — this page predates it`} />;
  }
}

// The network's four states are a rendering of the SAME found/ambiguous/
// not-found/not-built the text explain panel already speaks (D8/CG7): only
// `found` draws a graph; every other state says so honestly rather than
// inventing connections. It reuses the page's `explain` result, so the tab
// never makes a second graph query — one source of truth.
function NetworkView({ res }: { res: ExplainResult | null }) {
  const [selected, setSelected] = useState<NodeRef | null>(null);
  const net = useMemo(() => buildGraphNetwork(res), [res]);
  // A new explain result is a new graph, so drop any prior selection: a
  // coordinates line must never point at a symbol the graph no longer holds.
  useEffect(() => {
    setSelected(null);
  }, [res]);

  if (!res) {
    return (
      <div data-testid="graph-network-hint">
        <Alert
          tone="slate"
          title="network — the found symbol and its connections"
          description="explain a symbol above to draw its network: the central symbol with its callers and callees, coloured by kind. It is one view of the same found answer, not a second query."
        />
      </div>
    );
  }
  if (res.state === "ambiguous") {
    return (
      <div data-testid="graph-network-ambiguous">
        <Alert
          tone="amber"
          title="network — the found symbol and its connections"
          description="no single match, so no graph: every candidate is listed and none is picked. Re-ask with the qualified name to draw one."
        />
      </div>
    );
  }
  if (res.state === "not-found") {
    return (
      <div data-testid="graph-network-not-found">
        <Alert
          tone="amber"
          title="network — the found symbol and its connections"
          description="the root is indexed and nothing matched — there is nothing to connect. The answer names the query, not a failure."
        />
      </div>
    );
  }
  if (res.state === "not-built") {
    return (
      <div data-testid="graph-network-not-built">
        <Alert
          tone="red"
          title="network — the found symbol and its connections"
          description="this root has no rows — run Build to fill the index, then the network draws the symbol and its connections."
        />
      </div>
    );
  }
  // found, but the node is absent (the daemon's contract guarantees it is not);
  // report it rather than drawing a graph from nothing.
  if (!net) {
    return (
      <div data-testid="graph-network-unknown">
        <Alert
          tone="amber"
          title="network — the found symbol and its connections"
          description={`the daemon answered "${res.state}" with no node to draw — reported, not rendered.`}
        />
      </div>
    );
  }

  // Clicking a node reveals its coordinates (file:lines) — CG8: coordinates,
  // never content. The network shows symbols and connections only.
  return (
    <div data-testid="graph-network-found">
      <NetworkGraph
        nodes={net.nodes}
        edges={net.edges}
        communities={net.communities}
        height={420}
        variant="outlined"
        showLabels
        title="network — the found symbol and its connections"
        ariaLabel="Code graph network"
        onNodeSelect={(n) => setSelected(n ? net.byId.get(n.id) ?? null : null)}
      />
      {selected && (
        <div className="mt-2" data-testid="graph-network-selected">
          <NodeLine node={selected} testid="graph-network-selected-line" />
        </div>
      )}
    </div>
  );
}

export function GraphPage({ connected = true }: { connected?: boolean } = {}) {
  const [root, setRoot] = useState("");
  const [roots, setRoots] = useState<string[]>([]);
  const [status, setStatus] = useState<StatusResult | null>(null);
  const [symbol, setSymbol] = useState("");
  const [explain, setExplain] = useState<ExplainResult | null>(null);
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const [path, setPath] = useState<PathResult | null>(null);
  const [kind, setKind] = useState("");
  const [prefix, setPrefix] = useState("");
  const [nodes, setNodes] = useState<QueryResult | null>(null);
  const [reparsePath, setReparsePath] = useState("");
  const [report, setReport] = useState<{ verb: string; rep: BuildAccepted | ReparseResult | { root: string; nodesDeleted: number } } | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<RpcError | null>(null);

  // S64: the ledger's rows for THIS root, read on mount and repainted by
  // asyncworker.* events — the page-load answer to "is a build already
  // going?" (the reopen-mid-build case the slice exists for) arrives
  // from the database, not from a remembered client state.
  const { workers: workerRows, error: ledgerError } = useAsyncWorkers(
    connected && root.trim() !== "",
    { workerType: "graph.build", componentRoot: root.trim() },
  );
  const builds = useMemo(() => workerRows ?? [], [workerRows]);
  const liveBuild = builds.find((w) => isLiveState(w.state)) ?? null;

  // When the last live row for this root lands (event or read), the
  // status row is stale — re-read it so "not built" becomes the counter
  // without the operator pressing anything. Errors stay silent here: the
  // status is repainted, and the explicit buttons report their own.
  const wasLiveRef = useRef(false);
  useEffect(() => {
    const nowLive = builds.some((w) => isLiveState(w.state));
    if (wasLiveRef.current && !nowLive && root.trim()) {
      void statusGraph(root.trim()).then(setStatus).catch(() => undefined);
    }
    wasLiveRef.current = nowLive;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [builds]);


  const loadRoots = useCallback(async () => {
    try {
      // The daemon answers `null` for an empty root list (Go marshals a nil
      // slice as JSON null, not []), and the contract still types the result
      // as Array<string>. Storing the null straight through would leave `roots`
      // null and crash `roots.map` / `roots.length` on the next render, so the
      // state is coerced to an array here — the one place the list is set —
      // rather than guarding every read site. An empty datalist is the honest
      // "nothing is indexed" the no-roots message already speaks.
      const r = await rootsGraph();
      setRoots(Array.isArray(r) ? r : []);
    } catch (e) {
      setError(toRpcError(e));
    }
  }, []);

  useEffect(() => {
    void loadRoots();
  }, [loadRoots]);

  const run = useCallback(
    async (fn: () => Promise<void>) => {
      setBusy(true);
      setError(null);
      try {
        await fn();
      } catch (e) {
        setError(toRpcError(e));
      } finally {
        setBusy(false);
      }
    },
    [],
  );

  const requireRoot = (): boolean => {
    if (!root.trim()) {
      // The refusal the DAEMON would give, spoken locally before the call:
      // T5 §5's door is "name the missing argument", and the bench shows
      // what that refusal looks like rather than waiting a round trip.
      setError(
        new RpcError(-31105, "refused: componentRoot", {
          what: "graph needs a componentRoot — it is required on every door (T5 §5), never defaulted",
          means: "graph rows hang off exactly one root; without one there is nothing to answer about",
          todo: "pick a root above (the list is graph.roots, the daemon's own answer)",
          fatal: false,
        }),
      );
      return false;
    }
    return true;
  };

  return (
    <div className="space-y-4">
      <Panel tone="slate" data-testid="graph-root-panel">
        <div className="flex flex-wrap items-end gap-2">
          <FormField label="component root (required — the graph is keyed by root, never ambient)">
            <Input
              value={root}
              onChange={(e) => setRoot(e.target.value)}
              placeholder="/home/dev/shop"
              list="graph-roots-list"
              data-testid="graph-root"
            />
          </FormField>
          <Button size="sm" variant="ghost" onClick={() => void run(loadRoots)} disabled={busy}>
            refresh roots
          </Button>
          <Button
            size="sm"
            variant="ghost"
            disabled={busy || !root.trim()}
            data-testid="graph-status"
            onClick={() =>
              void run(async () => {
                if (!requireRoot()) return;
                setStatus(await statusGraph(root.trim()));
              })
            }
          >
            status
          </Button>
        </div>
        <datalist id="graph-roots-list">
          {roots.map((r) => (
            <option key={r} value={r} />
          ))}
        </datalist>
        {status && (
          <div className="mt-2 flex flex-wrap items-center gap-2 text-sm" data-testid="graph-status-row">
            <Pill tone={status.built ? "green" : "red"} size="sm">{status.built ? "built" : "not built"}</Pill>
            <span className="opacity-80">
              {status.files} files · {status.nodes} nodes · {status.edges} edges
            </span>
          </div>
        )}
        {roots.length === 0 && (
          <div className="mt-2 text-xs opacity-70" data-testid="graph-no-roots">
            no root has a graph yet — `graph.roots` answered empty, which is data: nothing is indexed, not "listing failed".
          </div>
        )}
      </Panel>

      <Panel data-testid="graph-maintenance-panel">
        <div className="text-sm font-medium">index maintenance — writes go through the daemon</div>
        <div className="mt-1 text-xs opacity-70">
          These are the operator&apos;s index doors (S30 T5): deliberate fills, fixes and empties of the
          operator&apos;s own working tree — the same tier as retrieval.reindex. Nothing here is scheduled by
          the app; the daemon updates the graph itself on loop writes.
        </div>
        <div className="mt-3 flex flex-wrap items-end gap-2">
          <Button
            size="sm"
            disabled={busy || !root.trim()}
            data-testid="graph-build"
            onClick={() =>
              void run(async () => {
                if (!requireRoot()) return;
                setReport({ verb: "build", rep: await buildGraph(root.trim()) });
                setStatus(await statusGraph(root.trim()));
              })
            }
          >
            build / rebuild whole root
          </Button>
          <FormField label="re-parse one file (relative to the root)">
            <Input value={reparsePath} onChange={(e) => setReparsePath(e.target.value)} placeholder="internal/order/process.go" data-testid="graph-reparse-path" />
          </FormField>
          <Button
            size="sm"
            disabled={busy || !root.trim() || !reparsePath.trim()}
            data-testid="graph-reparse"
            onClick={() =>
              void run(async () => {
                if (!requireRoot()) return;
                setReport({ verb: "reparse", rep: await reparseGraph(root.trim(), reparsePath.trim()) });
              })
            }
          >
            reparse file
          </Button>
          <Button
            size="sm"
            disabled={busy || !root.trim()}
            data-testid="graph-delete"
            onClick={() =>
              void run(async () => {
                if (!requireRoot()) return;
                setReport({ verb: "delete", rep: await deleteGraph(root.trim()) });
                setStatus(await statusGraph(root.trim()));
              })
            }
          >
            delete this root&apos;s graph
          </Button>
        </div>
        {liveBuild && (
          <div className="mt-2 flex flex-wrap items-center gap-2 text-sm" data-testid="graph-build-banner">
            <Pill tone="amber" size="sm">{liveBuild.state === "queued" ? "build queued" : "building"}</Pill>
            <span className="opacity-80">
              worker <code>{liveBuild.workerId}</code>
              {liveBuild.progressNote ? ` — ${liveBuild.progressNote}` : ""}
              {liveBuild.progress != null ? ` (${Math.round(liveBuild.progress * 100)}%)` : ""}
            </span>
            {liveBuild.cancelRequested ? (
              <Pill tone="slate" size="sm">cancel requested</Pill>
            ) : (
              <Button
                size="sm"
                data-testid="graph-build-cancel"
                onClick={() => void cancelWorker(liveBuild.workerId).catch((e) => setError(toRpcError(e)))}
              >
                cancel build
              </Button>
            )}
          </div>
        )}
        {report && (
          <div className="mt-2 text-sm" data-testid="graph-report">
            <Pill tone="violet" size="sm">{report.verb}</Pill>{" "}
            {"workerId" in report.rep ? (
              <>
                {report.rep.started ? "build started" : "a build for this root was already flying"} — worker{" "}
                <code>{report.rep.workerId}</code>. It runs in the daemon: close this window, come back, the
                ledger still knows.
              </>
            ) : "files" in report.rep ? (
              <>
                {report.rep.files} files · {report.rep.filesSkipped} skipped · {report.rep.nodes} nodes ·{" "}
                {report.rep.edges} edges · {report.rep.unresolvedEdges} unresolved
              </>
            ) : (
              <>{report.rep.nodesDeleted} nodes deleted from {report.rep.root}</>
            )}
          </div>
        )}
        {ledgerError && <div className="mt-2"><ErrorPanel error={ledgerError} /></div>}
      </Panel>

      <Panel data-testid="graph-explain-panel">
        <div className="text-sm font-medium">explain a symbol — the four states</div>
        <div className="mt-3 flex flex-wrap items-end gap-2">
          <FormField label="symbol (qualified names resolve one; bare names may answer ambiguous)">
            <Input value={symbol} onChange={(e) => setSymbol(e.target.value)} placeholder="order.ProcessOrder" data-testid="graph-symbol" />
          </FormField>
          <Button
            size="sm"
            disabled={busy || !root.trim() || !symbol.trim()}
            data-testid="graph-explain"
            onClick={() =>
              void run(async () => {
                if (!requireRoot()) return;
                setExplain(await explainGraph(root.trim(), symbol.trim()));
              })
            }
          >
            explain
          </Button>
        </div>
        {explain && (
          <div className="mt-3 space-y-2">
            <Pill tone={STATE_TONES[explain.state] ?? "slate"} size="sm" data-testid="graph-state-pill">
              {explain.state}
            </Pill>
            <StateBody res={explain} />
          </div>
        )}
      </Panel>

      <NetworkView res={explain} />

      <Panel data-testid="graph-path-panel">
        <div className="text-sm font-medium">connect two symbols — within 6 hops (PathHopBound), or it honestly says not-found</div>
        <div className="mt-3 flex flex-wrap items-end gap-2">
          <FormField label="from">
            <Input value={from} onChange={(e) => setFrom(e.target.value)} placeholder="api.HandleOrderRequest" data-testid="graph-path-from" />
          </FormField>
          <FormField label="to">
            <Input value={to} onChange={(e) => setTo(e.target.value)} placeholder="order.Validate" data-testid="graph-path-to" />
          </FormField>
          <Button
            size="sm"
            disabled={busy || !root.trim() || !from.trim() || !to.trim()}
            data-testid="graph-path"
            onClick={() =>
              void run(async () => {
                if (!requireRoot()) return;
                setPath(await pathGraph(root.trim(), from.trim(), to.trim()));
              })
            }
          >
            find path
          </Button>
        </div>
        {path && (
          <div className="mt-3 space-y-1" data-testid="graph-path-result">
            <Pill tone={STATE_TONES[path.state] ?? "slate"} size="sm">{path.state}</Pill>
            {path.state === "found" && (
              <>
                {path.start && <NodeLine node={path.start} testid="graph-path-start" />}
                {path.hops.map((h, i) => (
                  <div key={i} className="flex flex-wrap items-baseline gap-2" data-testid="graph-hop">
                    <span className="text-xs opacity-60">hop {i + 1}</span>
                    <Pill tone="violet" size="sm">{h.kind}</Pill>
                    <Pill tone={h.resolution === "extracted" ? "green" : "amber"} size="sm">{h.resolution}</Pill>
                    <NodeLine node={h.to} />
                  </div>
                ))}
              </>
            )}
            {path.state === "ambiguous" && "candidates" in path && (path as { candidates?: NonNullable<ExplainResult["candidates"]> }).candidates && (
              <CandidateList candidates={(path as { candidates?: NonNullable<ExplainResult["candidates"]> }).candidates!} />
            )}
          </div>
        )}
      </Panel>

      <Panel data-testid="graph-query-panel">
        <div className="text-sm font-medium">browse — kind and name prefix, coordinates only</div>
        <div className="mt-3 flex flex-wrap items-end gap-2">
          <FormField label="kind">
            <Select value={kind} onChange={(e) => setKind(e.target.value)} data-testid="graph-query-kind">
              {NODE_KINDS.map((k) => (
                <option key={k} value={k}>{k === "" ? "any kind" : k}</option>
              ))}
            </Select>
          </FormField>
          <FormField label="name prefix">
            <Input value={prefix} onChange={(e) => setPrefix(e.target.value)} placeholder="order." data-testid="graph-query-prefix" />
          </FormField>
          <Button
            size="sm"
            disabled={busy || !root.trim()}
            data-testid="graph-query"
            onClick={() =>
              void run(async () => {
                if (!requireRoot()) return;
                setNodes(await queryGraph(root.trim(), kind, prefix, 100));
              })
            }
          >
            browse
          </Button>
        </div>
        {nodes && (
          <div className="mt-3 space-y-1">
            <Pill tone={STATE_TONES[nodes.state] ?? "slate"} size="sm">{nodes.state}</Pill>
            {(nodes.nodes ?? []).map((n) => (
              <NodeLine key={`${n.id}`} node={n} testid="graph-query-node" />
            ))}
            {(nodes.nodes ?? []).length === 0 && nodes.state === "found" && (
              <div className="text-sm opacity-70">the filter matched nothing in this root — the answer names the filter, not a failure</div>
            )}
          </div>
        )}
      </Panel>

      {error && <ErrorPanel error={error} data-testid="graph-error" />}
    </div>
  );
}

registerBenchPage({
  id: "graph",
  label: "Code graph",
  path: "/bench/graph",
  // Past every pre-registry page and past the trust bench's range — this
  // slice appends; it does not re-order history.
  order: 4200,
  needsConnection: true,
  render: (ctx) => <GraphPage connected={ctx.connected} />,
});
