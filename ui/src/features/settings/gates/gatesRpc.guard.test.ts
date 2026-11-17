import { describe, expect, it } from "vitest";
import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";

// S57 T11 (VG17/VG20/VG22/VG23 on the app side) — the gates section and the
// loop's DoD card reach the daemon ONLY through the registered verification
// doors below and ONLY through the generic rpc bridge — never a socket, a
// fetch, or browser storage (§4c; no new Tauri command).
//
// Not vacuous, and says so: the verify.delete canary IS caught and is NOT
// allowed (the plane archives, it never deletes); a synthetic direct fetch IS
// caught (refetch is not); the scan runs over real sources; every tab's
// surface is hit; and the vocabulary law has teeth BOTH ways — a quoted
// protocol word compared in code is an offender UNLESS the line carries the
// `// wire-member` marker, and the marker count is pinned, so the exemption
// cannot quietly grow into a loophole.

const GATES_DIR = __dirname;
const DOD_CARD = join(__dirname, "../../loops/components/DodCard.tsx");
const CONTRACT = join(__dirname, "../../../proto/contract.ts");
const VERIFY_VOCABULARY = join(__dirname, "../../../../../internal/protocol/conformance/verify/vocabulary.json");

/** The S57 gate-authoring doors: the thirteen reads, the human-door writes
 *  this panel holds, and the roster the component picker reads. Everything
 *  outside this set offends the guard. */
const ALLOWED = new Set([
  "verify.vocabulary",
  "verify.list",
  "verify.templates",
  "verify.runs",
  "verify.runShow",
  "verify.add",
  "verify.update",
  "verify.archive",
  "verify.restore",
  "verify.applyTemplate",
  "verify.try",
  "mergerule.vocabulary",
  "mergerule.get",
  "mergerule.set",
  "mergerule.clear",
  "mergerule.preview",
  "dod.list",
  "dod.mark",
  "component.list",
]);

/** ≥ 1 literal per tab — the guard saw the whole surface, not one file. */
const PER_TAB: Record<string, string[]> = {
  gates: ["verify.list", "verify.vocabulary", "verify.templates", "verify.add", "verify.update", "verify.archive", "verify.restore", "verify.applyTemplate", "verify.try"],
  merge: ["mergerule.vocabulary", "mergerule.get", "mergerule.set", "mergerule.clear", "mergerule.preview"],
  runs: ["verify.runs", "verify.runShow"],
  dod: ["dod.list", "dod.mark"],
  picker: ["component.list"],
};

const CANARY_METHOD = "verify.delete"; // the plane archives; deletion is not a door

/** The method-name literal carried by rpc / rpcTyped / useRpcQuery. */
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
  const files = readdirSync(GATES_DIR)
    .filter(
      (f) =>
        (f.endsWith(".ts") || f.endsWith(".tsx")) &&
        !f.endsWith(".test.ts") &&
        !f.endsWith(".test.tsx") &&
        f !== "gatesRpc.guard.test.ts",
    )
    .map((f) => ({ file: join("gates", f), src: readFileSync(join(GATES_DIR, f), "utf8") }));
  files.push({ file: "loops/components/DodCard.tsx", src: readFileSync(DOD_CARD, "utf8") });
  return files;
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

/** The structural `// wire-member` exemption: a line that compares a
 *  protocol-constant member of a WIRE STRUCT (the source column's "own",
 *  the DoD state a human may stamp) may name it, on its own line, marked.
 *  The count is pinned in its own test — three lines exist today and
 *  exactly three may. */
const WIRE_MEMBER = "// wire-member";

function comparedVocabularyLines(src: string): Array<{ line: string; exempt: boolean }> {
  const words = ["required", "advisory", "pass", "fail", "unverified", "skipped", "own", "root", "baseline", "squash", "rebase", "allow", "refuse", "loop", "manual", "met", "unmet"];
  const hits: Array<{ line: string; exempt: boolean }> = [];
  for (const line of src.split("\n")) {
    for (const word of words) {
      const re = new RegExp(`(===|!==|==)\\s*["']${word}["']|["']${word}["']\\s*(===|!==|==)`);
      if (re.test(line)) {
        hits.push({ line, exempt: line.includes(WIRE_MEMBER) });
        break;
      }
    }
  }
  return hits;
}

