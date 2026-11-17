import { menuConfigPages } from "../config/lib/configRegistry";
import { workPath, WORK_TABS, type WorkTab } from "../work/workModel";

/**
 * Where Orion can take a person: the pages of the app, the config pages, every project (component), every loop and every
 * model, each with the address that opens it. Kept in memory and kept warm, so "go to the QA project" is answered from
 * memory without a round trip, and refreshed when it goes stale or a name is not found.
 */
export type DestinationKind = "page" | "config" | "component" | "loop" | "model";
export interface Destination { kind: DestinationKind; label: string; /** Other things a person may call it. */ aliases: string[]; to: string }

interface ComponentRow { id: string; name: string; slug: string; root: string }
interface LoopRow { id: string; slug: string; title?: string; status: string; componentRoot: string }
interface RouteRow { id: string; displayName?: string; model?: string }

const PAGES: Destination[] = [
  { kind: "page", label: "Home", aliases: ["start", "dashboard", "main page"], to: "/" },
  { kind: "page", label: "Work", aliases: ["workspace", "projects"], to: "/work" },
  { kind: "page", label: "Flows", aliases: ["blueprints", "flow"], to: "/blueprints" },
  { kind: "page", label: "Models", aliases: ["model list", "all models"], to: "/models" },
  { kind: "page", label: "Add model", aliases: ["new model", "add a model"], to: "/models/new" },
  { kind: "page", label: "Settings", aliases: ["preferences", "appearance", "orion settings"], to: "/settings" },
  { kind: "page", label: "System", aliases: ["daemon", "connection", "daemon status"], to: "/system" },
  { kind: "page", label: "Test bench", aliases: ["bench"], to: "/bench" },
  { kind: "page", label: "Configuration", aliases: ["config"], to: "/config" },
];

/** Words that carry no name: how a person asks, not what they ask for. */
const FILLER = new Set(["go", "goto", "to", "the", "a", "an", "open", "show", "me", "take", "navigate", "please", "my", "page", "screen", "project", "component", "tab", "of", "in", "on", "for", "and", "now", "view", "see", "look", "at", "switch", "bring", "up", "loop", "model"]);

/** A word that says what KIND of thing is meant ("the QA project", "the greeting loop"), so a page that merely shares a word does not beat it. */
function kindHint(request: string): DestinationKind | undefined {
  const w = words(request);
  if (w.some((x) => x === "project" || x === "component")) return "component";
  if (w.includes("loop")) return "loop";
  if (w.includes("model")) return "model";
  return undefined;
}

const norm = (s: string) => s.toLowerCase().normalize("NFD").replace(/[̀-ͯ]/g, "").replace(/[^a-z0-9]+/g, " ").trim();
const words = (s: string) => norm(s).split(" ").filter(Boolean);

function near(a: string, b: string): boolean {
  if (a === b || (a.length >= 3 && (b.startsWith(a) || a.startsWith(b)))) return true;
  if (a.length < 5 || Math.abs(a.length - b.length) > 1) return false;
  let i = 0; let j = 0; let miss = 0;
  while (i < a.length && j < b.length) {
    if (a[i] === b[j]) { i += 1; j += 1; continue; }
    miss += 1;
    if (miss > 1) return false;
    if (a.length > b.length) i += 1; else if (b.length > a.length) j += 1; else { i += 1; j += 1; }
  }
  return miss + (a.length - i) + (b.length - j) <= 1;
}

/** How well a spoken request names a destination: the share of the request's words that appear in its label or aliases. */
function score(asked: readonly string[], d: Destination): number {
  if (asked.length === 0) return 0;
  const phrase = asked.join(" ");
  if ([d.label, ...d.aliases].some((n) => norm(n) === phrase)) return 2;
  let best = 0;
  for (const name of [d.label, ...d.aliases]) {
    const have = words(name);
    const hits = asked.filter((w) => have.some((h) => near(w, h))).length;
    // A request that names only part of the label still counts; one word in four hit is not an answer.
    best = Math.max(best, hits / asked.length);
  }
  return best;
}

export interface Resolution { destination?: Destination; ambiguous?: Destination[]; none?: true; tab?: WorkTab }

