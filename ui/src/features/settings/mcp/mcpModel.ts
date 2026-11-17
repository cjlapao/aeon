/**
 * The MCP section's pure half (S46 T11, MG5/MG7/MG13/MG14/P34): wire types,
 * wire-param shims (the generated contract is exact for most methods and
 * lossy where Go carries pointers — one honest cast at each edge, the
 * `ProposalListParamsWire` precedent from S45 T9), and the pure logic the
 * components bind to. NOTHING here hardcodes the closed sets: transports,
 * rerun classes, effect classes and grammars come from `mcp.vocabulary`;
 * the guard test greps this directory for the banned literals to keep the
 * claim mechanical.
 */
import type { RpcMethods } from "../../../proto/contract";

export type McpVocabulary = RpcMethods["mcp.vocabulary"]["result"];

export type ServerRow = RpcMethods["mcp.servers"]["result"]["servers"][number];
export type ServerToolRow = ServerRow["tools"][number];
export type ServerDetail = RpcMethods["mcp.server"]["result"]["server"];
export type ChangesRow = RpcMethods["mcp.changes"]["result"]["changes"][number];
export type ImportAnswer = RpcMethods["mcp.importFile"]["result"];
export type CheckAnswer = RpcMethods["mcp.check"]["result"];
export type MineStatusRow = RpcMethods["mcp.status"]["result"]["servers"];

/** The MCP proposal kinds this section is the view of (S46 T6). */
export const MCP_PROPOSAL_KINDS = ["mcp_server_add", "mcp_server_remove", "mcp_grant_revoke"] as const;

// --- wire shims (pointer fields the generator flattened away) -------------

export interface TransportInputWire {
  type: string;
  command?: string;
  args?: string[];
  env?: string[];
  url?: string;
  /** env name -> the stored variable that supplies it. */
  envFrom?: Record<string, string>;
  /** env name -> a value typed for it. WRITE-ONLY: the daemon keeps it as a hidden secret and stores only the binding. */
  envValues?: Record<string, string>;
}

export interface ToolInputWire {
  name: string;
  rerun: string;
  effect: string;
}

/** `mcp.servers` filters (Go carries pointers; the contract flattened). */
export type ServersParamsWire = {
  tier?: string;
  agent?: string;
  enabled?: boolean;
  origin?: string;
};

/** `mcp.server.update` — pointer fields: absent keeps, present replaces. */
export type ServerUpdateParamsWire = {
  name: string;
  title?: string;
  description?: string;
  transport?: TransportInputWire;
  tools?: ToolInputWire[];
};

/** `mcp.core.set` — null (the WHOLE core set) and [] (none) are different facts. */
export type CoreSetParamsWire = {
  agent: string;
  core: string[] | null;
};

/** `mcp.grants`' answer as the wire carries it (Go `core` is a slice or null). */
export interface GrantsAnswer {
  agent: string;
  core: string[] | null;
  declares: string[];
  grants: GrantView[];
}

export interface GrantView {
  qualified: string;
  state: string;
  tier: string;
  reason?: string;
  missingClasses?: string[];
}

export interface AgentGrants {
  agent: string;
  before: GrantView[];
  after: GrantView[];
}

// --- the drafts the form edits ---------------------------------------------

export interface ToolDraft {
  name: string;
  /** "" = UNCLASSIFIED — never preselected to a "safe" value (MC6). */
  rerun: string;
  /** "" = UNCLASSIFIED. */
  effect: string;
}

export interface ServerDraft {
  name: string;
  /** The friendly name the app shows; the name above stays the identifier. */
  title: string;
  /** What the server is for. */
  description: string;
  tier: string;
  /** Owning agent — required for a private server, forbidden for a public one. */
  agent: string;
  enabled: boolean;
  transportType: string;
  command: string;
  /** One argument per entry. */
  args: string[];
  /** Variable NAMES only — never values (§4c's shape, MG env law). */
  env: string[];
  /** Which stored variable supplies each env name. */
  envFrom: Record<string, string>;
  /** A value typed for an env name; sent once, never read back. */
  envValues: Record<string, string>;
  url: string;
  tools: ToolDraft[];
}

