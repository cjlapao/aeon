import { useCallback, useEffect, useState } from "react";
import { Alert, Button, FormField, Input, Panel, Pill, Select } from "../../../components/kit";
import { rpc, toRpcError, type RpcError } from "../../../lib/bridge";
import { ErrorPanel } from "../../../components/ErrorPanel";
import { useFact } from "../lib/fact";
import { FactCell } from "../lib/FactCell";
import { ProposalRowView } from "../lib/ProposalRowView";
import type { ProposalRow } from "../lib/proposals";
import type { RpcMethods } from "../../../proto/contract";

/**
 * Policy state, where it will be consumed (S10, T14).
 *
 * # What this page is, and the one thing it is not
 *
 * Ruling R4 (and N2 of the slice's brief: *"No authoring UI"*) gives the app a
 * **window**, not a keyboard. The four sections below call the four read-only
 * socket methods T13 registered — `policy.list`, `policy.why`, `policy.inForce`,
 * `policy.events` — through the **generic `rpc()` bridge**, so this slice adds
 * no Rust command (AGENTS §4b's surviving rule: a command per method is a
 * second method table somebody forgets to update). There is no form element, no
 * editor, and no authoring method of any kind reachable from the web layer —
 * the two write verbs live in the CLI and nowhere else (PL15/D16), and
 * `test/policy.test.tsx` asserts both against the DOM and against every method
 * name this page ever sends.
 *
 * # The strings here are not this page's words
 *
 * `verdict.go` is **the one formatter in the product** (T4). `policy.why`'s
 * `sentence` and `policy.inForce`'s `render` arrive already rendered by Go, and
 * this file prints them **whole and unedited** — no trimming, no re-casing, no
 * re-segmenting, no substituting its own prefix. A status Pill beside a sentence
 * is allowed precisely because it is *additive* and the sentence stays present
 * in full; a surface that turns `refused:` into `DENIED` has built a second
 * formatter, and the four surfaces drift exactly the way that rule prevents.
 *
 * The one place a client decision is allowed is `unimplemented` (SEC-033):
 * *"nobody can answer this yet"* and *"a rule refused you"* share the
 * `refused: ` prefix, so branching on the **field** — never on a prefix test
 * against the sentence — is what keeps D10's third answer from
 * collapsing into the denial it is not. `ui/src/proto/contract.ts` carries the
 * field because `conformance/policy/why-unimplemented.json` shows it; the type
 * here is read from that generated contract rather than re-declared, so the page
 * cannot drift from the wire without failing `make check-protogen`.
 *
 * # Four sections, four independent loads
 *
 * Each panel owns its own fetch and its own error. A `policy.list` that the
 * boundary refuses (a row that escaped the write boundary — SEC-015's read-side
 * discipline) must not blank the other three, which is how the Store tab once
 * took the whole window with it. Nothing here writes, and **nothing policy-shaped
 * enters `localStorage` or any browser storage** (§4c; the rule
 * `test/no-socket-in-web-layer.test.ts` guards).
 */

/** The generated wire types, read off the fixture-derived contract. */
type PolicyListResult = RpcMethods["policy.list"]["result"];
type PolicyWhyResult = RpcMethods["policy.why"]["result"];
type PolicyInForceResult = RpcMethods["policy.inForce"]["result"];
type PolicyEventsResult = RpcMethods["policy.events"]["result"];

/**
 * The seven types `types.go` closes (PL3).
 *
 * This is a **query** list, not an authoring list: it exists so the Why-checker
 * can ask about one of the seven instead of free-typing a string. A name outside
 * it cannot be smuggled through — `policy.why` parses the type at the boundary
 * and refuses anything else by name.
 */
const POLICY_TYPES = [
  "tool_access",
  "command_access",
  "route_access",
  "effect_class",
  "cost_cap",
  "skill_access",
  "trigger_access",
] as const;