export class NavMap {
  private entries: Destination[] = [...PAGES];
  private loadedAt = -Infinity; // nothing read yet: the first refresh always reads
  private loading: Promise<void> | undefined;

  constructor(private readonly staleMs = 60_000, private readonly now: () => number = Date.now) {}

  get size(): number { return this.entries.length; }
  all(): readonly Destination[] { return this.entries; }

  /** Replace what is known about the daemon's things; the fixed pages and the config pages stay. */
  set(components: readonly ComponentRow[], loops: readonly LoopRow[], models: readonly RouteRow[]): void {
    const config = menuConfigPages().filter((p) => p.status === "ready").map<Destination>((p) => ({ kind: "config", label: p.label, aliases: [`${p.label} configuration`, `${p.label} settings`], to: `/config/${p.id}` }));
    const comps = components.map<Destination>((c) => ({ kind: "component", label: c.name, aliases: [c.slug, `${c.name} project`], to: workPath(c.id) }));
    const ls = loops.map<Destination>((l) => ({ kind: "loop", label: l.title || l.slug, aliases: [l.slug], to: `/loops/${encodeURIComponent(l.id)}` }));
    const ms = models.map<Destination>((m) => ({ kind: "model", label: m.displayName || m.model || m.id, aliases: [m.id, m.model ?? ""].filter(Boolean), to: `/models/${encodeURIComponent(m.id)}/edit` }));
    this.entries = [...PAGES, ...config, ...comps, ...ls, ...ms];
    this.loadedAt = this.now();
    this.componentRows = components;
  }

  private componentRows: readonly ComponentRow[] = [];

  /** Reads the daemon's lists, at most one read at a time, and only when what is held is stale (or `force`). */
  refresh(rpc: (method: string, params: unknown) => Promise<unknown>, force = false): Promise<void> {
    if (!force && this.now() - this.loadedAt < this.staleMs) return Promise.resolve();
    this.loading ??= (async () => {
      const [c, l, m] = await Promise.allSettled([rpc("component.list", {}), rpc("loop.list", { all: true }), rpc("routes.list", {})]);
      const got = <T,>(r: PromiseSettledResult<unknown>, key: string): T[] => (r.status === "fulfilled" ? ((r.value as Record<string, T[]>)?.[key] ?? []) : []);
      this.set(got<ComponentRow>(c, "components"), got<LoopRow>(l, "loops"), got<RouteRow>(m, "routes"));
    })().finally(() => { this.loading = undefined; });
    return this.loading;
  }

  /**
   * What a spoken request means. A tab word ("loops of the QA project") picks the tab on a project. A single clear best
   * match wins; a close second means ambiguous, and nothing close means none, so Orion asks rather than guesses.
   */
  resolve(request: string): Resolution {
    const hint = kindHint(request);
    let asked = words(request).filter((w) => !FILLER.has(w));
    const tabWord = asked.find((w) => (WORK_TABS as readonly string[]).includes(w) || w === "tasks" || w === "knowledge");
    const tab = tabWord ? (tabWord === "tasks" ? "plan" : (tabWord as WorkTab)) : undefined;
    if (tab && asked.length > 1) asked = asked.filter((w) => w !== tabWord);
    if (asked.length === 0) return { none: true };
    const ranked = this.entries
      .map((d) => ({ d, s: score(asked, d) + (hint && score(asked, d) > 0 ? (d.kind === hint ? 0.3 : -0.2) : 0) }))
      .filter((r) => r.s >= 0.5)
      .sort((a, b) => b.s - a.s);
    if (ranked.length === 0) return { none: true };
    const top = ranked[0]!;
    const close = ranked.filter((r) => r.s >= top.s - 0.1 && r.d.to !== top.d.to);
    if (close.length > 0 && top.s < 2) return { ambiguous: [top.d, ...close.map((r) => r.d)].slice(0, 4) };
    if (tab && top.d.kind === "component") {
      const row = this.componentRows.find((c) => c.name === top.d.label);
      if (row) return { destination: { ...top.d, to: workPath(row.id, tab) }, tab };
    }
    return { destination: top.d, tab };
  }
}

/** The one map, shared by everything that navigates by voice, so it stays in memory between requests. */
export const navMap = new NavMap();
