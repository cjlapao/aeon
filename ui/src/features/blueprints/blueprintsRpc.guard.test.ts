import { describe, expect, it } from "vitest";
import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";

// P34 — S48 T12: the blueprint management surface reaches the daemon ONLY through
// the registered blueprint doors below, plus the one seed door that restores a
// shipped row and the S45 proposal doors the reused desk opens. Never a socket,
// never a fetch, never browser storage.
//
// The sharper half of this guard is the second describe: the management surface
// parses no YAML. Everything it shows — whether a row can be deleted, what a copy
// is called, which revisions differ, what a flow is used by — arrives as an ANSWER
// (`canDelete` reads the `origin` the daemon sent; the copy's name comes back from
// `blueprint.duplicate`). The lane editor still rewrites TEXT, which is a draft
// tool and not a decision, so it is scanned for methods and not for the parse ban.
//
// Not vacuous, and says so: the `blueprints.frobnicate` canary IS caught and is NOT
// allowed; a synthetic `fetch(` IS caught (`refetch` is not); a synthetic
// `parseDocument` IS caught; every tab's surface is hit; both catalogs are checked.

const FEATURE_DIR = __dirname;
const POLICY_DIR = join(__dirname, "../settings/policy");
const CONTRACT = join(__dirname, "../../proto/contract.ts");

/** The eleven blueprint doors this surface opens plus the seed door that
 *  restores a shipped row. No proposal door is listed on purpose: the proposal
 *  desk is S45's own component under features/settings/policy — the human door
 *  that is allowed to name a decision — and this surface REUSES it rather than
 *  re-implementing the decision, so nothing in here names one. */
const ALLOWED = new Set([
  "blueprint.list",
  "blueprint.show",
  "blueprint.validate",
  "blueprint.save",
  "blueprint.duplicate",
  "blueprint.delete",
  "blueprint.usage",
  "blueprint.lint",
  "blueprint.history",
  "blueprint.diff",
  "blueprint.revert",
  "seed.restore",
  // The form doors: the daemon turns a flow's text into the structure the builder
  // edits and back, and states the vocabulary it offers. The browser parses no YAML.
  "blueprint.model",
  "blueprint.render",
  "blueprint.vocabulary",
  // The agents an agent step may name.
  "agent.list",
  // A shipped flow is locked; the route a lane runs on is the one thing a person sets
  // beside it, and the routes that may be named.
  "blueprint.override",
  "routes.list",
]);

/** ≥ 1 literal per surface — the guard saw the whole feature, not one file. */
const PER_TAB: Record<string, string[]> = {
  list: ["blueprint.list"],
  build: ["blueprint.show", "blueprint.validate", "blueprint.model", "blueprint.render", "blueprint.vocabulary", "agent.list"],
  edit: ["blueprint.save", "blueprint.duplicate", "blueprint.delete", "blueprint.lint"],
  revisions: ["blueprint.history", "blueprint.diff", "blueprint.revert", "seed.restore"],
  usage: ["blueprint.usage"],
};

/** Built by concatenation, the way `test/policy.test.tsx` builds its needles:
 *  naming a decision door here would trip the very ban that keeps decisions
 *  behind the human door, and this file must be greppable for what it forbids. */
const proposalDoor = (suffix: string): string => ["prop", "osal.", suffix].join("");

const CANARY_METHOD = "blueprints.frobnicate";

/** The files that decide NOTHING on their own: no YAML may be parsed in here. */
const MANAGEMENT_FILES = [
  "BlueprintsScreen.tsx",
  "panels/DiffView.tsx",
  "panels/UsagePanel.tsx",
  "panels/LintPanel.tsx",
  "panels/RevisionsPanel.tsx",
  "panels/Diagnostics.tsx",
  "blueprintsModel.ts",
];

/** i18n-shaped strings that are NOT catalog keys. */
const NON_KEYS = new Set([
  // The daemon's seed-set id, sent to `seed.restore` as a param.
  "blueprints.standard",
]);

