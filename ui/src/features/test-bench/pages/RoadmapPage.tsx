import { useCallback, useEffect, useState } from "react";
import { Alert, Button, FormField, Input, Panel, Pill, Select } from "../../../components/kit";
import { rpc, toRpcError, type RpcError } from "../../../lib/bridge";
import { ErrorPanel } from "../../../components/ErrorPanel";
import {
  METHOD_ROADMAP_ADD,
  METHOD_ROADMAP_CANCEL,
  METHOD_ROADMAP_ESTIMATE,
  METHOD_ROADMAP_IDEATE,
  METHOD_ROADMAP_LIST,
  METHOD_ROADMAP_MATERIALIZE,
  METHOD_ROADMAP_PLAN,
  METHOD_ROADMAP_PRIORITY,
  ROADMAP_PRIORITY_STATES,
  ROADMAP_PROVENANCES,
  ROADMAP_STATUSES,
  type RoadmapDispatchResult,
  type RoadmapItemView,
  type RoadmapListResult,
  type RoadmapPriorityState,
  type RoadmapProvenance,
} from "../../../lib/protocol";

/**
 * The roadmap bench (S26, plus the user-requested full CRUD) — the forward
 * Gantt's lab door: every one of the nine `roadmap.*` socket verbs, through
 * the generic `rpc()` bridge and no new Tauri command (§4b).
 *
 * # The CRUD is the machine's own verbs, not an invented one
 *
 * Create is `roadmap.add` (provenance has **no default** — D5 — so the form
 * makes a person answer where the item came from); Read is `roadmap.list`
 * through the door's own filters (epic/status/priority — the Gantt order is
 * the door's, this page adds none); Update is `roadmap.estimate` +
 * `roadmap.priority` (both whole-fact writes — D3's all-present-or-all-absent
 * trio and D2's state-with-label travel whole, the panel sends one decision
 * per click and the door refuses halves); Delete is `roadmap.cancel` — which
 * **never deletes** (D7: the row rests as `cancelled`, which is why
 * `cancelled` renders and is never filtered out). The three dispatches —
 * `roadmap.ideate`, `roadmap.plan`, `roadmap.materialize` — are the item's
 * rungs, offered where the machine is at: the buttons follow the statuses,
 * and where the door refuses (a `running` item's cancel, a not-ready
 * materialization) the page does not offer the act and the door still
 * refuses anyway — the page is a convenience, the door is the law.
 *
 * # What this page is NOT
 *
 * It is a bench, not the product (the designed forward Gantt is the app
 * view, `src/features/roadmap/`), and it calls nothing the CLI cannot —
 * both are secondary clients of the same daemon surface (P34).
 */

// The honest words are defined ONCE (features/roadmap/render.ts) and
// re-exported here so both doors of the web layer render the same D2/D3
// sentences; the vitest gate asserts them through this file, as the queue
// bench's does.
import { CANCELLABLE, STATUS_LABELS, estimateWords, priorityWords } from "../../roadmap/render";
export { CANCELLABLE, STATUS_LABELS, estimateWords, priorityWords };

