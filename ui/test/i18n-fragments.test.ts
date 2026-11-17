import { describe, expect, it } from "vitest";
import { readdirSync, readFileSync } from "node:fs";
import { resolve } from "node:path";

// Every catalog fragment (`en-<name>.json`) has a Portuguese twin (`pt-<name>.json`) with the same keys
// and the same {placeholders} in every message. Whole-core parity is in i18n.test.tsx; this covers the
// fragments, `work.*` among them, so a string added in English only fails here and not on a screen.

const dir = resolve(process.cwd(), "src/lib/i18n");
const read = (file: string): Record<string, unknown> => JSON.parse(readFileSync(resolve(dir, file), "utf8"));

function leaves(node: unknown, path = "", out = new Map<string, string>()): Map<string, string> {
  if (typeof node === "string") out.set(path, node);
  else if (node && typeof node === "object") for (const [k, v] of Object.entries(node)) leaves(v, path ? `${path}.${k}` : k, out);
  return out;
}

/** The simple argument names in an ICU message: {count}, {name, plural, …} → count, name. */
const argsOf = (message: string): string[] => [...message.matchAll(/\{\s*([A-Za-z_][\w]*)\s*[,}]/g)].map((m) => m[1]!).sort();

const names = readdirSync(dir).filter((f) => /^en-.+\.json$/.test(f)).map((f) => f.slice(3, -5));

describe("i18n fragments", () => {
  it("finds the fragments, work among them", () => {
    expect(names).toContain("work");
  });

  it.each(names)("%s: pt has exactly the keys en has", (name) => {
    const en = leaves(read(`en-${name}.json`));
    const pt = leaves(read(`pt-${name}.json`));
    expect([...pt.keys()].filter((k) => !en.has(k))).toEqual([]);
    expect([...en.keys()].filter((k) => !pt.has(k))).toEqual([]);
  });

  it.each(names)("%s: every message keeps its placeholders in both languages", (name) => {
    const en = leaves(read(`en-${name}.json`));
    const pt = leaves(read(`pt-${name}.json`));
    const drift = [...en].filter(([k, v]) => pt.has(k) && argsOf(v).join() !== argsOf(pt.get(k)!).join()).map(([k]) => k);
    expect(drift).toEqual([]);
  });

  it("the new core namespaces (home.stat, home.needs, home.projects, palette) are in both languages", () => {
    const en = leaves(read("en.json"));
    const pt = leaves(read("pt.json"));
    const wanted = [...en.keys()].filter((k) => /^(home\.(stat|needs|projects)|palette)\./.test(k));
    expect(wanted.length).toBeGreaterThan(30);
    expect(wanted.filter((k) => !pt.has(k))).toEqual([]);
  });
});
