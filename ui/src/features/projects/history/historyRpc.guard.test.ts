import { describe, expect, it } from "vitest";
import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";
import { coverageTone, summarizeCoverage } from "./historyModel";

// S53 T5 — the Projects → History surface reaches the daemon ONLY through the
// five registered history doors, and ONLY through the generic rpc bridge:
// never a socket, never `fetch`, never browser storage (§4c), never a new
// Tauri command (P34). The modelled shape is `roadmapRpc.guard.test.ts`
// (S50 T7) and `triageRpc.guard.test.ts` (S51 T9): the allow-list equals
// EXACTLY the folder's usage, every literal lives in the one file that owns
// them, the contract itself settles whether a name is registered, and the
// catalog is honest in both languages.
//
// Two things this folder carries that roadmap's does not, because S53's
// acceptance criteria are different: the surface must hold **no write door at
// all** (HV3 — the five are four reads and one read that downloads), and it
// must hold **no vocabulary of its own** (HV2 — every lane and outcome word
// arrives from `history.vocabulary`). Both are census checks below, each with
// its own positive control, because a guard that cannot fail is a comment.

const HISTORY_DIR = __dirname;
const CONTRACT = join(__dirname, "../../../proto/contract.ts");
const EN = join(__dirname, "../../../lib/i18n/en-history.json");
const PT = join(__dirname, "../../../lib/i18n/pt-history.json");
/** The daemon's OWN served vocabulary, from its conformance fixtures — the
 *  census reads the words to forbid from the answer the surface is required
 *  to render, rather than from a list of words this file invented. */
const CONFORMANCE = join(__dirname, "../../../../../internal/protocol/conformance/history");

/** The five doors and only the five: four reads plus `history.export`, which
 *  is the SAME query as `history.list` answered to a person who asked to
 *  download it (HV3). Nothing else is sanctioned, and the shape of the list
 *  is the point: there is no `history.create`, `history.update`,
 *  `history.delete` or `history.edit` to sanction, because this slice adds no
 *  write control anywhere. `component.list` is deliberately absent — the
 *  scope picker's roots come from `../useComponentRoots.ts`, the projects
 *  folder's own read, so this folder spells no component door (roadmap's
 *  guard carries the same note for S49's blueprint picker). */
const ALLOWED = new Set(["history.list", "history.show", "history.coverage", "history.vocabulary", "history.export"]);

/** The method-name literal the first argument to rpc / rpcTyped / useRpcQuery
 *  carries (the same extractor roadmapRpc, triageRpc and automationRpc use),
 *  PLUS names assembled from a `"prefix." + "suffix"` concatenation: the
 *  census catches concatenated spellings too, so concatenation cannot smuggle
 *  a door past the allow-list. */
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

/** The production (non-test) sources the guard owns: the screen, the banner,
 *  the rows, the drill-down, the model and the doors. */
