import { describe, expect, it } from "vitest";
import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";
import { healthTone, priorityTone, validationTone } from "./triageModel";

// S51 T7 — the triage management surface reaches the daemon ONLY through
// the registered doors below, and ONLY through the generic rpc bridge:
// never a socket, never `fetch`, never browser storage (§4c), never a new
// Tauri command (P34). The plane's own guards (the daemon side) say the
// verbs are human doors and the actor is the peer's; this file keeps the
// PANEL from ever building a request the daemon would refuse, keeps every
// method literal in the one file that owns them (`triageRpc.ts`), keeps
// every authority sentence out of the code (it rides the vocabulary
// answer and nothing else), keeps the two closed sets VISIBLY distinct
// (TM10), and keeps the catalog honest across both languages.

const TRIAGE_DIR = __dirname;
const CONTRACT = join(__dirname, "../../../proto/contract.ts");
const EN = join(__dirname, "../../../lib/i18n/en-triage.json");
const PT = join(__dirname, "../../../lib/i18n/pt-triage.json");

/** Every door the surface uses: the plane's five reads (`board`, `show`,
 *  `history`, `vocabulary`, `health`), the four human acts (`update`,
 *  `validate`, `handoff`, `confirm-duplicate`), and the two settings doors
 *  for the one declared blueprint key. Nothing else —
 *  `triage.file`/`triage.fileAs` are filing doors (the bench and the agent
 *  tool own them), `triage.list`/`triage.set-priority`/`triage.suggest-duplicates`
 *  are S29's bench doors this panel does not re-open, and `blueprint.list`
 *  is spelled inside S49's `ScheduleForm.tsx` by the reused BlueprintPicker,
 *  never in this folder. The allow-list equals EXACTLY the folder's usage —
 *  a door unused here must not ride the list, a door used here must appear. */
const ALLOWED = new Set([
  "triage.board",
  "triage.show",
  "triage.history",
  "triage.vocabulary",
  "triage.health",
  "triage.update",
  "triage.validate",
  "triage.handoff",
  "triage.confirm-duplicate",
  "settings.get",
  "settings.set",
]);

/** The method-name literal the first argument to rpc / rpcTyped / useRpcQuery
 *  carries (the same extractor automationRpc, roadmapRpc and gatesRpc use),
 *  PLUS names assembled from a `"prefix." + "suffix"` string concatenation:
 *  the census catches concatenated spellings too, so concatenation cannot
 *  smuggle a door past the allow-list. */
