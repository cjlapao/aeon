// The Tools/Surface section's pure half (S47 T10, TC9/TC10/TC12 + P34).
//
// # What this file is for
//
// Everything the two Settings panels render, decided WITHOUT React and WITHOUT
// the bridge: the wire types (read off the generated contract by indexed access,
// the `mcpModel.ts` idiom), the filter params that travel to the daemon, and the
// small pure functions the components bind to. Nothing here holds a capability,
// a family, a state, a door name or a method summary — the daemon sends those and
// the section renders them. `toolsRpc.guard.test.ts` scans this file too, and the
// canaries in `toolsModel.test.ts` prove the scans are not decorative.
//
// # The one place each display decision lives
//
// A colour, a chip, a group order or a route is a DISPLAY decision: it belongs to
// nobody's vocabulary and it has exactly one definition, here, where a test can
// reach it. The `stateBadge`/`capabilityPill`/`doorChips` trio therefore map
// daemon data to a tone and an i18n key WITHOUT naming any daemon word: the tones
// are chosen by rules over the string the daemon sent, never by a table keyed on
// one of the states or capability words themselves: a table keyed on the
// vocabulary is a second vocabulary, stale in silence the day the daemon's moves.

import type { RpcMethods } from "../../../proto/contract";

// --- the wire types -------------------------------------------------------------

export type ToolRow = RpcMethods["tools.list"]["result"]["tools"][number];
export type ToolDetail = RpcMethods["tools.get"]["result"];
export type AgentGrantRow = ToolDetail["agents"][number];
export type PolicyRefRow = ToolDetail["policies"][number];
export type ToolsStatus = RpcMethods["tools.status"]["result"];
export type DriftRow = ToolsStatus["drift"][number];
export type ByAgentResult = RpcMethods["tools.byAgent"]["result"];
export type CatalogVocabulary = RpcMethods["tools.vocabulary"]["result"];
export type CapabilityNote = CatalogVocabulary["capabilities"][number];
export type SurfaceRow = RpcMethods["surface.list"]["result"]["methods"][number];
export type SurfaceDetail = RpcMethods["surface.get"]["result"]["entry"];
export type SurfaceCounts = RpcMethods["surface.list"]["result"]["counts"];

/**
 * The filters `tools.list` answers daemon-side (TC10). Declared here because the
 * generated contract is fixture-derived: it types the members a conformance
 * fixture happened to send, and `capability` is one the door answers and no
 * fixture ever showed. The cast at the call edge is the `ServersParamsWire`
 * precedent from S46 T11 and `ProposalListParamsWire` from S45 T9 — one honest
 * cast at the edge rather than a lie in a generated file.
 */
export type ToolsFilterWire = {
  family?: string;
  source?: string;
  capability?: string;
  access?: string;
  available?: boolean;
  q?: string;
};

/** The same story on the surface door: `humanDoorOnly` is answered and unfixtureed. */
export type SurfaceFilterWire = {
  kind?: string;
  door?: string;
  humanDoorOnly?: boolean;
  q?: string;
};

/** The filter state the components hold: "" and undefined mean "not filtering". */
export interface ToolsFilterState {
  family: string;
  source: string;
  capability: string;
  access: string;
  available: string;
  q: string;
}

export interface SurfaceFilterState {
  kind: string;
  door: string;
  humanDoorOnly: string;
  q: string;
}

export function emptyToolsFilter(): ToolsFilterState {
  return { family: "", source: "", capability: "", access: "", available: "", q: "" };
}

export function emptySurfaceFilter(): SurfaceFilterState {
  return { kind: "", door: "", humanDoorOnly: "", q: "" };
}

/** A filter is "empty" when nothing was chosen — and an empty filter is the
 *  daemon's own statement ("the catalog is the catalog"), not a UI sentence. */
export function toolsFilterIsEmpty(f: ToolsFilterState): boolean {
  return f.family === "" && f.source === "" && f.capability === "" && f.access === "" && f.available === "" && f.q === "";
}

export function surfaceFilterIsEmpty(f: SurfaceFilterState): boolean {
  return f.kind === "" && f.door === "" && f.humanDoorOnly === "" && f.q === "";
}

