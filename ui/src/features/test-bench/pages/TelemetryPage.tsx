import { useCallback, useEffect, useRef, useState } from "react";
import { Alert, Button, FormField, Input, Panel, Pill, Select } from "../../../components/kit";
import { rpc, toRpcError, RpcError } from "../../../lib/bridge";
import { ErrorPanel } from "../../../components/ErrorPanel";
import { registerBenchPage } from "../lib/benchRegistry";

/**
 * The counting tiers, where they will be read (S33 T9).
 *
 * # What this page is
 *
 * A window onto S33's telemetry through the daemon's OWN doors and no
 * others: the three reads `telemetry.loop · telemetry.component ·
 * telemetry.workspace`, the move that exercises the transfer
 * (`component.attach` / `component.detach`, the same-transaction
 * arithmetic), and the honest Delete (`loop.archive`,
 * whose entire D8 claim — the rollups do not ride along — this page can
 * demonstrate by reading a tier before and after). Everything arrives
 * through the generic `rpc()` bridge; this slice adds no Rust command
 * (AGENTS §4b).
 *
 * # CRUD, said honestly
 *
 * **Create**: counted work comes only from a loop actually running — the
 * Create panel starts one through `loop.start`, the identical door the
 * Loops bench uses. There is deliberately NO form here that writes a
 * telemetry row: the detail rows are written by the runtime's capture
 * seams and a bench that could fabricate one could fabricate any number,
 * which is the failure the whole slice exists to prevent.
 * **Read**: all three tiers, plus the loop tier's full detail — per-turn
 * rows, compactions, file/command activity, and BOTH of D2's numbers.
 * **Update**: the component MOVE (the transfer with its cost-state
 * recompute, visible as the two roots' numbers changing in the same
 * breath) — an edit of a number is impossible by construction and the
 * page has no control that even looks like one.
 * **Delete**: `loop.archive` — cold storage with state, rollups unmoved.
 *
 * # The three states are three renderings (TL11/P31)
 *
 * Cost renders as `$0.034 (known)`, `$0.02 (partial — at least)`, or
 * `(unknown — no figure claimed)`; a null token count renders as a
 * em-dash, never 0 — absence and zero are different facts and this page
 * must be unable to confuse them. The workspace singleton carries
 * `exists:false` as a STATE ("nothing counted yet"), not an error and not
 * a zero-filled table. A loop's composition prints "not recorded" when
 * the step never froze one — not an empty list posing as a composition.
 */

type Tier = {
  componentId?: string;
  loopsRun: number;
  wallMsTotal: number;
  inputTokensTotal: number;
  outputTokensTotal: number;
  cacheReadTokensTotal: number;
  cacheWriteTokensTotal: number;
  reasoningTokensTotal: number;
  costUsdTotal: number;
  costState: string;
  filesTouchedTotal: number;
  filesReadTotal: number;
  filesWrittenTotal: number;
  linesAddedTotal: number;
  linesRemovedTotal: number;
  commandsRunTotal: number;
  commandsFailedTotal: number;
  commandWallMsTotal: number;
  updatedAt: string;
  exists: boolean;
};

type LoopTelemetry = {
  step: { id: number; loopSlug: string; componentRoot: string; status: string };
  turns: Array<{
    iteration: number;
    category: string;
    wallMs: number;
    route: string;
    inputTokens?: number;
    outputTokens?: number;
    costUsd?: number;
    costState: string;
  }>;
  compactions: Array<{
    startIdx: number;
    endIdx: number;
    estimateBefore: number;
    estimateAfter: number;
    inputTokens?: number;
    outputTokens?: number;
    costUsd?: number;
    costState: string;
  }>;
  fileActivities: Array<{ path: string; action: string; linesAdded?: number; linesRemoved?: number }>;
  commandActivities: Array<{ command: string; exitCode?: number; wallMs: number }>;
  currentContextTokens: number;
  cumulativeTokensTotal: number;
  composition?: { systemChars?: number; sections?: Array<{ title: string; category: string; chars: number }> };
};

const COST_TONES: Record<string, "green" | "amber" | "slate"> = {
  known: "green",
  partial: "amber",
  unknown: "slate",
};

/** The three-state cost sentence, ONE renderer (like the policy verdict's
 * one formatter): a state can never arrive without its word, and a
 * partial sum can never be printed as an exact number. */