describe("gatesRpc guard (S57 VG17)", () => {
  it("runs over the real section source (not vacuous)", () => {
    expect(productionSources().length).toBeGreaterThanOrEqual(11);
  });

  it("calls only the registered S57 methods", () => {
    const registered = registeredMethods();
    expect(registered.size).toBeGreaterThan(0);
    const all = productionSources().flatMap(({ src }) => extractRpcMethods(src));
    expect(all.length).toBeGreaterThan(0);
    const offenders = all.filter((m) => !ALLOWED.has(m));
    expect(offenders, `methods outside the S57 gate set: ${offenders.join(", ")}`).toEqual([]);
    const unregistered = all.filter((m) => !registered.has(m));
    expect(unregistered, `methods the contract does not register: ${unregistered.join(", ")}`).toEqual([]);
  });

  it("reaches every tab's surface at least once", () => {
    const all = new Set(productionSources().flatMap(({ src }) => extractRpcMethods(src)));
    for (const [tab, methods] of Object.entries(PER_TAB)) {
      const hit = methods.filter((m) => all.has(m));
      expect(hit.length, `tab ${tab} called none of ${methods.join(", ")}`).toBeGreaterThan(0);
    }
  });

  it("has teeth (positive control): a synthetic verify.delete is caught and refused", () => {
    const canary = `const x = rpcTyped("${CANARY_METHOD}", {});`;
    expect(extractRpcMethods(canary)).toContain(CANARY_METHOD);
    expect(ALLOWED.has(CANARY_METHOD)).toBe(false);
    expect(registeredMethods().has(CANARY_METHOD)).toBe(false);
  });

  it("has teeth (positive control): a synthetic direct fetch is caught, refetch is not", () => {
    const illegal = ["const r = await fet", 'ch("/verify/list"); q.refet', "ch();"].join("");
    const fetchCall = new RegExp("\\bfe" + "tch\\s*\\(");
    expect(fetchCall.test(illegal)).toBe(true);
    expect(fetchCall.test("queries.forEach((q) => q.refet" + "ch());")).toBe(false);
  });

  it("never opens a socket or stores state: no raw request object or browser storage", () => {
    for (const { file, src } of productionSources()) {
      expect(new RegExp("\\bfe" + "tch\\s*\\(").test(src), `${file} calls fet` + "ch").toBe(false);
      expect(new RegExp("\\bXM" + "LHttpRequest\\b").test(src), `${file} opens a raw request`).toBe(false);
      expect(/\blocalStorage\b/.test(src), `${file} uses localStorage`).toBe(false);
      expect(/\bsessionStorage\b/.test(src), `${file} uses sessionStorage`).toBe(false);
    }
  });

  it("names no closed set: the daemon's multi-word enum values appear nowhere in the section source", () => {
    // Values from the vocabulary and the floor codes that can never be
    // ordinary prose. The single-word states are policed by the
    // comparison law below (a word like "pass" appears in prose; an
    // enum VALUE compared in code is the thing that must never be hardcoded).
    const forbidden = [
      "all_met",
      "reported_green",
      "required_only",
      "forge-review-only",
      "local-not-required",
      "no-required-gates",
      "dod-not-required",
      "checks-not-binding",
      "no-gates-configured",
      "start-failed",
      "aeon_review",
    ];
    for (const { file, src } of productionSources()) {
      for (const word of forbidden) {
        expect(src.includes(word), `${file} hardcodes the daemon's value ${word}`).toBe(false);
      }
    }
  });

  it("compares no vocabulary word in code — except the three marked wire-member lines", () => {
    const hits = productionSources().flatMap(({ file, src }) =>
      comparedVocabularyLines(src).map((h) => ({ ...h, file })),
    );
    const offenders = hits.filter((h) => !h.exempt);
    expect(offenders.map((o) => `${o.file}: ${o.line.trim()}`), `unmarked vocabulary comparisons: ${offenders.length}`).toEqual([]);
    const exempt = hits.filter((h) => h.exempt);
    // Exactly the gatesModel source-member lines exist today: inheritedGates,
    // ownedRow, isOwnSource. A fourth marker anywhere is a new decision that
    // must be argued, not slipped in.
    expect(exempt.length, "marked wire-member comparisons").toBe(3);
    expect(new Set(exempt.map((e) => e.file))).toEqual(new Set(["gates/gatesModel.ts"]));
  });
});

/**
 * i18n coverage: every settings.gates / loops.dod key the sources reference
 * exists in BOTH catalogs — AND every word the conformance vocabulary ships
 * has a caption, because the selects and pills render
 * `t(\`settings.gates.word.${word}\`)` and a missing caption would silently
 * print the key.
 */