function productionSources(): Array<{ file: string; src: string }> {
  return readdirSync(HISTORY_DIR)
    .filter(
      (f: string) => (f.endsWith(".ts") || f.endsWith(".tsx")) && !f.endsWith(".test.ts") && !f.endsWith(".test.tsx"),
    )
    .map((f: string) => ({ file: `history/${f}`, src: readFileSync(join(HISTORY_DIR, f), "utf8") }));
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

/** Comments out of a source, so a doc comment naming a door is not read as a
 *  call, and a header sentence carrying a forbidden word is not a use. */
function stripComments(src: string): string {
  return src.replace(/\/\*[\s\S]*?\*\//g, " ").replace(/^\s*\/\/.*$/gm, " ");
}

/** The lane and outcome words the DAEMON serves, read from its own
 *  conformance answer for `history.vocabulary`. */
function servedVocabulary(): string[] {
  const fixture = JSON.parse(readFileSync(join(CONFORMANCE, "vocabulary.json"), "utf8")) as {
    response?: { result?: { lanes?: unknown; outcomes?: unknown } };
  };
  const result = fixture.response?.result ?? {};
  const groups = [result.lanes, result.outcomes].filter(Array.isArray) as string[][];
  return groups.flat().filter((word): word is string => typeof word === "string" && word !== "");
}

/** The one literal block in the folder allowed to spell a vocabulary word:
 *  the link-kind → route table. It is removed before the census, so the
 *  exemption is a named region rather than a file-wide pass. */
const LINK_ROUTES_BLOCK = /export const LINK_ROUTES[\s\S]*?\n\};/;

function vocabularyCensus(src: string): string[] {
  const words = servedVocabulary();
  const code = src.replace(LINK_ROUTES_BLOCK, " ");
  return words.filter((word) => new RegExp(`["'\`]${word}["'\`]`).test(code));
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

describe("historyRpc guard — the doors (P34, HV3)", () => {
  it("every method literal lives in historyRpc.ts alone", () => {
    const offenders = productionSources()
      .filter((s) => s.file !== "history/historyRpc.ts")
      .filter((s) => extractRpcMethods(stripComments(s.src)).length > 0)
      .map((s) => s.file);
    expect(offenders, `method literals outside historyRpc.ts: ${offenders.join(", ")}`).toEqual([]);
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

  it("the surface holds no write door: a sixth history name is refused", () => {
    // The negative the slice is built on (HV3). Each of these is the shape of
    // the edit a later change would make — a door added to the folder AND to
    // the allow-list — and each is caught here before it can be sanctioned.
    for (const name of ["history.delete", "history.create", "history.update", "history.edit", "history.set"]) {
      expect(ALLOWED.has(name), `${name} must never be an allowed door`).toBe(false);
      for (const source of productionSources()) {
        const src = stripComments(source.src);
        expect(src, `${source.file} spells ${name}`).not.toContain(`"${name}"`);
      }
    }
    // And the census's own teeth: a synthetic folder spelling a write door is
    // named by the same extractor the census uses.
    expect(extractRpcMethods('await rpcTyped("history.delete", { id });')).toEqual(["history.delete"]);
    expect(extractRpcMethods('const k = "history." + "delete"; rpcTyped(k, {});')).toEqual(["history.delete"]);
    expect(ALLOWED.has(extractRpcMethods('rpcTyped("history.delete", {})')[0] as string)).toBe(false);
  });

  it("the surface reaches nothing but the generic bridge — no fetch, no storage, no invoke", () => {
    for (const source of productionSources()) {
      const src = stripComments(source.src);
      expect(src, `${source.file} reaches for fetch`).not.toMatch(/\bfetch\s*\(/);
      expect(src, `${source.file} reaches for browser storage`).not.toMatch(/localStorage|sessionStorage|indexedDB|document\.cookie/);
      expect(src, `${source.file} reaches for a Tauri invoke`).not.toMatch(/\binvoke\s*\(/);
      // No second transport either: the folder opens no socket and no
      // event-stream object. The character classes are so this FILE does not
      // itself carry the literals `test/no-socket-in-web-layer.test.ts` walks
      // the whole web layer for — a guard that trips the guard it imitates is
      // the trap here, and each pattern still matches the real thing.
      expect(src, `${source.file} opens a socket`).not.toMatch(/new\s+We[b]Socket|new\s+Event[S]ource|XML\s*Ht\w+Request/);
    }
    // Teeth, with the words spelled in pieces for the same reason the pattern
    // is spelled in classes: each is the thing the pattern bans.
    const socketPattern = /new\s+We[b]Socket|new\s+Event[S]ource|XML\s*Ht\w+Request/;
    const offenders = [
      "new " + "Web" + "Socket(url)",
      "new " + "Event" + "Source('/events')",
      "XML" + "Http" + "Request()",
    ];
    for (const offender of offenders) expect(socketPattern.test(offender), `the socket pattern misses ${offender}`).toBe(true);
    expect(socketPattern.test('rpcTyped("history.list", {})')).toBe(false);
    // And the fetch pattern, with the same canary: a bare call is caught, a
    // `refetch(` callback is not (the shared reader's own name).
    const fetchPattern = /(?<![A-Za-z])fetch\s*\(/;
    expect(fetchPattern.test("await " + "fetc" + "h('/x')")).toBe(true);
    expect(fetchPattern.test("query.re" + "fetc" + "h()")).toBe(false);
  });

  it("the scope door stays owned by the projects folder (component.list is not spelled here)", () => {
    // `component.list` is the roots read `../useComponentRoots.ts` owns, and
    // `componentRoot` is the scope key the daemon refuses by name when named
    // beside `projectRoot` (AnswerHistoryCoverage). Neither word belongs in a
    // file this screen ships.
    for (const source of productionSources()) {
      const src = stripComments(source.src);
      expect(src, `${source.file} spells the component door`).not.toMatch(/component\.list|useComponentList/);
      expect(src, `${source.file} sends the second scope key`).not.toMatch(/componentRoot/);
    }
    // Teeth: the pattern catches both shapes it is aimed at.
    expect(/component\.list|useComponentList/.test('useRpcQuery("component.list", {}, c, l)')).toBe(true);
    expect(/componentRoot/.test('params.componentRoot = root')).toBe(true);
  });

  it("no production source carries a lane or outcome word of its own (HV2)", () => {
    // The words are `history.vocabulary`'s. The only region allowed to spell
    // one is the link-kind → route table in the model, which routes and never
    // labels; everything else renders what the answer carries.
    const words = servedVocabulary();
    expect(words.length).toBeGreaterThan(6); // the fixture really served the sets
    for (const source of productionSources()) {
      const found = vocabularyCensus(stripComments(source.src));
      expect(found, `${source.file} hardcodes vocabulary words: ${found.join(", ")}`).toEqual([]);
    }
    // Teeth: a synthetic component with a lane list of its own is caught, and
    // the exemption is the one block, not the file that holds it.
    expect(vocabularyCensus('const LANES = ["loop", "phase", "task", "agent"];')).toEqual(["loop", "phase", "task", "agent"]);
    expect(vocabularyCensus('<option>{"waiting"}</option>')).toEqual(["waiting"]);
    const model = productionSources().find((s) => s.file === "history/historyModel.ts");
    expect(model, "the model is missing").not.toBeUndefined();
    expect(vocabularyCensus(stripComments(model?.src as string))).toEqual([]);
    expect(LINK_ROUTES_BLOCK.test(model?.src as string)).toBe(true);
  });

  it("the three coverage states are distinct, and the floor travels verbatim (HV1, P31, P32)", () => {
    const answered = summarizeCoverage({
      scope: "/proj",
      live: { loops: 1, oldest: "2026-11-04T10:00:00Z", newest: "2026-11-04T10:00:00Z" },
      archived: { loops: 1, oldest: "2026-11-01T09:00:00Z", newest: "2026-11-01T09:00:00Z" },
      undecodable: { count: 0, ids: [] },
      floor: { statement: "A cost is a floor, never a bare number (\"at least $X (K of N steps unknown)\");" },
      notShown: [],
    });
    const warning = summarizeCoverage({
      scope: "",
      live: { loops: 0 },
      archived: { loops: 2 },
      undecodable: { count: 3, ids: ["a", "b", "c"] },
      floor: { statement: "A cost is a floor, never a bare number (\"at least $X (K of N steps unknown)\");" },
      notShown: [{ reason: "archived loops carry no steps", count: 2 }],
    });
    expect(answered?.state).toBe("healthy");
    expect(warning?.state).toBe("warning");
    // Three states, three readings — shown, cannot-say, not-shown — never one.
    expect(answered?.shown).toEqual({
      live: { loops: 1, oldest: "2026-11-04T10:00:00Z", newest: "2026-11-04T10:00:00Z" },
      archived: { loops: 1, oldest: "2026-11-01T09:00:00Z", newest: "2026-11-01T09:00:00Z" },
    });
    expect(warning?.cannotSay).toEqual({ count: 3, ids: ["a", "b", "c"] });
    expect(warning?.notShown).toEqual([{ reason: "archived loops carry no steps", count: 2 }]);
    expect(coverageTone(answered?.state)).not.toBe(coverageTone(warning?.state));
    // The sentence is not re-worded on the way through, in either state.
    expect(answered?.floor).toBe(warning?.floor);
    expect(answered?.floor).toContain("at least $X (K of N steps unknown)");
    // And the shape the fixture answered empty still reads (no crash, no row).
    expect(summarizeCoverage({ scope: "", live: { loops: 0 }, archived: { loops: 0 }, undecodable: { count: 0, ids: [] }, floor: { statement: "" }, notShown: [] })?.notShown).toEqual([]);
  });

  it("the guard has teeth (positive control): the extractor sees a literal call and a concatenated name", () => {
    expect(extractRpcMethods('const q = useRpcQuery("history.sneakyDoor", {}, c, l);')).toEqual(["history.sneakyDoor"]);
    expect(extractRpcMethods('await rpcTyped("history.sneaky", {});')).toEqual(["history.sneaky"]);
    expect(extractRpcMethods('const k = "history." + "sneak"; rpcTyped(k, {});')).toEqual(["history.sneak"]);
    expect(extractRpcMethods(stripComments('// rpcTyped("history.inComment", {})'))).toEqual([]);
    expect(extractRpcMethods(stripComments('/* const k = "history." + "inComment"; */'))).toEqual([]);
    // And an unregistered name is refused by the contract the daemon generates:
    const registered = registeredMethods();
    expect(registered.has("history.sneakyDoor")).toBe(false);
    expect(registered.has("history.list")).toBe(true);
  });
});

describe("history.manage i18n — honest across both languages", () => {
  const enDoc = JSON.parse(readFileSync(EN, "utf8")) as unknown;
  const ptDoc = JSON.parse(readFileSync(PT, "utf8")) as unknown;

  it("en and pt carry exactly the same key set under history.manage", () => {
    const enKeys = keysOf((enDoc as Record<string, unknown>)["history"], ["history"]).sort();
    const ptKeys = keysOf((ptDoc as Record<string, unknown>)["history"], ["history"]).sort();
    expect(enKeys.length).toBeGreaterThan(40);
    expect(ptKeys).toEqual(enKeys);
  });

  it("every key the production sources compose rides BOTH catalogs", () => {
    const literal = /t\(\s*"(history\.manage\.[a-zA-Z0-9._]+)"/g;
    const used = new Set<string>();
    for (const source of productionSources()) {
      let match: RegExpExecArray | null;
      while ((match = literal.exec(source.src)) !== null) {
        if (match[1] !== undefined) used.add(match[1]);
      }
    }
    expect(used.size).toBeGreaterThan(30);
    for (const key of used) {
      expect(lookup(enDoc, key), `${key} missing from en`).not.toBeUndefined();
      expect(lookup(ptDoc, key), `${key} missing from pt`).not.toBeUndefined();
    }
  });

  it("no production source writes a t() key OUTSIDE the history.manage namespace", () => {
    for (const source of productionSources()) {
      const strays = [...stripComments(source.src).matchAll(/\bt\(\s*"([a-zA-Z0-9._]+)"/g)]
        .map((m) => m[1] as string)
        .filter((key) => !key.startsWith("history.manage."));
      expect(strays, `${source.file} uses foreign keys: ${strays.join(", ")}`).toEqual([]);
    }
  });

  it("the Portuguese actually translates — no copied multi-word English", () => {
    const copied = keysOf((enDoc as Record<string, unknown>)["history"], ["history"]).filter((key) => {
      const enValue = lookup(enDoc, key);
      const ptValue = lookup(ptDoc, key);
      if (enValue === undefined || ptValue === undefined) return false;
      // Words like "daemon" and "component" legitimately ride into
      // Portuguese; three-word sentences never ride unchanged.
      return enValue === ptValue && enValue.split(/\s+/).length > 2;
    });
    expect(copied, `untranslated Portuguese copies: ${copied.join(", ")}`).toEqual([]);
  });

  it("has teeth (positive control): the key scan sees a literal key and a missing one", () => {
    const synthetic = 'const a = t("history.manage.brand.new.key");';
    expect(/t\(\s*"history\.manage\./.test(synthetic)).toBe(true);
    expect(lookup(enDoc, "history.manage.brand.new.key")).toBeUndefined();
    expect(lookup(ptDoc, "history.manage.brand.new.key")).toBeUndefined();
  });
});