/**
 * Which param carries "the thing being asked about", per type.
 *
 * Mirrors `protocol.PolicyWhyParams` field for field (`tool`, `command`,
 * `route`, `effect`, `cost`, `skill`, `trigger`) rather than inventing one
 * free-text field: what a value MEANS belongs to the type that owns it, and the
 * boundary owns the parsing of it (only the identity dimensions pass the
 * identifier alphabet — a command with a space in it is a real question).
 */
const VALUE_FIELD_BY_TYPE: Record<(typeof POLICY_TYPES)[number], string> = {
  tool_access: "tool",
  command_access: "command",
  route_access: "route",
  effect_class: "effect",
  cost_cap: "cost",
  skill_access: "skill",
  trigger_access: "trigger",
};

const DEFAULT_EVENTS_DEPTH = 20;

export function PolicyPage() {
  return (
    <div className="space-y-4" data-testid="policy-page">
      <Alert
        tone="slate"
        title="Policies, as the daemon decides with them"
        description="Read-only: this window shows what the boundary answers. Authoring is the CLI (`aeon policy add|remove`), because a surface that can widen a guard is a surface a caller can widen the guard written to constrain it (PL15). Every sentence below is the daemon's own rendering, printed verbatim — this page adds none of its own."
        data-testid="policy-note"
      />
      <PoliciesPanel />
      <ProposedPolicyPanel />
      <WhyPanel />
      <InForcePanel />
      <EventsPanel />
    </div>
  );
}

// --- 1. Policies ---------------------------------------------------------------

/**
 * The stored set, and the empty state that **speaks** (D4).
 *
 * An empty table is not `0`. The daemon carries the default-deny sentence in
 * `statement` for exactly this moment, and the page prints it VERBATIM —
 * a guard whose empty state reads as "fine" is the guard that fails open in the
 * reader's head. The two empty branches below are different facts: `[]` says
 * "read and empty", and an empty set with no sentence says "an older daemon
 * answered", which the page refuses to paraphrase on the daemon's behalf.
 */
