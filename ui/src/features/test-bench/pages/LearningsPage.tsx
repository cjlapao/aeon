import { useCallback, useEffect, useState } from "react";
import { Alert, Button, FormField, Input, Panel, Pill, Select } from "../../../components/kit";
import { rpc, toRpcError, type RpcError } from "../../../lib/bridge";
import { ErrorPanel } from "../../../components/ErrorPanel";
import {
  LEARNINGS_PROVENANCES,
  LEARNINGS_SCOPES,
  METHOD_LEARNINGS_ADD,
  METHOD_LEARNINGS_LIST,
  METHOD_LEARNINGS_QUERY,
  METHOD_LEARNINGS_SHOW,
  type LearningsProvenance,
  type LearningsScope,
  type LearningsView,
} from "../../../lib/protocol";
import { registerBenchPage } from "../lib/benchRegistry";

/**
 * The learnings bench (S28 T5) — the classified, queryable record of what an
 * agent, a project or a person has learned, through the four `learnings.*`
 * socket verbs and no new Tauri command (P34's ordering: the capability
 * rides the socket first; the bench and the CLI are secondary clients of
 * the same daemon surface).
 *
 * # The "CRUD" is the record's own verbs — and the record is append-only
 *
 * Create is `learnings.add` (provenance has **no default** — the form makes a
 * person answer where the learning came from, and the door refuses
 * `relaxed` at the seam, M1: a hand-authored board answer makes the label
 * lie about its own source). Read is `learnings.list` through the owner's
 * scope and `learnings.show` by id. The query is `learnings.query` — the
 * tag filter runs first and always (D5), and a configured embedding route
 * ranks the tag-filtered set WITHIN it (D6 — a learning the tag filter
 * excluded is never pulled in by a similar vector).
 *
 * There is no update and no delete: a learning is a record of what was
 * learned, and re-writing it would make the record lie about how it came to
 * be. The honesty is M3's — the provenance is ALWAYS its own word, and the
 * observed count rides only with an observed learning, so a `told` or
 * `assumed` learning never prints a count, not even a zero. What this bench
 * cannot do is what the record cannot be.
 */

