import { describe, expect, it } from "vitest";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";

/**
 * The web layer never touches a socket.
 *
 * A browser cannot speak a unix socket, which is why the Tauri shell IS the UI
 * rather than optional packaging. **If the web layer ever imports a socket
 * library, something has gone wrong** — and it would go wrong silently, because
 * such an import would work in a Node test and fail only in the window.
 *
 * Every byte on the wire belongs to `src-tauri/`. The web layer's entire access
 * to the daemon is `invoke(...)`.
 */
function sourceFiles(dir: string): string[] {
  const out: string[] = [];
  for (const entry of readdirSync(dir)) {
    const path = join(dir, entry);
    if (statSync(path).isDirectory()) {
      out.push(...sourceFiles(path));
    } else if (/\.(ts|tsx)$/.test(entry)) {
      out.push(path);
    }
  }
  return out;
}

/** Strip comments, so prose explaining why sockets are forbidden is not a hit. */
function code(src: string): string {
  return src
    .replace(/\/\*[\s\S]*?\*\//g, "")
    .split("\n")
    .filter((line) => !line.trim().startsWith("//") && !line.trim().startsWith("*"))
    .join("\n");
}

describe("the web layer", () => {
  const files = sourceFiles("src");

  it("has sources to check", () => {
    expect(files.length).toBeGreaterThan(2);
  });

  it("imports no socket or network library", () => {
    const forbidden = [
      "node:net",
      "node:dgram",
      "node:tls",
      '"net"',
      "'net'",
      "ws",
      "socket.io",
      "unix-dgram",
    ];
    for (const file of files) {
      const body = code(readFileSync(file, "utf8"));
      const imports = [...body.matchAll(/(?:import|require)\s*\(?\s*["']([^"']+)["']/g)].map(
        (m) => m[1],
      );
      for (const spec of imports) {
        for (const bad of forbidden) {
          const cleaned = bad.replace(/["']/g, "");
          expect(
            spec === cleaned,
            `${file} imports ${spec}; the web layer reaches the daemon only through Tauri commands`,
          ).toBe(false);
        }
      }
    }
  });

  it("makes no direct fetch or WebSocket call", () => {
    for (const file of files) {
      const body = code(readFileSync(file, "utf8"));
      for (const bad of ["new WebSocket", "XMLHttpRequest", "EventSource"]) {
        expect(body.includes(bad), `${file} uses ${bad}`).toBe(false);
      }
      // A plain substring check for "fetch(" also flags `refetch(`/`prefetch(`
      // — legitimate local callback names (`useRpcQuery`'s own `refetch`) that
      // are not the global `fetch` API. Still catches a bare `fetch(...)` and
      // `window.fetch(...)`: only a preceding LETTER (not a dot) means "fetch"
      // is the tail of a different identifier.
      expect(/(?<![A-Za-z])fetch\(/.test(body), `${file} uses fetch(`).toBe(false);
    }
  });

  it("reaches the daemon only through the bridge", () => {
    // `invoke` is imported in exactly one place. A second would be a second
    // path to the daemon, and S9's generic bridge could not then be the only
    // place the method table lives (U11).
    // Both forms. bridge.ts imports Tauri DYNAMICALLY — `await
    // import("@tauri-apps/api/core")`, so a test never loads Tauri's runtime —
    // and the first version of this check only looked for `from "..."`, so it
    // found nothing and would have passed even with five importers.
    const importers = files.filter((f) =>
      /(?:from\s*|import\s*\(\s*)["']@tauri-apps\/api/.test(code(readFileSync(f, "utf8"))),
    );
    expect(importers, `expected only src/lib/bridge.ts, got ${importers.join(", ")}`).toEqual([
      join("src", "lib", "bridge.ts"),
    ]);
  });
});