describe("gates i18n coverage", () => {
  // The gates namespace lives in its own catalog fragment (SIZE1 split of
  // the core catalogs — see lib/i18n/index.ts); the guard reads that
  // fragment, which is exactly what index.ts deep-merges back in.
  const en = JSON.parse(readFileSync(join(__dirname, "../../../lib/i18n/en-gates.json"), "utf8")) as Record<string, unknown>;
  const pt = JSON.parse(readFileSync(join(__dirname, "../../../lib/i18n/pt-gates.json"), "utf8")) as Record<string, unknown>;
  const enCore = JSON.parse(readFileSync(join(__dirname, "../../../lib/i18n/en.json"), "utf8")) as Record<string, unknown>;
  const ptCore = JSON.parse(readFileSync(join(__dirname, "../../../lib/i18n/pt.json"), "utf8")) as Record<string, unknown>;

  function lookup(catalog: Record<string, unknown>, key: string): string | undefined {
    let node: unknown = catalog;
    for (const part of key.split(".")) {
      if (typeof node !== "object" || node === null) return undefined;
      node = (node as Record<string, unknown>)[part];
    }
    return typeof node === "string" ? node : undefined;
  }

  it("has teeth (positive control): a synthetic key is refused by the lookup", () => {
    expect(lookup(en, "settings.gates.nope.not.a.key")).toBeUndefined();
    expect(lookup(en, "settings.gates.title")).toBeTypeOf("string");
  });

  it("every settings.gates/loops.dod key the sources reference exists in en and pt", () => {
    const keys = new Set<string>();
    const re = /"((?:settings\.gates|loops\.dod)\.[^"']+)"/g;
    for (const { src } of productionSources()) {
      let match: RegExpExecArray | null;
      while ((match = re.exec(src)) !== null) {
        if (match[1] !== undefined) keys.add(match[1].split("#")[0] as string);
      }
    }
    expect(keys.size).toBeGreaterThan(60);
    const missing: string[] = [];
    for (const key of keys) {
      if (lookup(en, key) === undefined) missing.push(`en:${key}`);
      if (lookup(pt, key) === undefined) missing.push(`pt:${key}`);
    }
    expect(missing, `catalog gaps: ${missing.join(", ")}`).toEqual([]);
  });

  it("every vocabulary word has a caption in en and pt (dynamic t() keys)", () => {
    const vocabulary = JSON.parse(readFileSync(VERIFY_VOCABULARY, "utf8")) as {
      response: { result: { requirements: string[]; states: string[] } };
    };
    const words = [
      ...vocabulary.response.result.requirements,
      ...vocabulary.response.result.states,
      // run triggers are protocol constants (loop | manual) with no
      // vocabulary door; the ledger captions them the same way.
      "loop",
      "manual",
    ];
    const missing: string[] = [];
    for (const word of words) {
      const key = `settings.gates.word.${word}`;
      if (lookup(en, key) === undefined) missing.push(`en:${key}`);
      if (lookup(pt, key) === undefined) missing.push(`pt:${key}`);
    }
    expect(missing, `word captions missing: ${missing.join(", ")}`).toEqual([]);
    // The DoD card's constants (the migration's CHECK, no dod.vocabulary
    // door) are captioned by name — pinned here so the card's dynamic key
    // can never render a raw constant.
    for (const state of ["unchecked", "met", "unmet", "unverifiable"]) {
      const key = `loops.dod.word.${state}`;
      expect(lookup(en, key), key).toBeTypeOf("string");
      expect(lookup(pt, key), key).toBeTypeOf("string");
    }
  });

  it("the split moved the namespaces — the core catalogs hold no gates copy", () => {
    // A duplicated subtree is the split failure that bites: index.ts merges
    // last-wins, so a stale core copy would silently win nothing or lose
    // silently. Exactly one home for each namespace, pinned.
    expect(lookup(enCore, "settings.gates.title")).toBeUndefined();
    expect(lookup(enCore, "loops.dod.failed")).toBeUndefined();
    expect(lookup(ptCore, "settings.gates.title")).toBeUndefined();
    expect(lookup(ptCore, "loops.dod.failed")).toBeUndefined();
    // And core keeps what core owns — the fragment took only its two
    // namespaces (the merge is additive, not a replacement).
    expect(lookup(enCore, "settings.mcp.title")).toBeTypeOf("string");
    expect(lookup(ptCore, "settings.mcp.title")).toBeTypeOf("string");
  });
});
