import { describe, expect, it } from "vitest";
import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";
import { configMountCount, configPageRegisteredOnce, settingsScreenMounts } from "../../../../test/configMounts";

// S62 T10 (P7/P34) — the Backups panel is the app's door to the database's
// safety net. It reaches the daemon through the five registered backup
// methods and nothing else: two reads (status, list) and three writes
// (run, verify, prune). Restore is NOT a method at all — it replaces the
// database and needs the daemon stopped, so it is a CLI verb the panel can
// only point at, never call.
//
// Not vacuous, and says so: the `backup.restore` canary IS caught and IS
// refused (the contract never registered it — refusing a typo would be the
// guard's only value); the scan runs over the real section sources; every
// surface's method is hit; the daemon's state words are searched for in the
// files where they would do damage.

const BACKUPS_DIR = __dirname;
const CONTRACT = join(__dirname, "../../../proto/contract.ts");

/** The five doors this panel owns. Everything outside this set offends. */
const ALLOWED = new Set(["backup.status", "backup.list", "backup.run", "backup.verify", "backup.prune"]);

/** ≥ 1 literal per surface — the guard saw the whole surface, not one file. */
const PER_SURFACE: Record<string, string[]> = {
  status: ["backup.status"],
  list: ["backup.list"],
  actions: ["backup.run", "backup.verify", "backup.prune"],
};

// Restore is a CLI verb, not a socket method (the daemon refused the door;
// the CLI alone opens it). A panel that called it would be the slice's one
// prohibition broken — and because the contract never registered it, the
// canary proves the guard catches both a wrong door and a typo.
const CANARY_METHOD = "backup.restore";

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
  return readdirSync(BACKUPS_DIR)
    .filter(
      (f) =>
        (f.endsWith(".ts") || f.endsWith(".tsx")) &&
        !f.endsWith(".test.ts") &&
        !f.endsWith(".test.tsx"),
    )
    .map((f) => ({ file: join("backups", f), src: readFileSync(join(BACKUPS_DIR, f), "utf8") }));
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

describe("backupRpc guard (T10, P7/P34)", () => {
  it("runs over the real section source (not vacuous)", () => {
    expect(productionSources().length).toBeGreaterThanOrEqual(4);
  });

  it("calls only the registered backup doors", () => {
    const registered = registeredMethods();
    expect(registered.size).toBeGreaterThan(0);
    const all = productionSources().flatMap(({ src }) => extractRpcMethods(src));
    expect(all.length).toBeGreaterThan(0);
    const offenders = all.filter((m) => !ALLOWED.has(m));
    expect(offenders, `methods outside the S62 backup set: ${offenders.join(", ")}`).toEqual([]);
    const unregistered = all.filter((m) => !registered.has(m));
    expect(unregistered, `methods the contract does not register: ${unregistered.join(", ")}`).toEqual([]);
  });

  it("reaches every surface at least once", () => {
    const all = new Set(productionSources().flatMap(({ src }) => extractRpcMethods(src)));
    for (const [surface, methods] of Object.entries(PER_SURFACE)) {
      const hit = methods.filter((m) => all.has(m));
      expect(hit.length, `surface ${surface} called none of ${methods.join(", ")}`).toBeGreaterThan(0);
    }
  });

  it("has teeth (positive control): a synthetic backup.restore is caught and refused", () => {
    const canary = `const x = rpcTyped("${CANARY_METHOD}", {});`;
    expect(extractRpcMethods(canary)).toContain(CANARY_METHOD);
    expect(ALLOWED.has(CANARY_METHOD)).toBe(false);
    // And the canary is a name the contract truly does not carry: restore is
    // a CLI verb, so a guard that only refused names it had never heard of
    // would pass on this one.
    expect(registeredMethods().has(CANARY_METHOD)).toBe(false);
  });

  it("no restore literal at all: the panel points at the CLI, it never calls it", () => {
    // The word "restore" is allowed in a static hint sentence (the catalog
    // carries it), but never as an rpc literal and never as a method-shaped
    // string in the section source.
    for (const { file, src } of productionSources()) {
      for (const method of extractRpcMethods(src)) {
        expect(method.includes("restore"), `${file} calls a restore-shaped method ${method}`).toBe(false);
      }
      expect(new RegExp("\\brestore\\b\\s*\\(").test(src), `${file} calls restore directly`).toBe(false);
    }
  });

  it("never opens a socket or stores state: no raw request object or browser storage", () => {
    for (const { file, src } of productionSources()) {
      expect(new RegExp("\\bfe" + "tch\\s*\\(").test(src), `${file} calls fet` + "ch").toBe(false);
      expect(new RegExp("\\bXM" + "LHttpRequest\\b").test(src), `${file} opens a raw request`).toBe(false);
      expect(/\bWebSocket\b/.test(src), `${file} opens a socket`).toBe(false);
      expect(/\blocalStorage\b/.test(src), `${file} uses localStorage`).toBe(false);
      expect(/\bsessionStorage\b/.test(src), `${file} uses sessionStorage`).toBe(false);
    }
  });

  it("names no state the daemon owns: never/fresh/stale/failing stay in the catalog", () => {
    // The four state words are the daemon's enum (S62 T6). They may appear in
    // the i18n catalogs as caption keys — they must not appear in the
    // section's source: a panel that spells a state has started deciding
    // what the state is, and the caption-by-key design exists so a state the
    // daemon newly reports arrives as a missing caption, not an invented word.
    for (const word of ["never", "fresh", "stale", "failing"]) {
      for (const { file, src } of productionSources()) {
        expect(src.includes(`"${word}"`), `${file} hardcodes the daemon state word ${word}`).toBe(false);
      }
    }
  });

  it("mounts beside the panels it joined, and disturbs none of them", () => {
    // S72 T2 re-pin (R3): this pinned SettingsScreen's stack mounts. Every
    // section now owns a Configuration page (CF3/CF8); the law it defends —
    // each section mounts EXACTLY ONCE in the product — is scanned at
    // features/config/pages, and its Settings half is now that Settings
    // mounts none of them. Recorded in the slice Notes.
    for (const [component, page] of [["DaemonSettingsPanel", "daemon-settings"], ["BackupsSection", "backups"], ["SystemRecordsPanel", "system-records"], ["PolicySection", "policy"], ["McpSection", "mcp"], ["ToolsSection", "tools"]] as const) {
      expect(configMountCount(component), `${component} mounts ${configMountCount(component)} times in config pages`).toBe(1);
      expect(configPageRegisteredOnce(page), `${page} is registered`).toBe(true);
      expect(settingsScreenMounts(component), `${component} still mounts on Settings`).toBe(false);
    }
  });
});