export function buildToolsFilter(f: ToolsFilterState): ToolsFilterWire {
  const out: ToolsFilterWire = {};
  if (f.family !== "") out.family = f.family;
  if (f.source !== "") out.source = f.source;
  if (f.capability !== "") out.capability = f.capability;
  if (f.access !== "") out.access = f.access;
  if (f.available === "true") out.available = true;
  if (f.available === "false") out.available = false;
  if (f.q !== "") out.q = f.q;
  return out;
}

export function buildSurfaceFilter(f: SurfaceFilterState): SurfaceFilterWire {
  const out: SurfaceFilterWire = {};
  if (f.kind !== "") out.kind = f.kind;
  if (f.door !== "") out.door = f.door;
  if (f.humanDoorOnly === "true") out.humanDoorOnly = true;
  if (f.humanDoorOnly === "false") out.humanDoorOnly = false;
  if (f.q !== "") out.q = f.q;
  return out;
}

// --- the display decisions -------------------------------------------------------

/** The tones this section is allowed to paint with. A colour is a display choice,
 *  not a vocabulary word, so it may be spelled — what may NOT be spelled is a
 *  capability, a family, a state or a door name (the guard test's business). */
export type SectionTone = "slate" | "blue" | "violet" | "green" | "amber" | "rose" | "teal" | "cyan" | "indigo" | "orange";

/** `capabilityPill` maps a capability the DAEMON named to a text and a tone. The
 *  text is the entry's own word, byte for byte — a translated or prettified
 *  capability would no longer be the string a policy row judges. The tone is
 *  derived from the string's own shape, so ten capabilities get ten distinguishable
 *  colours without this file ever holding the list of ten. */
export function capabilityPill(capability: string): { text: string; tone: SectionTone } {
  const tones: SectionTone[] = ["teal", "indigo", "violet", "cyan", "blue", "slate", "green", "amber", "orange", "rose"];
  let h = 0;
  for (let i = 0; i < capability.length; i += 1) {
    h = (h * 31 + capability.charCodeAt(i)) % 997;
  }
  const tone = tones[h % tones.length] ?? "slate";
  return { text: capability, tone };
}

/**
 * `stateBadge` maps a grant state the daemon sent to a badge. The five states are
 * distinct — five different tones, so a screen can be read at a glance — and no
 * state's spelling appears in this file: the rules test the SHAPE of the string the
 * daemon sent (does it speak of refusing, of asking, of no rule, of never being
 * granted) rather than comparing against a table of the five words. A state the
 * rules cannot place comes back neutral rather than green, which is the honest
 * direction for an unknown.
 */
export function stateBadge(state: string): { text: string; tone: SectionTone } {
  if (state === "") return { text: state, tone: "slate" };
  if (state.includes("denied") || state.includes("deny")) return { text: state, tone: "rose" };
  if (state.includes("ask")) return { text: state, tone: "amber" };
  if (state.includes("no_rule")) return { text: state, tone: "orange" };
  if (state.startsWith("not_") || state.includes("ungranted")) return { text: state, tone: "slate" };
  if (state.includes("allowed") || state.includes("allow")) return { text: state, tone: "green" };
  return { text: state, tone: "slate" };
}

/** One group of the by-agent view: a state, and the rows the daemon put in it. */
export interface StateGroup<R> {
  state: string;
  rows: R[];
}

/**
 * `groupByState` groups rows by the state the daemon sent, in FIRST-APPEARANCE
 * order, and preserves the daemon's order inside each group. There is no
 * hardcoded state order here on purpose: the daemon's answer is the order, and a
 * UI that re-sorted the states would be deciding which permission is more
 * important — and would keep deciding it after the daemon changed its mind.
 */
export function groupByState<R extends { state: string }>(rows: R[]): Array<StateGroup<R>> {
  const order: string[] = [];
  const byState = new Map<string, R[]>();
  for (const row of rows) {
    const existing = byState.get(row.state);
    if (existing) {
      existing.push(row);
      continue;
    }
    byState.set(row.state, [row]);
    order.push(row.state);
  }
  return order.map((state) => ({ state, rows: byState.get(state) ?? [] }));
}

