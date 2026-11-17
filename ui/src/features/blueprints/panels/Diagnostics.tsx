import { Pill } from "../../../components/kit";
import { ValidationNotice } from "../../../lib/uncertain";

// Diagnostics.tsx — the validator's report, drawn.
//
// Lifted out of `BlueprintsScreen.tsx` by S48 T12 for cohesion: the report is
// rendered by the screen's summary column AND by the drawer's edit tab, and the
// file that renders it should be one thing that does that and nothing else.
//
// The label map is the app's vocabulary for a diagnostic KIND — the daemon
// answers kinds, the app names them in a person's words. It is deliberately
// pinned by `test/blueprints-screen.test.tsx` and `test/product-recovery.test.tsx`:
// no label may point at a closed slice as the future. The daemon's own sentence
// (`message`) rides beneath the label, verbatim — the label says what kind of
// finding this is, the message says what the daemon actually found, and dropping
// either would make the report harder to trust, not easier.

export interface Diagnostic {
  state: "valid" | "invalid" | "unverifiable";
  kind: string;
  path: string;
  message: string;
  hint?: string;
  slice?: string;
}

export interface Report {
  blueprint: string;
  validCount: number;
  diagnostics: Diagnostic[];
}

export const diagnosticLabels: Record<string, string> = {
  "unknown-key": "Unknown flow field", yaml: "Flow YAML cannot be parsed",
  required: "Required field is missing", "schema-version": "Unsupported flow schema version",
  "duplicate-lane": "Lane id is duplicated", "unknown-abort-handler": "Abort handler lane is absent",
  "lane-shape": "Lane must contain one step or a ladder", "advisory-route": "Advisory lane cannot route rejects",
  "iterate-field": "Iteration needs a collection field", "iterate-shape": "Iteration and ladder must be declared together",
  "duplicate-step": "Step id is duplicated", "unknown-rework": "Rework target is outside this ladder",
  "step-shape": "Step has fields for another kind",
  // S13 closed 2026-10-25 and S16 closed 2026-10-26: a deferral names the
  // PRESENT — a body S13 recorded as a follow-up, and a validator at rest
  // without the closed list. The dead "resolver-registry" kind (retired by
  // D13's flip) has no entry, so it falls to the honest generic label.
  "agent-registry": "Agent body not shipped — S13 named it a follow-up", "unknown-agent": "Agent is not registered",
  "action-registry": "Code action registry not given to this validator", "unknown-question": "Question is not declared",
  "unknown-kind": "Step kind is unsupported", "machine-status": "Status is not in the task machine",
  "machine-transition": "Status cannot occur in this mode", "missing-then": "Capped edge needs a then target",
  "unbounded-back-edge": "Back edge needs a bound", "unknown-counter": "Counter is not declared",
  "unknown-goto": "Go-to lane is absent", "empty-condition": "Condition is empty",
  "enum-value": "Condition value is outside its enum", "invalid-operator": "Condition operator is invalid",
  "type-mismatch": "Condition compares incompatible types", "unknown-field": "Condition field is not declared",
};

export function diagnosticLabel(kind: string): string {
  return diagnosticLabels[kind] ?? `Flow diagnostic: ${kind.replaceAll("-", " ")}`;
}

export function Diagnostics({ report, strict }: { report: Report; strict: boolean }) {
  const invalid = report.diagnostics.filter((item) => item.state === "invalid");
  const unverifiable = report.diagnostics.filter((item) => item.state === "unverifiable");
  const verdict = invalid.length || (strict && unverifiable.length) ? "fails validation" : "passes validation";
  return <div className="space-y-4" data-testid="blueprint-diagnostics">
    <p className="font-medium">{report.blueprint}: {verdict}{strict ? " (strict)" : ""}</p>
    <div className="flex flex-wrap gap-3"><Pill tone="green">valid {report.validCount}</Pill>
      <Pill tone="slate">unverifiable {unverifiable.length}</Pill><Pill tone="red">invalid {invalid.length}</Pill></div>
    {report.diagnostics.length === 0 ? <p className="text-sm text-slate-600">No diagnostics.</p> :
      <ol className="space-y-2">{report.diagnostics.map((item, index) =>
        <li key={`${item.path}-${index}`} className="rounded-lg border border-white/10 p-3 text-sm" data-testid={`diagnostic-${index}`}>
          <code className="break-all aeon-accent-text">{item.path}</code><br />
          <ValidationNotice state={item.state} message={diagnosticLabel(item.kind)} slice={item.slice} />
          {item.message && item.message !== diagnosticLabel(item.kind) && (
            <p className="mt-1 text-slate-400" data-testid={`diagnostic-message-${index}`}>{item.message}</p>
          )}
          {item.hint && <p className="mt-1 text-slate-600">{item.hint}</p>}
        </li>)}</ol>}
  </div>;
}
