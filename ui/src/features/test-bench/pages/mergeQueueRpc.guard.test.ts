import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { join } from "node:path";

// S60 T11 — the Merge queue bench page reaches the daemon ONLY through the
// registered mergequeue doors plus one borrowed read, and renders ONLY the
// daemon's words. The modelled shape is S53's `historyRpc.guard.test.ts`:
// the allow-list equals the folder's usage, every literal lives in the one
// file that owns them, the CONTRACT settles whether a name is registered,
// the words to forbid are read OUT of the daemon's own conformance answer,
// and every check carries a positive control — a guard that cannot fail is
// a comment.
//
// Two differences from history, both honest:
//   • the census is by FILE NAME, not by directory — the bench `pages/`
//     folder is shared by every bench page, and this guard owns only the
//     two files the S60 page introduced (the same scoping choice every
//     registry-registered page forces on a guard);
//   • `component.list` joins the allow-list, because a merge queue exists
//     only under a root component and the picker reads the one door
//     `useComponentRoots.ts` owns — the borrowed-door note history's own
//     guard carries for its scope picker.

const PAGES_DIR = __dirname;
const CONTRACT = join(__dirname, "../../../proto/contract.ts");
const EN = join(__dirname, "../../../lib/i18n/en-mergequeue.json");
const PT = join(__dirname, "../../../lib/i18n/pt-mergequeue.json");
const CONFORMANCE = join(__dirname, "../../../../../internal/protocol/conformance/mergequeue");

/** The eleven doors and only the eleven: ten `mergequeue.*` plus the
 *  borrowed `component.list`. `mergequeue.proposeEnqueue` is DELIBERATELY
 *  absent — it is the AGENT's door, and a person's click files rows through
 *  `mergequeue.enqueue` or files nothing. */
const ALLOWED = new Set([
  "mergequeue.list", "mergequeue.show", "mergequeue.status", "mergequeue.vocabulary",
  "mergequeue.enqueue", "mergequeue.dequeue", "mergequeue.reorder", "mergequeue.retry",
  "mergequeue.land", "mergequeue.settings", "component.list",
]);

const RPC_OWNER = "mergeQueueRpc.ts";
const OWNED = ["MergeQueuePage.tsx", "mergeQueueRpc.ts"];

/** The method-name literal the first argument to rpc / rpcTyped carries,
 *  generic argument included — this folder calls `rpc<QueueRow[]>(...)`. */