function costSentence(state: string, usd: number): string {
  switch (state) {
    case "known":
      return `$${usd.toFixed(6)} (known)`;
    case "partial":
      return `$${usd.toFixed(6)} (partial — at least this, never exactly)`;
    default:
      return "(unknown — no figure claimed)";
  }
}

/** Absence is a dash, never zero — the same rule the CLI printer obeys. */
function num(v: number | undefined | null): string {
  return v === undefined || v === null ? "—" : String(v);
}

function TierTable({ tier }: { tier: Tier }) {
  return (
    <div className="space-y-1 text-sm" data-testid="telemetry-tier">
      <div className="flex items-center gap-2">
        <Pill tone={COST_TONES[tier.costState] ?? "slate"} size="sm">{tier.costState}</Pill>
        <span data-testid="telemetry-cost">{costSentence(tier.costState, tier.costUsdTotal)}</span>
      </div>
      <dl className="grid grid-cols-2 gap-x-6 gap-y-1">
        <dt className="opacity-60">loops run</dt><dd>{tier.loopsRun}</dd>
        <dt className="opacity-60">wall</dt><dd>{tier.wallMsTotal} ms</dd>
        <dt className="opacity-60">tokens in / out</dt><dd>{tier.inputTokensTotal} / {tier.outputTokensTotal}</dd>
        <dt className="opacity-60">cache read / write</dt><dd>{tier.cacheReadTokensTotal} / {tier.cacheWriteTokensTotal}</dd>
        <dt className="opacity-60">reasoning</dt><dd>{tier.reasoningTokensTotal}</dd>
        <dt className="opacity-60">files touched</dt><dd>{tier.filesTouchedTotal} (read {tier.filesReadTotal}, written {tier.filesWrittenTotal})</dd>
        <dt className="opacity-60">lines</dt><dd>+{tier.linesAddedTotal} / −{tier.linesRemovedTotal}</dd>
        <dt className="opacity-60">commands</dt><dd>{tier.commandsRunTotal} run · {tier.commandsFailedTotal} failed · {tier.commandWallMsTotal} ms</dd>
        <dt className="opacity-60">updated</dt><dd>{tier.updatedAt}</dd>
      </dl>
    </div>
  );
}

