import { confirmation } from "./speech";
import type { NavMap } from "./navmap";

/** A call into the daemon, as the person. The orb gets this from the app; tests give it a fake. */
export type Rpc = (method: string, params: unknown) => Promise<unknown>;

/** The app itself, as far as Orion may touch it: where the person is, taking them somewhere, and the map of places. */
export interface Ui {
  navigate(to: string): void;
  /** The address of the page on screen. */
  where(): string;
  map: NavMap;
}

/** What the person actually said lately (never the model's words, never a notification), for checks that an action was asked for. */
export interface ToolContext { said: readonly string[] }

export type Verification = { ok: true; sentence: string; input: Record<string, unknown> } | { ok: false; problem: string };

export interface OrionTool {
  name: string;
  description: string;
  parameters: Record<string, unknown>;
  /** `control` changes something: it is never run until the person has said yes aloud. */
  kind: "read" | "control";
  /** What Orion asks before a control tool runs, in plain words. */
  ask?: (input: Record<string, unknown>) => string;
  /**
   * Checked in code BEFORE the person is asked to confirm, and again when it runs. A control tool that has one never runs on
   * the model's say-so: the sentence the person confirms comes from here, and the input that runs is the one returned here.
   */
  verify?: (input: Record<string, unknown>, rpc: Rpc, ui: Ui | undefined, ctx: ToolContext) => Promise<Verification>;
  run: (input: Record<string, unknown>, rpc: Rpc, ui?: Ui, ctx?: ToolContext) => Promise<string>;
}

type LoopRow = { id: string; slug: string; title?: string; status: string; phase?: string; componentRoot: string };
type Show = { status: string; position?: { lane?: string }; lastReason?: string; blueprint?: string; lanes?: { id: string; status: string }[] };
type StepRow = { stepId: number; lane?: string; agent?: string; status?: string; turns: number; toolCalls: number; costUsd?: number; costState: string };
type Steps = { steps: StepRow[]; totals?: { turns: number; toolCalls: number; costUsd?: number; costState: string } };

const s = (v: unknown) => (typeof v === "string" ? v.trim() : "");
const money = (usd: number | undefined, state: string) => (usd === undefined ? "cost not reported" : `${state === "partial" ? "at least " : ""}$${usd.toFixed(2)}`);
const say = (l: LoopRow) => `${l.title || l.slug} (${l.status}${l.phase ? `, ${l.phase}` : ""})`;

async function loops(rpc: Rpc): Promise<LoopRow[]> {
  const res = (await rpc("loop.list", { all: true })) as { loops?: LoopRow[] };
  return res.loops ?? [];
}

export { findLoop } from "./loopfind";
import { findLoop } from "./loopfind";

async function runningSteps(rpc: Rpc, loopId: string): Promise<StepRow[]> {
  const res = (await rpc("loop.steps", { id: loopId, limit: 50 })) as Steps;
  return (res.steps ?? []).filter((x) => x.status === "running");
}

const loopArg = { type: "object", properties: { loop: { type: "string", description: "The loop's id, slug, or a few words of its title. Empty means the one loop that is running." } } };

import { ATTENTION_TOOLS } from "./attention";

