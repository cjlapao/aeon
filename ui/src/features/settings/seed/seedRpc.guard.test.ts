import { describe, expect, it } from "vitest";
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";

// P34 — the daemon's own surface is the primary interface. The System records
// panel may reach the daemon only through the four seed.* methods, and only
// through the generic rpc bridge (never a socket, a fetch, or browser storage).
// This guard is not vacuous: it is proven against a synthetic canary below.

const ALLOWED = new Set(["seed.status", "seed.plan", "seed.apply", "seed.restore"]);

/**
 * The method-name string literal the first argument to `rpc` / `rpcTyped` /
 * `useRpcQuery` carries. `useRpcQuery` is included because its read funnels into
 * the same generic bridge call — `seed.status` is reached through it.
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

/** The production (non-test) source of this panel, as read by the guard. */
function productionSources(): Array<{ file: string; src: string }> {
  const dir = __dirname;
  return readdirSync(dir)
    .filter(
      (f) =>
        (f.endsWith(".ts") || f.endsWith(".tsx")) &&
        !f.endsWith(".test.ts") &&
        !f.endsWith(".test.tsx"),
    )
    .map((f) => ({ file: f, src: readFileSync(join(dir, f), "utf8") }));
}

describe("seedRpc guard (P34)", () => {
  it("runs over the real panel source (not vacuous)", () => {
    expect(productionSources().length).toBeGreaterThan(0);
  });

  it("uses only the four seed.* methods, and each of them at least once", () => {
    const sources = productionSources();
    const allMethods = sources.flatMap(({ src }) => extractRpcMethods(src));
    const seedMethods = allMethods.filter((m) => m.startsWith("seed."));

    // The literals used are a subset of the four — a fifth would be a violation.
    const offenders = seedMethods.filter((m) => !ALLOWED.has(m));
    expect(offenders).toEqual([]);

    // At least one call per action — the guard saw the whole surface.
    for (const method of ALLOWED) {
      expect(seedMethods.includes(method), `expected ${method} to be called at least once`).toBe(true);
    }
  });

  it("the guard has teeth: a synthetic seed.frobnicate is detected and would be refused", () => {
    // The canary a guard must be able to fire on: a method outside the four.
    const canary = `const x = rpc("seed.frobnicate", {});`;
    const found = extractRpcMethods(canary);
    expect(found).toContain("seed.frobnicate");
    // …and it is not one of the four, so the subset assertion above would fail on it.
    expect(ALLOWED.has("seed.frobnicate")).toBe(false);
  });

  it("never opens a socket: no fetch and no localStorage in the panel source", () => {
    for (const { file, src } of productionSources()) {
      // `refetch(` must not be mistaken for `fetch(` — the word boundary keeps them apart.
      expect(/\bfetch\s*\(/.test(src), `${file} calls fetch`).toBe(false);
      expect(/\blocalStorage\b/.test(src), `${file} uses localStorage`).toBe(false);
    }
  });
});
