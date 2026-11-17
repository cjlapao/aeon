import { describe, expect, it } from "vitest";
import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";

// S49 T9 — the Automation section reaches the daemon ONLY through the
// registered `automation.*` / `trigger.*` doors below (plus `blueprint.list`,
// `component.list` and `proposal.*`), and ONLY through the generic rpc bridge:
// never a socket, never fet`ch`, never browser storage (§4c), never a new
// Tauri command (AN10/P34). The plane's own guards say the daemon refuses
// identity params by key and that a webhook secret travels as a NAME only;
// this file keeps the PANEL from ever building a request the daemon would
// refuse, and keeps the catalog honest across both languages (AN11: the
// section quotes daemon sentences — its OWN keys are the only text it may
// write, and both languages must carry every one of them).

const AUTOMATION_DIR = __dirname;
const CONTRACT = join(__dirname, "../../../proto/contract.ts");
const EN = join(__dirname, "../../../lib/i18n/en-automation.json");
const PT = join(__dirname, "../../../lib/i18n/pt-automation.json");

/** Every door the section uses: the plane's reads, the schedule family's
 *  `trigger.*` doors, the event family's `automation.trigger.*` doors, the
 *  archive pair, the S48/S38 lists the forms pick from, and the shared desk.
 *  Nothing else — `automation.proposeDisable` is an AGENT's door (a person
 *  disables directly; an agent proposes), so this panel must never call it. */
const ALLOWED = new Set([
  "automation.list",
  "automation.show",
  "automation.health",
  "automation.vocabulary",
  "automation.fires",
  "automation.preview",
  "automation.test",
  "automation.dryrun",
  "automation.changes",
  "automation.runs",
  "automation.archive",
  "automation.unarchive",
  "automation.trigger.create",
  "automation.trigger.update",
  "automation.trigger.enable",
  "automation.trigger.disable",
  "trigger.add",
  "trigger.update",
  "trigger.pause",
  "trigger.resume",
  "trigger.fireNow",
  "blueprint.list",
  "component.list",
  "proposal.list",
]);

/** The method-name literal the first argument to rpc / rpcTyped / useRpcQuery
 *  carries (the same extractor policyRpc, gatesRpc and environmentsRpc use). */
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

/** The production (non-test) sources the guard owns: the section, the tabs,
 *  the three forms, the drawer, the rows, the fire log, the model, the doors. */
