import { describe, expect, it } from "vitest";
import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";

// S59 T11 — the daemon's own surface is the primary interface, and this guard
// keeps the Environments section that way: the section reaches the daemon ONLY
// through the registered `environment.*`, `secretref.*` and `component.*`
// methods below, and ONLY through the generic rpc bridge — never a socket,
// never fet`ch`, never browser storage (§4c), never a new Tauri command.
//
// What the section may NOT contain, and each rule says so with its own
// positive control so the green suite above means something:
//   • a method the contract does not register (canary: `environment.frobnicate`);
//   • a closed set typed by hand — backend names, egress modes, source kinds,
//     deny words (the vocabulary door publishes them; the panel reads them);
//   • an identity param (`actor`, `origin`, …) or a credential param (`value`,
//     `token`, …) — the plane's own door guard refuses those keys, so a panel
//     that sent one would be building a door the daemon already closed;
//   • a catalog key that ships in one language only.

const ENVIRONMENTS_DIR = join(__dirname, "../src/features/settings/environments");
const CONTRACT = join(__dirname, "../src/proto/contract.ts");
const EN = join(__dirname, "../src/lib/i18n/en-environments.json");
const PT = join(__dirname, "../src/lib/i18n/pt-environments.json");

/** The 21 doors S59 T11 uses: thirteen `environment.*`, seven `secretref.*`,
 *  two `component.*`. Nothing else — `environment.proposeRestrict` is the
 *  agent's door and this panel has no business calling it. */
const ALLOWED = new Set([
  "environment.list",
  "environment.show",
  "environment.effective",
  "environment.vocabulary",
  "environment.changes",
  "environment.provisions",
  "environment.create",
  "environment.update",
  "environment.archive",
  "environment.restore",
  "environment.bind",
  "environment.unbind",
  "environment.teardown",
  "secretref.list",
  "secretref.show",
  "secretref.check",
  "secretref.create",
  "secretref.archive",
  "secretref.restore",
  "component.list",
  "component.show",
]);

/** At least one literal from every tab — the guard saw the whole surface, not
 *  one lucky file. The floor is each tab's full contracted surface: a tab that
 *  quietly stops reading its own door drops below it and fails. */
const PER_TAB: Record<string, string[]> = {
  environments: ["environment.list", "environment.show", "environment.changes", "environment.create", "environment.update", "environment.archive", "environment.restore", "environment.bind", "environment.unbind", "component.list"],
  secretrefs: ["secretref.list", "secretref.show", "secretref.check", "secretref.create", "secretref.archive", "secretref.restore"],
  provisions: ["environment.provisions", "environment.teardown"],
  effective: ["environment.effective", "component.list", "component.show"],
  vocabulary: ["environment.vocabulary"],
};

/** The canary the positive control fires on — a method that exists nowhere. */
const CANARY_METHOD = "environment.frobnicate";

/** The closed sets the vocabulary door publishes and the panel must therefore
 *  never name: backend names, egress modes, source kinds, deny words. */
const FORBIDDEN_VOCABULARY = [
  "host-inherit",
  "host-scrubbed",
  "container",
  "vm",
  "open",
  "proxy",
  "proxy-required",
  "unenforced",
  "env-var",
  "key-file",
  "store",
  "KEY",
  "TOKEN",
  "SECRET",
  "PASSWORD",
];

/** The identity vocabulary `internal/protocol` refuses by key (EV17): the
 *  caller names what to act on, never who acted or where a value lives.
 *  `source` and `sourceKind` are the identity vocabulary on the `environment.*`
 *  doors and the whole POINT of a secret reference (the pointer is its
 *  `sourceKind` + `source`), so they are checked per-call against the
 *  `environment.*` payloads below rather than banned outright. */
const FORBIDDEN_IDENTITY_KEYS = [
  "actor",
  "actorKind",
  "origin",
  "legacy",
  "revision",
  "createdAt",
  "updatedAt",
  "createdBy",
  "updatedBy",
];

/** Refused on the `environment.*` WRITE doors in addition to the list above. */
const FORBIDDEN_ENVIRONMENT_KEYS = ["source", "sourceKind"];

/** The doors that change a row. Identity keys are refused on these absolutely
 *  (`internal/protocol` refuses them by key before semantic validation). */
