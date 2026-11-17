import { describe, expect, it } from "vitest";
import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";
import { actorTone, provenanceTone, scopeTone } from "./memoryModel";

// S52 T6 — the memory management surface reaches the daemon ONLY through
// the registered doors below, and ONLY through the generic rpc bridge:
// never a socket, never `fetch`, never browser storage (§4c), never a new
// Tauri command (P34). The plane's daemon-side guards (T1–T5) say the two
// retraction verbs are human doors and the actor is the peer's; this file
// keeps the PANEL from ever building a request the daemon would refuse,
// keeps every method literal of this surface in the one file that owns
// them (`memoryRpc.ts`), keeps the retract law out of the code — it rides
// the vocabulary answer and nothing else (LM9, RM9's one-string law),
// keeps the four provenances VISIBLY distinct (LM10), and keeps the
// catalog honest across both languages.

const MEMORY_DIR = __dirname;
const POLICY_DIR = join(__dirname, "../policy");
const AGENT_PICKER = join(__dirname, "../../../components/AgentPicker.tsx");
const CONTRACT = join(__dirname, "../../../proto/contract.ts");
const EN = join(__dirname, "../../../lib/i18n/en-memory.json");
const PT = join(__dirname, "../../../lib/i18n/pt-memory.json");

/** Every door the surface uses: the plane's five reads (`board`, `show`,
 *  `history`, `vocabulary`, `forAgent`), the two human acts (`retract`,
 *  `restore`), the one foreign read the source-loop link resolves through
 *  (`loop.list` — the loop view routes by id, a learning remembers only
 *  the slug), the S45 desk doors the REUSED `ProposalsDesk`/`ProposalCard`
 *  open for `learning_retract`, and the roster the reused `AgentPicker`
 *  reads. Nothing else — `learnings.list`/`learnings.query`/`learnings.add`
 *  are S28/bench doors this panel does not re-open, `learnings.embed` is a
 *  maintenance verb, and `learnings.mine.*` are the AGENT's doors (an
 *  agent speaks those, this person's panel never does; and no proposal is
 *  filed from here — the desk only ever decides ones a person or an agent
 *  already filed). The allow-list equals EXACTLY the folder's usage. */
const ALLOWED = new Set([
  "learnings.board",
  "learnings.map",
  "learnings.show",
  "learnings.history",
  "learnings.vocabulary",
  "learnings.forAgent",
  "learnings.retract",
  "learnings.restore",
  "loop.list",
  "proposal.list",
  "proposal.get",
  "proposal.commit",
  "proposal.reject",
  "proposal.discard",
  "agent.list",
]);

/** The method-name literal the first argument to rpc / rpcTyped / useRpcQuery
 *  carries (the same extractor automationRpc, roadmapRpc and triageRpc use),
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
 *  board, the drawer, the agent panel, the model and the doors — plus the
 *  reused S45 desk pieces and S43's picker ("reuse, not copy" means the
 *  reused code is held to this section's standard while it is used here). */
function productionSources(): Array<{ file: string; src: string }> {
  const files: Array<{ file: string; src: string }> = readdirSync(MEMORY_DIR)
    .filter(
      (f: string) =>
        (f.endsWith(".ts") || f.endsWith(".tsx")) &&
        !f.endsWith(".test.ts") &&
        !f.endsWith(".test.tsx"),
    )
    .map((f: string) => ({ file: `memory/${f}`, src: readFileSync(join(MEMORY_DIR, f), "utf8") }));
  for (const reused of ["ProposalsDesk.tsx", "ProposalCard.tsx"]) {
    files.push({ file: join("policy", reused), src: readFileSync(join(POLICY_DIR, reused), "utf8") });
  }
  files.push({ file: "components/AgentPicker.tsx", src: readFileSync(AGENT_PICKER, "utf8") });
  return files;
}

/** Files allowed to carry method literals: this surface's own door file,
 *  and the reused components whose OWN folders own their doors (the desk
 *  pair answers to `policy/`'s guard, the picker to its own). */