function productionSources(): Array<{ file: string; src: string }> {
  return readdirSync(AUTOMATION_DIR)
    .filter(
      (f) =>
        (f.endsWith(".ts") || f.endsWith(".tsx")) &&
        !f.endsWith(".test.ts") &&
        !f.endsWith(".test.tsx"),
    )
    .map((f) => ({ file: `automation/${f}`, src: readFileSync(join(AUTOMATION_DIR, f), "utf8") }));
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

/** Comments out of a source, so a doc comment naming a refused key is not
 *  read as a param (the guard's own vocabulary lists the keys it forbids). */
function stripComments(src: string): string {
  return src.replace(/\/\*[\s\S]*?\*\//g, " ").replace(/^\s*\/\/.*$/gm, " ").replace(/`[\s\S]*?`/g, "``");
}

/** A key position for `word`: `word:`, `"word":`, `'word':` — not an identifier
 *  that merely ends with it. */
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

function automationOf(catalog: Record<string, unknown>): Record<string, unknown> {
  const settings = catalog["settings"];
  if (typeof settings !== "object" || settings === null) throw new Error("the catalog has no settings section");
  const automation = (settings as Record<string, unknown>)["automation"];
  if (typeof automation !== "object" || automation === null) throw new Error("the catalog has no settings.automation section");
  return automation as Record<string, unknown>;
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

/** Every key the sources reference, literal or composed. Composed keys
 *  (`settings.automation.outcome.${word}` …) are INVISIBLE to a literal scan,
 *  so they are named here explicitly — if a component starts composing a
 *  sixth outcome word, the catalog check below is the one that must catch it. */
function referencedKeys(): string[] {
  const keys = new Set<string>();
  for (const { src } of productionSources()) {
    const code = stripComments(src);
    for (const match of code.matchAll(/\bt\(\s*"((?:settings\.automation)\.[^"]+)"/g)) {
      if (match[1] !== undefined) keys.add(match[1]);
    }
    // Composed keys live INSIDE template literals, which stripComments blanks
    // — scan the raw source for the composition sites.
    for (const word of ["enabled", "disabled", "paused", "archived"]) {
      if (new RegExp(`settings\\.automation\\.state\\.\\$\\{`).test(src)) keys.add(`settings.automation.state.${word}`);
    }
    for (const word of ["started", "refused", "limited", "skipped", "could-not-say"]) {
      if (new RegExp(`settings\\.automation\\.outcome\\.\\$\\{`).test(src)) keys.add(`settings.automation.outcome.${word}`);
    }
    if (/settings\.automation\.health\.\$\{/.test(src)) {
      for (const word of ["blueprint", "webhook", "workspace", "scope", "cannotFire"]) {
        keys.add(`settings.automation.health.${word}`);
      }
    }
  }
  return [...keys];
}

const WRITE_METHODS = new Set([
  "automation.archive",
  "automation.unarchive",
  "automation.trigger.create",
  "automation.trigger.update",
  "automation.trigger.enable",
  "automation.trigger.disable",
  "trigger.add",
  "trigger.update",
  "trigger.pause",
  "trigger.resume",
  "trigger.fireNow",
]);

/** The identity vocabulary the plane refuses by key: the caller names WHAT to
 *  act on, never who acted or when the row moved. */
const FORBIDDEN_IDENTITY_KEYS = [
  "actor",
  "actorKind",
  "actorId",
  "origin",
  "createdByKind",
  "createdById",
  "updatedBy",
  "legacy",
  "revision",
];

/** The credential vocabulary (§4c, C17): the webhook secret field is a NAME.
 *  No key below may ever appear in a payload this section sends. */
const FORBIDDEN_CREDENTIAL_KEYS = [
  "value",
  "secretValue",
  "secret",
  "token",
  "apiKey",
  "password",
  "credential",
  "contents",
  "raw",
  "bytes",
  "envValue",
];

describe("automationRpc guard (S49 T9)", () => {
  it("runs over the real section sources (not vacuous)", () => {
    expect(productionSources().length).toBeGreaterThan(8);
  });

  it("calls only the registered S49 doors", () => {
    const registered = registeredMethods();
    expect(registered.size).toBeGreaterThan(0);
    const all = productionSources().flatMap(({ src }) => extractRpcMethods(src));
    expect(all.length).toBeGreaterThan(18);
    const offenders = all.filter((m) => !ALLOWED.has(m));
    expect(offenders, `methods outside the S49 set: ${offenders.join(", ")}`).toEqual([]);
    const unregistered = all.filter((m) => !registered.has(m));
    expect(unregistered, `methods the contract does not register: ${unregistered.join(", ")}`).toEqual([]);
  });

  it("reaches every family's surface — list, both form families, the desk", () => {
    const all = new Set(productionSources().flatMap(({ src }) => extractRpcMethods(src)));
    for (const surface of [
      ["list", ["automation.list", "automation.health", "automation.show"]],
      ["schedule", ["trigger.add", "trigger.update", "trigger.pause", "trigger.resume", "trigger.fireNow", "automation.preview"]],
      ["event", ["automation.trigger.create", "automation.trigger.update", "automation.trigger.enable", "automation.trigger.disable", "automation.test"]],
      ["fire log + audit", ["automation.fires", "automation.changes", "automation.runs"]],
      ["desk", ["proposal.list"]],
    ]) {
      const [name, methods] = surface as [string, string[]];
      const hit = methods.filter((m) => all.has(m));
      expect(hit.length, `${name} calls none of ${methods.join(", ")}`).toBeGreaterThan(0);
    }
    // Every allowed door is actually called — the set is a claim, not a wish.
    const uncalled = [...ALLOWED].filter((m) => !all.has(m));
    expect(uncalled, `declared but never called: ${uncalled.join(", ")}`).toEqual([]);
    // And the AGENT's own door is never the person's panel's door.
    expect(all.has("automation.proposeDisable")).toBe(false);
  });

  it("has teeth (positive control): a synthetic automation.frobnicate is caught and refused", () => {
    const canary = `const x = rpcTyped("automation.frobnicate", {});`;
    expect(extractRpcMethods(canary)).toContain("automation.frobnicate");
    expect(ALLOWED.has("automation.frobnicate")).toBe(false);
    expect(registeredMethods().has("automation.frobnicate")).toBe(false);
  });

  it("never opens a socket or stores state: no raw request object or browser storage in the sources", () => {
    for (const { file, src } of productionSources()) {
      expect(new RegExp("\\bfe" + "tch\\s*\\(").test(src), `${file} calls fet` + "ch").toBe(false);
      expect(new RegExp("\\bXM" + "LHttpRequest\\b").test(src), `${file} opens a raw request`).toBe(false);
      expect(/\bWebSocket\b/.test(src), `${file} opens a socket`).toBe(false);
      expect(/\blocalStorage\b/.test(src), `${file} uses localStorage`).toBe(false);
      expect(/\bsessionStorage\b/.test(src), `${file} uses sessionStorage`).toBe(false);
      expect(/\binvoke\s*\(/.test(src), `${file} calls a Tauri command directly`).toBe(false);
    }
  });

  it("sends no identity param and no credential param — and no secret VALUE, ever (§4c, C17)", () => {
    for (const { file, src } of productionSources()) {
      const code = stripComments(src);
      for (const word of [...FORBIDDEN_IDENTITY_KEYS, ...FORBIDDEN_CREDENTIAL_KEYS]) {
        expect(declaresKey(code, word), `${file} sends the param ${word}`).toBe(false);
      }
    }
    for (const { file, src } of productionSources()) {
      for (const [, method, payload] of extractCallPayloads(stripComments(src))) {
        const banned = WRITE_METHODS.has(method)
          ? [...FORBIDDEN_IDENTITY_KEYS, ...FORBIDDEN_CREDENTIAL_KEYS]
          : FORBIDDEN_CREDENTIAL_KEYS;
        for (const word of banned) {
          expect(declaresKey(payload, word), `${file} sends ${word} to ${method}`).toBe(false);
        }
      }
    }
  });

  it("has teeth (positive control): a synthetic stolen actor and a secret value are caught", () => {
    const synthetic = 'rpcTyped("automation.trigger.create", { name: "n", actorKind: "agent", secretValue: "sk-live-abc123" });';
    expect(declaresKey(synthetic, "actorKind")).toBe(true);
    expect(declaresKey(synthetic, "secretValue")).toBe(true);
    // And a webhook payload that carried the VALUE, not the name, would fail:
    const webhookSynthetic = 'trigger.add({ kind: "webhook", secretValue: "hunter2" })';
    expect(declaresKey(webhookSynthetic, "secretValue")).toBe(true);
    // The real form's field is a NAME key — checked, not banned:
    expect(declaresKey('trigger.add({ kind: "webhook", secretEnv: "AEON_WEBHOOK_SECRET" })', "secretEnv")).toBe(true);
  });

  it("the catalog covers every key the section references, in BOTH languages", () => {
    const enDoc = JSON.parse(readFileSync(EN, "utf8")) as Record<string, unknown>;
    const ptDoc = JSON.parse(readFileSync(PT, "utf8")) as Record<string, unknown>;
    const en = automationOf(enDoc);
    const pt = automationOf(ptDoc);
    const enKeys = flatten(en).map((k) => `settings.automation.${k}`);
    const ptKeys = flatten(pt).map((k) => `settings.automation.${k}`);
    expect(enKeys.sort()).toEqual(ptKeys.sort());
    const referenced = referencedKeys();
    expect(referenced.length).toBeGreaterThan(80);
    const missing = referenced.filter((key) => lookup(enDoc, key) === undefined || lookup(ptDoc, key) === undefined);
    expect(missing, `keys missing from a catalog: ${missing.join(", ")}`).toEqual([]);
  });

  it("the Portuguese is a translation, not a copy (AN11 cuts both ways)", () => {
    const en = automationOf(JSON.parse(readFileSync(EN, "utf8")));
    const pt = automationOf(JSON.parse(readFileSync(PT, "utf8")));
    const copied = flatten(en).filter((key) => {
      const enValue = lookup(en, key);
      const ptValue = lookup(pt, key);
      if (enValue === undefined || ptValue === undefined) return false;
      // Words like "workspace", "blueprint", "Audit" legitimately ride into
      // Portuguese; three-word sentences never ride unchanged.
      return enValue === ptValue && enValue.split(/\s+/).length > 2;
    });
    expect(copied, `untranslated Portuguese copies: ${copied.join(", ")}`).toEqual([]);
  });

  it("has teeth (positive control): the key scan sees a literal AND a composed key", () => {
    const synthetic = 'const a = t("settings.automation.brand.new.key"); const b = t(`settings.automation.state.${word}`);';
    expect(/t\(\s*"settings\.automation\./.test(synthetic)).toBe(true);
    expect(/settings\.automation\.state\.\$\{/.test(synthetic)).toBe(true);
    // And a key no catalog carries is refused by the lookup the test uses:
    const enDoc = JSON.parse(readFileSync(EN, "utf8")) as Record<string, unknown>;
    expect(lookup(enDoc, "settings.automation.brand.new.key")).toBeUndefined();
  });
});
