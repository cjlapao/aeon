import { describe, expect, it } from "vitest";
import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";
import { costTone, costWords, stateTone } from "./systemModel";

// S54 T6 — Settings → System reaches the daemon ONLY through the eight
// doors below, and ONLY through the generic rpc bridge: never a socket,
// never `fetch`, never browser storage (§4c), never a new Tauri command
// (P34). The plane's daemon-side guards (S54 T3) say the operations are
// human doors and the ledger stamps the peer; this file keeps the PANEL
// honest to its own half of SP8: the census equals the allow-list and
// every name is registered; no `settings.*` door appears anywhere — SP5
// shipped ZERO settings and the panel writes none; the state words and
// cost states exist in exactly one file (the tone map), so no panel
// compares a hardcoded state; the daemon's sentences ("already running",
// the starter naming) are never DEFINED here — they arrive in answers and
// ride verbatim; and the en/pt catalogs carry the same keys with real
// Portuguese, not copies.

const SYSTEM_DIR = __dirname;
const CONTRACT = join(__dirname, "../../../proto/contract.ts");
const EN = join(__dirname, "../../../lib/i18n/en-system.json");
const PT = join(__dirname, "../../../lib/i18n/pt-system.json");

/** The whole surface: the three S54 reads, the reindex the plan-confirm
 *  launches, the three graph human doors, and the four read-only lookups the
 *  explorer and the search probe make. Nothing else — no `settings.*`
 *  (SP5's zero), no bench-only surface (`retrieval.status`, `graph.status`,
 *  `graph.roots` answer to the bench pages' own files), no desk doors
 *  (S54 ships no proposal kinds), and no `graph.delete`-adjacent offline
 *  road: this surface IS the socket. */
const ALLOWED = new Set([
  "platform.status",
  "platform.changes",
  "retrieval.reindexPlan",
  "retrieval.reindex",
  "graph.build",
  "graph.reparse",
  "graph.delete",
  // The read-only lookups the explorer and the search probe make; they change nothing.
  "graph.query",
  "graph.explain",
  "graph.path",
  "search.query",
  // Follows a reindex or build pass the person started, to say how it ended; it only reads the worker's row.
  "asyncworkers.get",
  // Whether a model with the embedding capability exists, so Index can say what is missing before it asks anything.
  "routes.list",
]);