const WRITE_METHODS = new Set([
  "environment.create",
  "environment.update",
  "environment.archive",
  "environment.restore",
  "environment.bind",
  "environment.unbind",
  "environment.teardown",
  "secretref.create",
  "secretref.archive",
  "secretref.restore",
]);

/** The three identity words a READ legitimately filters on: the daemon's own
 *  rule is "the identity vocabulary is refused on the writes and — minus the
 *  filters a read legitimately takes — on the reads". `environment.list` filters
 *  by `origin`, `secretref.list` by `sourceKind` + `source`. Nothing else. */
const READ_FILTER_KEYS = new Set(["origin", "sourceKind", "source"]);

/** The credential vocabulary (EV7, §4c): a reference is a name and a pointer. */
const FORBIDDEN_CREDENTIAL_KEYS = [
  "value",
  "secret",
  "secretValue",
  "token",
  "apiKey",
  "key",
  "password",
  "credential",
  "contents",
  "data",
  "raw",
  "bytes",
  "envValue",
];

/** The method-name literal the first argument to rpc / rpcTyped / useRpcQuery
 *  carries (the same extractor policyRpc and gatesRpc use). */
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

/** The production (non-test) sources the guard owns: the section, the four tab
 *  bodies, the form, the dialog, the rows, the model, the doors. */