export function emptyServerDraft(): ServerDraft {
  return {
    name: "", title: "", description: "", tier: "", agent: "", enabled: false,
    transportType: "", command: "", args: [], env: [], envFrom: {}, envValues: {}, url: "", tools: [],
  };
}

/** Duplicate = a NEW draft seeded from a detail row: same definition, no
 *  name (the daemon's name-collision refusal is what forces the rename). */
export function duplicateDraftFrom(detail: ServerDetail): ServerDraft {
  return {
    name: "", title: "", description: detail.description ?? "", tier: detail.tier, agent: detail.agent, enabled: false,
    transportType: detail.transport.type,
    command: detail.transport.command ?? "",
    args: [...(detail.transport.args ?? [])],
    env: [...(detail.transport.envNames ?? [])],
    envFrom: { ...((detail.transport as { envFrom?: Record<string, string> }).envFrom ?? {}) },
    envValues: {},
    url: "",
    tools: (detail.tools ?? []).map((tool) => ({ name: tool.name, rerun: tool.rerun ?? "", effect: tool.effect ?? "" })),
  };
}

// --- the inspect answer (mcp.server.inspect) --------------------------------

/** One tool as the live server described itself. The generated contract is
 *  fixture-derived (the sample's schema became a literal shape, the optional
 *  fields became required), so the answer is read through these widened types —
 *  Go: `protocol.MCPServerInspectResult`. */
export interface InspectedTool {
  name: string;
  description: string;
  inputSchema?: { properties?: Record<string, unknown> } | null;
  /** The server's own hints, as it keyed them — shown, never read as a class. */
  annotations?: Record<string, boolean | undefined> | null;
}

export interface InspectAnswer {
  serverName: string;
  serverVersion: string;
  protocolVersion: string;
  ms: number;
  tools: InspectedTool[];
}

/** The params `mcp.server.inspect` takes — the transport of the draft, as an add would send it. */
export function buildInspectParams(draft: ServerDraft, useVariables = false): RpcMethods["mcp.server.inspect"]["params"] {
  const params: Record<string, unknown> = { transport: buildAddParams(draft).transport };
  // Opt-in, never implied: the command receives the stored VALUES of its env names.
  if (useVariables) params.useVariables = true;
  return params as unknown as RpcMethods["mcp.server.inspect"]["params"];
}

/** True when the draft's transport is complete enough to run: the type, and the
 *  command (or the URL). The daemon validates the rest with the stored server's own rules. */
export function canInspect(draft: ServerDraft, vocabulary: McpVocabulary | undefined): boolean {
  if (draft.transportType === "" || !vocabulary?.transports.includes(draft.transportType)) return false;
  return isUrlTransport(vocabulary, draft.transportType) ? draft.url !== "" : draft.command !== "";
}

/** A namespace suggestion from what the server called itself: lower-case, the
 *  grammar's characters only, starting with a letter. "" when nothing usable is left. */
export function suggestServerName(serverName: string): string {
  const slug = serverName.toLowerCase().replace(/[^a-z0-9_-]+/g, "-").replace(/^[^a-z]+/, "").replace(/-+$/, "");
  return slug.slice(0, 64);
}

/** The parameter names a tool's input schema declares, for a one-line summary. */
export function schemaParams(tool: InspectedTool): string[] {
  const props = tool.inputSchema?.properties;
  return props && typeof props === "object" ? Object.keys(props) : [];
}

/** Put the picked tools into the draft: UNCLASSIFIED (the server's hints never
 *  classify — MC6), keeping any class the person already set on a same-named row. */
export function mergeInspectedTools(current: ToolDraft[], picked: InspectedTool[]): ToolDraft[] {
  const byName = new Map(current.map((tool) => [tool.name, tool]));
  const merged = [...current];
  for (const tool of picked) {
    if (!byName.has(tool.name)) merged.push({ name: tool.name, rerun: "", effect: "" });
  }
  return merged;
}