function extractRpcMethods(src: string): string[] {
  const found: string[] = [];
  const re = /\b(?:rpc|rpcTyped|useRpcQuery)\s*\(\s*["']([^"']+)["']/g;
  let match: RegExpExecArray | null;
  while ((match = re.exec(src)) !== null) {
    const method = match[1];
    if (method !== undefined) found.push(method);
  }
  return found;
}

function productionSources(): Array<{ file: string; src: string }> {
  const sourceFiles = (dir: string, prefix: string) =>
    readdirSync(dir)
      .filter((f) => (f.endsWith(".ts") || f.endsWith(".tsx")) && !f.endsWith(".test.ts") && !f.endsWith(".test.tsx"))
      .map((f) => ({ file: join(prefix, f), src: readFileSync(join(dir, f), "utf8") }));
  return [...sourceFiles(FEATURE_DIR, "blueprints"), ...sourceFiles(join(FEATURE_DIR, "panels"), join("blueprints", "panels")), ...sourceFiles(join(FEATURE_DIR, "builder"), join("blueprints", "builder"))];
}

/** The S45 desk this tab REUSES. It lives behind the human door and may name a
 *  decision; it is scanned for the bans that hold everywhere (no socket, no
 *  storage, no YAML parsed in a management surface) and never for the door set,
 *  which is what THIS surface is allowed to name. */
function deskSource(): { file: string; src: string } {
  return { file: join("policy", "ProposalsDesk.tsx"), src: readFileSync(join(POLICY_DIR, "ProposalsDesk.tsx"), "utf8") };
}

function reusedSources(): Array<{ file: string; src: string }> {
  return [...productionSources(), deskSource()];
}

function managementSources(): Array<{ file: string; src: string }> {
  const builder = productionSources().filter((s) => s.file.includes("builder"));
  return [...MANAGEMENT_FILES.map((f) => ({ file: join("blueprints", f), src: readFileSync(join(FEATURE_DIR, f), "utf8") })), ...builder];
}

function registeredMethods(): Set<string> {
  const src = readFileSync(CONTRACT, "utf8");
  const names = new Set<string>();
  const re = /^ {2}"([a-zA-Z.]+)": \{ params:/gm;
  let match: RegExpExecArray | null;
  while ((match = re.exec(src)) !== null) {
    if (match[1] !== undefined) names.add(match[1]);
  }
  return names;
}

describe("blueprintsRpc guard (P34)", () => {
  it("runs over the real feature source (not vacuous)", () => {
    expect(productionSources().length).toBeGreaterThan(10);
  });

  it("calls only the registered blueprint and seed doors", () => {
    const registered = registeredMethods();
    expect(registered.size).toBeGreaterThan(0);
    const all = productionSources().flatMap(({ src }) => extractRpcMethods(src));
    expect(all.length).toBeGreaterThan(0);
    const offenders = all.filter((m) => !ALLOWED.has(m));
    expect(offenders, `methods outside the S48 set: ${offenders.join(", ")}`).toEqual([]);
    const unregistered = all.filter((m) => !registered.has(m));
    expect(unregistered, `methods the contract does not register: ${unregistered.join(", ")}`).toEqual([]);
  });

  it("names no proposal door: the desk is reused, not re-implemented", () => {
    // The proposals tab is S45's desk, imported. A flow-library surface that
    // called a decision itself would be a second authoring door — PL15's whole
    // point is that there is exactly one.
    const named = productionSources().flatMap(({ file, src }) =>
      extractRpcMethods(src).filter((m) => m.startsWith(proposalDoor(""))).map((m) => `${file} → ${m}`));
    expect(named, `proposal doors named here: ${named.join(", ")}`).toEqual([]);
    // And the reuse is real: the screen imports the desk from the human door.
    const screen = readFileSync(join(FEATURE_DIR, "BlueprintsScreen.tsx"), "utf8");
    expect(screen).toContain("ProposalsDesk");
    expect(screen).toContain("settings/policy");
    // The desk itself is not in this file's scan set — that is the point.
    expect(MANAGEMENT_FILES.some((f) => f.includes("ProposalsDesk"))).toBe(false);
  });

  it("reaches every surface's door at least once", () => {
    const all = new Set(productionSources().flatMap(({ src }) => extractRpcMethods(src)));
    for (const [tab, methods] of Object.entries(PER_TAB)) {
      const hit = methods.filter((m) => all.has(m));
      expect(hit.length, `surface ${tab} called none of ${methods.join(", ")}`).toBeGreaterThan(0);
    }
  });

  it("parses no YAML in the management surface: every answer comes from a door", () => {
    const parse = /\b(parseDocument|load|visit)\s*\(|from\s+["']yaml["']/;
    for (const { file, src } of [...managementSources(), deskSource()]) {
      expect(parse.test(src), `${file} parses YAML instead of asking the daemon`).toBe(false);
    }
    // And the ban has teeth: a synthetic duplicator is caught.
    expect(parse.test('import { parseDocument } from "yaml"; parseDocument(src);')).toBe(true);
  });

  it("has teeth (positive control): a synthetic blueprints.frobnicate is caught and refused", () => {
    const canary = `const x = rpcTyped("${CANARY_METHOD}", {});`;
    expect(extractRpcMethods(canary)).toContain(CANARY_METHOD);
    expect(ALLOWED.has(CANARY_METHOD)).toBe(false);
    expect(registeredMethods().has(CANARY_METHOD)).toBe(false);
  });

  it("has teeth (positive control): a synthetic direct fetch is caught, refetch is not", () => {
    const illegal = ["const r = await fet", 'ch("/blueprints"); q.refet', "ch();"].join("");
    const fetchCall = new RegExp("\\bfe" + "tch\\s*\\(");
    expect(fetchCall.test(illegal)).toBe(true);
    expect(fetchCall.test("queries.forEach((q) => q.refet" + "ch());")).toBe(false);
  });

  it("never opens a socket or stores state: no raw request object or browser storage", () => {
    for (const { file, src } of reusedSources()) {
      expect(new RegExp("\\bfe" + "tch\\s*\\(").test(src), `${file} calls fet` + "ch").toBe(false);
      expect(new RegExp("\\bXM" + "LHttpRequest\\b").test(src), `${file} opens a raw request`).toBe(false);
      expect(/\blocalStorage\b/.test(src), `${file} uses localStorage`).toBe(false);
      expect(/\bsessionStorage\b/.test(src), `${file} uses sessionStorage`).toBe(false);
    }
  });

  it("decides the origin badge in one place, and renders no English label", () => {
    const model = readFileSync(join(FEATURE_DIR, "blueprintsModel.ts"), "utf8");
    expect(/export function originBadge/.test(model)).toBe(true);
    // Both surfaces that show a badge ask the model rather than comparing the
    // wire value themselves — otherwise the list and the drawer can disagree
    // about what the same row is.
    for (const name of ["builder/FlowLibrary.tsx", "builder/FlowWorkspace.tsx"]) {
      const src = readFileSync(join(FEATURE_DIR, name), "utf8");
      expect(src.includes("originBadge("), `${name} decides the badge itself`).toBe(true);
    }
    // Labels come from the catalogs, never from a literal in a component.
    const literal = /["'](Shipped|Yours|Edited)["']/;
    expect(literal.test('const label = "Shipped";')).toBe(true);
    for (const { file, src } of managementSources()) {
      expect(literal.test(src), `${file} hardcodes an English label`).toBe(false);
    }
  });
});

/**
 * i18n coverage: every `blueprints.*` key the sources reference exists in BOTH
 * catalogs — parity is required, and a key that exists only in English is a
 * sentence half-translated.
 */
describe("blueprints i18n coverage", () => {
  const en = JSON.parse(readFileSync(join(__dirname, "../../lib/i18n/en.json"), "utf8")) as Record<string, unknown>;
  const pt = JSON.parse(readFileSync(join(__dirname, "../../lib/i18n/pt.json"), "utf8")) as Record<string, unknown>;

  function lookup(catalog: Record<string, unknown>, key: string): string | undefined {
    let node: unknown = catalog;
    for (const part of key.split(".")) {
      if (typeof node !== "object" || node === null) return undefined;
      node = (node as Record<string, unknown>)[part];
    }
    return typeof node === "string" ? node : undefined;
  }

  it("has teeth (positive control): a synthetic key is refused by the lookup", () => {
    expect(lookup(en, "blueprints.nope.not.a.key")).toBeUndefined();
    expect(lookup(en, "blueprints.title")).toBeTypeOf("string");
  });

  it("every blueprints key the sources reference exists in en and pt", () => {
    const keys = new Set<string>();
    const re = /"((?:blueprints)\.[^"']+)"/g;
    for (const { src } of productionSources()) {
      let match: RegExpExecArray | null;
      while ((match = re.exec(src)) !== null) {
        const key = match[1];
        if (key !== undefined && !NON_KEYS.has(key)) keys.add(key.split("#")[0] as string);
      }
    }
    expect(keys.size).toBeGreaterThan(30);
    const missing: string[] = [];
    for (const key of keys) {
      if (lookup(en, key) === undefined) missing.push(`en:${key}`);
      if (lookup(pt, key) === undefined) missing.push(`pt:${key}`);
    }
    expect(missing, `catalog gaps: ${missing.join(", ")}`).toEqual([]);
  });
});
