import { useCallback, useEffect, useState } from "react";
import { Alert, Button, FormField, Input, Panel, Pill } from "../../../components/kit";
import { rpc, toRpcError, type RpcError } from "../../../lib/bridge";
import { ErrorPanel } from "../../../components/ErrorPanel";
import {
  LEARNINGS_SCOPES,
  METHOD_LEARNINGS_QUERY,
  type LearningsScope,
  type LearningsView,
} from "../../../lib/protocol";
import { registerBenchPage } from "../lib/benchRegistry";

/**
 * Retrieval — whether a component root is indexed, and what it returns.
 *
 * # What this page is (S22 / S28 completion)
 *
 * A window onto the retrieval daemon doors, keyed by component root — the
 * same key the graph uses, and the SAME roots (graph.roots feeds the
 * selector; there is no second root list, which would be a second truth).
 * Everything arrives through the generic `rpc()` bridge: this slice adds no
 * Rust command (AGENTS §4b — a command per method is a second method table
 * somebody forgets to update), and it calls the daemon's OWN methods,
 * `retrieval.status · retrieval.reindex · search.query` (S22) and
 * `learnings.query` (S28) — never a second implementation behind them.
 *
 * # The four availability states stay four in the DOM (§4b, no collapse)
 *
 * `retrieval.status` answers `is this root indexed?` with a per-corpus
 * `available` / `keywordOnly` / `reasons`. The four reasons are a CLOSED
 * set (V19): `index-empty` (the corpus holds nothing — not a refusal, just
 * not filled), `no-provider` (a route exists but FTS5 answers with
 * KeywordOnly, no embeddings), `model-mismatch` and `dimension-mismatch`
 * (a route the caller named cannot query the recorded vectors — a refusal,
 * stated). Each renders its own tone and its own sentence; collapsing two
 * into one is how "not indexed" becomes "found nothing". An unknown reason
 * is reported, never swallowed (P31).
 *
 * # What this page does NOT do
 *
 * It does NOT fuse the retrieval query with the code graph — the two are
 * separate doors by design (the combined query was tried and dropped: it
 * returned bad results), so the agent decides which to call. It does NOT
 * parse anything in the browser (the chunking and the walk are daemon-side).
 * It does NOT persist a root in any browser storage (the root list is
 * graph.roots, data not memory). There is NO answer box: a query surface
 * that could display a hand-typed answer could display a hand-typed fact,
 * so the only input is a search term and the only output is the daemon's
 * own hits — file:line ranges with a score, never a sentence the daemon
 * did not send.
 */

// --- the result types --------------------------------------------------------
// Defined here (not the fixture-derived contract): retrieval.status is the
// one method whose result the contract predates (it is missing the reasons
// and keywordOnly the daemon actually returns), so this page reads the raw
// response through the untyped `rpc` and casts to the shapes the daemon
// really sends — the same discipline LearningsPage keeps for learnings.

type StatusCorpus = {
  corpus: string;
  files: number;
  staleFiles: number;
  pendingEmbed: number;
  available: boolean;
  keywordOnly: boolean;
  reasons: string[];
};
type StatusReport = {
  root: string;
  corpora: StatusCorpus[];
  embedRoute: string;
  embedModel: string;
  embedDim: number;
};
type ReindexResult = {
  chunksWritten: number;
  corpus: string;
  costKnown: boolean;
  deleted: number;
  embedded: number;
  embeddedUsd: number;
  files: number;
  skips: number;
};
type SearchHit = { lineEnd: number; lineStart: number; score: number; source: string };
type SearchQueryResult = { hits: SearchHit[]; keywordOnly?: boolean };

// --- the calls ---------------------------------------------------------------

function roots() {
  return rpc<string[]>("graph.roots", {});
}
function status(root: string) {
  return rpc<StatusReport>("retrieval.status", { componentRoot: root });
}
function reindex(root: string, corpus: string) {
  return rpc<ReindexResult>("retrieval.reindex", { componentRoot: root, corpus });
}
function query(root: string, corpus: string, term: string) {
  return rpc<SearchQueryResult>("search.query", { componentRoot: root, corpus, query: term });
}
function learningsQuery(scope: string, scopeKey: string, queryText: string) {
  const params: Record<string, unknown> = { scope, scopeKey };
  if (queryText.trim()) params.queryText = queryText.trim();
  return rpc<{ learnings: LearningsView[] }>(METHOD_LEARNINGS_QUERY, params);
}

const CORPORA = ["code", "history"] as const;

// --- the four availability states, one rendering each -------------------------

const REASON_TONE: Record<string, "green" | "amber" | "slate" | "red"> = {
  "index-empty": "slate",
  "no-provider": "amber",
  "model-mismatch": "amber",
  "dimension-mismatch": "amber",
};
const REASON_LABEL: Record<string, string> = {
  "index-empty": "not indexed — the corpus holds nothing yet",
  "no-provider": "keyword-only — a route answers by FTS5, no embeddings",
  "model-mismatch": "unavailable — the recorded vectors were built by another model",
  "dimension-mismatch": "unavailable — the recorded vectors are a different dimension",
};

