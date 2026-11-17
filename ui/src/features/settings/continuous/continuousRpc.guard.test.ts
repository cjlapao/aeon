import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";
import { configMountCount, configPageRegisteredOnce, settingsScreenMounts } from "../../../../test/configMounts";
import { describe, expect, it } from "vitest";

/**
 * continuousRpc.guard.test.ts — S27 T10's pin: Settings → Continuous speaks
 * the eleven scheduler doors, exactly, through `continuousRpc.ts` and the
 * tab components — nothing wider (no `scheduler.enableTree`, no
 * `settings.*`), nothing narrower (a tab that silently lost its door fails
 * here, not at the demo). Same mechanism as `platformRpc.guard.test.ts`
 * (S54) and `triageRpc.guard.test.ts` (S51): census == allow-list, every
 * door present in the GENERATED contract, no banned road out of the web
 * layer, the i18n catalogs in parity with every `t()` key used, and a
 * canary proving the extractor has teeth.
 */

const DIR = __dirname;
const CONTRACT = join(__dirname, "../../../proto/contract.ts");
const EN = join(__dirname, "../../../lib/i18n/en-continuous.json");
const PT = join(__dirname, "../../../lib/i18n/pt-continuous.json");
const SOCKET_GO = join(__dirname, "../../../../../internal/daemon/continuous_socket.go");
const CORE_GO = join(__dirname, "../../../../../internal/continuous/types.go");
const ALL_SRCS = join(__dirname, "../../..");

/** The panel's whole door set: four reads, the master pair, the pause
 *  pair, the row pair, stop-all. Eleven — no bulk `scheduler.enableTree`
 *  (the CLI keeps the tree-wide act), no `settings.*` (the panel reads the
 *  scheduler's settings inside `scheduler.status` and does not edit them). */
const ALLOWED = new Set([
  "scheduler.status",
  "scheduler.preview",
  "scheduler.decisions",
  "scheduler.attention",
  "scheduler.enable",
  "scheduler.disable",
  "scheduler.pause",
  "scheduler.resume",
  "scheduler.stopAll",
  "scheduler.root.set",
  "scheduler.root.disable",
]);

