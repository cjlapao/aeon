import { useCallback, useEffect, useState } from "react";
import { Alert, Button, FormField, Input, Panel, Pill, Select } from "../../../components/kit";
import { rpc, toRpcError, type RpcError } from "../../../lib/bridge";
import { ErrorPanel } from "../../../components/ErrorPanel";
import {
  METHOD_TRIAGE_FILE,
  METHOD_TRIAGE_LIST,
  METHOD_TRIAGE_SET_PRIORITY,
  METHOD_TRIAGE_CONFIRM_DUPLICATE,
  METHOD_TRIAGE_VALIDATE,
  METHOD_TRIAGE_HANDOFF,
  METHOD_TRIAGE_SUGGEST,
  TRIAGE_VALIDATION_STATES,
  TRIAGE_PRIORITY_STATES,
  type TriageDispatchResult,
  type TriageListResult,
  type TriagePriorityState,
  type TriageView,
} from "../../../lib/protocol";

/**
 * The triage bench (S29, plus the user-requested full CRUD) — the bug
 * backlog's lab door: every one of the eight `triage.*` socket verbs,
 * through the generic `rpc()` bridge and no new Tauri command (§4b).
 *
 * # The CRUD is the machine's own verbs, not an invented one
 *
 * Create is `triage.file` (D7's self-file: validation arrives
 * **unvalidated** — the schema carries no state from the model, so the
 * form asks a person to file, and a priority a person decided may ride
 * but a machine's may not); Read is `triage.list` through the door's own
 * filters (validation/priority — never a combined badge, R4); Update is
 * `triage.set-priority` (state-with-label whole, D2); the dedup is
 * `triage.suggest-duplicates` (a read — the detector never writes
 * `duplicate_of`) and `triage.confirm-duplicate` (D5/N3: a person's act,
 * the one write path). The two dispatches — `triage.validate` (T3) and
 * `triage.handoff` (T6/D8) — are the bug's rungs, offered where the
 * machine is at: a validated, prioritized bug gets a hand-off button.
 *
 * # The two states render apart (R4)
 *
 * `validation` (D1's four) and `priorityState` (D2's three) are TWO
 * separate closed sets. The row renders each in its OWN cell — a single
 * combined badge would collapse the machine and hide `unknown`. `unknown`
 * priority says "unknown", never blank; `unvalidated` never reads as
 * `could-not-tell`.
 *
 * # What this page is NOT
 *
 * It is a bench, not the product, and it calls nothing the CLI cannot —
 * both are secondary clients of the same daemon surface (P34).
 */

const VALIDATION_LABELS: Record<TriageView["validation"], string> = {
  unvalidated: "unvalidated — the attempt has not run",
  reproduced: "reproduced",
  "not-reproduced": "not-reproduced",
  "could-not-tell": "could-not-tell",
};

