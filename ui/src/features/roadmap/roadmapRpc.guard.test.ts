import { describe, expect, it } from "vitest";
import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";

// S50 T7 — the roadmap management surface reaches the daemon ONLY through
// the registered doors below, and ONLY through the generic rpc bridge: never
// a socket, never `fetch`, never browser storage (§4c), never a new Tauri
// command (P34). The management half's own guards (the daemon side) say the
// verbs are human doors and the actor is the peer's; this file keeps the
// PANEL from ever building a request the daemon would refuse, keeps every
// method literal in the one file that owns them (`roadmapRpc.ts`), and keeps
// the catalog honest across both languages — the surface quotes daemon
// sentences, so its OWN keys are the only text it may write, and both
// languages must carry every one of them.

const ROADMAP_DIR = __dirname;
const CONTRACT = join(__dirname, "../../proto/contract.ts");
const EN = join(__dirname, "../../lib/i18n/en-roadmap.json");
const PT = join(__dirname, "../../lib/i18n/pt-roadmap.json");

/** Every door the surface uses: the plane's nine reads and verbs (`board`,
 *  `show`, `history`, `update`, `vocabulary`, `health` — the reads; `ideate`,
 *  `plan`, `materialize` — the person's dispatches), the two settings doors
 *  for the blueprint defaults, and the shared desk's four reads and
 *  decisions. Nothing else —
 *  `roadmap.proposeIdea` is an AGENT's door (a person authors directly; an
 *  agent proposes), so this panel must never call it, and `blueprint.list`
 *  is spelled inside S49's `ScheduleForm.tsx` by the reused BlueprintPicker,
 *  never in this folder. The allow-list equals EXACTLY the folder's usage —
 *  a door unused here must not ride the list, a door used here must appear. */
const ALLOWED = new Set([
  "roadmap.board",
  "roadmap.show",
  "roadmap.history",
  "roadmap.update",
  "roadmap.vocabulary",
  "roadmap.health",
  "roadmap.ideate",
  "roadmap.plan",
  "roadmap.materialize",
  "settings.get",
  "settings.set",
  "proposal.list",
  "proposal.get",
  // The desk's two decision doors. This folder is `test/policy.test.tsx`'s
  // SECOND NAMED caller for exactly these two (its proposalDoor predicate
  // exempts features/roadmap/ by name, 2026-11-04 S50 T7) — plain literals,
  // not a textual dodge. What the strip may DO with them stays narrowed to
  // roadmap_idea rows by the panel and by the parity/census checks below.
  "proposal.commit",
  "proposal.reject",
]);

/** The method-name literal the first argument to rpc / rpcTyped / useRpcQuery
 *  carries (the same extractor automationRpc, policyRpc and gatesRpc use),
 *  PLUS names assembled from a `"prefix." + "suffix"` string concatenation:
 *  the census catches concatenated spellings too, so concatenation cannot
 *  smuggle a door past the allow-list — the census would then name a usage
 *  ALLOWED never sanctioned. */
