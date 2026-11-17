import { describe, expect, it } from "vitest";
import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";
import { configMountCount, configPageRegisteredOnce, settingsScreenMounts } from "../../../../test/configMounts";

// TC9/TC10/TC12 + P34 — S47 T10 keeps the daemon's surface the primary interface
// and the catalog reads the ONLY doors this section opens. The Tools/Surface
// section, and the shared `AgentPicker` it reuses, reach the daemon through the
// registered methods below and through the generic bridge only — never a socket,
// never a raw request, never browser storage (§4c), and never a write: TC1 says
// this slice ships no write method, tool or verb, so a control that changed
// anything would not be a styling slip but the slice's one prohibition broken.
//
// Not vacuous, and says so: the `tools.update` canary IS caught and IS refused; a
// synthetic direct fetch IS caught (`refetch` is not); the scan runs over real
// section sources; every tab's surface is hit; the vocabulary words are searched
// for in the files where they would do damage.

const TOOLS_DIR = __dirname;
const AGENT_PICKER = join(__dirname, "../../../components/AgentPicker.tsx");
const CONTRACT = join(__dirname, "../../../proto/contract.ts");

/** The six catalog/surface reads this slice owns, the vocabulary door added under
 *  the operator ruling of 2026-11-02, and the roster the shared picker reads.
 *  `policy.list` is deliberately NOT here: the drawer's rules come from
 *  `tools.get`, whose rows carry their own `describe`, so a second read of the
 *  policy set would be a second answer to one question. Everything outside this
 *  set offends the guard. */
const ALLOWED = new Set([
  "tools.list",
  "tools.get",
  "tools.byAgent",
  "tools.status",
  "tools.vocabulary",
  "surface.list",
  "surface.get",
  "agent.list",
]);

/** ≥ 1 literal per tab — the guard saw the whole surface, not one file. */
const PER_TAB: Record<string, string[]> = {
  tools: ["tools.list", "tools.status", "tools.vocabulary", "tools.get"],
  byAgent: ["tools.byAgent"],
  surface: ["surface.list", "surface.get"],
};

const CANARY_METHOD = "tools.update";

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
  return readdirSync(TOOLS_DIR)
    .filter(
      (f) =>
        (f.endsWith(".ts") || f.endsWith(".tsx")) &&
        !f.endsWith(".test.ts") &&
        !f.endsWith(".test.tsx"),
    )
    .map((f) => ({ file: join("tools", f), src: readFileSync(join(TOOLS_DIR, f), "utf8") }))
    .concat([{ file: "components/AgentPicker.tsx", src: readFileSync(AGENT_PICKER, "utf8") }]);
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