export function TelemetryPage() {
  const [components, setComponents] = useState<Array<{ id: string; name: string; slug: string; root: string; parentId: string }>>([]);
  const [componentId, setComponentId] = useState("");
  const [componentTier, setComponentTier] = useState<Tier | null>(null);
  const [workspace, setWorkspace] = useState<Tier | null>(null);
  const [stepId, setStepId] = useState("");
  const [loop, setLoop] = useState<LoopTelemetry | null>(null);
  const [moveTarget, setMoveTarget] = useState("");
  const [archiveId, setArchiveId] = useState("");
  const [archiveNote, setArchiveNote] = useState("");
  const [lastAction, setLastAction] = useState<{ text: string; tone: "amber" | "red" | "slate" } | null>(null);
  // The tone is chosen HERE, at the moment the outcome is known — never by
  // reading the sentence's prefix back (the SEC-033 shape the policy guard
  // test forbids across the tree). The text is printed
  // verbatim whatever it says.
  const act = (text: string, tone: "amber" | "red" | "slate") => setLastAction({ text, tone });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<RpcError | null>(null);
  const mounted = useRef(true);

  useEffect(() => {
    mounted.current = true;
    void (async () => {
      try {
        const cs = await rpc<{ components: Array<{ id: string; name: string; slug: string; root: string; parentId: string }> }>("component.list", {});
        if (!mounted.current) return;
        setComponents(cs.components ?? []);
      } catch (err) {
        if (mounted.current) setError(toRpcError(err));
      }
    })();
    return () => {
      mounted.current = false;
    };
  }, []);

  const run = useCallback(async (fn: () => Promise<void>) => {
    setBusy(true);
    setError(null);
    try {
      await fn();
    } catch (err) {
      setError(toRpcError(err));
    } finally {
      setBusy(false);
    }
  }, []);

  const readComponent = () =>
    run(async () => {
      if (!componentId.trim()) {
        setError(null);
        act("refused: pick a component first — the read names the id it needs", "amber");
        return;
      }
      setComponentTier(await rpc<Tier>("telemetry.component", { componentId: componentId.trim() }));
    });

  const readWorkspace = () =>
    run(async () => {
      setWorkspace(await rpc<Tier>("telemetry.workspace", {}));
    });

  const readLoop = () =>
    run(async () => {
      const id = Number(stepId);
      if (!Number.isInteger(id) || id <= 0) {
        act("refused: telemetry.loop requires a step id greater than zero", "amber");
        return;
      }
      setLoop(await rpc<LoopTelemetry>("telemetry.loop", { stepId: id }));
    });

  // UPDATE: the component MOVE (attach / detach). The transfer itself is
  // the daemon's same-transaction act; this page only names the id and the
  // destination root, then re-reads the component tier to prove it moved.
  const moveComponent = () =>
    run(async () => {
      const comp = components.find((c) => c.id === componentId);
      if (!comp || !moveTarget) {
        act("refused: pick a component and a destination root — both are named arguments", "amber");
        return;
      }
      await rpc("component.attach", { id: comp.id, parentId: moveTarget });
      act(`moved ${comp.name} under ${roots.find((r) => r.id === moveTarget)?.name ?? moveTarget} — re-read the component: its totals moved in the same transaction`, "slate");
      setComponentTier(await rpc<Tier>("telemetry.component", { componentId: comp.id }));
    });

  const detachComponent = () =>
    run(async () => {
      if (!componentId.trim()) {
        act("refused: pick a component first — the read names the id it needs", "amber");
        return;
      }
      const comp = components.find((c) => c.id === componentId);
      await rpc("component.detach", { id: componentId });
      act(`detached ${comp?.name ?? componentId} to the root — it is its own root again`, "slate");
      setComponentTier(await rpc<Tier>("telemetry.component", { componentId: componentId }));
    });

  // DELETE: the honest one. Archive, then re-read the component tier —
  // D8's claim is that the two reads agree to the field.
  const archiveLoop = () =>
    run(async () => {
      if (!archiveId.trim()) {
        act("refused: loop.archive names the loop by id — `aeon loop list` prints the ids", "amber");
        return;
      }
      const res = await rpc<{ refused?: { kind: string; detail: string }; id?: string; stateBytes?: number }>(
        "loop.archive",
        { id: archiveId.trim(), note: archiveNote },
      );
      if (res.refused) {
        act(`refused: ${res.refused.kind} — ${res.refused.detail}`, "amber");
        return;
      }
      let note = `archived ${archiveId} to cold storage (${res.stateBytes ?? 0} bytes of state)`;
      let tone: "amber" | "red" | "slate" = "slate";
      if (componentTier) {
        const after = await rpc<Tier>("telemetry.component", { componentId: componentId.trim() });
        const same = JSON.stringify({ ...after, updatedAt: null }) === JSON.stringify({ ...componentTier, updatedAt: null });
        if (same) {
          note += " — the rollup did not move (D8 holds at the bench)";
        } else {
          note += " — THE ROLLUP MOVED; D8 has been betrayed, this page says so in red";
          tone = "red";
        }
      }
      act(note, tone);
    });

  // CREATE: counted work only from a loop actually running. No form on
  // this page writes a telemetry row, and says so out loud.
  const [goal, setGoal] = useState("");
  const runACountedLoop = () =>
    run(async () => {
      const comp = components.find((c) => c.id === componentId);
      if (!comp || !goal.trim()) {
        act("refused: a counted loop needs a component and a goal — both are named", "amber");
        return;
      }
      const started = await rpc<{ id: string }>("loop.start", {
        blueprint: "two-step",
        goal: goal.trim(),
        workspaceRoot: comp.root,
        mode: "standard",
      });
      act(`loop ${started.id} started — its turns, files and commands appear in the loop panel below as the runtime's capture seams write them (nothing on this page can) · `, "slate");
      setArchiveId(started.id);
    });

  const roots = components.filter((c) => !c.parentId);

  return (
    <div className="space-y-4" data-testid="telemetry-page">
      <Panel tone="slate">
        <div className="flex items-center justify-between gap-3">
          <div>
            <p className="text-xs uppercase tracking-widest text-slate-400">S33 · telemetry</p>
            <h2 className="mt-1 text-lg font-semibold">The counting tiers</h2>
          </div>
          <Pill tone="slate" size="sm">reads only — writes ride the runtime's capture seams</Pill>
        </div>
        <p className="mt-2 text-sm opacity-70">
          Every number here came from a loop that actually ran. Null token counts render —, never 0;
          cost keeps its state word beside it; the workspace row shows “nothing counted yet” as a state.
        </p>
      </Panel>

      <Panel tone="slate">
        <p className="text-xs uppercase tracking-widest text-slate-400">read · component tier</p>
        <div className="mt-2 flex flex-wrap items-end gap-3">
          <FormField label="component">
            <Select value={componentId} onChange={(e) => setComponentId(e.target.value)} data-testid="telemetry-component-pick">
              <option value="">pick a component…</option>
              {components.map((c) => (
                <option key={c.id} value={c.id}>{c.name} · {c.root}</option>
              ))}
            </Select>
          </FormField>
          <Button disabled={busy} onClick={() => void readComponent()} data-testid="telemetry-read-component">read rollup</Button>
        </div>
        {componentTier ? (
          <div className="mt-3"><TierTable tier={componentTier} /></div>
        ) : (
          <p className="mt-2 text-sm opacity-60" data-testid="telemetry-component-empty">no read yet — an unknown id refuses naming it; a silent component refuses too, and those are different sentences on the wire</p>
        )}
      </Panel>

      <Panel tone="slate">
        <p className="text-xs uppercase tracking-widest text-slate-400">update · move a component (attach / detach)</p>
        <div className="mt-2 flex flex-wrap items-end gap-3">
          <FormField label="destination root">
            <Select value={moveTarget} onChange={(e) => setMoveTarget(e.target.value)} data-testid="telemetry-move-target">
              <option value="">pick a root…</option>
              {roots.map((r) => (
                <option key={r.id} value={r.id}>{r.name}</option>
              ))}
            </Select>
          </FormField>
          <Button disabled={busy || !componentId || !moveTarget} onClick={() => void moveComponent()} data-testid="telemetry-move">
            move (attach)
          </Button>
          <Button disabled={busy || !componentId} onClick={() => void detachComponent()} data-testid="telemetry-detach">
            detach
          </Button>
        </div>
        <p className="mt-2 text-xs opacity-60">
          The move attaches the component to a root (or detaches it back to its own root); the daemon re-reads
          the component tier after. There is no control here to edit a number by hand, by design.
        </p>
      </Panel>

      <Panel tone="slate">
        <p className="text-xs uppercase tracking-widest text-slate-400">read · workspace singleton</p>
        <div className="mt-2">
          <Button disabled={busy} onClick={() => void readWorkspace()} data-testid="telemetry-read-workspace">read the singleton</Button>
        </div>
        {workspace &&
          (workspace.exists ? (
            <div className="mt-3"><TierTable tier={workspace} /></div>
          ) : (
            <p className="mt-2 text-sm opacity-60" data-testid="telemetry-workspace-absent">
              nothing counted yet — exists:false is a state, not a failure and not a zero-filled table
            </p>
          ))}
      </Panel>

      <Panel tone="slate">
        <p className="text-xs uppercase tracking-widest text-slate-400">create · counted work from a loop that runs</p>
        <div className="mt-2 flex flex-wrap items-end gap-3">
          <FormField label="goal">
            <Input value={goal} onChange={(e) => setGoal(e.target.value)} placeholder="what should the loop do?" data-testid="telemetry-goal" />
          </FormField>
          <Button disabled={busy || !componentId || !goal.trim()} onClick={() => void runACountedLoop()} data-testid="telemetry-run-loop">
            run a counted loop
          </Button>
        </div>
        <p className="mt-2 text-xs opacity-60">
          This starts a real loop through `loop.start` — the same door the Loops bench uses. There is no
          form on this page that writes a telemetry row: the detail rows belong to the runtime's capture
          seams, and a bench that could fabricate one could fabricate anything.
        </p>
      </Panel>

      <Panel tone="slate">
        <p className="text-xs uppercase tracking-widest text-slate-400">read · loop tier (D2's two numbers)</p>
        <div className="mt-2 flex flex-wrap items-end gap-3">
          <FormField label="step id">
            <Input value={stepId} onChange={(e) => setStepId(e.target.value)} placeholder="42" data-testid="telemetry-step-id" />
          </FormField>
          <Button disabled={busy} onClick={() => void readLoop()} data-testid="telemetry-read-loop">read the loop</Button>
        </div>
        {loop && (
          <div className="mt-3 space-y-3 text-sm" data-testid="telemetry-loop">
            <div>
              step {loop.step.id} · loop <code>{loop.step.loopSlug}</code> · {loop.step.componentRoot} ({loop.step.status})
            </div>
            <div className="flex flex-wrap gap-4">
              <span data-testid="telemetry-current">
                context now: <strong>{loop.currentContextTokens}</strong> tokens estimated — what the next call would carry
              </span>
              <span data-testid="telemetry-cumulative">
                cumulative: <strong>{loop.cumulativeTokensTotal}</strong> tokens — the step_turn rows’ sum
              </span>
            </div>
            <table className="w-full text-left text-xs">
              <thead className="opacity-60">
                <tr><th>iter</th><th>category</th><th>wall</th><th>in</th><th>out</th><th>cost</th><th>route</th></tr>
              </thead>
              <tbody>
                {(loop.turns ?? []).map((t, i) => (
                  <tr key={i} data-testid="telemetry-turn">
                    <td>{t.iteration}</td>
                    <td><Pill tone={t.category === "assistant_output" ? "green" : "slate"} size="sm">{t.category}</Pill></td>
                    <td>{t.wallMs} ms</td>
                    <td>{num(t.inputTokens)}</td>
                    <td>{num(t.outputTokens)}</td>
                    <td><Pill tone={COST_TONES[t.costState] ?? "slate"} size="sm">{t.costState}</Pill>{" "}{t.costState === "unknown" ? "" : `$${(t.costUsd ?? 0).toFixed(6)}`}</td>
                    <td>{t.route}</td>
                  </tr>
                ))}
              </tbody>
            </table>
            {(loop.compactions ?? []).length > 0 && (
              <div data-testid="telemetry-compactions">
                <div className="text-xs uppercase opacity-60">compactions</div>
                {(loop.compactions ?? []).map((c, i) => (
                  <div key={i} className="text-xs">
                    rows {c.startIdx}→{c.endIdx} · est {c.estimateBefore}→{c.estimateAfter} · in {num(c.inputTokens)} out {num(c.outputTokens)} · {costSentence(c.costState, c.costUsd ?? 0)}
                  </div>
                ))}
              </div>
            )}
            <div className="flex flex-wrap gap-4 text-xs">
              <span>files: {(loop.fileActivities ?? []).length}</span>
              <span>commands: {(loop.commandActivities ?? []).length}</span>
              {loop.composition ? (
                <span data-testid="telemetry-composition">
                  composition: {loop.composition.systemChars ?? 0} system chars · {(loop.composition.sections ?? []).length} sections
                </span>
              ) : (
                <span data-testid="telemetry-composition-absent">composition: not recorded — unknown, not empty</span>
              )}
            </div>
          </div>
        )}
      </Panel>

      <Panel tone="slate">
        <p className="text-xs uppercase tracking-widest text-slate-400">delete · loop.archive (cold storage, not destruction)</p>
        <div className="mt-2 flex flex-wrap items-end gap-3">
          <FormField label="loop id">
            <Input value={archiveId} onChange={(e) => setArchiveId(e.target.value)} placeholder="loop id from aeon loop list" data-testid="telemetry-archive-id" />
          </FormField>
          <FormField label="note">
            <Input value={archiveNote} onChange={(e) => setArchiveNote(e.target.value)} placeholder="why it goes cold (optional)" data-testid="telemetry-archive-note" />
          </FormField>
          <Button disabled={busy || !archiveId.trim()} onClick={() => void archiveLoop()} data-testid="telemetry-archive">
            archive
          </Button>
        </div>
        <p className="mt-2 text-xs opacity-60">
          The honest Delete: the loop and its documents ride into cold storage WITH their state; the
          rollups must come back byte-identical, and this page re-reads one to prove it in the line above.
        </p>
      </Panel>

      {lastAction && (
        <Alert
          tone={lastAction.tone}
          title="last action"
          description={lastAction.text}
          data-testid="telemetry-action-note"
        />
      )}
      {error && <ErrorPanel error={error} data-testid="telemetry-error" />}
    </div>
  );
}

registerBenchPage({
  id: "telemetry",
  label: "Telemetry",
  path: "/bench/telemetry",
  // Past the graph's 4200 — the registry appends; history is not re-ordered.
  order: 4300,
  needsConnection: true,
  render: () => <TelemetryPage />,
});