function availabilitySentence(c: StatusCorpus): { tone: "green" | "amber" | "slate" | "red"; label: string } {
  // A corpus with a recorded embedding and a matching route is fully
  // available — the green state, the only one without a reason.
  if (c.available && !c.keywordOnly && c.reasons.length === 0) {
    return { tone: "green", label: "indexed — available by keyword and by embedding" };
  }
  // A reason names the state. The first reason wins; the closed set is the
  // four, so a reason this page has never seen is reported, not rendered as
  // one it knows (P31).
  const r = c.reasons[0];
  if (r) {
    if (r in REASON_TONE) return { tone: REASON_TONE[r] ?? "amber", label: REASON_LABEL[r] ?? "no label" };
    return { tone: "amber", label: `unknown reason "${r}" — this page predates it, not a silent fall-back` };
  }
  // Available, keyword-only, no reason: a route exists but nothing is
  // embedded under it yet — a working state, spoken.
  if (c.available && c.keywordOnly) {
    return { tone: "green", label: "indexed — keyword-only, no embeddings recorded yet" };
  }
  return { tone: "slate", label: "no status" };
}

function AvailabilityPill({ c }: { c: StatusCorpus }) {
  const { tone, label } = availabilitySentence(c);
  return (
    <div data-testid="retrieval-availability" className="space-y-1">
      <Pill tone={tone}>{label}</Pill>
      <div className="text-xs opacity-70">
        {c.files} files · {c.pendingEmbed} pending embed · {c.staleFiles} stale
        {c.keywordOnly ? " · keyword-only" : ""}
      </div>
    </div>
  );
}

// --- the page ----------------------------------------------------------------

