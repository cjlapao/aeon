import { describe, expect, it } from "vitest";
import { readFileSync, readdirSync } from "node:fs";
import { join, relative } from "node:path";

function files(dir: string, skip = new Set(["node_modules"])): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) return skip.has(entry.name) ? [] : files(path, skip);
    return [path];
  });
}

describe("S9 v1 port boundary", () => {
  const manifest = readFileSync("PORT.md", "utf8");

  it("accounts for every v1 source module and every new component", () => {
    for (const path of files("../../packages/loop-state-ui/src")) {
      expect(manifest, `missing v1 module ${path}`).toContain(relative("../../packages/loop-state-ui/src", path));
    }
    for (const path of files("src/components")) {
      expect(manifest, `missing Aeon component ${path}`).toContain(relative("src", path));
    }
  });

  it("keeps HTTP, browser filesystem access and v1 modals out of the product", () => {
    for (const path of files("src")) {
      // A `*.guard.test.ts` file documents the patterns it scans FOR (its
      // canaries quote them), so it may mention them; it is not product code
      // reaching for HTTP or the browser filesystem.
      if (path.endsWith(".guard.test.ts")) continue;
      const source = readFileSync(path, "utf8");
      expect(source, path).not.toMatch(/\b(?:fetch|EventSource|readTextFile|readDir)\s*\(/);
      expect(source, path).not.toMatch(/@tauri-apps\/plugin-(?:fs|http)/);
      expect(path, path).not.toMatch(/(?:ProjectEdit|TaskStatus)Modal/);
    }
    const capability = JSON.parse(readFileSync("src-tauri/capabilities/default.json", "utf8")) as { permissions: string[] };
    expect(capability.permissions).not.toContain("fs:default");
  });

  it("uses the joined-menu and tracker-capable ui-kit release", () => {
    const packageJson = JSON.parse(readFileSync("package.json", "utf8")) as { dependencies: Record<string, string> };
    // 0.2.17 is the release that introduced the joined menu and the tracker
    // the S9 shell relies on; later slices bump the kit, so the floor is the
    // property, not the exact pin.
    const pinned = packageJson.dependencies["@cjlapao/ui-kit"] ?? "";
    const version = pinned.replace(/^[^\d]*/, "");
    // Absent or malformed pin → NaN → the floor check fails the test, which
    // is the honest answer for "I cannot prove the floor is met".
    const [major = NaN, minor = NaN, patch = NaN] = version.split(".").map(Number);
    const atLeast = major > 0 || (major === 0 && (minor > 2 || (minor === 2 && patch >= 17)));
    expect(atLeast, `@cjlapao/ui-kit ${version} predates the joined-menu/tracker release 0.2.17`).toBe(true);
  });
});
