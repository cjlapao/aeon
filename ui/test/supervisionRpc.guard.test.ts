import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

// S58 T8 — the method-literal guard (the S47 TC-suite shape, app half):
// every protocol method the T8-touched files pass to the rpc helpers must
// exist in the generated `contract.ts`. A method renamed on the daemon and
// missed here is a `-32601` at runtime; this makes it a red test now. The
// canary proves the extractor has teeth on both ends.

const TOUCHED = [
  "src/app/NavigateListener.tsx",
  "src/app/NotificationListener.tsx",
  "src/lib/notifications.ts",
  "src/lib/osNotify.ts",
  "src/lib/bridge.ts",
];

/**
 * Method literals passed to the rpc helpers: `rpc("m"…`, `rpcTyped("m"…`
 * (the generic bridge call and its typed twin — the two shapes a method
 * name reaches the daemon through in the web layer).
 */
export function methodLiterals(source: string): string[] {
  const out: string[] = [];
  const re = /\brpc(?:Typed)?(?:<[^(>]*>)?\s*\(\s*"([^"]+)"/g;
  for (const m of source.matchAll(re)) if (m[1]) out.push(m[1]);
  return out;
}

/** Every method key the generated contract declares (`"loop.show": { params:`). */
function contractMethods(): Set<string> {
  const raw = readFileSync(resolve(process.cwd(), "src/proto/contract.ts"), "utf8");
  const keys = new Set<string>();
  for (const m of raw.matchAll(/^\s{2}"([^"]+)": \{ params:/gm)) if (m[1]) keys.add(m[1]);
  return keys;
}

function flagged(literals: string[], known: Set<string>): string[] {
  return literals.filter((m) => !known.has(m));
}

describe("supervisionRpc guard (S58 T8)", () => {
  const known = contractMethods();

  it("finds the literals it is guarding, and every one is registered", () => {
    const found = new Set<string>();
    for (const file of TOUCHED) {
      for (const m of methodLiterals(readFileSync(resolve(process.cwd(), file), "utf8"))) found.add(m);
    }
    const literals = [...found];
    // ≥ 1 found: the guard itself must not silently guard nothing — the
    // existence read `NavigateListener` takes is the literal that anchors
    // it (SV9: the only verb a link-shaped flow holds).
    expect(literals.length).toBeGreaterThanOrEqual(1);
    expect(literals).toContain("loop.show");
    expect(flagged(literals, known)).toEqual([]);
  });

  it("canary: a bogus method is flagged, a registered one is not", () => {
    expect(flagged(["bogus.method"], known)).toEqual(["bogus.method"]);
    expect(flagged(["loop.show"], known)).toEqual([]);
    // And the extractor pulls literals out of the call shape they actually
    // occur in — including the typed form with its generic argument:
    expect(methodLiterals(`rpc<LoopDetail>("loop.show", p)`)).toEqual(["loop.show"]);
    expect(methodLiterals(`rpcTyped("subscribe", { after: 0 })`)).toEqual(["subscribe"]);
    // A bare string that is not a call shape is NOT pulled (the extractor
    // guards calls, not prose):
    expect(methodLiterals(`const m = "loop.show";`)).toEqual([]);
  });
});
