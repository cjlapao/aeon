import { describe, expect, it } from "vitest";
import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";

// P34 — S46 T11 keeps the daemon's surface the primary interface: the MCP
// section, the shared picker, and the S45 components it REUSES (the desk,
// the card, the authoring form) reach the daemon ONLY through the
// registered methods below and ONLY through the generic rpc bridge — never
// a socket, a fetch, or browser storage (§4c; no new Tauri command).
//
// Not vacuous, and says so: the mcp.frobnicate canary IS caught and is NOT
// allowed; a synthetic direct fetch IS caught (refetch is not); the scan
// runs over real sources; every tab's surface is hit.

const MCP_DIR = __dirname;
const POLICY_DIR = join(__dirname, "../policy");
const AGENT_PICKER = join(__dirname, "../../../components/AgentPicker.tsx");
const CONTRACT = join(__dirname, "../../../proto/contract.ts");

/** The S46 management doors (reads and human-door writes), the S45 doors
 *  the two-step flow and the reused desk open, and the roster the picker
 *  reads. Everything outside this set offends the guard. */
const ALLOWED = new Set([
  "mcp.servers",
  "mcp.server",
  "mcp.check",
  "mcp.vocabulary",
  "mcp.server.add",
  // The inspect door: runs a candidate server and lists its tools; stores nothing.
  "mcp.server.inspect",
  // The variable manager's list: the binding picker offers its names (values never arrive).
  "vault.list",
  "mcp.server.update",
  "mcp.server.remove",
  "mcp.server.setEnabled",
  // The supervisor: state/PID per server, captured output, Start / Stop / Restart.
  "mcp.server.share",
  // The access panel writes the one allow rule for all agents, and takes it back.
  "policy.remove",
  "mcp.processes",
  "mcp.process.logs",
  "mcp.process.start",
  "mcp.process.stop",
  "mcp.process.restart",
  "mcp.grants",
  "mcp.grant.declare",
  "mcp.grant.revoke",
  "mcp.core.set",
  "mcp.changes",
  "mcp.importFile",
  "policy.vocabulary",
  "policy.add",
  "policy.inForce",
  "proposal.list",
  "proposal.get",
  "proposal.commit",
  "proposal.reject",
  "proposal.discard",
  "agent.list",
]);

/** ≥ 1 literal per tab — the guard saw the whole surface, not one file. */
const PER_TAB: Record<string, string[]> = {
  servers: ["mcp.servers", "mcp.server", "mcp.server.add", "mcp.server.inspect", "mcp.server.update", "mcp.server.remove", "mcp.server.setEnabled", "mcp.vocabulary", "mcp.processes", "mcp.process.logs", "mcp.process.start", "mcp.process.stop", "mcp.process.restart", "mcp.server.share", "policy.remove"],
  grants: ["mcp.grants", "mcp.grant.declare", "mcp.grant.revoke", "mcp.core.set", "policy.vocabulary", "policy.add"],
  proposals: ["proposal.list", "proposal.commit", "proposal.reject"],
  audit: ["mcp.changes"],
  import: ["mcp.importFile"],
};

const CANARY_METHOD = "mcp.frobnicate";

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
  const files: Array<{ file: string; src: string }> = readdirSync(MCP_DIR)
    .filter(
      (f) =>
        (f.endsWith(".ts") || f.endsWith(".tsx")) &&
        !f.endsWith(".test.ts") &&
        !f.endsWith(".test.tsx") &&
        f !== "mcpRpc.guard.test.ts",
    )
    .map((f) => ({ file: join("mcp", f), src: readFileSync(join(MCP_DIR, f), "utf8") }));
  // The reused S45 pieces are scanned too — "reuse, not copy" means the
  // reused code is held to this section's standard while it is used here.
  for (const reused of ["ProposalsDesk.tsx", "ProposalCard.tsx", "PolicyForm.tsx"]) {
    files.push({ file: join("policy", reused), src: readFileSync(join(POLICY_DIR, reused), "utf8") });
  }
  files.push({ file: "components/AgentPicker.tsx", src: readFileSync(AGENT_PICKER, "utf8") });
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

describe("mcpRpc guard (P34)", () => {
  it("runs over the real section source (not vacuous)", () => {
    expect(productionSources().length).toBeGreaterThan(10);
  });

  it("calls only the registered S46/S45 methods", () => {
    const registered = registeredMethods();
    expect(registered.size).toBeGreaterThan(0);
    const all = productionSources().flatMap(({ src }) => extractRpcMethods(src));
    expect(all.length).toBeGreaterThan(0);
    const offenders = all.filter((m) => !ALLOWED.has(m));
    expect(offenders, `methods outside the S46/S45 set: ${offenders.join(", ")}`).toEqual([]);
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

  it("has teeth (positive control): a synthetic mcp.frobnicate is caught and refused", () => {
    const canary = `const x = rpcTyped("${CANARY_METHOD}", {});`;
    expect(extractRpcMethods(canary)).toContain(CANARY_METHOD);
    expect(ALLOWED.has(CANARY_METHOD)).toBe(false);
    expect(registeredMethods().has(CANARY_METHOD)).toBe(false);
  });

  it("has teeth (positive control): a synthetic direct fetch is caught, refetch is not", () => {
    const illegal = ["const r = await fet", 'ch("/mcp/servers"); q.refet', "ch();"].join("");
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

  it("names no closed set: no vocabulary or grammar literal in the section source", () => {
    // The words the DAEMON's vocabulary owns. The only place they may live
    // is the catalog — the section reads them from mcp.vocabulary.
    const forbidden = ["stdio", "outward", "irreversible", "idempotent"];
    for (const { file, src } of productionSources()) {
      for (const word of forbidden) {
        expect(src.includes(word), `${file} hardcodes the vocabulary word ${word}`).toBe(false);
      }
    }
  });
});

/**
 * i18n coverage: every settings.mcp key the sources reference (t() calls AND
 * the model's quoted error keys) exists in BOTH catalogs.
 */
describe("mcp i18n coverage", () => {
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
    expect(lookup(en, "settings.mcp.nope.not.a.key")).toBeUndefined();
    expect(lookup(en, "settings.mcp.title")).toBeTypeOf("string");
  });

  it("every settings.mcp key the sources reference exists in en and pt", () => {
    const keys = new Set<string>();
    // Broad on purpose: quoted settings.mcp keys count whether they arrive
    // through t() or as a model-returned error key the renderer looks up.
    const re = /"((?:settings\.mcp)\.[^"']+)"/g;
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
});