function extractRpcMethods(src: string): string[] {
  const found: string[] = [];
  const re = /\b(?:rpc|rpcTyped|useRpcQuery)\s*\(\s*["']([^"']+)["']/g;
  let match: RegExpExecArray | null;
  while ((match = re.exec(src)) !== null) {
    const method = match[1];
    if (method !== undefined) found.push(method);
  }
  const concat = /"([a-z.-]+)\." \+ "([a-z.-]+)"/g;
  while ((match = concat.exec(src)) !== null) {
    if (match[1] !== undefined && match[2] !== undefined) found.push(`${match[1]}.${match[2]}`);
  }
  return found;
}

/** The production (non-test) sources the guard owns: the section, the
 *  board, the drawer, the blueprint card, the model and the doors. */
function productionSources(): Array<{ file: string; src: string }> {
  return readdirSync(TRIAGE_DIR)
    .filter(
      (f: string) =>
        (f.endsWith(".ts") || f.endsWith(".tsx")) &&
        !f.endsWith(".test.ts") &&
        !f.endsWith(".test.tsx"),
    )
    .map((f: string) => ({ file: `triage/${f}`, src: readFileSync(join(TRIAGE_DIR, f), "utf8") }));
}

/** The method names the generated contract registers — parsed from the
 *  generated file itself, so "registered" means the CONTRACT says so. */
function registeredMethods(): Set<string> {
  const src = readFileSync(CONTRACT, "utf8");
  const names = new Set<string>();
  const re = /^ {2}"([a-zA-Z.-]+)": \{ params:/gm;
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

describe("triageRpc guard — the doors (P34)", () => {
  it("every method literal lives in triageRpc.ts alone", () => {
    const offenders = productionSources()
      .filter((s) => s.file !== "triage/triageRpc.ts")
      .filter((s) => extractRpcMethods(stripComments(s.src)).length > 0)
      .map((s) => s.file);
    expect(offenders, `method literals outside triageRpc.ts: ${offenders.join(", ")}`).toEqual([]);
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

  it("no authority sentence is the code's — statements are READ from the vocabulary answer (RM9's one-string law)", () => {
    // Statements exist exactly once on each side of the wire: `validateStatement`
    // and `handoffStatement` in Go print them, and this folder only ever
    // READS the fields off the vocabulary answer. A sentence DEFINED here
    // would be a second authority the daemon never said.
    for (const source of productionSources()) {
      const src = stripComments(source.src);
      expect(src, `${source.file} defines a statement literal`).not.toMatch(/(validate|handoff|dispatch|authority)Statement\s*[:=]\s*["'`]/i);
      // Field READS (`vocabulary?.validateStatement`) stay; a long quoted
      // imperative in code would not survive review and is caught here:
      expect(src, `${source.file} quotes a sentence into JSX`).not.toMatch(/>\s*["'`][^"'`]*[a-z]+ [a-z]+ [a-z]+ [a-z]+ ["'`]\s*</);
    }
    // And the model carries no vocabulary word as a CONSTANT of its own —
    // the only closed-set spellings allowed are the tone maps' case labels.
    const model = productionSources().find((s) => s.file === "triage/triageModel.ts");
    expect(model).toBeDefined();
    const caseLabels = [...(model as { src: string }).src.matchAll(/case "([a-z-]+)":/g)].map((m) => m[1] as string);
    expect(caseLabels.sort()).toEqual(["assumed", "could-not-tell", "invalid", "known", "missing", "not-reproduced", "ok", "reproduced", "unvalidated"].sort());
  });

  it("the two state sets render visibly apart — four validation tones, three priority tones (TM10)", () => {
    const validation = ["unvalidated", "reproduced", "not-reproduced", "could-not-tell"].map(validationTone);
    // `unvalidated` and `could-not-tell` in particular: different facts,
    // different colours — never a shared slate they fall into together.
    expect(new Set(validation).size).toBe(4);
    expect(validation[0]).not.toBe(validation[3]);
    const priority = ["known", "assumed", "unknown"].map(priorityTone);
    expect(new Set(priority).size).toBe(3);
    // An unknown word is its own honest dot, never a rename or a silence:
    expect(validationTone("some-future-state")).toBe("slate");
    expect(priorityTone("some-future-state")).toBe("slate");
    expect(new Set(["ok", "missing", "invalid", "unverifiable"].map(healthTone)).size).toBe(4);
  });

  it("the guard has teeth (positive control): the extractor sees a literal call and a concatenated name", () => {
    expect(extractRpcMethods('const q = useRpcQuery("triage.sneakyDoor", {}, c, l);')).toEqual(["triage.sneakyDoor"]);
    expect(extractRpcMethods('await rpcTyped("triage.sneaky", {});')).toEqual(["triage.sneaky"]);
    expect(extractRpcMethods('const k = "settings." + "sneak"; rpcTyped(k, {});')).toEqual(["settings.sneak"]);
    expect(extractRpcMethods(stripComments('// rpcTyped("triage.inComment", {})'))).toEqual([]);
    expect(extractRpcMethods(stripComments('/* const k = "triage." + "inComment"; */'))).toEqual([]);
    // …and a statement literal would be caught by the sentence scan:
    expect("const validateStatement = \"the daemon will start a loop\"".match(/(validate|handoff|dispatch|authority)Statement\s*[:=]\s*["'`]/i)).not.toBeNull();
  });
});

describe("settings.triage i18n — honest across both languages", () => {
  const enDoc = JSON.parse(readFileSync(EN, "utf8")) as unknown;
  const ptDoc = JSON.parse(readFileSync(PT, "utf8")) as unknown;

  it("en and pt carry exactly the same key set under settings.triage", () => {
    const enKeys = keysOf((enDoc as Record<string, unknown>)["settings"], ["settings"]).sort();
    const ptKeys = keysOf((ptDoc as Record<string, unknown>)["settings"], ["settings"]).sort();
    expect(enKeys.length).toBeGreaterThan(40);
    expect(ptKeys).toEqual(enKeys);
  });

  it("every key the production sources compose rides BOTH catalogs", () => {
    const literal = /t\(\s*"(settings\.triage\.[a-zA-Z0-9._]+)"/g;
    const used = new Set<string>();
    for (const source of productionSources()) {
      let match: RegExpExecArray | null;
      while ((match = literal.exec(source.src)) !== null) {
        if (match[1] !== undefined) used.add(match[1]);
      }
    }
    // The two verbs, composed at the confirm's go-button — a verb added
    // without a catalog entry must fail here, not render a raw key.
    for (const verb of ["validate", "handoff"]) used.add(`settings.triage.actions.go.${verb}`);
    expect(used.size).toBeGreaterThan(30);
    for (const key of used) {
      expect(lookup(enDoc, key), `${key} missing from en`).not.toBeUndefined();
      expect(lookup(ptDoc, key), `${key} missing from pt`).not.toBeUndefined();
    }
  });

  it("no production source writes a t() key OUTSIDE the settings.triage namespace", () => {
    for (const source of productionSources()) {
      const strays = [...stripComments(source.src).matchAll(/\bt\(\s*"([a-zA-Z0-9._]+)"/g)]
        .map((m) => m[1] as string)
        .filter((key) => !key.startsWith("settings.triage."));
      expect(strays, `${source.file} uses foreign keys: ${strays.join(", ")}`).toEqual([]);
    }
  });

  it("the Portuguese actually translates — no copied multi-word English", () => {
    const copied = keysOf((enDoc as Record<string, unknown>)["settings"], ["settings"]).filter((key) => {
      const enValue = lookup(enDoc, key);
      const ptValue = lookup(ptDoc, key);
      if (enValue === undefined || ptValue === undefined) return false;
      // Words like "daemon" and "blueprint" legitimately ride into
      // Portuguese; three-word sentences never ride unchanged.
      return enValue === ptValue && enValue.split(/\s+/).length > 2;
    });
    expect(copied, `untranslated Portuguese copies: ${copied.join(", ")}`).toEqual([]);
  });

  it("has teeth (positive control): the key scan sees a literal AND a composed key", () => {
    const synthetic = 'const a = t("settings.triage.brand.new.key"); const b = t(`settings.triage.actions.go.${verb}`);';
    expect(/t\(\s*"settings\.triage\./.test(synthetic)).toBe(true);
    expect(/settings\.triage\.actions\.go\.\$\{/.test(synthetic)).toBe(true);
    expect(lookup(enDoc, "settings.triage.brand.new.key")).toBeUndefined();
    expect(lookup(ptDoc, "settings.triage.brand.new.key")).toBeUndefined();
  });
});