function productionSources(): Array<{ file: string; src: string }> {
  return readdirSync(ENVIRONMENTS_DIR)
    .filter(
      (f) =>
        (f.endsWith(".ts") || f.endsWith(".tsx")) &&
        !f.endsWith(".test.ts") &&
        !f.endsWith(".test.tsx"),
    )
    .map((f) => ({ file: `environments/${f}`, src: readFileSync(join(ENVIRONMENTS_DIR, f), "utf8") }));
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

/** Comments out of a source, so a doc comment naming a refused key is not read
 *  as a param (the guard's own vocabulary lists the keys it forbids). */
function stripComments(src: string): string {
  return src.replace(/\/\*[\s\S]*?\*\//g, " ").replace(/^\s*\/\/.*$/gm, " ").replace(/`[\s\S]*?`/g, "``");
}

/** A key position for `word`: `word:`, `"word":`, `'word':` — not an identifier
 *  that merely ends with it (`expectedRevision`, `secretRefNames`). */
function declaresKey(src: string, word: string): boolean {
  return new RegExp(`(?:^|[{,(\\s])['"]?${word}['"]?\\s*:`, "m").test(src);
}

/** The payload literal each rpc call carries, per call site. */
function extractCallPayloads(src: string): Array<[string, string, string]> {
  const found: Array<[string, string, string]> = [];
  const re = /\b(?:rpc|rpcTyped|useRpcQuery)\s*\(\s*"([^"]+)"\s*,\s*([\s\S]*?)\)\s*[;,)\]]/g;
  let match: RegExpExecArray | null;
  while ((match = re.exec(src)) !== null) {
    if (match[1] !== undefined && match[2] !== undefined) found.push([match[0], match[1], match[2]]);
  }
  return found;
}

function environmentsOf(catalog: Record<string, unknown>): Record<string, unknown> {
  const settings = catalog["settings"];
  if (typeof settings !== "object" || settings === null) throw new Error("the catalog has no settings section");
  const environments = (settings as Record<string, unknown>)["environments"];
  if (typeof environments !== "object" || environments === null) throw new Error("the catalog has no settings.environments section");
  return environments as Record<string, unknown>;
}

function lookup(catalog: Record<string, unknown>, key: string): string | undefined {
  let node: unknown = catalog;
  for (const part of key.split(".")) {
    if (typeof node !== "object" || node === null) return undefined;
    node = (node as Record<string, unknown>)[part];
  }
  return typeof node === "string" ? node : undefined;
}

function flatten(catalog: Record<string, unknown>, prefix = ""): string[] {
  const keys: string[] = [];
  for (const [key, value] of Object.entries(catalog)) {
    const path = prefix === "" ? key : `${prefix}.${key}`;
    if (typeof value === "string") keys.push(path);
    else if (typeof value === "object" && value !== null) keys.push(...flatten(value as Record<string, unknown>, path));
  }
  return keys;
}

describe("environmentsRpc guard (S59 T11)", () => {
  it("runs over the real section sources (not vacuous)", () => {
    expect(productionSources().length).toBeGreaterThan(8);
  });

  it("calls only the registered S59 methods", () => {
    const registered = registeredMethods();
    expect(registered.size).toBeGreaterThan(0);
    const all = productionSources().flatMap(({ src }) => extractRpcMethods(src));
    expect(all.length).toBeGreaterThan(15);
    const offenders = all.filter((m) => !ALLOWED.has(m));
    expect(offenders, `methods outside the S59 set: ${offenders.join(", ")}`).toEqual([]);
    const unregistered = all.filter((m) => !registered.has(m));
    expect(unregistered, `methods the contract does not register: ${unregistered.join(", ")}`).toEqual([]);
  });

  it("reaches every tab's surface at least once", () => {
    const all = new Set(productionSources().flatMap(({ src }) => extractRpcMethods(src)));
    for (const [tab, methods] of Object.entries(PER_TAB)) {
      const hit = methods.filter((m) => all.has(m));
      expect(hit.length, `tab ${tab} called none of ${methods.join(", ")}`).toBeGreaterThan(0);
    }
    // Every allowed door is actually reached: a door in the set that nothing
    // calls is a door the section does not have, and the set is a claim.
    const uncalled = [...ALLOWED].filter((m) => !all.has(m));
    expect(uncalled, `declared but never called: ${uncalled.join(", ")}`).toEqual([]);
  });

  it("has teeth (positive control): a synthetic environment.frobnicate is caught and refused", () => {
    const canary = `const x = rpcTyped("${CANARY_METHOD}", {});`;
    expect(extractRpcMethods(canary)).toContain(CANARY_METHOD);
    expect(ALLOWED.has(CANARY_METHOD)).toBe(false);
    expect(registeredMethods().has(CANARY_METHOD)).toBe(false);
  });

  it("has teeth (positive control): a synthetic direct fet`ch` is caught, refet`ch` is not", () => {
    const illegal = ["const r = await fet", 'ch("/environment/list?x=1"); q.refet', "ch();"].join("");
    const fetchCall = new RegExp("\\bfe" + "tch\\s*\\(");
    expect(fetchCall.test(illegal)).toBe(true);
    expect(fetchCall.test("queries.forEach((q) => q.refet" + "ch());")).toBe(false);
  });

  it("never opens a socket or stores state: no raw request object or browser storage in the sources", () => {
    for (const { file, src } of productionSources()) {
      // Tokens built by concatenation: the repo-wide guards grep this very file
      // for the contiguous forms they forbid.
      expect(new RegExp("\\bfe" + "tch\\s*\\(").test(src), `${file} calls fet` + "ch").toBe(false);
      expect(new RegExp("\\bXM" + "LHttpRequest\\b").test(src), `${file} opens a raw request`).toBe(false);
      expect(/\bWebSocket\b/.test(src), `${file} opens a socket`).toBe(false);
      expect(/\blocalStorage\b/.test(src), `${file} uses localStorage`).toBe(false);
      expect(/\bsessionStorage\b/.test(src), `${file} uses sessionStorage`).toBe(false);
      expect(/\binvoke\s*\(/.test(src), `${file} calls a Tauri command directly`).toBe(false);
    }
  });

  it("names no closed set: no backend, egress mode, source kind or deny word is typed by hand", () => {
    for (const { file, src } of productionSources()) {
      for (const word of FORBIDDEN_VOCABULARY) {
        expect(src.includes(`"${word}"`), `${file} hardcodes the vocabulary literal ${word}`).toBe(false);
        expect(src.includes(`'${word}'`), `${file} hardcodes the vocabulary literal ${word}`).toBe(false);
      }
    }
  });

  it("has teeth (positive control): a synthetic source naming a backend is caught", () => {
    const synthetic = 'const options = ["host-scrubbed", "container"]; if (row.backend === "host-inherit") { void 0 }';
    const hits = FORBIDDEN_VOCABULARY.filter((word) => synthetic.includes(`"${word}"`));
    expect(hits).toEqual(expect.arrayContaining(["host-scrubbed", "container", "host-inherit"]));
  });

  it("sends no identity param and no credential param (EV7, EV17)", () => {
    // `environment.create` carries an OPTIONAL `actor` in the contract; the
    // plane's own door guard refuses it by key, and so does this panel: the
    // caller names what to act on, never who acted.
    //
    // Comments are stripped first — a doc comment that QUOTES a refused key
    // (this file's own rule says the keys out loud, and so do the sources) is
    // not a param. What is scanned is code: a key position (`word:` /
    // `"word":`) or a quoted string literal.
    for (const { file, src } of productionSources()) {
      const code = stripComments(src);
      for (const word of [...FORBIDDEN_IDENTITY_KEYS, ...FORBIDDEN_CREDENTIAL_KEYS]) {
        if (READ_FILTER_KEYS.has(word)) continue;
        expect(declaresKey(code, word), `${file} sends the param ${word}`).toBe(false);
        expect(code.includes(`"${word}"`), `${file} carries the string ${word}`).toBe(false);
      }
    }
    // Per call site: a WRITE carries no identity word at all (not even the
    // three a read may filter on) and no credential word; a READ carries none
    // beyond the filters the daemon itself accepts.
    for (const { file, src } of productionSources()) {
      for (const [, method, payload] of extractCallPayloads(stripComments(src))) {
        const identity = [...FORBIDDEN_IDENTITY_KEYS, ...FORBIDDEN_ENVIRONMENT_KEYS];
        const banned = WRITE_METHODS.has(method)
          ? identity
          : identity.filter((word) => !READ_FILTER_KEYS.has(word));
        for (const word of [...banned, ...FORBIDDEN_CREDENTIAL_KEYS]) {
          expect(declaresKey(payload, word), `${file} sends ${word} to ${method}`).toBe(false);
        }
      }
    }
  });

  it("has teeth (positive control): a synthetic stolen actor and value field are caught", () => {
    const synthetic = 'rpcTyped("environment.create", { name: "n", actor: "operator", value: "sk-live-abc123" });';
    expect(FORBIDDEN_IDENTITY_KEYS.filter((word) => synthetic.includes(`${word}:`))).toContain("actor");
    expect(FORBIDDEN_CREDENTIAL_KEYS.filter((word) => synthetic.includes(`${word}:`))).toContain("value");
  });
});

/**
 * i18n — the fragment catalogs this slice adds (the whole-file en/pt parity is
 * `test/i18n.test.tsx`'s job; these two files are the new ones, checked here
 * the way gatesRpc.guard checks its fragment).
 */
describe("environments i18n coverage (S59 T11)", () => {
  const en = JSON.parse(readFileSync(EN, "utf8")) as Record<string, unknown>;
  const pt = JSON.parse(readFileSync(PT, "utf8")) as Record<string, unknown>;

  it("has teeth (positive control): a synthetic key is refused by the lookup", () => {
    expect(lookup(en, "settings.environments.nope.not.a.key")).toBeUndefined();
    expect(lookup(en, "settings.environments.title")).toBeTypeOf("string");
  });

  it("every t() key the sources reference exists in en and pt", () => {
    const keys = new Set<string>();
    const re = /\bt\(\s*["']((?:settings\.environments)\.[^"']+)["']/g;
    for (const { src } of productionSources()) {
      let match: RegExpExecArray | null;
      while ((match = re.exec(src)) !== null) {
        if (match[1] !== undefined) keys.add(match[1]);
      }
    }
    // Non-vacuous: the scan itself must find the keys the section uses.
    expect(keys.size).toBeGreaterThan(80);
    const missing: string[] = [];
    for (const key of keys) {
      if (lookup(en, key) === undefined) missing.push(`en:${key}`);
      if (lookup(pt, key) === undefined) missing.push(`pt:${key}`);
    }
    expect(missing, `catalog gaps: ${missing.join(", ")}`).toEqual([]);
  });

  it("the two catalogs hold exactly the same keys, in both directions", () => {
    const enKeys = flatten(environmentsOf(en), "settings.environments").sort();
    const ptKeys = flatten(environmentsOf(pt), "settings.environments").sort();
    expect(enKeys.length).toBeGreaterThan(80);
    expect(ptKeys.filter((k) => !enKeys.includes(k))).toEqual([]);
    expect(enKeys.filter((k) => !ptKeys.includes(k))).toEqual([]);
  });

  it("the Portuguese is Portuguese, not a copy of the English", () => {
    const enValues = flatten(environmentsOf(en)).map(
      (key) => lookup(en, key) ?? "",
    );
    const ptValues = flatten(environmentsOf(pt)).map(
      (key) => lookup(pt, key) ?? "",
    );
    const identical = ptValues.filter((value, index) => value === enValues[index]);
    // Copying the English across is the half-translation this catches; short
    // technical words (Home, Image, Slug, Backend…) are legitimately the same.
    expect(identical.filter((value) => value.split(" ").length > 2)).toEqual([]);
  });
});