describe("toolsRpc guard (TC9/TC10/TC12, P34)", () => {
  it("runs over the real section source (not vacuous)", () => {
    expect(productionSources().length).toBeGreaterThan(8);
  });

  it("calls only the registered catalog reads", () => {
    const registered = registeredMethods();
    expect(registered.size).toBeGreaterThan(0);
    const all = productionSources().flatMap(({ src }) => extractRpcMethods(src));
    expect(all.length).toBeGreaterThan(0);
    const offenders = all.filter((m) => !ALLOWED.has(m));
    expect(offenders, `methods outside the S47 read set: ${offenders.join(", ")}`).toEqual([]);
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

  it("has teeth (positive control): a synthetic tools.update is caught and refused", () => {
    const canary = `const x = rpcTyped("${CANARY_METHOD}", {});`;
    expect(extractRpcMethods(canary)).toContain(CANARY_METHOD);
    expect(ALLOWED.has(CANARY_METHOD)).toBe(false);
    // And the canary is a REAL method name shape: a guard that only refused
    // names the contract had never heard of would pass on any typo.
    expect(CANARY_METHOD.startsWith("tools.")).toBe(true);
    expect(registeredMethods().has(CANARY_METHOD)).toBe(false);
  });

  it("has teeth (positive control): a synthetic direct fetch is caught, refetch is not", () => {
    const illegal = ["const r = await fet", 'ch("/tools/list"); q.refet', "ch();"].join("");
    const fetchCall = new RegExp("\\bfe" + "tch\\s*\\(");
    expect(fetchCall.test(illegal)).toBe(true);
    expect(fetchCall.test("queries.forEach((q) => q.refet" + "ch());")).toBe(false);
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

  it("names no closed set: no capability, state or door literal in the section source", () => {
    // The words the DAEMON owns. They may appear in a conformance fixture or a
    // test's mock answer, and nowhere else: a section that spells them has
    // started deciding what a capability is, which is the failure TC3 exists to
    // prevent and the reason `tools.vocabulary` was added.
    const forbidden = ["spawn_process", "network_egress", "granted_allowed", "agent service"];
    for (const { file, src } of productionSources()) {
      for (const word of forbidden) {
        expect(src.includes(word), `${file} hardcodes the daemon word ${word}`).toBe(false);
      }
    }
  });

  it("mounts beside the panels it joined, and disturbs none of them", () => {
    // S72 T2 re-pin (R3): this pinned the Settings stack mounts. Every
    // section now owns a Configuration page (CF3/CF8); the structural check
    // — each section mounts exactly once, no panel edited into another —
    // now scans features/config/pages, and Settings mounts none of them.
    for (const [component, page] of [["DaemonSettingsPanel", "daemon-settings"], ["SystemRecordsPanel", "system-records"], ["PolicySection", "policy"], ["McpSection", "mcp"], ["ToolsSection", "tools"]] as const) {
      expect(configMountCount(component), `${component} mounts ${configMountCount(component)} times in config pages`).toBe(1);
      expect(configPageRegisteredOnce(page), `${page} is registered`).toBe(true);
      expect(settingsScreenMounts(component), `${component} still mounts on Settings`).toBe(false);
    }
  });

  it("offers no control that changes the daemon: the section holds no write verb", () => {
    // TC1: no write method, tool or verb. A section that only reads has no
    // reason to name an action-shaped method at all.
    const writeShaped = /^(tools|surface)\.(add|create|update|delete|remove|enable|disable|set|grant|revoke|import|apply|commit|write|seed|repair|sync|clear|prune)/;
    for (const { file, src } of productionSources()) {
      for (const method of extractRpcMethods(src)) {
        expect(writeShaped.test(method), `${file} calls the write-shaped method ${method}`).toBe(false);
      }
    }
  });
});

/**
 * i18n coverage: every `settings.tools` key the sources reference exists in BOTH
 * catalogs — and the three drift captions, which are reached by a key built from
 * the daemon's own state string (so no literal in the source to scan), are checked
 * by name here.
 */
describe("tools i18n coverage", () => {
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
    expect(lookup(en, "settings.tools.nope.not.a.key")).toBeUndefined();
    expect(lookup(en, "settings.tools.title")).toBeTypeOf("string");
  });

  it("every settings.tools key the sources reference exists in en and pt", () => {
    const keys = new Set<string>();
    const re = /"((?:settings\.tools)\.[^"']+)"/g;
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

  it("carries a caption for every drift state the daemon can report", () => {
    // These keys are built at render time from the state the daemon sent, so a
    // source scan cannot see them. A state with no caption would fall back to the
    // bare word; a state with a caption only in one language is a lie in the other.
    for (const state of ["missing_row", "retired", "differs"]) {
      const key = `settings.tools.drift.states.${state}`;
      expect(lookup(en, key), `en has no caption for ${state}`).toBeTypeOf("string");
      expect(lookup(pt, key), `pt has no caption for ${state}`).toBeTypeOf("string");
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
    leafKeys((en as Record<string, Record<string, unknown>>).settings?.tools, "settings.tools", enKeys);
    leafKeys((pt as Record<string, Record<string, unknown>>).settings?.tools, "settings.tools", ptKeys);
    expect(enKeys.length).toBeGreaterThan(60);
    expect(ptKeys).toEqual(enKeys);
  });
});