export function RoadmapPage() {
  const [root, setRoot] = useState("");
  const [items, setItems] = useState<RoadmapItemView[] | null>(null);
  const [error, setError] = useState<RpcError | null>(null);
  const [notice, setNotice] = useState("");

  // The board's filters — the door's own (the page re-sorts nothing).
  const [filterStatus, setFilterStatus] = useState("");
  const [filterPriority, setFilterPriority] = useState("");
  const [filterEpic, setFilterEpic] = useState("");

  // The add form. Provenance has NO default anywhere (D5): the empty option
  // is the honest "you have not answered yet" and Add stays disabled until
  // the person answers — whoever writes the row says where it came from.
  const [title, setTitle] = useState("");
  const [epic, setEpic] = useState("");
  const [provenance, setProvenance] = useState<"" | RoadmapProvenance>("");
  const [addPriority, setAddPriority] = useState<"" | RoadmapPriorityState>("");
  const [addPriorityLabel, setAddPriorityLabel] = useState("");
  const [addEstMin, setAddEstMin] = useState("");
  const [addEstMax, setAddEstMax] = useState("");
  const [addEstBasis, setAddEstBasis] = useState("");

  // The update panel — one whole decision per write (D2/D3 travel whole).
  const [selected, setSelected] = useState<RoadmapItemView | null>(null);
  const [estMin, setEstMin] = useState("");
  const [estMax, setEstMax] = useState("");
  const [estBasis, setEstBasis] = useState("");
  const [priState, setPriState] = useState<"" | RoadmapPriorityState>("");
  const [priLabel, setPriLabel] = useState("");

  // The dispatch forms — the rungs, collected as their params say.
  const [workspaceRoot, setWorkspaceRoot] = useState("");
  const [ideateBlueprint, setIdeateBlueprint] = useState("");
  const [matBlueprint, setMatBlueprint] = useState("");
  const [matGoal, setMatGoal] = useState("");
  const [matTech, setMatTech] = useState("");
  const [matWith, setMatWith] = useState("");
  const [matAttended, setMatAttended] = useState<"default" | "attended" | "unattended">("default");

  const load = useCallback(async () => {
    if (!root.trim()) return;
    try {
      setError(null);
      const params: Record<string, unknown> = { componentRoot: root.trim() };
      if (filterStatus) params.status = filterStatus;
      if (filterPriority) params.priorityState = filterPriority;
      if (filterEpic.trim()) params.epic = filterEpic.trim();
      const board = await rpc<RoadmapListResult>(METHOD_ROADMAP_LIST, params);
      setItems(board.items);
    } catch (e) {
      setError(toRpcError(e));
    }
  }, [root, filterStatus, filterPriority, filterEpic]);

  useEffect(() => {
    setItems(null);
    setSelected(null);
  }, [root]);

  const add = async () => {
    try {
      setError(null);
      setNotice("");
      const payload: Record<string, unknown> = {
        componentRoot: root.trim(),
        title: title.trim(),
        // D5: provenance is sent always — there is no default to hide behind.
        provenance,
      };
      if (epic.trim()) payload.epic = epic.trim();
      // D2: state and label travel together or not at all — a move to
      // `unknown` carries no label, and the door refuses the mismatch.
      if (addPriority) {
        payload.priorityState = addPriority;
        if (addPriority !== "unknown" && addPriorityLabel.trim()) payload.priorityLabel = addPriorityLabel.trim();
      }
      // D3: the trio is all-present or the field set is all-absent — half a
      // range is refused by the door before anything is written. (The add
      // form's own trio — addEst* — not the update panel's.)
      if (estPresent(addEstMin, addEstMax, addEstBasis)) {
        payload.estimateMin = Number(addEstMin);
        payload.estimateMax = Number(addEstMax);
        payload.estimateBasis = addEstBasis.trim();
      } else if (estSome(addEstMin, addEstMax, addEstBasis)) {
        setNotice("an estimate is all-present or all-absent (D3) — min, max AND basis together, or none of them.");
        return;
      }
      await rpc(METHOD_ROADMAP_ADD, payload);
      setNotice(`added "${title.trim()}" at idea (its only door) — from ${provenance}.`);
      setTitle("");
      await load();
    } catch (e) {
      setError(toRpcError(e));
    }
  };

  const cancel = async (id: number) => {
    try {
      setError(null);
      await rpc(METHOD_ROADMAP_CANCEL, { id });
      setNotice(`#${id} cancelled — the row rests as its own outcome, never deleted (D7).`);
      await load();
    } catch (e) {
      setError(toRpcError(e));
    }
  };

  const writeEstimate = async () => {
    if (!selected || !estPresent(estMin, estMax, estBasis)) return;
    try {
      setError(null);
      setNotice("");
      await rpc(METHOD_ROADMAP_ESTIMATE, {
        id: selected.id,
        estimateMin: Number(estMin),
        estimateMax: Number(estMax),
        estimateBasis: estBasis.trim(),
      });
      setNotice(`#${selected.id} estimated ${estMin}–${estMax} (${estBasis.trim()}) — the trio whole (D3).`);
      await load();
    } catch (e) {
      setError(toRpcError(e));
    }
  };

  const writePriority = async () => {
    if (!selected || !priState) return;
    try {
      setError(null);
      setNotice("");
      const payload: Record<string, unknown> = { id: selected.id, priorityState: priState };
      if (priState !== "unknown" && priLabel.trim()) payload.priorityLabel = priLabel.trim();
      await rpc(METHOD_ROADMAP_PRIORITY, payload);
      setNotice(`#${selected.id} priority is now ${priState} — the label travels with the state (D2).`);
      await load();
    } catch (e) {
      setError(toRpcError(e));
    }
  };

  const dispatch = async (verb: string, payload: Record<string, unknown>, said: string) => {
    try {
      setError(null);
      setNotice("");
      const res = await rpc<RoadmapDispatchResult>(verb, payload);
      // The seam answers are the daemon's own words — rendered as they arrive.
      setNotice(`${said} — ${JSON.stringify(res)}`);
      await load();
    } catch (e) {
      setError(toRpcError(e));
    }
  };

  const ideate = (item: RoadmapItemView) => {
    if (!workspaceRoot.trim()) {
      setNotice("ideation runs a loop — name the workspace root it runs in.");
      return;
    }
    const payload: Record<string, unknown> = { id: item.id, workspaceRoot: workspaceRoot.trim() };
    if (ideateBlueprint.trim()) payload.blueprint = ideateBlueprint.trim();
    void dispatch(METHOD_ROADMAP_IDEATE, payload, `#${item.id} ideating`);
  };

  const plan = (item: RoadmapItemView) => {
    if (!workspaceRoot.trim()) {
      setNotice("planning runs the project-planner — name the workspace root.");
      return;
    }
    void dispatch(METHOD_ROADMAP_PLAN, { id: item.id, workspaceRoot: workspaceRoot.trim() }, `#${item.id} planning dispatched`);
  };

  const materialize = (item: RoadmapItemView) => {
    if (!matBlueprint.trim() || !matGoal.trim() || !workspaceRoot.trim()) {
      setNotice("materializing is a loop.start: blueprint, goal and workspace root, all named.");
      return;
    }
    const payload: Record<string, unknown> = {
      id: item.id,
      blueprint: matBlueprint.trim(),
      goal: matGoal.trim(),
      workspaceRoot: workspaceRoot.trim(),
    };
    if (matTech.trim()) payload.tech = matTech.trim();
    const withEntries: Record<string, string> = {};
    for (const line of matWith.split(/[;\n]/)) {
      const at = line.indexOf("=");
      if (at > 0) withEntries[line.slice(0, at).trim()] = line.slice(at + 1).trim();
    }
    if (Object.keys(withEntries).length > 0) payload.with = withEntries;
    // Attendedness is DECLARED, never inferred (D5/G14) — absent means the
    // project's declared default decides, never whoever is watching.
    if (matAttended !== "default") payload.attended = matAttended === "attended";
    void dispatch(METHOD_ROADMAP_MATERIALIZE, payload, `#${item.id} materializing`);
  };

  const select = (item: RoadmapItemView) => {
    setSelected(item);
    setEstMin("");
    setEstMax("");
    setEstBasis("");
    setPriState("");
    setPriLabel("");
  };

  return (
    <div className="stack">
      {error && <ErrorPanel error={error} onRetry={() => void load()} />}
      {notice && <Alert tone="slate">{notice}</Alert>}

      <Panel tone="slate" data-testid="roadmap-board">
        <FormField label="Component root">
          <Input
            value={root}
            onChange={(e) => setRoot(e.target.value)}
            placeholder="/srv/projects/ledger"
            aria-label="component root"
          />
        </FormField>
        <FormField label="Filters (the door's own — this page re-sorts nothing)">
          <Select value={filterStatus} onChange={(e) => setFilterStatus(e.target.value)} aria-label="status filter">
            <option value="">any status</option>
            {ROADMAP_STATUSES.map((s) => (
              <option key={s} value={s}>{s}</option>
            ))}
          </Select>
          <Select value={filterPriority} onChange={(e) => setFilterPriority(e.target.value)} aria-label="priority filter">
            <option value="">any priority</option>
            {ROADMAP_PRIORITY_STATES.map((p) => (
              <option key={p} value={p}>{p}</option>
            ))}
          </Select>
          <Input value={filterEpic} onChange={(e) => setFilterEpic(e.target.value)} placeholder="epic (exact tag)" aria-label="epic filter" />
        </FormField>
        <Button onClick={() => void load()} disabled={!root.trim()}>
          Load the board
        </Button>
        {items === null ? (
          <Alert tone="slate">No board loaded — the roadmap is per component root; name one.</Alert>
        ) : items.length === 0 ? (
          // D8: the empty board speaks, and says what does not happen.
          <Alert tone="slate">
            The roadmap of {root.trim()} is EMPTY — and loop.start never asked about any of this.
          </Alert>
        ) : (
          <table>
            <thead>
              <tr>
                <th>#</th>
                <th>status</th>
                <th>title</th>
                <th>epic</th>
                <th>priority</th>
                <th>estimate (min–max, basis)</th>
                <th>came from</th>
                <th>brief / plan / loop</th>
                <th>actions</th>
              </tr>
            </thead>
            <tbody>
              {items.map((item) => (
                <tr key={item.id} data-status={item.status} data-priority={item.priorityState}>
                  <td>
                    <button type="button" onClick={() => select(item)} data-testid={`select-${item.id}`}>
                      #{item.id}
                    </button>
                  </td>
                  <td>
                    {/* Eight distinct renderings; the label map is asserted distinct by test. */}
                    <Pill data-testid={`status-${item.id}`}>{STATUS_LABELS[item.status]}</Pill>
                  </td>
                  <td>{item.title}</td>
                  <td>{item.epic ?? <span className="text-muted">no epic — a tag, not a row (R2)</span>}</td>
                  <td>
                    <span data-testid={`priority-${item.id}`} data-state={item.priorityState}>
                      {priorityWords(item)}
                    </span>
                  </td>
                  <td>
                    <span data-testid={`estimate-${item.id}`} className={item.estimateMin === null ? "text-muted" : undefined}>
                      {estimateWords(item)}
                    </span>
                  </td>
                  <td>
                    {/* D5 rendered as words — where an item actually came from. */}
                    <span data-testid={`provenance-${item.id}`}>from {item.provenance}</span>
                  </td>
                  <td>
                    {item.briefPath ?? "— no brief"} / {item.planPath ?? "— no plan"} /{" "}
                    {item.loopSlug ? (
                      <>loop={item.loopSlug}</>
                    ) : (
                      <span className="text-muted">not materialized (D6 — the column is simply unset)</span>
                    )}
                  </td>
                  <td>
                    <span className="stack-inline">
                      {(item.status === "idea" || item.status === "ideating") && (
                        <Button onClick={() => ideate(item)} data-testid={`ideate-${item.id}`}>
                          Ideate
                        </Button>
                      )}
                      {item.status === "brief-agreed" && (
                        <Button onClick={() => plan(item)} data-testid={`plan-${item.id}`}>
                          Plan
                        </Button>
                      )}
                      {item.status === "ready" && (
                        <Button onClick={() => materialize(item)} data-testid={`materialize-${item.id}`}>
                          Materialize
                        </Button>
                      )}
                      {CANCELLABLE.includes(item.status) ? (
                        <Button onClick={() => void cancel(item.id)} data-testid={`cancel-${item.id}`}>
                          Cancel
                        </Button>
                      ) : (
                        // D7: a running item's cancel is the loop's own business;
                        // done/cancelled are terminal. The board does not offer it.
                        <span className="text-muted">—</span>
                      )}
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Panel>

      <Panel tone="slate" data-testid="roadmap-add">
        <FormField label="Title">
          <Input value={title} onChange={(e) => setTitle(e.target.value)} placeholder="Surface the retry counter" />
        </FormField>
        <FormField label="Epic (optional — R2: a tag on the row, never its own table)">
          <Input value={epic} onChange={(e) => setEpic(e.target.value)} placeholder="reliability" />
        </FormField>
        <FormField label="Provenance (D5: required — there is NO default; whoever writes the row answers)">
          <Select value={provenance} onChange={(e) => setProvenance(e.target.value as RoadmapProvenance)} aria-label="provenance">
            <option value="">— answer: where did this item come from? —</option>
            {ROADMAP_PROVENANCES.map((pv) => (
              <option key={pv} value={pv}>
                {pv} — {pv === "authored" ? "a person wrote it" : pv === "triggered" ? "a trigger produced it" : "a triage handed it off"}
              </option>
            ))}
          </Select>
        </FormField>
        <FormField label="Priority (D2: state and label travel together; absent means unknown — NOT low)">
          <Select value={addPriority} onChange={(e) => setAddPriority(e.target.value as RoadmapPriorityState)} aria-label="add priority state">
            <option value="">not stated — unknown</option>
            <option value="known">known</option>
            <option value="assumed">assumed</option>
          </Select>
          <Input
            value={addPriorityLabel}
            onChange={(e) => setAddPriorityLabel(e.target.value)}
            placeholder="label (meaningful only when stated)"
            aria-label="add priority label"
          />
        </FormField>
        <FormField label="Estimate (D3: min, max AND basis together — or none of them, never a bare number)">
          <Input value={addEstMin} onChange={(e) => setAddEstMin(e.target.value)} placeholder="min" aria-label="add estimate min" />
          <Input value={addEstMax} onChange={(e) => setAddEstMax(e.target.value)} placeholder="max" aria-label="add estimate max" />
          <Input value={addEstBasis} onChange={(e) => setAddEstBasis(e.target.value)} placeholder="basis (what the range stands on)" aria-label="add estimate basis" />
        </FormField>
        <Button onClick={() => void add()} disabled={!title.trim() || !provenance} data-testid="add-button">
          Add to the roadmap
        </Button>
      </Panel>

      <Panel tone="slate" data-testid="roadmap-update">
        {selected === null ? (
          <Alert tone="slate">Select a row's # to write it. An estimate moves as the trio whole (D3); a priority as state-with-label (D2).</Alert>
        ) : (
          <>
            <p>
              updating <strong>#{selected.id}</strong> — {selected.title}
            </p>
            <FormField label="Write an estimate (all three, or the button stays shut — D3)">
              <Input value={estMin} onChange={(e) => setEstMin(e.target.value)} placeholder="min" aria-label="estimate min" />
              <Input value={estMax} onChange={(e) => setEstMax(e.target.value)} placeholder="max" aria-label="estimate max" />
              <Input value={estBasis} onChange={(e) => setEstBasis(e.target.value)} placeholder="basis" aria-label="estimate basis" />
            </FormField>
            <Button onClick={() => void writeEstimate()} disabled={!estPresent(estMin, estMax, estBasis)} data-testid="write-estimate">
              Write the estimate
            </Button>
            <FormField label="Write a priority (one decision — moving to unknown clears the question, never a silent label)">
              <Select value={priState} onChange={(e) => setPriState(e.target.value as RoadmapPriorityState)} aria-label="priority state">
                <option value="">— choose —</option>
                {ROADMAP_PRIORITY_STATES.map((p) => (
                  <option key={p} value={p}>{p}</option>
                ))}
              </Select>
              <Input value={priLabel} onChange={(e) => setPriLabel(e.target.value)} placeholder="label (not for unknown)" aria-label="priority label" />
            </FormField>
            <Button onClick={() => void writePriority()} disabled={!priState} data-testid="write-priority">
              Write the priority
            </Button>
          </>
        )}
      </Panel>

      <Panel tone="slate" data-testid="roadmap-dispatch">
        <FormField label="Workspace root (shared by ideate / plan / materialize — the loop's workbench)">
          <Input value={workspaceRoot} onChange={(e) => setWorkspaceRoot(e.target.value)} placeholder="/srv/repos/ledger" aria-label="workspace root" />
        </FormField>
        <FormField label="Ideation blueprint (optional — must already declare produces: brief.md, or the door refuses by name; empty means brainstorm, the shipped brief-producer)">
          <Input value={ideateBlueprint} onChange={(e) => setIdeateBlueprint(e.target.value)} placeholder="(blank = brainstorm)" aria-label="ideation blueprint" />
        </FormField>
        <FormField label="Materialization payload (T4: the same loop.start facts either branch carries — queue or direct, decided by the project's own rows)">
          <Input value={matBlueprint} onChange={(e) => setMatBlueprint(e.target.value)} placeholder="blueprint" aria-label="materialize blueprint" />
          <Input value={matGoal} onChange={(e) => setMatGoal(e.target.value)} placeholder="goal (the loop's own text, verbatim)" aria-label="materialize goal" />
          <Input value={matTech} onChange={(e) => setMatTech(e.target.value)} placeholder="tech (optional)" aria-label="materialize tech" />
          <Input value={matWith} onChange={(e) => setMatWith(e.target.value)} placeholder="with entries (k=v; k=v, optional)" aria-label="materialize with" />
          <Select value={matAttended} onChange={(e) => setMatAttended(e.target.value as typeof matAttended)} aria-label="materialize attended">
            <option value="default">the project's declared default decides</option>
            <option value="attended">declared attended</option>
            <option value="unattended">declared unattended</option>
          </Select>
        </FormField>
        <Alert tone="slate">
          Dispatch buttons live on the rows, offered where the machine is at. A blocked ideation leaves the item
          ideating and re-dispatch is yours to press again; a not-ready materialization is refused at the attempt
          by name (D4 is decided now, never cached).
        </Alert>
      </Panel>
    </div>
  );
}

/** D3 helpers — the trio whole, or the field set absent; half is refused at the form as at the door. */
function estPresent(min: string, max: string, basis: string): boolean {
  return min.trim() !== "" && max.trim() !== "" && basis.trim() !== "";
}

function estSome(min: string, max: string, basis: string): boolean {
  return min.trim() !== "" || max.trim() !== "" || basis.trim() !== "";
}
