import { describe, expect, it } from "vitest";
import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";

// P34 — the daemon's own surface is the primary interface, and S45 T9 keeps
// it that way: the Policy section and the shared AgentPicker reach the daemon
// ONLY through the registered methods below and ONLY through the generic rpc
// bridge — never a socket, a fetch, or browser storage (no `localStorage`,
// §4c; no new Tauri command).
//
// This guard is NOT vacuous, and says so out loud (each positive control is
// its own test):
//   • the synthetic `policy.frobnicate` canary IS caught by the extractor and
//     is NOT in the allowed set (the subset assertion would fire on it);
//   • a synthetic direct `fetch("/policy")` IS caught by the no-fetch
//     assertion (and `refetch(` — the query hook's own word — is not);
//   • the scan runs over the real production sources (it asserts non-empty).

const POLICY_DIR = __dirname;
const AGENT_PICKER = join(__dirname, "../../../components/AgentPicker.tsx");
const AGENT_TAG_PICKER = join(__dirname, "../../../components/AgentTagPicker.tsx");
const CONTRACT = join(__dirname, "../../../proto/contract.ts");

/** The methods this slice's doors expose (S45 PM1/PM5): the policy reads and
 *  authoring doors, the proposal decision doors, the two seed reads the
 *  corpus view uses, and the roster read the picker uses. */
const ALLOWED = new Set([
  "policy.list",
  "policy.get",
  "policy.add",
  "policy.remove",
  "policy.seedCorpus",
  "policy.vocabulary",
  "policy.why",
  "policy.inForce",
  "policy.changes",
  "proposal.list",
  "proposal.get",
  "proposal.commit",
  "proposal.reject",
  "proposal.discard",
  "seed.status",
  "seed.restore",
  "agent.list",
]);

/** At least one literal from every tab — the guard saw the whole surface,
 *  not one lucky file. */
const PER_TAB: Record<string, string[]> = {
  rules: ["policy.list", "policy.vocabulary", "policy.add", "policy.remove", "policy.seedCorpus", "seed.status", "seed.restore"],
  proposals: ["proposal.list", "proposal.get", "proposal.commit", "proposal.reject", "proposal.discard"],
  inforce: ["policy.inForce", "policy.why"],
  audit: ["policy.changes"],
};

/** The P34 canary the positive control fires on — a method that exists
 *  nowhere on the daemon. */
const CANARY_METHOD = "policy.frobnicate";

/**
 * The method-name string literal the first argument to `rpc` / `rpcTyped` /
 * `useRpcQuery` carries (same extractor discipline as seedRpc.guard.test.ts).
 */
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

/** The production (non-test) sources the guard owns: the section, the doors'
 *  components, the pure model — and the shared picker. */