function extractRpcMethods(src: string): string[] {
  const found: string[] = [];
  const re = /\b(?:rpc|rpcTyped|useRpcQuery)\s*\(\s*["']([^"']+)["']/g;
  let match: RegExpExecArray | null;
  while ((match = re.exec(src)) !== null) {
    const method = match[1];
    if (method !== undefined) found.push(method);
  }
  const concat = /"([a-z.]+)\." \+ "([a-z.]+)"/g;
  while ((match = concat.exec(src)) !== null) {
    if (match[1] !== undefined && match[2] !== undefined) found.push(`${match[1]}.${match[2]}`);
  }
  return found;
}

/** The production (non-test) sources the guard owns: the page, the filters,
 *  the drawer, the edit form, the history panel, the settings card, the
 *  model and the doors. `render.ts` is S26's and untouched — it is scanned
 *  too: a method literal must never creep into it either. */
function productionSources(): Array<{ file: string; src: string }> {
  return readdirSync(ROADMAP_DIR)
    .filter(
          (f: string) =>
        (f.endsWith(".ts") || f.endsWith(".tsx")) &&
        !f.endsWith(".test.ts") &&
        !f.endsWith(".test.tsx"),
    )
    .map((f: string) => ({ file: `roadmap/${f}`, src: readFileSync(join(ROADMAP_DIR, f), "utf8") }));
}

/** The method names the generated contract registers — parsed from the
 *  generated file itself, so "registered" means the CONTRACT says so. */
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

/** Comments out of a source, so a doc comment naming a door is not read as
 *  a call, and a header sentence carrying a forbidden word is not a use. */
function stripComments(src: string): string {
  return src.replace(/\/\*[\s\S]*?\*\//g, " ").replace(/^\s*\/\/.*$/gm, " ");
}

function keysOf(catalog: unknown, prefix: string[]): string[] {
  if (typeof catalog !== "object" || catalog === null) return [];
  const out: string[] = [];
  for (const [key, value] of Object.entries(catalog as Record<string, unknown>)) {
    const path = [...prefix, key];
    if (typeof value === "string") out.push(path.join("."));
    else out.push(...keysOf(value, path));
  }
  return out;
}

function lookup(catalog: unknown, dotted: string): string | undefined {
  let node: unknown = catalog;
  for (const part of dotted.split(".")) {
    if (typeof node !== "object" || node === null) return undefined;
    node = (node as Record<string, unknown>)[part];
  }
  return typeof node === "string" ? node : undefined;
}

describe("roadmapRpc guard — the doors (P34)", () => {
  it("every method literal lives in roadmapRpc.ts alone", () => {
    const offenders = productionSources()
      .filter((s) => s.file !== "roadmap/roadmapRpc.ts")
      .filter((s) => extractRpcMethods(stripComments(s.src)).length > 0)
      .map((s) => s.file);
    expect(offenders, `method literals outside roadmapRpc.ts: ${offenders.join(", ")}`).toEqual([]);
  });

  it("the folder's usage equals the allow-list exactly — no more, no less", () => {
    const used = new Set<string>();
    for (const source of productionSources()) {
      for (const method of extractRpcMethods(stripComments(source.src))) used.add(method);
    }
    const extra = [...used].filter((m) => !ALLOWED.has(m));
    const unused = [...ALLOWED].filter((m) => !used.has(m));
    expect(extra, `unlisted doors: ${extra.join(", ")}`).toEqual([]);
    expect(unused, `allow-listed but never called: ${unused.join(", ")}`).toEqual([]);
  });

  it("every door the surface uses is registered in the generated contract", () => {
    const registered = registeredMethods();
    for (const method of ALLOWED) {
      expect(registered.has(method), `${method} is not in contract.ts`).toBe(true);
    }
  });

  it("the surface reaches nothing but the generic bridge — no fetch, no storage, no invoke", () => {
    for (const source of productionSources()) {
      const src = stripComments(source.src);
      expect(src, `${source.file} reaches for fetch`).not.toMatch(/\bfetch\s*\(/);
      expect(src, `${source.file} reaches for browser storage`).not.toMatch(/localStorage|sessionStorage/);
      expect(src, `${source.file} reaches for a Tauri invoke`).not.toMatch(/\binvoke\s*\(/);
    }
  });

  it("the guard has teeth (positive control): the extractor sees a literal call and a concatenated name", () => {
    expect(extractRpcMethods('const q = useRpcQuery("roadmap.sneakyDoor", {}, c, l);')).toEqual(["roadmap.sneakyDoor"]);
    expect(extractRpcMethods('await rpcTyped("proposal.sneaky", {});')).toEqual(["proposal.sneaky"]);
    // The concatenated spelling is counted too — concatenation cannot hide a
    // door from the census, since the assembled name is matched against the
    // same allow-list (this folder's sanctioned doors are spelled plain,
    // under test/policy.test.tsx's named proposalDoor exemption):
    expect(extractRpcMethods('const k = "proposal." + "sneak"; rpcTyped(k, {});')).toEqual(["proposal.sneak"]);
    // …and the comment-stripper keeps a named-but-uncalled door out:
    expect(extractRpcMethods(stripComments('// rpcTyped("roadmap.inComment", {})'))).toEqual([]);
    expect(extractRpcMethods(stripComments('/* const k = "roadmap." + "inComment"; */'))).toEqual([]);
  });
});

describe("roadmap.manage i18n — honest across both languages (AN11's discipline)", () => {
  const enDoc = JSON.parse(readFileSync(EN, "utf8")) as unknown;
  const ptDoc = JSON.parse(readFileSync(PT, "utf8")) as unknown;

  it("en and pt carry exactly the same key set under roadmap.manage", () => {
    const enKeys = keysOf((enDoc as Record<string, unknown>)["roadmap"], ["roadmap"]).sort();
    const ptKeys = keysOf((ptDoc as Record<string, unknown>)["roadmap"], ["roadmap"]).sort();
    expect(enKeys.length).toBeGreaterThan(40);
    expect(ptKeys).toEqual(enKeys);
  });

  it("every key the production sources compose rides BOTH catalogs", () => {
    const literal = /t\(\s*"(roadmap\.manage\.[a-zA-Z0-9._]+)"/g;
    const used = new Set<string>();
    for (const source of productionSources()) {
      let match: RegExpExecArray | null;
      while ((match = literal.exec(source.src)) !== null) {
        if (match[1] !== undefined) used.add(match[1]);
      }
    }
    // The two composed dials, spelled out — a tab or a verb added without a
    // catalog entry must fail here, not render a raw key.
    for (const name of ["edit", "history"]) used.add(`roadmap.manage.drawer.tab.${name}`);
    for (const verb of ["ideate", "plan", "materialize"]) used.add(`roadmap.manage.drawer.go.${verb}`);
    expect(used.size).toBeGreaterThan(30);
    for (const key of used) {
      expect(lookup(enDoc, key), `${key} missing from en`).not.toBeUndefined();
      expect(lookup(ptDoc, key), `${key} missing from pt`).not.toBeUndefined();
    }
  });

  it("no production source writes a t() key OUTSIDE the roadmap.manage namespace", () => {
    for (const source of productionSources()) {
      const strays = [...stripComments(source.src).matchAll(/\bt\(\s*"([a-zA-Z0-9._]+)"/g)]
        .map((m) => m[1] as string)
        .filter((key) => !key.startsWith("roadmap.manage."));
      expect(strays, `${source.file} uses foreign keys: ${strays.join(", ")}`).toEqual([]);
    }
  });

  it("the Portuguese actually translates — no copied multi-word English", () => {
    const copied = keysOf((enDoc as Record<string, unknown>)["roadmap"], ["roadmap"]).filter((key) => {
      const enValue = lookup(enDoc, key);
      const ptValue = lookup(ptDoc, key);
      if (enValue === undefined || ptValue === undefined) return false;
      // Words like "daemon", "blueprint" and "workspace" legitimately ride
      // into Portuguese; three-word sentences never ride unchanged.
      return enValue === ptValue && enValue.split(/\s+/).length > 2;
    });
    expect(copied, `untranslated Portuguese copies: ${copied.join(", ")}`).toEqual([]);
  });

  it("has teeth (positive control): the key scan sees a literal AND a composed key", () => {
    const synthetic = 'const a = t("roadmap.manage.brand.new.key"); const b = t(`roadmap.manage.drawer.tab.${name}`);';
    expect(/t\(\s*"roadmap\.manage\./.test(synthetic)).toBe(true);
    expect(/roadmap\.manage\.drawer\.tab\.\$\{/.test(synthetic)).toBe(true);
    // And a key no catalog carries is refused by the lookup the test uses:
    expect(lookup(enDoc, "roadmap.manage.brand.new.key")).toBeUndefined();
    expect(lookup(ptDoc, "roadmap.manage.brand.new.key")).toBeUndefined();
  });
});