function stripComments(src: string): string {
  return src.replace(/\/\*[\s\S]*?\*\//g, "").replace(/\/\/[^\n]*/g, "");
}

/** Door calls, including the concatenation-smuggling shape. */
function extractRpcMethods(src: string): string[] {
  const out: string[] = [];
  const code = stripComments(src);
  const direct = /\b(?:rpc|rpcTyped|useRpcQuery)\s*\(\s*["']([^"']+)["']/g;
  for (const m of code.matchAll(direct)) out.push(m[1] as string);
  const concat = /\b(?:rpc|rpcTyped|useRpcQuery)\s*\(\s*["']([a-zA-Z.-]+)["']\s*\+\s*["']([a-zA-Z.-]+)["']/g;
  for (const m of code.matchAll(concat)) out.push(`${m[1] as string}${m[2] as string}`);
  return out;
}

function productionSources(): string[] {
  return readdirSync(DIR)
    .filter((f) => /\.(ts|tsx)$/.test(f) && !f.includes(".test."))
    .map((f) => join(DIR, f));
}

function registeredMethods(): Set<string> {
  const src = readFileSync(CONTRACT, "utf8");
  const out = new Set<string>();
  for (const m of src.matchAll(/^ {2}"([a-zA-Z.-]+)": \{ params:/gm)) out.add(m[1] as string);
  return out;
}

function keysOf(obj: unknown, prefix = ""): string[] {
  if (typeof obj !== "object" || obj === null) return [prefix];
  return Object.entries(obj as Record<string, unknown>).flatMap(([k, v]) => keysOf(v, prefix ? `${prefix}.${k}` : k));
}

function tKeys(): string[] {
  const out: string[] = [];
  for (const file of productionSources()) {
    const code = stripComments(readFileSync(file, "utf8"));
    for (const m of code.matchAll(/\bt\(\s*"([a-zA-Z0-9._]+)"/g)) out.push(m[1] as string);
  }
  return out;
}

describe("continuous folder door census (S27 T10)", () => {
  it("rpc guard + canary", () => {
    const found = productionSources().flatMap((f) => extractRpcMethods(readFileSync(f, "utf8")));
    const unique = new Set(found);
    // Census == allow-list: no extra door, no missing one, never vacuous.
    expect([...unique].sort()).toEqual([...ALLOWED].sort());
    expect(unique.size).toBeGreaterThanOrEqual(11);
    // Every door must be REGISTERED in the generated contract — the UI
    // never calls a name the daemon does not answer.
    const registered = registeredMethods();
    for (const door of ALLOWED) {
      expect(registered.has(door), `${door} missing from the contract`).toBe(true);
    }
    // CANARY: the extractor catches a door the census would refuse.
    const canary = extractRpcMethods(
      `const p = await rpcTyped("scheduler.bulkEnable", {rootId:"x"}); void useRpcQuery("settings.set", {key:"x"}, c, l);`,
    );
    expect(canary).toContain("scheduler.bulkEnable");
    expect(canary).toContain("settings.set");
    // A comment is NOT a call (stripping has teeth in the other direction).
    expect(extractRpcMethods(`// rpcTyped("scheduler.bulkEnable", {})\nexport const x = 1;`)).toEqual([]);
  });

  it("the folder holds no banned road (no fetch, no storage, no direct invoke)", () => {
    const banned: Array<[string, RegExp]> = [
      ["fetch", /\bfetch\s*\(/],
      ["localStorage", /\blocalStorage\b/],
      ["sessionStorage", /\bsessionStorage\b/],
      ["indexedDB", /\bindexedDB\b/],
      ["invoke", /\binvoke\s*\(/],
      ["WebSocket", /\bWebSocket\b/],
      // Sibling spelling (S54): the truncated stem only — the whole-source
      // scan (test/no-socket-in-web-layer.test.ts) flags the full literal,
      // and this file is one of the files it reads.
      ["XMLHttp", /XMLHttp/],
    ];
    for (const file of productionSources()) {
      const code = stripComments(readFileSync(file, "utf8"));
      for (const [name, re] of banned) {
        expect(re.test(code), `${name} in ${file}`).toBe(false);
      }
    }
    // The subscribe door belongs to lib/useLiveSubscription — this folder
    // never subscribes on its own road.
    for (const file of productionSources()) {
      expect(extractRpcMethods(readFileSync(file, "utf8"))).not.toContain("subscribe");
    }
  });

  it("no hardcoded vocabulary", async () => {
    // CN13/CN21/CN22: the panel renders the daemon's words; it never holds
    // a reason, decision, priority or needs-you kind as a literal. The
    // ONLY words allowed are the tone-map keys in continuousModel.ts, and
    // those must EXACTLY match the words the daemon's own source answers —
    // a word the daemon never says, or one it says that the map forgot,
    // fails here.
    const socket = readFileSync(SOCKET_GO, "utf8");
    const daemonStates = new Set<string>();
    for (const m of socket.matchAll(/ContinuousRepoState\{Kind: "([a-z-]+)"/g)) daemonStates.add(m[1] as string);
    expect(daemonStates.size).toBeGreaterThanOrEqual(8);
    const core = readFileSync(CORE_GO, "utf8");
    const daemonVerdicts = new Set<string>();
    for (const m of core.matchAll(/Verdict\w+\s+Verdict = "([a-z-]+)"/g)) daemonVerdicts.add(m[1] as string);
    expect(daemonVerdicts.size).toBeGreaterThanOrEqual(3);

    const model = await import("./continuousModel");
    expect([...model.REPO_STATE_WORDS].sort()).toEqual([...daemonStates].sort());
    expect([...model.VERDICT_WORDS].sort()).toEqual([...daemonVerdicts].sort());

    // The needs-you kinds are NEVER matched anywhere in the folder — CN21:
    // the daemon attaches its note to the kinds that carry one; a panel
    // that recognizes `cap-hit` would be interpreting.
    // The reason words and the tree door belong to the daemon, not here.
    const forbidden = ["cap-hit", "window-closed", "repo-off", "repo-occupied", "rate-limit", "enableTree"];
    for (const file of productionSources()) {
      const code = stripComments(readFileSync(file, "utf8"));
      for (const word of forbidden) {
        expect(code.includes(word), `${word} hardcoded in ${file}`).toBe(false);
      }
    }
  });

  it("i18n parity", () => {
    const en = JSON.parse(readFileSync(EN, "utf8"));
    const pt = JSON.parse(readFileSync(PT, "utf8"));
    const enKeys = new Set(keysOf(en));
    const ptKeys = new Set(keysOf(pt));
    expect([...enKeys].sort()).toEqual([...ptKeys].sort());
    expect(enKeys.size).toBeGreaterThan(40);
    // Every t() key the folder uses exists in BOTH catalogs…
    for (const key of tKeys()) {
      expect(enKeys.has(key), `${key} missing from en-continuous`).toBe(true);
      expect(ptKeys.has(key), `${key} missing from pt-continuous`).toBe(true);
    }
    // …and every key lives in this section's own namespace.
    for (const key of tKeys()) {
      expect(key.startsWith("settings.continuous."), `${key} outside the namespace`).toBe(true);
    }
  });

  it("existing settings panels and bench pages unchanged", () => {
    // S72 T2 re-pin (R3): this pinned the thirteen `<Section ` mounts in
    // SettingsScreen. S72 moved each section to its own Configuration page
    // (CF3/CF8); the law it defends — the section arrived by ADDING a mount
    // and disturbing none — is scanned at features/config/pages now, with
    // Settings itself mounting none. The bench half is untouched, below.
    for (const [component, page] of [
      ["DaemonSettingsPanel", "daemon-settings"], ["BackupsSection", "backups"], ["SystemRecordsPanel", "system-records"],
      ["PolicySection", "policy"], ["McpSection", "mcp"], ["ToolsSection", "tools"], ["GatesSection", "gates"],
      ["EnvironmentsSection", "environments"], ["AutomationSection", "automation"], ["TriageSection", "triage"],
      ["MemorySection", "memory"], ["SystemSection", "platform-status"], ["ContinuousSection", "continuous"],
    ] as const) {
      expect(configMountCount(component), `${component} mounts ${configMountCount(component)} times in config pages`).toBe(1);
      expect(configPageRegisteredOnce(page), `${page} is registered`).toBe(true);
      expect(settingsScreenMounts(component), `${component} still mounts on Settings`).toBe(false);
    }
    // The bench dispatch was never in the settings tree — and nothing in
    // this folder reaches it: no imports above the features boundary.
    for (const file of productionSources()) {
      const code = stripComments(readFileSync(file, "utf8"));
      expect(code.includes("TestBench"), `bench reach in ${file}`).toBe(false);
    }
  });

  it("the borrowed doors stay owned by their folders", () => {
    // component.list and blueprint.list are IMPORTED (useComponentRoots,
    // BlueprintPicker) — never re-spelled as literals here.
    for (const file of productionSources()) {
      const found = extractRpcMethods(readFileSync(file, "utf8"));
      expect(found).not.toContain("component.list");
      expect(found).not.toContain("blueprint.list");
    }
    // And the imports that borrow them name the owning folders.
    const repos = readFileSync(join(DIR, "RepositoriesTab.tsx"), "utf8");
    expect(repos.includes("../../projects/useComponentRoots")).toBe(true);
    const form = readFileSync(join(DIR, "RootForm.tsx"), "utf8");
    expect(form.includes("../automation/ScheduleForm")).toBe(true);
  });

  it("no second copy of the whole-source scan's banned patterns", () => {
    // The lib/ roads are the only way out of the web layer; assert the
    // folder never imports node runtime or talks to Tauri directly.
    for (const file of productionSources()) {
      const code = stripComments(readFileSync(file, "utf8"));
      expect(/from\s+"node:/.test(code), `node import in ${file}`).toBe(false);
      expect(/from\s+"@tauri/.test(code), `tauri import in ${file}`).toBe(false);
    }
    expect(ALL_SRCS.length).toBeGreaterThan(0);
  });
});