function isDoorFile(file: string): boolean {
  return file === "memory/memoryRpc.ts" || file.startsWith("policy/") || file === "components/AgentPicker.tsx";
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

describe("memoryRpc guard — the doors (P34)", () => {
  it("runs over the real section source (not vacuous)", () => {
    expect(productionSources().length).toBeGreaterThan(7);
  });

  it("every method literal of this surface lives in memoryRpc.ts alone", () => {
    const offenders = productionSources()
      .filter((s) => !isDoorFile(s.file))
      .filter((s) => extractRpcMethods(stripComments(s.src)).length > 0)
      .map((s) => s.file);
    expect(offenders, `method literals outside memoryRpc.ts: ${offenders.join(", ")}`).toEqual([]);
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

  it("no agent-own door is ever opened from the person's panel: learnings.mine.* off the set", () => {
    // LM8's separation lives here as much as in the daemon: the person's
    // surface proposes NOTHING — it retracts, restores, and reads. A
    // `learnings.mine.proposeRetract` call from this folder would be the
    // panel acting as an agent, which is exactly the shape this slice
    // refuses. The canary below proves the census would see it.
    for (const method of ["learnings.mine.add", "learnings.mine.query", "learnings.mine.show", "learnings.mine.proposeRetract"]) {
      expect(ALLOWED.has(method), `${method} must never be the person's panel's door`).toBe(false);
    }
    expect(ALLOWED.has("learnings.add")).toBe(false);
    expect(ALLOWED.has("learnings.query")).toBe(false);
    expect(ALLOWED.has("learnings.list")).toBe(false);
    expect(ALLOWED.has("learnings.embed")).toBe(false);
  });

  it("the surface reaches nothing but the generic bridge — no fetch, no storage, no invoke", () => {
    for (const source of productionSources()) {
      const src = stripComments(source.src);
      expect(src, `${source.file} reaches for fetch`).not.toMatch(/\bfetch\s*\(/);
      expect(src, `${source.file} reaches for browser storage`).not.toMatch(/localStorage|sessionStorage/);
      expect(src, `${source.file} reaches for a Tauri invoke`).not.toMatch(/\binvoke\s*\(/);
    }
  });

  it("no authority sentence is the code's — the retract law is READ from the vocabulary answer (LM9, RM9's one-string law)", () => {
    // The law exists exactly once on the daemon side (`RetractLaw` in Go)
    // and this folder only ever READS `retractLaw` off the vocabulary
    // answer. A sentence DEFINED here would be a second authority the
    // daemon never said — the CLI prints the same string before `--yes`,
    // and the confirm must equal it byte for byte.
    for (const source of productionSources()) {
      const src = stripComments(source.src);
      expect(src, `${source.file} defines a law literal`).not.toMatch(/(retract|restore|authority)Law\s*[:=]\s*["'`]/i);
      // Field READS (`vocabulary?.retractLaw`) stay; a long quoted
      // imperative in code would not survive review and is caught here:
      expect(src, `${source.file} quotes a sentence into JSX`).not.toMatch(/>\s*["'`][^"'`]*[a-z]+ [a-z]+ [a-z]+ [a-z]+ ["'`]\s*</);
    }
    // And the model carries no vocabulary word as a CONSTANT of its own —
    // the only closed-set spellings allowed are the tone maps' case
    // labels, and the census below pins them EXACTLY (a provenance word
    // used as logic, not as a colour key, would widen this list).
    const model = productionSources().find((s) => s.file === "memory/memoryModel.ts");
    expect(model).toBeDefined();
    const caseLabels = [...(model as { src: string }).src.matchAll(/case "([a-z-]+)":/g)].map((m) => m[1] as string);
    expect(caseLabels.sort()).toEqual(
      ["told", "observed", "assumed", "relayed", "person", "project", "agent", "user", "cli", "agent", "system"].sort(),
    );
    // The four provenance words exist ONLY as those colour keys: no
    // branch, filter or sort in the model may consult one.
    const body = (model as { src: string }).src.replace(/case "(told|observed|assumed|relayed|person|project|agent|user|cli|system)":/g, "");
    for (const word of ["told", "observed", "assumed", "relayed"]) {
      expect(body.includes(`"${word}"`), `memoryModel consults the provenance ${word} as logic`).toBe(false);
    }
  });

  it("the four provenances render visibly apart, and relayed is its own colour (LM10)", () => {
    const provenances = ["told", "observed", "assumed", "relayed"].map(provenanceTone);
    expect(new Set(provenances).size).toBe(4);
    // `relayed` in particular: a channel fact never shares a colour with
    // a person's word, a loop's sight, or a guess.
    expect(provenanceTone("relayed")).not.toBe(provenanceTone("told"));
    expect(provenanceTone("relayed")).not.toBe(provenanceTone("observed"));
    expect(provenanceTone("relayed")).not.toBe(provenanceTone("assumed"));
    const scopes = ["person", "project", "agent"].map(scopeTone);
    expect(new Set(scopes).size).toBe(3);
    // An unknown word is its own honest dot, never a rename or a silence:
    expect(provenanceTone("some-future-provenance")).toBe("violet");
    expect(scopeTone("some-future-scope")).toBe("slate");
    expect(new Set(["user", "cli", "agent", "system", "ghost"].map(actorTone)).size).toBe(4);
  });

  it("the guard has teeth (positive control): the extractor sees a literal call and a concatenated name", () => {
    expect(extractRpcMethods('const q = useRpcQuery("learnings.sneakyDoor", {}, c, l);')).toEqual(["learnings.sneakyDoor"]);
    expect(extractRpcMethods('await rpcTyped("learnings.mine.proposeRetract", {});')).toEqual(["learnings.mine.proposeRetract"]);
    expect(extractRpcMethods('const k = "learnings." + "sneak"; rpcTyped(k, {});')).toEqual(["learnings.sneak"]);
    expect(extractRpcMethods(stripComments('// rpcTyped("learnings.inComment", {})'))).toEqual([]);
    expect(extractRpcMethods(stripComments('/* const k = "learnings." + "inComment"; */'))).toEqual([]);
    // …a synthetic canary is caught by the census AND refused by the list:
    expect(ALLOWED.has("learnings.frobnicate")).toBe(false);
    expect(registeredMethods().has("learnings.frobnicate")).toBe(false);
    // …and a law literal would be caught by the sentence scan:
    expect('const retractLaw = "retract is a stamp, never a delete"'.match(/(retract|restore|authority)Law\s*[:=]\s*["'`]/i)).not.toBeNull();
  });
});

/**
 * i18n coverage: every settings.memory key the sources references rides
 * BOTH catalogs, the languages actually differ, and the surface speaks no
 * foreign namespace (the desk's own `settings.policy.*` keys belong to
 * S45's files, which are not scanned by this folder's t() census).
 */
describe("settings.memory i18n — honest across both languages", () => {
  const enDoc = JSON.parse(readFileSync(EN, "utf8")) as unknown;
  const ptDoc = JSON.parse(readFileSync(PT, "utf8")) as unknown;

  it("en and pt carry exactly the same key set under settings.memory", () => {
    const enKeys = keysOf((enDoc as Record<string, unknown>)["settings"], ["settings"]).sort();
    const ptKeys = keysOf((ptDoc as Record<string, unknown>)["settings"], ["settings"]).sort();
    expect(enKeys.length).toBeGreaterThan(30);
    expect(ptKeys).toEqual(enKeys);
  });

  it("every key the production sources compose rides BOTH catalogs", () => {
    const literal = /t\(\s*"(settings\.memory\.[a-zA-Z0-9._]+)"/g;
    const used = new Set<string>();
    for (const source of productionSources()) {
      let match: RegExpExecArray | null;
      while ((match = literal.exec(source.src)) !== null) {
        if (match[1] !== undefined) used.add(match[1]);
      }
    }
    expect(used.size).toBeGreaterThan(25);
    for (const key of used) {
      expect(lookup(enDoc, key), `${key} missing from en`).not.toBeUndefined();
      expect(lookup(ptDoc, key), `${key} missing from pt`).not.toBeUndefined();
    }
  });

  it("no production source writes a t() key OUTSIDE the settings.memory namespace", () => {
    for (const source of productionSources()) {
      if (isDoorFile(source.file)) continue; // the reused files answer to their own folders
      const strays = [...stripComments(source.src).matchAll(/\bt\(\s*"([a-zA-Z0-9._]+)"/g)]
        .map((m) => m[1] as string)
        .filter((key) => !key.startsWith("settings.memory."));
      expect(strays, `${source.file} uses foreign keys: ${strays.join(", ")}`).toEqual([]);
    }
  });

  it("the Portuguese actually translates — no copied multi-word English", () => {
    const copied = keysOf((enDoc as Record<string, unknown>)["settings"], ["settings"]).filter((key) => {
      const enValue = lookup(enDoc, key);
      const ptValue = lookup(ptDoc, key);
      if (enValue === undefined || ptValue === undefined) return false;
      // Words like "daemon", "agent" and "loop" legitimately ride into
      // Portuguese; three-word sentences never ride unchanged.
      return enValue === ptValue && enValue.split(/\s+/).length > 2;
    });
    expect(copied, `untranslated Portuguese copies: ${copied.join(", ")}`).toEqual([]);
  });

  it("has teeth (positive control): the key scan sees a literal key and refuses a synthetic gap", () => {
    const synthetic = 'const a = t("settings.memory.brand.new.key");';
    expect(/t\(\s*"settings\.memory\./.test(synthetic)).toBe(true);
    expect(lookup(enDoc, "settings.memory.brand.new.key")).toBeUndefined();
    expect(lookup(ptDoc, "settings.memory.brand.new.key")).toBeUndefined();
  });
});
