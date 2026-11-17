import { describe, expect, it } from "vitest";
import { existsSync, readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";

// S61 T11 — the P7 guard SCAFFOLDING the new-project wizard inherits, in
// place BEFORE the wizard exists. The rule the future screen lives under:
// it reaches the daemon ONLY through registered `intake.*` (and the
// charter/template reads it shows), the vocabulary and the authority
// statements come from `intake.vocabulary` and never from a literal typed
// in `ui/src`, and nothing else is on the road.
//
// There is no wizard today, so the walk asserts the HONEST emptiness: it
// finds zero intake feature files. The moment a file matching
// `intake*.ts(x)` lands under src/features, this test FAILS LOUD until
// WIZARD_BUILT is flipped — a guard that silently inspects nothing is the
// vacuous guard T11 names, so the "found 0" case is itself an assertion,
// and the canary half runs whether or not the wizard exists.

const FEATURES_DIR = join(__dirname, "../src/features");
const CONTRACT = join(__dirname, "../src/proto/contract.ts");

/** The flip the wizard's task makes: 0 → ≥ 1 intake files per wizard tab.
 *  Until then, `intake*` feature files are an accident to be named. */
const WIZARD_BUILT = false; // ← the documented switch

/** The canary the positive control fires on — a method the plane does not
 *  have, in source shaped exactly like a call site. */
const CANARY_SOURCE = `const x = rpc("intake.frobnicate", {});`;

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

/** The plane the wizard may speak — the families the S61 surface registered. */
function plane(method: string): boolean {
  return /^(intake|charter|template)\./.test(method);
}

/** Every production source under src/features whose file name opens with
 *  `intake` — the wizard's own territory, walked recursively. */
function intakeSources(): Array<{ file: string; src: string }> {
  const out: Array<{ file: string; src: string }> = [];
  const walk = (dir: string, depth: number): void => {
    if (depth > 6 || !existsSync(dir)) return;
    for (const ent of readdirSync(dir, { withFileTypes: true })) {
      const full = join(dir, ent.name);
      if (ent.isDirectory()) walk(full, depth + 1);
      else if (ent.name.startsWith("intake") && (ent.name.endsWith(".ts") || ent.name.endsWith(".tsx")) && !ent.name.includes(".test.")) {
        out.push({ file: full.replace(__dirname + "/", ""), src: readFileSync(full, "utf8") });
      }
    }
  };
  walk(FEATURES_DIR, 0);
  return out;
}

describe("S61 T11 — the intake wizard RPC guard (scaffolding, P7)", () => {
  it("finds exactly zero intake feature files while the wizard does not exist", () => {
    const files = intakeSources();
    if (!WIZARD_BUILT) {
      expect(files.map((f) => f.file)).toEqual([]);
      // If this assertion ever fails, the wizard arrived without its
      // guard: flip WIZARD_BUILT, give the screen its per-tab floors
      // (≥ 1 method literal per tab, all inside the registered plane),
      // and re-run. That flip is part of the wizard's task, not here.
    } else {
      expect(files.length).toBeGreaterThanOrEqual(1);
    }
  });

  it("accepts only contract-registered methods from any intake source", () => {
    const registered = registeredMethods();
    for (const { file, src } of intakeSources()) {
      for (const m of extractRpcMethods(src)) {
        if (!registered.has(m)) {
          expect.fail(`${file} calls "${m}" — not in the generated contract (the guard is armed: no unregistered door)`);
        }
      }
    }
  });

  it("CANARY — the checker flags a method literal the contract does not register", () => {
    // The half that always runs: with or without a wizard, a bogus
    // literal must be catchable. If this test ever goes green while the
    // one above inspects nothing, the checker is broken, not the wizard.
    const registered = registeredMethods();
    const found = extractRpcMethods(CANARY_SOURCE);
    expect(found).toEqual(["intake.frobnicate"]);
    expect(registered.has("intake.frobnicate")).toBe(false);
    // and the plane's real doors pass the same check — the canary
    // discriminates, it does not simply reject everything.
    for (const real of ["intake.start", "intake.plan", "intake.commit", "intake.vocabulary", "charter.get", "template.list"]) {
      expect(registered.has(real)).toBe(true);
      expect(plane(real)).toBe(true);
    }
  });
});