/** The person's explicit bulk choice: every still-UNCLASSIFIED row gets these two
 *  classes. Rows that already carry a class are left alone. */
export function classifyUnclassified(tools: ToolDraft[], rerun: string, effect: string): ToolDraft[] {
  if (rerun === "" || effect === "") return tools;
  return tools.map((tool) => (tool.rerun === "" && tool.effect === "" ? { ...tool, rerun, effect } : tool));
}

// --- env names (MG: names only, values come from the daemon's environment) --

export type EnvNameVerdict = { ok: true; name: string } | { ok: false; key: "settings.mcp.form.envName.value" | "settings.mcp.form.envName.blank" | "settings.mcp.form.envName.shape" };

/** An entry is a NAME, or it is refused before the wire: `A=b` smuggles a
 *  value, surrounding whitespace means the person pasted a pair, and "" is
 *  nothing. */
export function envNamesOnly(text: string): EnvNameVerdict {
  if (text === "" || text.trim() === "") return { ok: false, key: "settings.mcp.form.envName.blank" };
  if (text.includes("=") || /\s/.test(text)) return { ok: false, key: "settings.mcp.form.envName.value" };
  return { ok: true, name: text };
}

// --- the grants matrix -------------------------------------------------------

export interface GrantCellState {
  /** The daemon's state word, UNREWRITTEN (the pill's label is the row's own). */
  state: string;
  /** Kit tone per state — presentation, never a re-wording. */
  tone: "green" | "amber" | "slate" | "rose";
  /** MC6: a tool missing its classes is shown as such. */
  unclassified: boolean;
}

export function grantCellState(view: GrantView): GrantCellState {
  const tone =
    view.state === "available" ? "green"
      : view.state === "ask" ? "amber"
        : view.state === "refused" ? "rose"
          : "slate"; // "unavailable" (the no-allow state MG7 names) and any word the daemon adds later
  return { state: view.state, tone, unclassified: (view.missingClasses ?? []).length > 0 };
}

/** True when a fresh declare landed `unavailable` — the MG7 moment that
 *  offers "Allow in policy…" (NEVER a hidden combined call). */
export function declareNeedsAllow(after: GrantView[], qualified: string): GrantView | undefined {
  return after.find((view) => view.qualified === qualified && view.state === "unavailable");
}

// --- the before/after the confirms show --------------------------------------

export interface BeforeAfter {
  changed: boolean;
  added: string[];
  removed: string[];
  changedState: Array<{ qualified: string; from: string; to: string }>;
}

/** Compare two grant lists by qualified name: identical sets (and states)
 *  read "no change"; differences arrive as name lists — reasons and states
 *  are the daemon's words, carried verbatim by the renderer. */
export function describeBeforeAfter(before: GrantView[], after: GrantView[]): BeforeAfter {
  const byKey = new Map<string, GrantView>();
  for (const view of before) byKey.set(view.qualified, view);
  const afterKeys = new Set(after.map((view) => view.qualified));
  const added: string[] = [];
  const changedState: BeforeAfter["changedState"] = [];
  for (const view of after) {
    const previous = byKey.get(view.qualified);
    if (!previous) added.push(view.qualified);
    else if (previous.state !== view.state) changedState.push({ qualified: view.qualified, from: previous.state, to: view.state });
  }
  const removed = before.filter((view) => !afterKeys.has(view.qualified)).map((view) => view.qualified);
  return { changed: added.length > 0 || removed.length > 0 || changedState.length > 0, added, removed, changedState };
}

// --- servers list helpers ------------------------------------------------------

export interface ServerGroup {
  tier: string;
  rows: ServerRow[];
}

export function groupServers(rows: ServerRow[]): ServerGroup[] {
  const order: string[] = [];
  const byTier = new Map<string, ServerRow[]>();
  for (const row of rows) {
    if (!byTier.has(row.tier)) {
      byTier.set(row.tier, []);
      order.push(row.tier);
    }
    (byTier.get(row.tier) as ServerRow[]).push(row);
  }
  return order.map((tier) => ({ tier, rows: byTier.get(tier) as ServerRow[] }));
}