export const ORION_TOOLS: OrionTool[] = [
  ...ATTENTION_TOOLS,
  {
    name: "navigate", kind: "read",
    description: "Take the person to a page of the app: a project (say its name; add a tab such as loops, plan, knowledge, files or history to open that tab), a loop, a model, Settings, System, Flows, Home, or a configuration page. Navigation changes nothing, so it needs no confirmation.",
    parameters: { type: "object", properties: { destination: { type: "string", description: "What the person asked to open, in their words, such as \"the QA project\", \"settings\", \"loops of aeon\", \"the memory configuration\"." } }, required: ["destination"] },
    run: async (input, rpc, ui) => {
      if (!ui) return "the app cannot be navigated from here";
      const asked = s(input.destination);
      await ui.map.refresh(rpc);
      // An address a notification carried: opened only if it is one the app itself lists, never an arbitrary path.
      if (asked.startsWith("/")) {
        const known = ui.map.all().find((d) => d.to === asked);
        if (!known) return `${asked} is not a place in the app`;
        ui.navigate(known.to);
        return `opened ${known.label}`;
      }
      let found = ui.map.resolve(asked);
      if (found.none) { await ui.map.refresh(rpc, true); found = ui.map.resolve(asked); } // a name added a minute ago
      if (found.destination) { ui.navigate(found.destination.to); return `opened ${found.destination.label}${found.tab ? ` (${found.tab})` : ""}`; }
      if (found.ambiguous) return `${found.ambiguous.length} places match "${asked}": ${found.ambiguous.map((d) => `${d.label} (${d.kind})`).join("; ")}. Ask which one.`;
      return `nothing in the app matches "${asked}"`;
    },
  },
  {
    name: "where_am_i", kind: "read",
    description: "Which page the person is looking at right now.",
    parameters: { type: "object", properties: {} },
    run: async (_input, _rpc, ui) => {
      if (!ui) return "the page is not known from here";
      const here = ui.where();
      const hit = ui.map.all().filter((d) => d.to === here)[0] ?? ui.map.all().filter((d) => here.startsWith(d.to) && d.to !== "/").sort((a, b) => b.to.length - a.to.length)[0];
      return hit ? `${hit.label} (${hit.kind}), at ${here}` : `at ${here}`;
    },
  },
  {
    name: "loops_list", kind: "read",
    description: "List the loops with their status, so you can say what is running, waiting, blocked or finished.",
    parameters: { type: "object", properties: { status: { type: "string", description: "Only loops with this status, such as running, waiting, blocked, done." } } },
    run: async (input, rpc) => {
      const want = s(input.status).toLowerCase();
      const all = (await loops(rpc)).filter((l) => !want || l.status.toLowerCase() === want);
      return all.length ? `${all.length} loops: ${all.slice(0, 12).map(say).join("; ")}` : "no loops match";
    },
  },
  {
    name: "agents_running", kind: "read",
    description: "Which agents are working right now, and in which loops.",
    parameters: { type: "object", properties: {} },
    run: async (_input, rpc) => {
      const live = (await loops(rpc)).filter((l) => l.status === "running");
      if (live.length === 0) return "no loop is running, so no agent is working";
      const lines: string[] = [];
      for (const l of live) {
        const steps = await runningSteps(rpc, l.id);
        lines.push(`${l.title || l.slug}: ${steps.length ? steps.map((x) => x.agent || "an agent").join(", ") : "between steps"}`);
      }
      return lines.join("; ");
    },
  },
  {
    name: "loop_status", kind: "read",
    description: "How one loop is going: its status, where it is, the agent working, what it has cost, and why it stopped if it did.",
    parameters: loopArg,
    run: async (input, rpc) => {
      const { loop, problem } = findLoop(await loops(rpc), s(input.loop));
      if (!loop) return problem ?? "no such loop";
      const show = (await rpc("loop.show", { id: loop.id, includeBlueprint: false })) as Show;
      const steps = (await rpc("loop.steps", { id: loop.id, limit: 50 })) as Steps;
      const working = (steps.steps ?? []).filter((x) => x.status === "running").map((x) => x.agent).filter(Boolean);
      const t = steps.totals;
      return [`${say(loop)}`, show.position?.lane ? `at ${show.position.lane}` : "", working.length ? `${working.join(", ")} working` : "",
        t ? `${t.turns} model turns, ${t.toolCalls} tool calls, ${money(t.costUsd, t.costState)}` : "", show.lastReason ? `last note: ${show.lastReason}` : ""].filter(Boolean).join("; ");
    },
  },
  {
    name: "components_list", kind: "read",
    description: "The projects (components) Aeon knows, so you can start a loop in the right one.",
    parameters: { type: "object", properties: {} },
    run: async (_input, rpc) => {
      const res = (await rpc("component.list", {})) as { components?: { name: string; root: string }[] };
      const rows = res.components ?? [];
      return rows.length ? rows.map((c) => `${c.name} at ${c.root}`).join("; ") : "no components";
    },
  },
  {
    name: "loop_start", kind: "control",
    description: "Start a new loop on a goal in a project. This changes things, so the person is asked to confirm first.",
    parameters: { type: "object", properties: {
      goal: { type: "string", description: "What the loop should achieve, in a sentence or two." },
      project: { type: "string", description: "The project's name or root folder." },
      blueprint: { type: "string", description: "The blueprint to run. Default dev-loop." },
    }, required: ["goal", "project"] },
    ask: (i) => `Start a ${s(i.blueprint) || "dev-loop"} loop in ${s(i.project)} to ${s(i.goal)}?`,
    run: async (input, rpc) => {
      const res = (await rpc("component.list", {})) as { components?: { name: string; slug: string; root: string }[] };
      const q = s(input.project).toLowerCase();
      const hits = (res.components ?? []).filter((c) => c.root === s(input.project) || c.name.toLowerCase() === q || c.slug === q || c.name.toLowerCase().includes(q));
      if (hits.length !== 1) return hits.length ? `${hits.length} projects match "${s(input.project)}"` : `no project matches "${s(input.project)}"`;
      const started = (await rpc("loop.start", { blueprint: s(input.blueprint) || "dev-loop", goal: s(input.goal), workspaceRoot: hits[0]!.root })) as { slug?: string };
      return `started loop ${started.slug ?? ""} in ${hits[0]!.name}`;
    },
  },
  {
    name: "agent_steer", kind: "control",
    description: "Send an instruction to the agent that is working in a loop. This changes things, so the person is asked to confirm first.",
    parameters: { type: "object", properties: { ...loopArg.properties, instruction: { type: "string", description: "What to tell the agent." } }, required: ["instruction"] },
    ask: (i) => `Tell the agent${s(i.loop) ? ` in ${s(i.loop)}` : ""}: ${s(i.instruction)}?`,
    run: async (input, rpc) => {
      const { loop, problem } = findLoop(await loops(rpc), s(input.loop));
      if (!loop) return problem ?? "no such loop";
      const steps = await runningSteps(rpc, loop.id);
      if (steps.length === 0) return `${say(loop)} has no agent working right now`;
      if (steps.length > 1) return `${steps.length} agents are working there: ${steps.map((x) => x.agent).join(", ")}; say which one`;
      await rpc("step.steer", { stepId: steps[0]!.stepId, text: s(input.instruction) });
      return `sent to ${steps[0]!.agent || "the agent"}; it takes effect at its next turn`;
    },
  },
  {
    name: "loop_stop", kind: "control",
    description: "Stop a running loop. This changes things, so the person is asked to confirm first.",
    parameters: loopArg,
    ask: (i) => `Stop ${s(i.loop) || "the running loop"}?`,
    run: async (input, rpc) => {
      const { loop, problem } = findLoop(await loops(rpc), s(input.loop));
      if (!loop) return problem ?? "no such loop";
      await rpc("loop.stop", { id: loop.id, reason: "stopped by voice" });
      return `stopped ${say(loop)}`;
    },
  },
];

export { confirmation };