export function TriagePage() {
  const [root, setRoot] = useState("");
  const [bugs, setBugs] = useState<TriageView[] | null>(null);
  const [error, setError] = useState<RpcError | null>(null);
  const [notice, setNotice] = useState("");

  const [filterValidation, setFilterValidation] = useState("");
  const [filterPriority, setFilterPriority] = useState("");

  // The file form (D7).
  const [title, setTitle] = useState("");
  const [body, setBody] = useState("");
  const [filedBy, setFiledBy] = useState("");
  const [filedPriority, setFiledPriority] = useState<"" | TriagePriorityState>("");
  const [filedPriorityLabel, setFiledPriorityLabel] = useState("");

  // The selected row + its update panel.
  const [selected, setSelected] = useState<TriageView | null>(null);
  const [priState, setPriState] = useState<"" | TriagePriorityState>("");
  const [priLabel, setPriLabel] = useState("");
  const [dupOf, setDupOf] = useState("");
  const [confirmedBy, setConfirmedBy] = useState("");

  const load = useCallback(async () => {
    if (!root.trim()) return;
    try {
      setError(null);
      const params: Record<string, unknown> = { componentRoot: root.trim() };
      if (filterValidation) params.validationState = filterValidation;
      if (filterPriority) params.priorityState = filterPriority;
      const res = await rpc<TriageListResult>(METHOD_TRIAGE_LIST, params);
      setBugs(res.items);
    } catch (e) {
      setError(toRpcError(e));
    }
  }, [root, filterValidation, filterPriority]);

  useEffect(() => {
    setBugs(null);
    setSelected(null);
  }, [root]);

  const file = async () => {
    try {
      setError(null);
      setNotice("");
      const payload: Record<string, unknown> = {
        componentRoot: root.trim(),
        title: title.trim(),
        body: body.trim(),
        // D7: filedBy is required — a bug names who filed it.
        filedBy: filedBy.trim(),
      };
      if (filedPriority) {
        payload.priorityState = filedPriority;
        // D2: a label rides only when the state is not unknown.
        if (filedPriority !== "unknown" && filedPriorityLabel.trim()) payload.priorityLabel = filedPriorityLabel.trim();
      }
      const res = await rpc<TriageView>(METHOD_TRIAGE_FILE, payload);
      setNotice(`#${res.id} filed — validation UNVALIDATED (the attempt has not run; D1/D7).`);
      setTitle("");
      setBody("");
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
      const payload: Record<string, unknown> = { id: selected.id, componentRoot: selected.componentRoot, state: priState };
      if (priState !== "unknown" && priLabel.trim()) payload.label = priLabel.trim();
      await rpc(METHOD_TRIAGE_SET_PRIORITY, payload);
      setNotice(`#${selected.id} priority is now ${priState} — the label travels with the state (D2).`);
      await load();
    } catch (e) {
      setError(toRpcError(e));
    }
  };

  const confirmDuplicate = async () => {
    if (!selected || !dupOf.trim() || !confirmedBy.trim()) return;
    try {
      setError(null);
      setNotice("");
      await rpc(METHOD_TRIAGE_CONFIRM_DUPLICATE, {
        id: selected.id,
        componentRoot: selected.componentRoot,
        of: Number(dupOf),
        confirmedBy: confirmedBy.trim(),
      });
      setNotice(`#${selected.id} is a duplicate of #${dupOf} — confirmed by ${confirmedBy.trim()} (D5/N3: a person's act, the one write path).`);
      setDupOf("");
      setConfirmedBy("");
      await load();
    } catch (e) {
      setError(toRpcError(e));
    }
  };

  const dispatch = async (verb: string, id: number, said: string) => {
    try {
      setError(null);
      setNotice("");
      const res = await rpc<TriageDispatchResult>(verb, { id });
      setNotice(`${said} — ${JSON.stringify(res)}`);
      await load();
    } catch (e) {
      setError(toRpcError(e));
    }
  };

  const suggest = async (b: TriageView) => {
    try {
      setError(null);
      setNotice("");
      const res = await rpc<TriageView[]>(METHOD_TRIAGE_SUGGEST, { id: b.id });
      if (res.length === 0) {
        setNotice(`no candidates within ${b.componentRoot} — the detector suggests, it never sets duplicate_of (D5/N3); a human confirms.`);
      } else {
        setNotice(`candidates for #${b.id} (a suggestion only — duplicate_of is set only by a human confirmation): ${res.map((c) => `#${c.id}`).join(", ")}`);
      }
    } catch (e) {
      setError(toRpcError(e));
    }
  };

  const select = (b: TriageView) => {
    setSelected(b);
    setPriState("");
    setPriLabel("");
    setDupOf("");
    setConfirmedBy("");
  };

  return (
    <div className="stack">
      {error && <ErrorPanel error={error} onRetry={() => void load()} />}
      {notice && <Alert tone="slate">{notice}</Alert>}

      <Panel tone="slate" data-testid="triage-backlog">
        <FormField label="Component root">
          <Input
            value={root}
            onChange={(e) => setRoot(e.target.value)}
            placeholder="/srv/projects/ledger"
            aria-label="component root"
          />
        </FormField>
        <FormField label="Filters (the door's own — validation and priority are SEPARATE, never a badge — R4)">
          <Select value={filterValidation} onChange={(e) => setFilterValidation(e.target.value)} aria-label="validation filter">
            <option value="">any validation</option>
            {TRIAGE_VALIDATION_STATES.map((s) => (
              <option key={s} value={s}>{s}</option>
            ))}
          </Select>
          <Select value={filterPriority} onChange={(e) => setFilterPriority(e.target.value)} aria-label="priority filter">
            <option value="">any priority</option>
            {TRIAGE_PRIORITY_STATES.map((p) => (
              <option key={p} value={p}>{p}</option>
            ))}
          </Select>
        </FormField>
        <Button onClick={() => void load()} disabled={!root.trim()}>
          Load the backlog
        </Button>
        {bugs === null ? (
          <Alert tone="slate">No backlog loaded — the bug backlog is per component root; name one.</Alert>
        ) : bugs.length === 0 ? (
          <Alert tone="slate">The backlog of {root.trim()} is EMPTY — a filter that matches nothing is the filter's answer, not the store's.</Alert>
        ) : (
          <table>
            <thead>
              <tr>
                <th>#</th>
                <th>validation (D1)</th>
                <th>priority (D2)</th>
                <th>title</th>
                <th>filed by</th>
                <th>dup / hand-off</th>
                <th>actions</th>
              </tr>
            </thead>
            <tbody>
              {bugs.map((b) => (
                <tr key={b.id} data-validation={b.validation} data-priority={b.priorityState}>
                  <td>
                    <button type="button" onClick={() => select(b)} data-testid={`select-${b.id}`}>
                      #{b.id}
                    </button>
                  </td>
                  {/* R4: validation and priority are TWO separate cells — never a badge. */}
                  <td>
                    <Pill data-testid={`validation-${b.id}`}>{VALIDATION_LABELS[b.validation]}</Pill>
                  </td>
                  <td>
                    <span data-testid={`priority-${b.id}`} data-state={b.priorityState}>
                      {b.priorityState}{b.priorityLabel ? ` (${b.priorityLabel})` : ""}
                    </span>
                  </td>
                  <td>{b.title}</td>
                  <td>{b.filedBy}</td>
                  <td>
                    {b.duplicateOf != null ? `dup of #${b.duplicateOf}` : b.roadmapItemId != null ? `→ roadmap #${b.roadmapItemId}` : "—"}
                  </td>
                  <td>
                    <span className="stack-inline">
                      <Button onClick={() => void suggest(b)} data-testid={`suggest-${b.id}`}>
                        Suggest dupes
                      </Button>
                      {b.validation === "unvalidated" && (
                        <Button onClick={() => void dispatch(METHOD_TRIAGE_VALIDATE, b.id, `#${b.id} validating`)} data-testid={`validate-${b.id}`}>
                          Validate
                        </Button>
                      )}
                      {b.validated && b.priorityState !== "unknown" && b.roadmapItemId == null && (
                        <Button onClick={() => void dispatch(METHOD_TRIAGE_HANDOFF, b.id, `#${b.id} handed off`)} data-testid={`handoff-${b.id}`}>
                          Hand off
                        </Button>
                      )}
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Panel>

      <Panel tone="slate" data-testid="triage-file">
        <FormField label="Title">
          <Input value={title} onChange={(e) => setTitle(e.target.value)} placeholder="Surface the retry counter" />
        </FormField>
        <FormField label="Body (how to reproduce)">
          <Input value={body} onChange={(e) => setBody(e.target.value)} placeholder="steps to reproduce the bug" />
        </FormField>
        <FormField label="Filed by (D7: an agent name or 'person' — required)">
          <Input value={filedBy} onChange={(e) => setFiledBy(e.target.value)} placeholder="golang-engineer | person" />
        </FormField>
        <FormField label="Priority (D2: a person's decision; absent means unknown — NOT low. The model path may not state it)">
          <Select value={filedPriority} onChange={(e) => setFiledPriority(e.target.value as TriagePriorityState)} aria-label="filed priority state">
            <option value="">not stated — unknown</option>
            <option value="known">known</option>
            <option value="assumed">assumed</option>
          </Select>
          <Input
            value={filedPriorityLabel}
            onChange={(e) => setFiledPriorityLabel(e.target.value)}
            placeholder="label (meaningful only when not unknown)"
            aria-label="filed priority label"
          />
        </FormField>
        <Button
          onClick={() => void file()}
          disabled={!title.trim() || !body.trim() || !filedBy.trim()}
          data-testid="file-button"
        >
          File the bug
        </Button>
      </Panel>

      <Panel tone="slate" data-testid="triage-update">
        {selected === null ? (
          <Alert tone="slate">Select a row's # to write it. A priority moves as state-with-label (D2); a duplicate confirmation is a person's act (D5/N3).</Alert>
        ) : (
          <>
            <p>
              updating <strong>#{selected.id}</strong> — {selected.title}
            </p>
            <FormField label="Write a priority (one decision — moving to unknown clears the label, never a silent one)">
              <Select value={priState} onChange={(e) => setPriState(e.target.value as TriagePriorityState)} aria-label="priority state">
                <option value="">— choose —</option>
                {TRIAGE_PRIORITY_STATES.map((p) => (
                  <option key={p} value={p}>{p}</option>
                ))}
              </Select>
              <Input value={priLabel} onChange={(e) => setPriLabel(e.target.value)} placeholder="label (not for unknown)" aria-label="priority label" />
            </FormField>
            <Button onClick={() => void writePriority()} disabled={!priState} data-testid="write-priority">
              Write the priority
            </Button>
            <FormField label="Confirm a duplicate (D5/N3: a person's act — the detector only suggests; this is the one write path)">
              <Input value={dupOf} onChange={(e) => setDupOf(e.target.value)} placeholder="id of the bug this is a duplicate of" aria-label="duplicate of" />
              <Input value={confirmedBy} onChange={(e) => setConfirmedBy(e.target.value)} placeholder="confirmed by (a person)" aria-label="confirmed by" />
            </FormField>
            <Button onClick={() => void confirmDuplicate()} disabled={!dupOf.trim() || !confirmedBy.trim()} data-testid="confirm-duplicate">
              Confirm the duplicate
            </Button>
          </>
        )}
      </Panel>
    </div>
  );
}