function productionSources(): Array<{ file: string; src: string }> {
  const files = [
    ...readdirSync(POLICY_DIR)
      .filter(
        (f) =>
          (f.endsWith(".ts") || f.endsWith(".tsx")) &&
          !f.endsWith(".test.ts") &&
          !f.endsWith(".test.tsx") &&
          f !== "policyRpc.guard.test.ts",
      )
      .map((f) => ({ file: join("policy", f), src: readFileSync(join(POLICY_DIR, f), "utf8") })),
    { file: "components/AgentPicker.tsx", src: readFileSync(AGENT_PICKER, "utf8") },
    { file: "components/AgentTagPicker.tsx", src: readFileSync(AGENT_TAG_PICKER, "utf8") },
  ];
  return files;
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

describe("policyRpc guard (P34)", () => {
  it("runs over the real section source (not vacuous)", () => {
    expect(productionSources().length).toBeGreaterThan(5);
  });

  it("calls only the registered S45 methods", () => {
    const registered = registeredMethods();
    expect(registered.size).toBeGreaterThan(0);
    const all = productionSources().flatMap(({ src }) => extractRpcMethods(src));
    expect(all.length).toBeGreaterThan(0);
    const offenders = all.filter((m) => !ALLOWED.has(m));
    expect(offenders, `methods outside the S45 set: ${offenders.join(", ")}`).toEqual([]);
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

  it("has teeth (positive control): a synthetic policy.frobnicate is caught and refused", () => {
    // The exact thing the assertion above guards against — an illegal direct
    // call — proven to be DETECTED, so the green suite above means something.
    const canary = `const x = rpcTyped("${CANARY_METHOD}", {});`;
    const found = extractRpcMethods(canary);
    expect(found).toContain(CANARY_METHOD);
    expect(ALLOWED.has(CANARY_METHOD)).toBe(false);
    expect(registeredMethods().has(CANARY_METHOD)).toBe(false);
  });

  it("has teeth (positive control): a synthetic direct fetch is caught, refetch is not", () => {
    // The banned token is BUILT by concatenation so this file can be grepped
    // for what it forbids (the same discipline as test/policy.test.tsx).
    const illegal = ["const r = await fet", 'ch("/policy/list?x=1"); q.refet', "ch();"].join("");
    const fetchCall = new RegExp("\\bfe" + "tch\\s*\\(");
    expect(fetchCall.test(illegal)).toBe(true);
    // …and the legitimate hook word is never mistaken for it.
    expect(fetchCall.test("queries.forEach((q) => q.refet" + "ch());")).toBe(false);
  });

  it("never opens a socket or stores state: no raw request object or browser storage in the sources", () => {
    for (const { file, src } of productionSources()) {
      // Tokens built by concatenation: the repo-wide no-socket guard greps
      // this very file for the contiguous forms it forbids.
      expect(new RegExp("\\bfe" + "tch\\s*\\(").test(src), `${file} calls fet` + "ch").toBe(false);
      expect(new RegExp("\\bXM" + "LHttpRequest\\b").test(src), `${file} opens a raw request`).toBe(false);
      expect(/\blocalStorage\b/.test(src), `${file} uses localStorage`).toBe(false);
      expect(/\bsessionStorage\b/.test(src), `${file} uses sessionStorage`).toBe(false);
    }
  });

  it("names no closed set: the section source carries no vocabulary literals (item 14)", () => {
    // The forbidden words, and the ONLY place each is allowed — the i18n
    // catalogs (test fixtures included) — is outside the section source.
    const forbidden = ["tool_access", "command_access", "all-agents", "lane-type"];
    for (const { file, src } of productionSources()) {
      for (const word of forbidden) {
        expect(src.includes(`"${word}"`), `${file} hardcodes the vocabulary literal ${word}`).toBe(false);
      }
    }
  });
});

/**
 * i18n coverage — the same guard's second job: every message key the section
 * and the picker reference must exist in BOTH catalogs (en and pt). A key
 * that ships in one language only is a half-translated surface, and nothing
 * else in the build notices.
 */
describe("policy i18n coverage", () => {
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
    expect(lookup(en, "settings.policy.nope.not.a.key")).toBeUndefined();
    expect(lookup(en, "settings.policy.title")).toBeTypeOf("string");
  });

  it("every t() key the sources reference exists in en and pt", () => {
    const keys = new Set<string>();
    const re = /\bt\(\s*["']((?:settings\.policy|components\.agentPicker)\.[^"']+)["']/g;
    for (const { src } of productionSources()) {
      let match: RegExpExecArray | null;
      while ((match = re.exec(src)) !== null) {
        if (match[1] !== undefined) keys.add(match[1]);
      }
    }
    // Non-vacuous: the scan itself must find the keys the section uses.
    expect(keys.size).toBeGreaterThan(40);
    const missing: string[] = [];
    for (const key of keys) {
      if (lookup(en, key) === undefined) missing.push(`en:${key}`);
      if (lookup(pt, key) === undefined) missing.push(`pt:${key}`);
    }
    expect(missing, `catalog gaps: ${missing.join(", ")}`).toEqual([]);
  });
});