function PoliciesPanel() {
  const [res, setRes] = useState<PolicyListResult | null>(null);
  const [error, setError] = useState<RpcError | null>(null);
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    setBusy(true);
    try {
      setRes(await rpc<PolicyListResult>("policy.list", {}));
      setError(null);
    } catch (cause) {
      setError(toRpcError(cause));
    } finally {
      setBusy(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const policies = res?.policies ?? [];

  return (
    <Panel tone="slate">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <p className="text-xs uppercase tracking-widest text-slate-400">S10 · policy</p>
          <h2 className="mt-1 text-lg font-semibold">Stored policies</h2>
        </div>
        <Button size="sm" variant="ghost" onClick={() => void load()} disabled={busy} data-testid="policies-refresh">
          {busy ? "loading…" : "refresh"}
        </Button>
      </div>

      {error && (
        <div className="mt-3" data-testid="policies-error">
          <ErrorPanel error={error} />
        </div>
      )}

      {res === null && !error && <p className="mt-3 text-sm opacity-60" data-testid="policies-loading">reading the policy table…</p>}

      {res !== null && policies.length === 0 && (
        res.statement !== undefined ? (
          // D4, verbatim. Not "0 policies", and not this page's wording.
          <Alert tone="amber" title="Nothing is granted" description={res.statement} data-testid="policies-empty" />
        ) : (
          <Alert
            tone="red"
            title="The set is empty and the daemon sent no default-deny sentence"
            description="This app prints the daemon's sentence rather than writing its own, so an older daemon that answers without one is reported as the version skew it is."
            data-testid="policies-empty-unstated"
          />
        )
      )}

      {policies.length > 0 && (
        <ul className="mt-3 space-y-2" data-testid="policies-list">
          {policies.map((p) => (
            <li key={p.id} className="flex flex-wrap items-center gap-2 text-sm" data-testid={`policy-${p.id}`}>
              <code className="font-medium">{p.id}</code>
              <Pill tone="slate" size="sm">{p.type}</Pill>
              <span className="font-mono text-xs opacity-70">{p.target}</span>
              <Pill tone={p.action === "deny" ? "red" : "green"} size="sm">{p.action}</Pill>
              <Pill tone={p.source === "operator" ? "violet" : "amber"} size="sm">source {p.source}</Pill>
              {/* PL7: no expiry is SIGNED, never skipped — a silent gap reads
                  as a lifetime nobody checked. */}
              <span className="font-mono text-xs opacity-70" data-testid={`policy-${p.id}-expiry`}>
                {p.notAfter === null ? "no expiry" : p.notAfter}
              </span>
            </li>
          ))}
        </ul>
      )}
    </Panel>
  );
}

// --- 1b. Proposed (S35 T7) ------------------------------------------------------

/**
 * A SEPARATE, visually distinct table beneath the live one — never merged
 * with `PoliciesPanel` above (TD8: live and proposed never render in one
 * list; a proposed rule is visibly a request, not a rule). Reuses
 * `ProposalRowView`, the SAME row rendering the desk (`ProposalsPage`)
 * uses, rather than a second implementation of it.
 */
function ProposedPolicyPanel() {
  const proposals = useFact("proposal.list", {});
  const policies = useFact("policy.list", {});

  const livePolicyIds =
    policies.fact.state === "ok" ? (policies.fact.value as PolicyListResult).policies.map((p) => p.id) : undefined;

  return (
    <Panel tone="amber" data-testid="policy-proposed-panel">
      <div className="flex items-center justify-between gap-3">
        <h3 className="text-sm font-semibold">Proposed — not yet policy</h3>
        <Button size="sm" variant="ghost" onClick={() => proposals.refetch()} data-testid="policy-proposed-refresh">
          refresh
        </Button>
      </div>
      <FactCell fact={proposals.fact}>
        {(value) => {
          const rows = ((value as unknown as { proposals: ProposalRow[] }).proposals ?? []).filter(
            (p) => p.status === "proposed" && (p.kind === "policy_add" || p.kind === "policy_remove" || p.kind === "seed_corpus"),
          );
          if (rows.length === 0) {
            return <p className="mt-3 text-sm text-slate-500" data-testid="policy-proposed-empty">no open policy proposals</p>;
          }
          return (
            <ul className="mt-3 space-y-3" data-testid="policy-proposed-list">
              {rows.map((row) => (
                <ProposalRowView key={row.id} row={row} livePolicyIds={livePolicyIds} />
              ))}
            </ul>
          );
        }}
      </FactCell>
    </Panel>
  );
}

// --- 2. The Why-checker --------------------------------------------------------

/**
 * "Why was this refused?" — a question, so it may only ever **ask**.
 *
 * D9: `policy.why` explains and writes NOTHING (the socket's own test pins the
 * boundary at zero writes). Three inputs therefore query: the identity the
 * decision is about, the type asked about, and the thing being asked about. The
 * Check button calls a read; it is not an authoring affordance and the page's
 * no-form test says so against the DOM.
 *
 * `unimplemented` is the branch (SEC-033). The status Pill is additive and the
 * sentence is printed whole, monospace, in a node whose text content is **only**
 * the sentence — so "the app re-formatted it" has nowhere to hide.
 */
function WhyPanel() {
  const [agent, setAgent] = useState("");
  // The wire's `policy.why` takes all four identity dimensions at once
  // (`internal/protocol/policy.go`'s `PolicyWhyParams` — unlike `in-force`,
  // which names exactly one party, `why` lets a caller ask as the CLI's own
  // `--agent/--loop/--lane-type/--mcp` flags do). A policy targeted at a
  // lane-type or an MCP server has NO other way to be exercised from this
  // page without these: `agent` alone cannot ask the question a `lane-type:`
  // or `mcp:` target answers.
  const [loop, setLoop] = useState("");
  const [laneType, setLaneType] = useState("");
  const [mcp, setMcp] = useState("");
  const [type, setType] = useState<string>(POLICY_TYPES[0]);
  const [subject, setSubject] = useState("");
  const [res, setRes] = useState<PolicyWhyResult | null>(null);
  const [error, setError] = useState<RpcError | null>(null);
  const [busy, setBusy] = useState(false);

  const identityGiven = agent !== "" || loop !== "" || laneType !== "" || mcp !== "";

  const check = useCallback(async () => {
    setBusy(true);
    try {
      // The params are what the caller typed — no defaulting of the identity,
      // because an explanation about nobody is not an explanation. Each
      // identity dimension travels only when given: a blank one is ABSENT
      // rather than "", the same rule the asked-about value follows below.
      const params: Record<string, string> = { type };
      if (agent !== "") params.agent = agent;
      if (loop !== "") params.loop = loop;
      if (laneType !== "") params.laneType = laneType;
      if (mcp !== "") params.mcp = mcp;
      if (subject !== "") params[VALUE_FIELD_BY_TYPE[type as (typeof POLICY_TYPES)[number]]] = subject;
      setRes(await rpc<PolicyWhyResult>("policy.why", params));
      setError(null);
    } catch (cause) {
      setRes(null);
      setError(toRpcError(cause));
    } finally {
      setBusy(false);
    }
  }, [agent, loop, laneType, mcp, type, subject]);

  const verdict = res === null ? null : whyStatus(res);

  return (
    <Panel>
      <div className="flex flex-wrap items-end gap-3">
        <div className="min-w-40 flex-1">
          <FormField label="agent (who is asking)">
            <Input
              value={agent}
              onChange={(e) => setAgent(e.target.value)}
              placeholder="boss"
              data-testid="why-agent"
            />
          </FormField>
        </div>
        <div className="min-w-40 flex-1">
          <FormField label="loop (optional — a loop-scoped identity)">
            <Input value={loop} onChange={(e) => setLoop(e.target.value)} placeholder="s10-policy" data-testid="why-loop" />
          </FormField>
        </div>
        <div className="min-w-40 flex-1">
          <FormField label="lane-type (optional — a lane-type target)">
            <Input value={laneType} onChange={(e) => setLaneType(e.target.value)} placeholder="review" data-testid="why-lane-type" />
          </FormField>
        </div>
        <div className="min-w-40 flex-1">
          <FormField label="mcp (optional — an mcp-server target)">
            <Input value={mcp} onChange={(e) => setMcp(e.target.value)} placeholder="webhooks" data-testid="why-mcp" />
          </FormField>
        </div>
        <div className="min-w-40 flex-1">
          <FormField label="type (the closed seven)">
            <Select value={type} onChange={(e) => setType(e.target.value)} data-testid="why-type">
              {POLICY_TYPES.map((t) => <option key={t} value={t}>{t}</option>)}
            </Select>
          </FormField>
        </div>
        <div className="min-w-40 flex-1">
          <FormField label="asked about (tool / command / route …)">
            <Input
              value={subject}
              onChange={(e) => setSubject(e.target.value)}
              placeholder="bash"
              data-testid="why-subject"
            />
          </FormField>
        </div>
        <Button size="sm" onClick={() => void check()} disabled={busy || !identityGiven || type === ""} data-testid="why-check">
          {busy ? "asking…" : "check"}
        </Button>
      </div>
      <p className="mt-1 text-xs opacity-60">
        This queries. It asks the same question <code>aeon policy why</code> asks, and prints the same
        sentence — <code>policy.why</code> writes nothing, so nothing here can change what is enforced.
        Give at least one identity — a lane-type or mcp target can only be reached that way, since no
        policy governs nobody.
      </p>

      {error && (
        <div className="mt-3" data-testid="why-error">
          <ErrorPanel error={error} />
        </div>
      )}

      {res !== null && verdict !== null && (
        <div className="mt-3 space-y-2" data-testid="why-answer">
          <div className="flex flex-wrap items-center gap-2 text-sm">
            <Pill tone={verdict.tone} size="sm" data-testid="why-verdict" data-tone={verdict.tone}>
              {verdict.label}
            </Pill>
            {res.unimplemented === true && (
              // SEC-033: the FIELD decides, never the sentence's prefix.
              <span className="text-xs opacity-70" data-testid="why-unimplemented">
                the engine cannot decide this type yet (D10) — not a denial
              </span>
            )}
          </div>
          {/* The daemon's sentence, whole. Its text content is the payload and
              nothing else: no prefix, no quotes, no trim, no re-case. */}
          <p className="whitespace-pre-wrap font-mono text-sm" data-testid="why-sentence">{res.sentence}</p>
        </div>
      )}
    </Panel>
  );
}

/**
 * The status beside the sentence — additive, and branched on the FIELD.
 *
 * Three answers, three labels: D10's "the binary cannot decide" is kept apart
 * from the denial it resembles, which is the whole SEC-033 argument.
 */
function whyStatus(res: PolicyWhyResult): { label: string; tone: "green" | "red" | "violet" } {
  if (res.unimplemented === true) return { label: "cannot decide yet", tone: "violet" };
  return res.allowed ? { label: "allowed", tone: "green" } : { label: "refused", tone: "red" };
}

// --- 3. In force ---------------------------------------------------------------

/**
 * The three states, none allowed to collapse into another (D14 / P31).
 *
 * `render` is T11's sentence VERBATIM — the page shows the daemon's answer and
 * never substitutes its own for it. The `state` value rides beside it as a Pill
 * whose tone differs per state, so the three are visibly three to somebody
 * scanning; the text of both stays the daemon's.
 *
 * A state this build does not know is its own tone rather than a guess at one of
 * the three: the failure mode this panel exists to prevent is precisely a reader
 * (or a renderer) folding "nobody can answer" into "not binding".
 */
function inForceTone(state: string): "green" | "amber" | "red" | "violet" {
  switch (state) {
    case "enforced_acknowledged": return "green";
    case "enforced_unacknowledged": return "amber";
    case "could_not_determine": return "red";
    default: return "violet"; // newer/older daemon: say so, never guess
  }
}

function InForcePanel() {
  const [agent, setAgent] = useState("");
  const [loop, setLoop] = useState("");
  const [res, setRes] = useState<PolicyInForceResult | null>(null);
  const [error, setError] = useState<RpcError | null>(null);
  const [busy, setBusy] = useState(false);

  // Exactly one party: "in force for whom" is the question, and the daemon
  // refuses both-or-neither. The button says so before the round trip rather
  // than showing a refusal the page could have avoided.
  const party: { agent: string } | { loop: string } | null =
    agent !== "" && loop === "" ? { agent } :
    loop !== "" && agent === "" ? { loop } : null;

  const ask = useCallback(async () => {
    if (party === null) return;
    setBusy(true);
    try {
      setRes(await rpc<PolicyInForceResult>("policy.inForce", party));
      setError(null);
    } catch (cause) {
      setRes(null);
      setError(toRpcError(cause));
    } finally {
      setBusy(false);
    }
  }, [party]);

  const answers = res?.answers ?? [];

  return (
    <Panel>
      <div className="flex flex-wrap items-end gap-3">
        <div className="min-w-40 flex-1">
          <FormField label="agent">
            <Input value={agent} onChange={(e) => setAgent(e.target.value)} placeholder="boss" data-testid="inforce-agent" />
          </FormField>
        </div>
        <div className="min-w-40 flex-1">
          <FormField label="loop">
            <Input value={loop} onChange={(e) => setLoop(e.target.value)} placeholder="s10-policy" data-testid="inforce-loop" />
          </FormField>
        </div>
        <Button size="sm" onClick={() => void ask()} disabled={busy || party === null} data-testid="inforce-ask">
          {busy ? "asking…" : "in force?"}
        </Button>
      </div>
      <p className="mt-1 text-xs opacity-60" data-testid="inforce-hint">
        {party === null ? "Name one party — an agent or a loop, never both." : `Asking for one party: ${"agent" in party ? `agent ${party.agent}` : `loop ${party.loop}`}`}
      </p>

      {error && (
        <div className="mt-3" data-testid="inforce-error">
          <ErrorPanel error={error} />
        </div>
      )}

      {res !== null && answers.length === 0 && (
        <p className="mt-3 text-sm opacity-60" data-testid="inforce-none">
          Nothing governs this party. With an empty set the default is deny — see the panel above.
        </p>
      )}

      {answers.length > 0 && (
        <ul className="mt-3 space-y-2" data-testid="inforce-list">
          {answers.map((a) => {
            const tone = inForceTone(a.state);
            return (
              <li
                key={`${a.policyID}-${a.state}`}
                className="flex flex-wrap items-center gap-2 text-sm"
                data-testid={`inforce-${a.policyID}`}
                data-state={a.state}
              >
                <code className="font-medium">{a.policyID}</code>
                <Pill tone={tone} size="sm" data-testid={`inforce-state-${a.policyID}`} data-tone={tone}>
                  {a.state}
                </Pill>
                {/* Verbatim: the render IS the answer. A tone beside it is a
                    convenience; replacing its words would be the collapse. */}
                <span className="whitespace-pre-wrap font-mono text-xs" data-testid={`inforce-render-${a.policyID}`}>
                  {a.render}
                </span>
              </li>
            );
          })}
        </ul>
      )}
    </Panel>
  );
}

// --- 4. The refusal log --------------------------------------------------------

/**
 * The audit log **with the visibility triple it owes** (D15/PL11).
 *
 * A log that trims itself silently is one that loses the row somebody came
 * looking for and never says so, so the summary line states the depth it read,
 * the age cap `TrimEvents` enforces, and the oldest instant still retained.
 * `oldest` is `null` exactly when the table is empty — said as `none`, which is
 * a stated fact rather than a gap.
 */
function EventsPanel() {
  const [res, setRes] = useState<PolicyEventsResult | null>(null);
  const [error, setError] = useState<RpcError | null>(null);
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    setBusy(true);
    try {
      setRes(await rpc<PolicyEventsResult>("policy.events", { limit: DEFAULT_EVENTS_DEPTH }));
      setError(null);
    } catch (cause) {
      setError(toRpcError(cause));
    } finally {
      setBusy(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const rows = res?.rows ?? [];

  return (
    <Panel>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <span className="text-sm font-medium">refusal log</span>
        <Button size="sm" variant="ghost" onClick={() => void load()} disabled={busy} data-testid="events-refresh">
          {busy ? "loading…" : "refresh"}
        </Button>
      </div>

      {error && (
        <div className="mt-3" data-testid="events-error">
          <ErrorPanel error={error} />
        </div>
      )}

      {res === null && !error && <p className="mt-3 text-sm opacity-60" data-testid="events-loading">reading the refusal log…</p>}

      {res !== null && (
        <>
          {/* D15: depth, cap and oldest, on the same line as the rows they
              describe — a report without its own extent is a browse. */}
          <p className="mt-2 font-mono text-xs opacity-80" data-testid="events-summary">
            {`depth ${res.depth} · cap ${res.capDays} days · oldest ${res.oldest ?? "none"}`}
          </p>
          {rows.length === 0 ? (
            <p className="mt-2 text-sm opacity-60" data-testid="events-none">no refusals recorded yet</p>
          ) : (
            <ul className="mt-2 space-y-2" data-testid="events-list">
              {rows.map((row, index) => (
                <li key={`${row.at}-${row.subject}-${index}`} className="text-sm" data-testid={`event-${index}`}>
                  <div className="flex flex-wrap items-center gap-2">
                    <Pill tone={row.kind === "refusal" ? "red" : "slate"} size="sm">{row.kind}</Pill>
                    <span className="font-mono text-xs opacity-70">{row.subject}</span>
                    {row.policyID !== "" ? (
                      <code className="text-xs">{row.policyID}</code>
                    ) : (
                      // An empty policyID is the default-deny row: it names no
                      // rule because no rule fired. Said, not blanked.
                      <span className="text-xs opacity-60">no policy named</span>
                    )}
                    <span className="font-mono text-xs opacity-60">{row.at}</span>
                  </div>
                  <p className="mt-1 whitespace-pre-wrap font-mono text-xs opacity-80" data-testid={`event-${index}-detail`}>
                    {row.detail}
                  </p>
                </li>
              ))}
            </ul>
          )}
        </>
      )}
    </Panel>
  );
}