/** An unclassified tool (MC6) — the row-level warning chip. */
export function unclassifiedTools(row: ServerRow): ServerToolRow[] {
  return (row.tools ?? []).filter((tool) => tool.classified === false || !tool.rerun || !tool.effect);
}

/** The URL-based transport is the vocabulary entry that names http; the
 *  command-based one is everything else. Derived from the daemon's own
 *  vocabulary so the section names no transport itself. */
export function isUrlTransport(vocabulary: McpVocabulary | undefined, type: string): boolean {
  if (!vocabulary) return false;
  return vocabulary.transports.includes(type) && type.toLowerCase().includes("http");
}

// --- the form's submit gate (the daemon still refuses; this refuses earlier) --

export interface SubmitDecision {
  ok: boolean;
  /** i18n keys for the blocking facts. */
  errors: string[];
  /** i18n keys for what is allowed but flagged (MC6's unclassified tools). */
  flags: string[];
}

/** The daemon's own server-name grammar, when it states one as an anchored
 *  pattern (`^…$`); a grammar written as prose is documentation, not a rule the
 *  form can run, and the daemon's validator still refuses a bad name. */
function nameMatchesGrammar(name: string, grammar: string | undefined): boolean {
  if (!grammar || !grammar.startsWith("^")) return true;
  try {
    return new RegExp(grammar).test(name);
  } catch {
    return true;
  }
}

export interface SubmitOptions {
  /** An ADD needs at least one tool (the daemon refuses a server with none);
   *  an edit may keep whatever it has. */
  requireTools?: boolean;
}

export function canSubmit(draft: ServerDraft, vocabulary: McpVocabulary | undefined, options: SubmitOptions = {}): SubmitDecision {
  const errors: string[] = [];
  const flags: string[] = [];
  if (draft.name === "") errors.push("settings.mcp.form.needName");
  else if (!nameMatchesGrammar(draft.name, vocabulary?.grammars?.server)) errors.push("settings.mcp.form.nameShape");
  if (options.requireTools && draft.tools.length === 0) errors.push("settings.mcp.form.needTools");
  if (draft.transportType === "" || !vocabulary?.transports.includes(draft.transportType)) errors.push("settings.mcp.form.needTransport");
  if (draft.tier === "") errors.push("settings.mcp.form.needTier");
  if (draft.tier === "private" && draft.agent === "") errors.push("settings.mcp.form.privateNeedsOwner");
  if (draft.tier === "public" && draft.agent !== "") errors.push("settings.mcp.form.publicForbidsOwner");
  if (draft.transportType !== "" && vocabulary) {
    if (isUrlTransport(vocabulary, draft.transportType)) {
      if (draft.url === "") errors.push("settings.mcp.form.needUrl");
    } else if (draft.command === "") {
      errors.push("settings.mcp.form.needCommand");
    }
  }
  for (const entry of draft.env) {
    const verdict = envNamesOnly(entry);
    if (!verdict.ok) errors.push(verdict.key);
  }
  // An environment variable cannot be empty: every name is bound to a stored
  // variable or has a value typed for it.
  if (unboundEnvNames(draft).length > 0) errors.push("settings.mcp.form.envUnbound");
  const seenTool = new Set<string>();
  draft.tools.forEach((tool, index) => {
    if (tool.name === "") errors.push("settings.mcp.form.tool.needName");
    else if (seenTool.has(tool.name)) errors.push("settings.mcp.form.tool.duplicate");
    else seenTool.add(tool.name);
    const hasRerun = tool.rerun !== "";
    const hasEffect = tool.effect !== "";
    if (hasRerun !== hasEffect) {
      errors.push("settings.mcp.form.tool.halfClassified");
    } else if (hasRerun) {
      if (vocabulary && (!vocabulary.rerun.includes(tool.rerun) || !vocabulary.effect.includes(tool.effect))) {
        errors.push("settings.mcp.form.tool.classOutsideVocabulary");
      }
    } else {
      flags.push(`settings.mcp.form.tool.unclassified#${index}`);
    }
  });
  return { ok: errors.length === 0, errors, flags };
}

