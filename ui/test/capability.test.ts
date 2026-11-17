import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { join } from "node:path";

/**
 * The capability that makes the event stream work.
 *
 * # Why a test guards a config file
 *
 * Deleting this file, or dropping its permission, breaks **nothing that any
 * other test can see**: the Rust compiles, the Go passes, every UI test passes
 * against a fake that always succeeds, and the app builds. The only symptom is
 * a window where every call works and every stream is silent — which is
 * precisely the bug that cost a debugging session.
 *
 * So the guard is here, where it is cheap, rather than relying on somebody
 * remembering.
 */
const capabilityPath = join("src-tauri", "capabilities", "default.json");

describe("the Tauri capability", () => {
  it("exists and grants the event permissions", () => {
    const raw = readFileSync(capabilityPath, "utf8");
    const cap = JSON.parse(raw) as {
      identifier?: string;
      windows?: string[];
      permissions?: string[];
    };

    expect(cap.permissions, `${capabilityPath} grants no permissions`).toBeDefined();
    expect(
      cap.permissions,
      "core:event:default is what allows listen/unlisten. Without it the webview cannot " +
        "receive a single event, while every invoke keeps working.",
    ).toContain("core:event:default");

    // The window label must match the one Tauri gives the declared window. A
    // capability naming a window that does not exist grants nothing, SILENTLY
    // — the same failure as having no capability at all.
    expect(cap.windows).toContain("main");
  });

  it("stays narrow", () => {
    const cap = JSON.parse(readFileSync(capabilityPath, "utf8")) as { permissions?: string[] };
    const granted = cap.permissions ?? [];

    // Least privilege, and not ceremony: the web layer's entire access to the
    // outside world is `invoke` of this app's own commands (not ACL-gated),
    // `listen` on two channels, and three individually-named notification
    // commands (S41's OS-mirror piece) — granted one at a time rather than
    // the plugin's own broader `notification:default` bundle, which also
    // hands out cancel/list/create-channel and more that nothing here uses.
    // Anything else widens what a compromised web layer could do.
    for (const tooBroad of ["core:default", "fs:default", "shell:default", "http:default", "notification:default"]) {
      expect(
        granted,
        `${tooBroad} is wider than this window needs. Adding a permission belongs in a ` +
          "reviewed diff with the reason written down.",
      ).not.toContain(tooBroad);
    }

    // A closed, named list rather than a bare count: adding a permission
    // means editing this list by name, not bumping a number that could be
    // satisfied by anything.
    const allowed = [
      "core:event:default",
      "notification:allow-is-permission-granted",
      "notification:allow-request-permission",
      "notification:allow-notify",
    ];
    for (const permission of granted) {
      expect(allowed, `${permission} is not in the reviewed allow-list — see capabilities/README.md`).toContain(permission);
    }
    expect(granted.length).toBeLessThanOrEqual(allowed.length);
  });

  it("is referenced by the Tauri config, or picked up by convention", () => {
    const conf = JSON.parse(readFileSync(join("src-tauri", "tauri.conf.json"), "utf8")) as {
      app?: { security?: { capabilities?: unknown[] } };
    };
    const listed = conf.app?.security?.capabilities ?? [];
    // An empty list means "use every capability in the directory", which is
    // what this project relies on. A NON-empty list that omits `default` would
    // silently exclude it — so if somebody starts listing capabilities, this
    // one has to be among them.
    if (listed.length > 0) {
      expect(JSON.stringify(listed)).toContain("default");
    }
  });
});