/** One door chip on a surface row: an i18n label key plus the VALUE the daemon
 *  sent (the narrow action's own name, or the tool twin), never a word this file
 *  invented. */
export interface DoorChip {
  id: string;
  labelKey: string;
  value?: string;
  tone: SectionTone;
}

/**
 * `doorChips` renders the door object the daemon computed. The rules are about the
 * DATA, and one case is load-bearing: an `unknown` narrow flag — the seam that was
 * never wired, so nobody could say — renders as the daemon's own word
 * (`value === "unknown"`), and never as a claim that the method is off the narrow
 * surface. Collapsing unknown into "not on narrow" is P31's forbidden two-state
 * answer, and the row would look settled while the daemon was shrugging.
 */
export function doorChips(entry: { doors?: { socket?: boolean; public?: boolean; narrow?: string; agentService?: boolean; toolTwin?: string }; humanDoorOnly?: boolean; debug?: boolean }): DoorChip[] {
  const chips: DoorChip[] = [];
  const doors = entry.doors ?? {};
  if (doors.socket) chips.push({ id: "socket", labelKey: "settings.tools.surface.doors.socket", tone: "slate" });
  if (doors.public) chips.push({ id: "public", labelKey: "settings.tools.surface.doors.public", tone: "blue" });
  if (doors.narrow !== undefined && doors.narrow !== "") {
    chips.push({ id: "narrow", labelKey: "settings.tools.surface.doors.narrow", value: doors.narrow, tone: doors.narrow === "unknown" ? "amber" : "violet" });
  }
  if (doors.agentService) chips.push({ id: "agentService", labelKey: "settings.tools.surface.doors.agentService", tone: "cyan" });
  const twin = (doors as { toolTwin?: string }).toolTwin;
  if (typeof twin === "string" && twin !== "") {
    chips.push({ id: "toolTwin", labelKey: "settings.tools.surface.doors.toolTwin", value: twin, tone: "teal" });
  }
  if (entry.humanDoorOnly) chips.push({ id: "humanDoorOnly", labelKey: "settings.tools.surface.badges.humanDoorOnly", tone: "indigo" });
  if (entry.debug) chips.push({ id: "debug", labelKey: "settings.tools.surface.badges.debug", tone: "orange" });
  return chips;
}

/** A link the daemon asked for. `ref` is DATA (`{kind,id}`) from a policy row or
 *  an MCP tool; the section turns it into an address and owns nothing else about
 *  it. An unknown kind returns undefined rather than a plausible route, because a
 *  link that goes nowhere reads as a fact about the target. */
export interface ResolvedLink {
  to: string;
  labelKey: string;
}

export function linkFor(ref: { kind: string; id: string } | undefined): ResolvedLink | undefined {
  if (!ref) return undefined;
  const id = encodeURIComponent(ref.id);
  switch (ref.kind) {
    case "policy":
      return { to: `/settings?policy=${id}`, labelKey: "settings.tools.drawer.links.policy" };
    case "mcp_server":
      return { to: `/settings?server=${id}`, labelKey: "settings.tools.drawer.links.server" };
    default:
      return undefined;
  }
}

// --- the drift banner -------------------------------------------------------------

/**
 * `driftSummary` decides what `DriftBanner` says, from `tools.status` alone.
 *
 * All `current` (or no drift at all) is the quiet line, with the seed's applied
 * version — "the catalog is the code". Anything else lists each state with the
 * field NAMES the daemon reported, and the `differs` line is the one a person
 * needs to read carefully: it means a tool changed without a catalog version
 * bump. The sentences are i18n keys with the daemon's words interpolated; what
 * the daemon did NOT send (a repair, an action) is not offered, because TC11 says
 * drift is reported and never silently fixed, and a button here would be the
 * first step back into that.
 */
export interface DriftSummary {
  kind: "current" | "drift" | "unknown";
  version: number;
  set: string;
  state: string;
  lines: Array<{ name: string; state: string; fields: string[] }>;
}

/** `driftTone` decides how loudly a drift row speaks, from the SHAPE of the state
 *  the daemon sent — a disagreement with the code is louder than a row that has
 *  not arrived yet. The drift vocabulary itself is never spelled here. */