/**
 * i18n coverage: every `settings.backups` key the sources reference exists in
 * BOTH catalogs — and the captions reached by a key built from the daemon's
 * own enum string (state, reason, verified: no literal in the source to scan)
 * are checked by name here.
 */
describe("backups i18n coverage", () => {
  const en = JSON.parse(readFileSync(join(__dirname, "../../../lib/i18n/en.json"), "utf8")) as Record<string, unknown>;
  const pt = JSON.parse(readFileSync(join(__dirname, "../../../lib/i18n/pt.json"), "utf8")) as Record<string, unknown>;

  function lookup(catalog: Record<string, unknown>, key: string): string | undefined {
    let node: unknown = catalog;
    for (const part of key.split(".")) {
      if (typeof node !== "object" || node === null) return undefined;
      node = (node as Record<string, unknown>)[part];
    }
    return typeof node === "string" ? node : undefined;
  }

  it("has teeth (positive control): a synthetic key is refused by the lookup", () => {
    expect(lookup(en, "settings.backups.nope.not.a.key")).toBeUndefined();
    expect(lookup(en, "settings.backups.title")).toBeTypeOf("string");
  });

  it("every settings.backups key the sources reference exists in en and pt", () => {
    const keys = new Set<string>();
    const re = /"((?:settings\.backups)\.[^"']+)"/g;
    for (const { src } of productionSources()) {
      let match: RegExpExecArray | null;
      while ((match = re.exec(src)) !== null) {
        if (match[1] !== undefined) keys.add(match[1].split("#")[0] as string);
      }
    }
    expect(keys.size).toBeGreaterThan(15);
    const missing: string[] = [];
    for (const key of keys) {
      if (lookup(en, key) === undefined) missing.push(`en:${key}`);
      if (lookup(pt, key) === undefined) missing.push(`pt:${key}`);
    }
    expect(missing, `catalog gaps: ${missing.join(", ")}`).toEqual([]);
  });

  it("carries a caption for every state, reason and verify word the daemon can report", () => {
    // Built at render time from the strings the daemon sent, so a source scan
    // cannot see them. A word with no caption would fall back to the bare
    // string; a caption only in one language is a lie in the other.
    for (const state of ["never", "fresh", "stale", "failing"]) {
      for (const base of ["status.state", "status.stateBody"]) {
        const key = `settings.backups.${base}.${state}`;
        expect(lookup(en, key), `en has no ${base} caption for ${state}`).toBeTypeOf("string");
        expect(lookup(pt, key), `pt has no ${base} caption for ${state}`).toBeTypeOf("string");
      }
    }
    for (const reason of ["manual", "scheduled", "pre-migration"]) {
      const key = `settings.backups.reason.${reason}`;
      expect(lookup(en, key), `en has no reason caption for ${reason}`).toBeTypeOf("string");
      expect(lookup(pt, key), `pt has no reason caption for ${reason}`).toBeTypeOf("string");
    }
    for (const verified of ["unverified", "verified", "verified+drill"]) {
      const key = `settings.backups.verified.${verified}`;
      expect(lookup(en, key), `en has no verified caption for ${verified}`).toBeTypeOf("string");
      expect(lookup(pt, key), `pt has no verified caption for ${verified}`).toBeTypeOf("string");
    }
  });

  it("keeps the two catalogs in the same shape for this section", () => {
    function leafKeys(node: unknown, prefix: string, into: string[]): void {
      if (typeof node === "string") {
        into.push(prefix);
        return;
      }
      if (typeof node !== "object" || node === null) return;
      for (const [key, value] of Object.entries(node as Record<string, unknown>)) {
        leafKeys(value, `${prefix}.${key}`, into);
      }
    }
    const enKeys: string[] = [];
    const ptKeys: string[] = [];
    leafKeys((en as Record<string, Record<string, unknown>>).settings?.backups, "settings.backups", enKeys);
    leafKeys((pt as Record<string, Record<string, unknown>>).settings?.backups, "settings.backups", ptKeys);
    expect(enKeys.length).toBeGreaterThan(30);
    expect(enKeys.sort()).toEqual(ptKeys.sort());
  });
});