/** The env names with neither a stored variable nor a typed value. */
export function unboundEnvNames(draft: Pick<ServerDraft, "env" | "envFrom" | "envValues">): string[] {
  return draft.env.filter((name) => !(draft.envFrom[name] ?? "") && !(draft.envValues[name] ?? ""));
}

/** The rows with exactly one of the two classes set — the state the daemon
 *  refuses ("declaring one alone is refusing the other"). Named by index so the
 *  form can mark the rows and list them; the rule itself stays in `canSubmit`. */
export function halfClassifiedTools(tools: ToolDraft[]): number[] {
  const at: number[] = [];
  tools.forEach((tool, index) => {
    if ((tool.rerun !== "") !== (tool.effect !== "")) at.push(index);
  });
  return at;
}

/** One binding per env name: a typed value wins over a stored variable (the person just typed it). */
function attachEnvBindings(transport: TransportInputWire, draft: ServerDraft) {
  const from: Record<string, string> = {};
  const values: Record<string, string> = {};
  for (const name of draft.env) {
    const typed = draft.envValues[name] ?? "";
    const variable = draft.envFrom[name] ?? "";
    if (typed !== "") values[name] = typed;
    else if (variable !== "") from[name] = variable;
  }
  if (Object.keys(from).length > 0) transport.envFrom = from;
  if (Object.keys(values).length > 0) transport.envValues = values;
}

export function buildAddParams(draft: ServerDraft): RpcMethods["mcp.server.add"]["params"] {
  const transport: TransportInputWire = { type: draft.transportType };
  if (draft.transportType && draft.url !== "") transport.url = draft.url;
  if (draft.transportType && draft.command !== "") {
    transport.command = draft.command;
    if (draft.args.length > 0) transport.args = draft.args.filter((arg) => arg !== "");
    if (draft.env.length > 0) {
      transport.env = draft.env;
      attachEnvBindings(transport, draft);
    }
  }
  const params: Record<string, unknown> = {
    name: draft.name,
    tier: draft.tier,
    enabled: draft.enabled,
    transport,
    tools: draft.tools.map((tool) => ({ name: tool.name, rerun: tool.rerun, effect: tool.effect })),
  };
  if (draft.agent !== "") params.agent = draft.agent;
  if (draft.title.trim() !== "") params.title = draft.title.trim();
  if (draft.description.trim() !== "") params.description = draft.description.trim();
  return params as RpcMethods["mcp.server.add"]["params"];
}

export function buildUpdateParams(name: string, draft: ServerDraft): ServerUpdateParamsWire {
  const transport: TransportInputWire = { type: draft.transportType };
  if (draft.url !== "") transport.url = draft.url;
  if (draft.command !== "") {
    transport.command = draft.command;
    transport.args = draft.args.filter((arg) => arg !== "");
    transport.env = draft.env;
    attachEnvBindings(transport, draft);
  }
  return {
    name,
    // Always sent on an edit: the form is the truth, and an emptied field clears the text.
    title: draft.title.trim(),
    description: draft.description.trim(),
    transport,
    tools: draft.tools.map((tool) => ({ name: tool.name, rerun: tool.rerun, effect: tool.effect })),
  };
}

/** The audit list's order promise — newest first is the daemon's answer
 *  (MCPChangeList orders by occurred_at DESC); this only guarantees the UI
 *  never re-sorts into a lie when a caller merges pages. */
export function mergeChanges(current: ChangesRow[], fresh: ChangesRow[]): ChangesRow[] {
  const seen = new Set<number>();
  const out: ChangesRow[] = [];
  for (const row of [...fresh, ...current]) {
    if (!seen.has(row.id)) {
      seen.add(row.id);
      out.push(row);
    }
  }
  return out;
}
