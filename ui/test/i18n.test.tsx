import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { backupStatus, installFakeDaemon, resetFakeDaemon } from "./helpers";
import { appSettings } from "../src/lib/appSettings";
import { ProductApp } from "../src/app/router";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

beforeEach(() => {
  vi.stubGlobal("ResizeObserver", class { observe() {} disconnect() {} });
  vi.stubGlobal("matchMedia", () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
  window.location.hash = "#/";
});
afterEach(() => {
  cleanup();
  resetFakeDaemon();
  vi.unstubAllGlobals();
  act(() => appSettings.reset());
});

function keySet(d: Record<string, unknown>, pre = ""): Set<string> {
  const out = new Set<string>();
  for (const [k, v] of Object.entries(d)) {
    const p = pre ? `${pre}.${k}` : k;
    out.add(p);
    if (v && typeof v === "object") for (const sub of keySet(v as Record<string, unknown>, p)) out.add(sub);
  }
  return out;
}

describe("i18n", () => {
  it("S58 T8: en and pt catalogs are in parity, including the two navigate strings", async () => {
    const en = JSON.parse(readFileSync(resolve(process.cwd(), "src/lib/i18n/en.json"), "utf8"));
    const pt = JSON.parse(readFileSync(resolve(process.cwd(), "src/lib/i18n/pt.json"), "utf8"));
    const ek = keySet(en);
    const pk = keySet(pt);
    // Parity is WHOLE-FILE (876 keys as of S58 T8) — the parity this slice
    // adds to (`notifications.link.refused`, `notifications.link.gone`) is
    // the only new content, but the rule is the rule: a key missing from
    // either catalog reddens the whole suite, not just the new screen.
    expect([...ek].filter((k) => !pk.has(k))).toEqual([]);
    expect([...pk].filter((k) => !ek.has(k))).toEqual([]);
    expect(ek.has("notifications.link.refused")).toBe(true);
    expect(ek.has("notifications.link.gone")).toBe(true);
  });
  it("renders the home screen in English by default, and in Portuguese once switched in Settings", async () => {
    installFakeDaemon({
      responses: { "loop.list": { loops: [], all: true }, "component.list": { components: [] }, "blueprint.list": { names: [] },
        "routes.list": { routes: [], providers: [], persisted: true }, "subscribe": { subscriptionId: "s1", from: 0 },
        // The settings screen mounts the backups header, which renders the
        // contract's `db` struct verbatim. The fake's generic `{}` fallback is
        // truthy and would crash it; a real daemon sends the full struct or an
        // error, never the fallback. `seed.status` is the same story: the
        // policy section does `status.data?.sets.find(…)` on the required
        // array, and the neutral answer is "no sets declared".
        "backup.status": backupStatus(), "seed.status": { sets: [] } },
    });
    render(<ProductApp />);
    expect(await screen.findByTestId("product-home")).toBeTruthy();
    expect(screen.getByText("What should we build?")).toBeTruthy();
    expect(screen.getByText("Quick starters")).toBeTruthy();

    window.location.hash = "#/settings";
    await screen.findByTestId("product-settings");
    fireEvent.click(screen.getByRole("radio", { name: "Português" }));

    window.location.hash = "#/";
    await waitFor(() => expect(screen.getByText("O que o Aeon deve orquestrar?")).toBeTruthy());
    expect(screen.getByText("Inícios rápidos")).toBeTruthy();
  });
});