function extractRpcMethods(src: string): string[] {
  const found: string[] = [];
  const re = /\b(?:rpc|rpcTyped|useRpcQuery)\s*\(\s*["']([^"']+)["']/g;
  let match: RegExpExecArray | null;
  while ((match = re.exec(src)) !== null) {
    if (match[1] !== undefined) found.push(match[1]);
  }
  const concat = /"([a-z.-]+)\." \+ "([a-z.-]+)"/g;
  while ((match = concat.exec(src)) !== null) {
    if (match[1] !== undefined && match[2] !== undefined) found.push(`${match[1]}.${match[2]}`);
  }
  return found;
}

function productionSources(): Array<{ file: string; src: string }> {
  return readdirSync(SYSTEM_DIR)
    .filter(
      (f: string) =>
        (f.endsWith(".ts") || f.endsWith(".tsx")) &&
        !f.endsWith(".test.ts") &&
        !f.endsWith(".test.tsx"),
    )
    .map((f: string) => ({ file: `system/${f}`, src: readFileSync(join(SYSTEM_DIR, f), "utf8") }));
}

function isDoorFile(file: string): boolean {
  return file === "system/systemRpc.ts";
}

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

const enDoc = JSON.parse(readFileSync(EN, "utf8"));
const ptDoc = JSON.parse(readFileSync(PT, "utf8"));

describe("platformRpc guard — the doors (P34, SP8)", () => {
  it("runs over the real section source (not vacuous)", () => {
    expect(productionSources().length).toBeGreaterThan(6);
  });

  it("every method literal of this surface lives in systemRpc.ts alone", () => {
    const offenders = productionSources()
      .filter((s) => !isDoorFile(s.file))
      .filter((s) => extractRpcMethods(stripComments(s.src)).length > 0)
      .map((s) => s.file);
    expect(offenders, `method literals outside systemRpc.ts: ${offenders.join(", ")}`).toEqual([]);
  });

  it("the folder's usage equals the allow-list exactly — no more, no less", () => {
    const used = new Set<string>();
    for (const source of productionSources()) {
      for (const method of extractRpcMethods(stripComments(source.src))) used.add(method);
    }
    expect([...used].sort(), "extra or missing doors").toEqual([...ALLOWED].sort());
  });

  it("every allowed door is a REGISTERED method in the generated contract", () => {
    const registered = registeredMethods();
    for (const method of ALLOWED) {
      expect(registered.has(method), `${method} is not in the generated contract`).toBe(true);
    }
  });

  it("SP5's verdict: no settings door and no foreign surface anywhere", () => {
    for (const source of productionSources()) {
      const methods = extractRpcMethods(stripComments(source.src));
      for (const method of methods) {
        expect(method.startsWith("settings."), `${source.file} opens ${method} — SP5 shipped zero settings`).toBe(false);
        expect(method.startsWith("retrieval.status"), `${source.file} re-opens the bench surface`).toBe(false);
        expect(["graph.status", "graph.roots"].includes(method), `${source.file} re-opens ${method}, a bench door`).toBe(false);
      }
    }
  });

  it("reaches the daemon through the bridge only — no fetch, no storage, no invoke", () => {
    const banned = [/\bfetch\s*\(/, /localStorage/, /sessionStorage/, /indexedDB/, /\binvoke\s*\(/, /\bWebSocket\b/, /XMLHttp/];
    for (const source of productionSources()) {
      const src = stripComments(source.src);
      for (const re of banned) {
        expect(re.test(src), `${source.file} contains a banned road past the bridge: ${re}`).toBe(false);
      }
    }
  });

  it("has teeth (positive control): the census catches a synthetic extra door", () => {
    const synthetic = `const p = await rpcTyped("settings.set", { key: "x" }); void useRpcQuery("platform.audit", {}, c, l);`;
    const found = extractRpcMethods(synthetic);
    expect(found).toContain("settings.set");
    expect(found).toContain("platform.audit");
  });
});

describe("no hardcoded states (SP8)", () => {
  // The five state words and the three cost states live ONLY in
  // systemModel.ts, as the keys of the tone maps. No panel compares a
  // daemon word to a literal of its own; the model is the single home,
  // and it is checked too (below) so it cannot quietly grow a branch.
  const STATE_WORDS = ["available", "stale", "building", "unwired", "error", "known", "partial", "unknown"];

  it("no production source outside systemModel.ts carries a state-word literal", () => {
    for (const source of productionSources()) {
      if (source.file === "system/systemModel.ts") continue;
      const src = stripComments(source.src);
      for (const word of STATE_WORDS) {
        const re = new RegExp(`["'\`]${word}["'\`]`);
        expect(re.test(src), `${source.file} hardcodes the state word "${word}"`).toBe(false);
      }
    }
  });

  it("has teeth (positive control): a synthetic comparison is refused", () => {
    const synthetic = `if (block.state === "stale") show();`;
    expect(/["']stale["']/.test(stripComments(synthetic))).toBe(true);
  });

  it("the tone map keeps the daemon's words distinct, unknown words included", () => {
    expect(stateTone("available")).toBe("green");
    expect(stateTone("stale")).toBe("amber");
    expect(stateTone("building")).toBe("blue");
    expect(stateTone("unwired")).toBe("slate");
    expect(stateTone("error")).toBe("red");
    expect(stateTone("whatever-the-daemon-said")).toBe("slate");
    expect(costTone("known")).not.toBe(costTone("partial"));
    expect(costTone("partial")).not.toBe(costTone("unknown"));
  });

  it("a cost figure never stands without its state, and absent cost invents nothing", () => {
    const priced = costWords({ state: "known", usd: 0.25 });
    expect(priced.figure).toBe("$0.2500");
    expect(priced.state).toBe("known");
    const partial = costWords({ state: "partial", usd: 0.25, reason: "no price for half the route" });
    expect(partial.reason).toBe("no price for half the route");
    const bare = costWords(undefined);
    expect(bare.figure).toBeUndefined();
    expect(bare.state).toBe(""); // the daemon said nothing about money — no pill, no invention
  });
});

describe("the daemon's sentences are quoted, never copied (RM9's law, kept)", () => {
  it("no production source defines an already-running or starter sentence", () => {
    for (const source of productionSources()) {
      const src = stripComments(source.src);
      expect(/["'][^"']*already running/i.test(src), `${source.file} DEFINES the daemon's refusal`).toBe(false);
      expect(/["'][^"']*started by/i.test(src), `${source.file} DEFINES the starter sentence`).toBe(false);
    }
  });

  it("has teeth (positive control)", () => {
    expect(/["'][^"']*already running/i.test('throw new Error("already running — started by you")')).toBe(true);
  });

  it("the catalogs keep every language's own words — no three-word copies", () => {
    const enKeys = keysOf(enDoc, []);
    const copied = enKeys.filter((key) => {
      const enValue = lookup(enDoc, key);
      const ptValue = lookup(ptDoc, key);
      if (enValue === undefined || ptValue === undefined) return false;
      // "daemon" and "loop" legitimately ride into Portuguese; full
      // sentences never ride unchanged.
      return enValue === ptValue && enValue.split(/\s+/).length > 2;
    });
    expect(copied, `untranslated Portuguese copies: ${copied.join(", ")}`).toEqual([]);
  });
});

describe("the catalogs (P7: en/pt parity)", () => {
  it("every t() key of the folder exists in BOTH catalogs", () => {
    const used = new Set<string>();
    for (const source of productionSources()) {
      if (isDoorFile(source.file)) continue;
      for (const m of stripComments(source.src).matchAll(/\bt\(\s*"([a-zA-Z0-9._]+)"/g)) {
        if (m[1] !== undefined) used.add(m[1]);
      }
    }
    expect(used.size).toBeGreaterThan(35);
    for (const key of used) {
      expect(lookup(enDoc, key), `${key} missing from en`).not.toBeUndefined();
      expect(lookup(ptDoc, key), `${key} missing from pt`).not.toBeUndefined();
    }
  });

  it("no production source writes a t() key OUTSIDE the settings.system namespace", () => {
    for (const source of productionSources()) {
      if (isDoorFile(source.file)) continue;
      const strays = [...stripComments(source.src).matchAll(/\bt\(\s*"([a-zA-Z0-9._]+)"/g)]
        .map((m) => m[1] as string)
        .filter((key) => !key.startsWith("settings.system."));
      expect(strays, `${source.file} uses foreign keys: ${strays.join(", ")}`).toEqual([]);
    }
  });

  it("en and pt carry exactly the same key sets", () => {
    const enKeys = keysOf(enDoc, []).sort();
    const ptKeys = keysOf(ptDoc, []).sort();
    expect(ptKeys).toEqual(enKeys);
    expect(enKeys.length).toBeGreaterThan(40);
  });

  it("has teeth (positive control): the key scan sees a literal key and refuses a synthetic gap", () => {
    const synthetic = 'const a = t("settings.system.brand.new.key");';
    expect(/t\(\s*"settings\.system\./.test(synthetic)).toBe(true);
    expect(lookup(enDoc, "settings.system.brand.new.key")).toBeUndefined();
    expect(lookup(ptDoc, "settings.system.brand.new.key")).toBeUndefined();
  });
});