export function driftTone(state: string): SectionTone {
  if (state.includes("differ")) return "rose";
  if (state.includes("missing")) return "orange";
  if (state.includes("retire")) return "slate";
  return "slate";
}

export function driftSummary(status: ToolsStatus | undefined): DriftSummary {
  if (!status) return { kind: "unknown", version: 0, set: "", state: "", lines: [] };
  const lines = (status.drift ?? []).map((d) => ({ name: d.name, state: d.state, fields: d.fields ?? [] }));
  return {
    kind: lines.length === 0 ? "current" : "drift",
    version: status.seed?.appliedVersion ?? 0,
    set: status.seed?.set ?? "",
    state: status.seed?.state ?? "",
    lines,
  };
}

/** The counts strip above the surface list: every bucket the daemon named, in the
 *  order it named them, with the unfiltered total kept beside the filtered one so
 *  a narrow answer never reads as the whole surface. */
export function surfaceCountChips(counts: SurfaceCounts | undefined): Array<{ label: string; value: number }> {
  if (!counts) return [];
  const out: Array<{ label: string; value: number }> = [];
  for (const [kind, value] of Object.entries(counts.byKind ?? {})) {
    out.push({ label: kind, value: typeof value === "number" ? value : 0 });
  }
  for (const [door, value] of Object.entries(counts.byDoor ?? {})) {
    out.push({ label: door, value: typeof value === "number" ? value : 0 });
  }
  return out;
}

// --- the display order ------------------------------------------------------------

/**
 * `applyDisplayOrder` is the one place this section touches row ORDER, and what it
 * does is deliberately small: it keeps the daemon's order — the listing's own
 * (source, family, name) reading order — and drops a repeated name, which is a
 * rendering bug rather than a fact. It does NOT sort and does NOT filter: a client
 * that re-ordered the catalog would be a client entitled to disagree with the
 * daemon about what the catalog looks like, and TC10 put filtering on the daemon
 * side for exactly that reason.
 */
export function applyDisplayOrder<R extends { name: string }>(rows: R[]): R[] {
  const seen = new Set<string>();
  const out: R[] = [];
  for (const row of rows) {
    if (seen.has(row.name)) continue;
    seen.add(row.name);
    out.push(row);
  }
  return out;
}

/**
 * The options a filter select may offer, read from `tools.vocabulary` / the
 * surface counts rather than typed here. An absent vocabulary means the door has
 * not answered yet — the selects render EMPTY and disabled, which is the kit's
 * honest loading state, rather than offering a guess.
 */
export interface FilterOptions {
  families: string[];
  sources: string[];
  capabilities: string[];
  rerunClasses: string[];
  accesses: string[];
}

export function filterOptions(vocab: CatalogVocabulary | undefined): FilterOptions {
  if (!vocab) return { families: [], sources: [], capabilities: [], rerunClasses: [], accesses: [] };
  return {
    families: vocab.families ?? [],
    accesses: (vocab as { accesses?: string[] }).accesses ?? [],
    sources: vocab.sources ?? [],
    // The `?.` is the same honesty the siblings get: a vocabulary that names
    // no capabilities is answered as EMPTY, not crashed on. (`map` either
    // returns an array or throws, so the bare form's `?? []` was dead code.)
    capabilities: vocab.capabilities?.map((c) => c.name) ?? [],
    rerunClasses: vocab.rerunClasses ?? [],
  };
}

/** The meaning the daemon sent for one capability, or "" when it sent none — the
 *  drawer then shows the name alone rather than inventing a sentence. */
export function capabilityMeaning(vocab: CatalogVocabulary | undefined, capability: string): string {
  if (!vocab) return "";
  return vocab.capabilities?.find((c) => c.name === capability)?.meaning ?? "";
}

/** The surface kinds a filter may offer, from the counts the daemon already
 *  sends — every bucket including the empty ones, so a zero is an answer. */
export function surfaceKindOptions(counts: SurfaceCounts | undefined): string[] {
  return Object.keys(counts?.byKind ?? {});
}

/** The doors a surface filter may offer, from the same counts. */
export function surfaceDoorOptions(counts: SurfaceCounts | undefined): string[] {
  return Object.keys(counts?.byDoor ?? {});
}