export function LearningsPage() {
  const [scope, setScope] = useState<"" | LearningsScope>("");
  const [scopeKey, setScopeKey] = useState("");
  const [items, setItems] = useState<LearningsView[] | null>(null);
  const [error, setError] = useState<RpcError | null>(null);
  const [notice, setNotice] = useState("");

  // The add form. Provenance has NO default anywhere: the empty option is
  // the honest "you have not answered yet" and Add stays disabled until a
  // person answers — whoever writes the row says where it came from.
  // `relaxed` is absent from the choices: it is the board's answer, never
  // hand-authored (M1).
  const [addBody, setAddBody] = useState("");
  const [addProvenance, setAddProvenance] = useState<"" | LearningsProvenance>("");
  const [addCount, setAddCount] = useState("");
  const [addTags, setAddTags] = useState("");

  // The query form — a tag filter (exact or prefix) plus an optional rank.
  const [queryTag, setQueryTag] = useState("");
  const [queryPrefix, setQueryPrefix] = useState(false);
  const [queryText, setQueryText] = useState("");

  const canScope = scope !== "" && scopeKey.trim() !== "";

  const load = useCallback(async () => {
    if (!canScope) return;
    try {
      setError(null);
      const res = await rpc<{ learnings: LearningsView[] }>(METHOD_LEARNINGS_LIST, {
        scope,
        scopeKey: scopeKey.trim(),
      });
      setItems(res.learnings);
    } catch (e) {
      setError(toRpcError(e));
    }
  }, [scope, scopeKey, canScope]);

  useEffect(() => {
    setItems(null);
  }, [scope, scopeKey]);

  const add = async () => {
    if (scope === "" || scopeKey.trim() === "" || addBody.trim() === "" || addProvenance === "") {
      setNotice("a learning needs a scope, a scope-key, a body and a provenance — the record names its owner and how it came to be.");
      return;
    }
    try {
      setError(null);
      setNotice("");
      const payload: Record<string, unknown> = {
        scope,
        scopeKey: scopeKey.trim(),
        body: addBody.trim(),
        // Provenance is sent always — there is no default to hide behind.
        provenance: addProvenance,
      };
      // The count rides only with an observed learning (M3): the strength is
      // the count, set once at write time. A non-observed learning sends no
      // count, and the door will not accept one anyway.
      if (addProvenance === "observed" && addCount.trim()) {
        payload.observedCount = Number(addCount);
      }
      const tags = addTags.split(",").map((t) => t.trim()).filter(Boolean);
      if (tags.length) payload.tags = tags;
      const res = await rpc<{ learning: LearningsView }>(METHOD_LEARNINGS_ADD, payload);
      setNotice(`#${res.learning.id} added as ${res.learning.provenance} — the record carries its provenance${res.learning.observedCount != null ? `, observed ${res.learning.observedCount} times` : ""}.`);
      setAddBody("");
      setAddCount("");
      setAddTags("");
      await load();
    } catch (e) {
      setError(toRpcError(e));
    }
  };

  const query = async () => {
    try {
      setError(null);
      setNotice("");
      const payload: Record<string, unknown> = { scope, scopeKey: scopeKey.trim() };
      if (queryTag.trim()) {
        payload.tags = [{ pattern: queryTag.trim(), prefix: queryPrefix }];
      }
      if (queryText.trim()) payload.queryText = queryText.trim();
      const res = await rpc<{ learnings: LearningsView[] }>(METHOD_LEARNINGS_QUERY, payload);
      setItems(res.learnings);
      setNotice(`${res.learnings.length} learning(s) — the tag filter ran first${queryText.trim() ? ", ranked within it" : ""}.`);
    } catch (e) {
      setError(toRpcError(e));
    }
  };

  const show = async (id: number) => {
    try {
      setError(null);
      setNotice("");
      const res = await rpc<{ learning: LearningsView }>(METHOD_LEARNINGS_SHOW, { id });
      setNotice(`#${res.learning.id} ${res.learning.body} — provenance ${res.learning.provenance}${res.learning.observedCount != null ? `, observed ${res.learning.observedCount} times` : ""}.`);
    } catch (e) {
      setError(toRpcError(e));
    }
  };

  return (
    <div>
      <Panel title="Learnings — the classified, queryable record (S28)">
        <div className="muted">
          A learning names its scope (person, project or agent), its body, and how it came to
          be (provenance). It is append-only: a record of what was learned, with no update and
          no delete — re-writing a learning would make the record lie about its own source. The
          provenance is always its own word, and the observed count rides only with an observed
          learning (M3).
        </div>
      </Panel>

      <Panel title="Read a scope — list and show">
        <div className="row">
          <FormField label="scope" className="grow">
            <Select
              value={scope}
              onChange={(e) => setScope(e.target.value as "" | LearningsScope)}
            >
              <option value="">choose a scope</option>
              {LEARNINGS_SCOPES.map((s) => (
                <option key={s} value={s}>{s}</option>
              ))}
            </Select>
          </FormField>
          <FormField label="scope-key" className="grow">
            <Input value={scopeKey} onChange={(e) => setScopeKey(e.target.value)} placeholder="a user / a root / an agent" />
          </FormField>
          <Button onClick={load} disabled={!canScope}>list</Button>
        </div>
        {error && <ErrorPanel error={error} />}
        {notice && <Alert>{notice}</Alert>}
        {items != null && (
          <ul className="list">
            {items.length === 0 && <li className="muted">no learnings — the record is empty for this scope; nothing was refused.</li>}
            {items.map((l) => (
              <li key={l.id} className="row">
                <Pill>#{l.id}</Pill>
                <Pill>{l.provenance}</Pill>
                <span className="grow">{l.body}</span>
                {l.observedCount != null && <Pill>observed {l.observedCount}×</Pill>}
                {l.tags && l.tags.length > 0 && <Pill>{l.tags.join(", ")}</Pill>}
                <Button onClick={() => show(l.id)}>show</Button>
              </li>
            ))}
          </ul>
        )}
      </Panel>

      <Panel title="Query — a tag filter, ranked within it (D5/D6)">
        <div className="row">
          <FormField label="tag" className="grow">
            <Input value={queryTag} onChange={(e) => setQueryTag(e.target.value)} placeholder="a tag to filter by" />
          </FormField>
          <FormField label="match">
            <Select value={queryPrefix ? "prefix" : "exact"} onChange={(e) => setQueryPrefix(e.target.value === "prefix")}>
              <option value="exact">exact</option>
              <option value="prefix">prefix (LIKE tag%)</option>
            </Select>
          </FormField>
          <FormField label="rank (optional)" className="grow">
            <Input value={queryText} onChange={(e) => setQueryText(e.target.value)} placeholder="a query, if an embedding route is configured" />
          </FormField>
          <Button onClick={query} disabled={!canScope}>query</Button>
        </div>
      </Panel>

      <Panel title="Add — the record's only door">
        <div className="row">
          <FormField label="scope" className="grow">
            <Select value={scope} onChange={(e) => setScope(e.target.value as "" | LearningsScope)}>
              <option value="">choose a scope</option>
              {LEARNINGS_SCOPES.map((s) => (
                <option key={s} value={s}>{s}</option>
              ))}
            </Select>
          </FormField>
          <FormField label="scope-key" className="grow">
            <Input value={scopeKey} onChange={(e) => setScopeKey(e.target.value)} placeholder="a user / a root / an agent" />
          </FormField>
        </div>
        <div className="row">
          <FormField label="body" className="grow">
            <Input value={addBody} onChange={(e) => setAddBody(e.target.value)} placeholder="what was learned" />
          </FormField>
        </div>
        <div className="row">
          <FormField label="provenance" className="grow">
            <Select
              value={addProvenance}
              onChange={(e) => setAddProvenance(e.target.value as "" | LearningsProvenance)}
            >
              <option value="">choose a provenance</option>
              {/* `relaxed` is deliberately absent: it is the board's answer,
                  never hand-authored (M1). */}
              {LEARNINGS_PROVENANCES.filter((p) => p !== "relaxed").map((p) => (
                <option key={p} value={p}>{p}</option>
              ))}
            </Select>
          </FormField>
          <FormField label="observed count (only if observed)" className="grow">
            <Input value={addCount} onChange={(e) => setAddCount(e.target.value)} placeholder="the count, if observed" disabled={addProvenance !== "observed"} />
          </FormField>
          <FormField label="tags (comma-separated)" className="grow">
            <Input value={addTags} onChange={(e) => setAddTags(e.target.value)} placeholder="go, concurrency" />
          </FormField>
          <Button onClick={add} disabled={scope === "" || scopeKey.trim() === "" || addBody.trim() === "" || addProvenance === ""}>add</Button>
        </div>
        <div className="muted">
          Provenance is sent always — there is no default to hide behind. The agent scope is
          validated by a Go-level check against the handler registry or a plugin_agent row
          (design §3); the scope vocabulary and the provenance close at the database.
        </div>
      </Panel>
    </div>
  );
}

registerBenchPage({
  id: "learnings",
  label: "Learnings",
  path: "/bench/learnings",
  order: 30,
  needsConnection: true,
  render: () => <LearningsPage />,
});