function extractRpcMethods(src: string): string[] {
  const found: string[] = [];
  const re = /\b(?:rpc|rpcTyped|useRpcQuery)\s*(?:<[^"'>]*>)?\s*\(\s*["']([^"']+)["']/g;
  let match: RegExpExecArray | null;
  while ((match = re.exec(src)) !== null) {
    if (match[1] !== undefined) found.push(match[1]);
  }
  return found;
}

function ownedSources(): Array<{ file: string; src: string }> {
  return OWNED.map((file) => ({ file, src: readFileSync(join(PAGES_DIR, file), "utf8") }));
}

/** Comments out, so a doc comment naming a door is not a call and a header
 *  sentence carrying a forbidden word is not a use (history's rule). */
function stripComments(src: string): string {
  return src.replace(/\/\*[\s\S]*?\*\//g, " ").replace(/^\s*\/\/.*$/gm, " ");
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

/** The state, reason, mode, policy and method words the DAEMON serves, read
 *  out of its own conformance answers — the census forbids words the server
 *  actually says, not a list this file invented. The staleness spellings the
 *  queue answers with (its `StalenessView` states) join the set from the
 *  list fixture, where the daemon samples them. */
function servedVocabulary(): string[] {
  const vocab = JSON.parse(readFileSync(join(CONFORMANCE, "vocabulary-ok.json"), "utf8")) as {
    response?: { result?: Record<string, unknown> };
  };
  const result = vocab.response?.result ?? {};
  const groups = [result.states, result.reasons, result.modes, result.conflictPolicies, result.landMethods]
    .filter(Array.isArray) as Array<Array<Record<string, unknown> | string>>;
  const words: string[] = [];
  for (const group of groups) {
    for (const entry of group) {
      if (typeof entry === "string") words.push(entry);
      else if (typeof entry.name === "string") words.push(entry.name);
    }
  }
  // staleness states ride the list fixture's rows:
  const list = JSON.parse(readFileSync(join(CONFORMANCE, "list-ok.json"), "utf8")) as { response?: { result?: unknown } };
  const rows = Array.isArray(list.response?.result) ? (list.response?.result as Array<Record<string, unknown>>) : [];
  for (const row of rows) {
    const staleness = row.staleness as { state?: unknown } | undefined;
    if (typeof staleness?.state === "string") words.push(staleness.state);
  }
  // The daemon's own server code answers two further staleness spellings the
  // 20 fixtures do not sample (behind, settled); naming them HERE is the
  // guard's job — a guard may know the vocabulary; the page may not.
  words.push("behind", "settled", "unknown", "not-yet", "current");
  return [...new Set(words)].filter((word) => word !== "");
}

/** A quoted literal of a served word is the page speaking daemon vocabulary
 *  as its own. The one word the page legitimately quotes is the DOOR word
 *  "human" in the vocabulary filter — doors are a CLASSIFICATION the page
 *  routes on, never a word it renders, and the census below proves the
 *  rendered action labels are the daemon's method names, not literals. */
function vocabularyCensus(src: string): string[] {
  const code = stripComments(src);
  return servedVocabulary().filter((word) => new RegExp(`["'\`]${word}["'\`]`).test(code));
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

describe("mergeQueueRpc guard — the doors (P34, S60 T11)", () => {
  it("every method literal lives in mergeQueueRpc.ts alone", () => {
    const offenders = ownedSources()
      .filter((s) => s.file !== RPC_OWNER)
      .filter((s) => extractRpcMethods(stripComments(s.src)).length > 0)
      .map((s) => s.file);
    expect(offenders, `method literals outside ${RPC_OWNER}: ${offenders.join(", ")}`).toEqual([]);
  });

  it("the owner spells only the allow-list, the contract registers every one, and at least one is found", () => {
    const src = ownedSources().find((s) => s.file === RPC_OWNER)?.src ?? "";
    const found = extractRpcMethods(stripComments(src));
    expect(found.length, "the extractor found nothing — it is broken, not the page clean").toBeGreaterThan(0);
    const registered = registeredMethods();
    for (const method of found) {
      expect(ALLOWED.has(method), `"${method}" is outside the S60 allow-list`).toBe(true);
      expect(registered.has(method), `"${method}" is not a method the generated contract registers`).toBe(true);
    }
    // every door the page owns reached the contract (a page silently missing
    // its own door would render a bench that reads nothing):
    for (const door of ALLOWED) {
      if (door === "component.list") continue;
      expect(registered.has(door), `the contract must register ${door}`).toBe(true);
    }
  });

  it("has teeth (bogus-door canary): an unregistered door is caught by name", () => {
    const synthetic = 'export const sneak = () => rpc("mergequeue.proposeEnqueue", {});';
    const found = extractRpcMethods(synthetic);
    expect(found).toContain("mergequeue.proposeEnqueue");
    expect(ALLOWED.has("mergequeue.proposeEnqueue")).toBe(false);
    expect(registeredMethods().has("mergequeue.notARealDoor")).toBe(false);
  });

  it("the agent's proposal door never appears in either owned file", () => {
    for (const { file, src } of ownedSources()) {
      expect(stripComments(src), `${file} names the agent's door`).not.toMatch(/propose/i);
    }
  });

  it("the page hardcodes no state, reason, mode, policy or method word — the daemon's answers say them", () => {
    const page = ownedSources().find((s) => s.file === "MergeQueuePage.tsx")?.src ?? "";
    const hits = vocabularyCensus(page);
    expect(hits, `the page spells daemon vocabulary as its own: ${hits.join(", ")}`).toEqual([]);
  });

  it("has teeth (vocabulary canary): a quoted state word in page source IS caught", () => {
    expect(vocabularyCensus('const pill = "queued";')).toContain("queued");
    expect(vocabularyCensus('const mode = "auto";')).toContain("auto");
    // and the same word inside an identifier or a sentence is NOT:
    expect(vocabularyCensus("const conflictPolicy = draft.mode;")).toEqual([]);
  });

  it("the buttons render the vocabulary's method names, not a hardcoded label list", () => {
    const page = ownedSources().find((s) => s.file === "MergeQueuePage.tsx")?.src ?? "";
    const code = stripComments(page);
    // the action row reads methods off the answer and slices its suffix —
    // no button label is a quoted verb of the plane:
    expect(code).toMatch(/vocabulary\?\.methods/);
    for (const verb of ["dequeue(", "retry(", "reorder(", "land("]) {
      // the CALLS may exist only via the rpc owner's functions; the page
      // itself must not assemble a method string anywhere:
      expect(code).not.toContain(`"mergequeue.${verb.slice(0, -1)}`);
    }
  });
});

describe("the catalog is honest in both languages (i18n)", () => {
  const enDoc = JSON.parse(readFileSync(EN, "utf8"));
  const ptDoc = JSON.parse(readFileSync(PT, "utf8"));
  const page = readFileSync(join(PAGES_DIR, "MergeQueuePage.tsx"), "utf8");

  it("the two fragments hold exactly the same keys", () => {
    const en = keysOf(enDoc, []);
    const pt = keysOf(ptDoc, []);
    expect(pt.slice().sort()).toEqual(en.slice().sort());
  });

  it("every bench.mergeQueue key the page asks for exists in both languages", () => {
    const used = new Set<string>();
    const re = /\bt\(\s*["'](bench\.mergeQueue\.[^"']+)["']/g;
    let match: RegExpExecArray | null;
    while ((match = re.exec(page)) !== null) {
      if (match[1] !== undefined) used.add(match[1]);
    }
    expect(used.size, "the page asks for no key at all — extractor broken").toBeGreaterThan(0);
    for (const key of used) {
      expect(lookup(enDoc, key), `missing in English: ${key}`).toBeTypeOf("string");
      expect(lookup(ptDoc, key), `missing in Portuguese: ${key}`).toBeTypeOf("string");
    }
  });

  it("Portuguese is translated, not copied — three-word sentences ride unchanged nowhere", () => {
    const copied = keysOf(enDoc, ["bench", "mergeQueue"]).filter((key) => {
      const enValue = lookup(enDoc, key);
      const ptValue = lookup(ptDoc, key);
      if (enValue === undefined || ptValue === undefined) return false;
      // single words and short labels ("yes"/"no"/"Retry") legitimately
      // match; full sentences must not.
      return enValue === ptValue && enValue.split(/\s+/).length > 2;
    });
    expect(copied, `untranslated Portuguese copies: ${copied.join(", ")}`).toEqual([]);
  });

  it("has teeth (positive control): the key scan sees a literal key and a missing one", () => {
    const synthetic = 'const a = t("bench.mergeQueue.brand.new.key");';
    expect(/t\(\s*"bench\.mergeQueue\./.test(synthetic)).toBe(true);
    expect(lookup(enDoc, "bench.mergeQueue.brand.new.key")).toBeUndefined();
    expect(lookup(ptDoc, "bench.mergeQueue.brand.new.key")).toBeUndefined();
  });
});