export function RetrievalPage({ connected = true }: { connected?: boolean } = {}) {
  const [allRoots, setAllRoots] = useState<string[] | null>(null);
  const [root, setRoot] = useState("");
  const [corpus, setCorpus] = useState<(typeof CORPORA)[number]>("code");
  const [report, setReport] = useState<StatusReport | null>(null);
  const [reindexNotice, setReindexNotice] = useState("");
  const [term, setTerm] = useState("");
  const [hits, setHits] = useState<SearchQueryResult | null>(null);

  // The learnings panel — a separate door (S28), kept beside retrieval, not
  // fused with it.
  const [scope, setScope] = useState<"" | LearningsScope>("");
  const [scopeKey, setScopeKey] = useState("");
  const [queryText, setQueryText] = useState("");
  const [learnings, setLearnings] = useState<LearningsView[] | null>(null);

  const [error, setError] = useState<RpcError | null>(null);

  const loadRoots = useCallback(async () => {
    try {
      const r = await roots();
      setAllRoots(r);
    } catch (e) {
      setError(toRpcError(e));
    }
  }, []);

  useEffect(() => {
    if (connected) loadRoots();
  }, [connected, loadRoots]);

  const loadStatus = useCallback(async () => {
    if (!root) return;
    try {
      setError(null);
      const r = await status(root);
      setReport(r);
    } catch (e) {
      setError(toRpcError(e));
    }
  }, [root]);

  useEffect(() => {
    setReport(null);
    setHits(null);
  }, [root]);

  const doIndex = async () => {
    if (!root) return;
    try {
      setError(null);
      setReindexNotice("");
      const r = await reindex(root, corpus);
      setReindexNotice(
        `indexed ${corpus}: ${r.chunksWritten} chunks (${r.files} files, ${r.skips} skipped, ${r.embedded} embedded${r.costKnown ? `, $${r.embeddedUsd.toFixed(4)}` : ", cost unknown"}).`,
      );
      await loadStatus();
    } catch (e) {
      setError(toRpcError(e));
    }
  };

  const doQuery = async () => {
    if (!root || !term.trim()) return;
    try {
      setError(null);
      setHits(null);
      const r = await query(root, corpus, term.trim());
      setHits(r);
    } catch (e) {
      setError(toRpcError(e));
    }
  };

  const doLearnings = async () => {
    if (!scope || !scopeKey.trim()) return;
    try {
      setError(null);
      setLearnings(null);
      const r = await learningsQuery(scope, scopeKey.trim(), queryText);
      setLearnings(r.learnings);
    } catch (e) {
      setError(toRpcError(e));
    }
  };

  const canScope = scope !== "" && scopeKey.trim() !== "";

  if (!connected) {
    return (
      <div data-testid="retrieval-no-connection">
        <Alert tone="amber" title="no daemon" description="connect the daemon — retrieval.status and search.query ride the socket." />
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <Panel title="Retrieval — is this component root indexed? (S22)">
        <div className="row">
          <FormField label="component root" className="grow">
            <Input
              data-testid="retrieval-root"
              value={root}
              onChange={(e) => setRoot(e.target.value)}
              list="retrieval-roots-list"
              placeholder="a component root (the selector is graph.roots)"
            />
            <datalist id="retrieval-roots-list">
              {(allRoots ?? []).map((r) => (
                <option key={r} value={r} />
              ))}
            </datalist>
          </FormField>
          <FormField label="corpus">
            <select value={corpus} onChange={(e) => setCorpus(e.target.value as (typeof CORPORA)[number])}>
              {CORPORA.map((c) => (
                <option key={c} value={c}>{c}</option>
              ))}
            </select>
          </FormField>
          <Button data-testid="retrieval-status-btn" onClick={loadStatus} disabled={!root}>status</Button>
          <Button data-testid="retrieval-index-btn" onClick={doIndex} disabled={!root}>index</Button>
        </div>
        {allRoots != null && allRoots.length === 0 && (
          <div className="muted">no component root is known — graph.roots answered empty, which is data: nothing is indexed, not "listing failed".</div>
        )}
        {report && (
          <div className="mt-3 space-y-2" data-testid="retrieval-status">
            <div className="text-xs uppercase opacity-60">{report.root}</div>
            {report.corpora.length === 0 && <div className="muted">this root has no corpus rows at all — it was never indexed.</div>}
            {report.corpora.map((c) => (
              <AvailabilityPill key={c.corpus} c={c} />
            ))}
            <div className="text-xs opacity-60">
              embedding route {report.embedRoute || "— none"} · model {report.embedModel || "—"} · dim {report.embedDim || "—"}
            </div>
          </div>
        )}
        {reindexNotice && <Alert tone="green" title="indexed" description={reindexNotice} />}
        <div className="mt-2 muted">
          “index” is the daemon’s own <code>retrieval.reindex</code> — the same door a write triggers automatically
          on the tree, shown here as a deliberate act, not a substitute for it. Writes never touch the
          browser; the index lives daemon-side.
        </div>
      </Panel>

      <Panel title="Query — locations only (S22)">
        <div className="row">
          <FormField label="search" className="grow">
            <Input data-testid="retrieval-search" value={term} onChange={(e) => setTerm(e.target.value)} placeholder="a search term" />
          </FormField>
          <Button data-testid="retrieval-query-btn" onClick={doQuery} disabled={!root || !term.trim()}>query</Button>
        </div>
        {hits && (
          <div className="mt-3 space-y-1" data-testid="retrieval-query">
            {hits.keywordOnly && (
              <Pill tone="amber">keyword-only — the daemon answered by FTS5, not by embedding (no route, or a route it could not query)</Pill>
            )}
            {(hits.hits ?? []).length === 0 && (
              <div className="muted">the query ran and matched nothing — an empty answer names the query, not a failure.</div>
            )}
            {(hits.hits ?? []).map((h, i) => (
              <div key={i} className="row" data-testid="retrieval-hit">
                <span className="grow text-sm">{h.source}:{h.lineStart}–{h.lineEnd}</span>
                <Pill tone="slate" size="sm">{h.score.toFixed(3)}</Pill>
              </div>
            ))}
          </div>
        )}
      </Panel>

      <Panel title="Learnings — the ranked set (S28, a separate door)">
        <div className="row">
          <FormField label="scope">
            <select data-testid="retrieval-scope" value={scope} onChange={(e) => setScope(e.target.value as "" | LearningsScope)}>
              <option value="">choose a scope</option>
              {LEARNINGS_SCOPES.map((s) => (
                <option key={s} value={s}>{s}</option>
              ))}
            </select>
          </FormField>
          <FormField label="scope-key" className="grow">
            <Input data-testid="retrieval-scope-key" value={scopeKey} onChange={(e) => setScopeKey(e.target.value)} placeholder="a user / a root / an agent" />
          </FormField>
          <FormField label="rank (optional)" className="grow">
            <Input data-testid="retrieval-rank" value={queryText} onChange={(e) => setQueryText(e.target.value)} placeholder="a query, if an embedding route is configured" />
          </FormField>
          <Button data-testid="retrieval-learnings-btn" onClick={doLearnings} disabled={!canScope}>query</Button>
        </div>
        {learnings != null && (
          <div className="mt-3 space-y-1" data-testid="retrieval-learnings">
            {learnings.length === 0 && <div className="muted">no learnings in this scope — the set is empty, nothing was refused.</div>}
            {learnings.map((l) => (
              <div key={l.id} className="row" data-testid="retrieval-learning">
                <Pill tone="slate">{l.provenance}</Pill>
                <span className="grow text-sm">{l.body}</span>
                {l.observedCount != null && <Pill>observed {l.observedCount}×</Pill>}
              </div>
            ))}
            <div className="text-xs opacity-60">the tag filter ran first; a configured embedding route ranks the set within it (D6).</div>
          </div>
        )}
      </Panel>

      {error && <ErrorPanel error={error} data-testid="retrieval-error" />}
    </div>
  );
}

registerBenchPage({
  id: "retrieval",
  label: "Retrieval",
  path: "/bench/retrieval",
  // Past the code graph (4200) and every pre-registry page — this slice
  // appends; it does not re-order history.
  order: 4250,
  needsConnection: true,
  render: (ctx) => <RetrievalPage connected={ctx.connected} />,
});
