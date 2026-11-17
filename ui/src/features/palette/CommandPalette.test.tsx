import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { I18nProvider } from "@cjlapao/ui-kit";
import { APP_LOCALES } from "../../lib/i18n";
import { installFakeDaemon, resetFakeDaemon } from "../../../test/helpers";
import { CommandPalette } from "./CommandPalette";

const live = { state: "current", endpoint: "/tmp/aeon.sock", daemonVersion: "1", protocolVersion: 1, generation: 1 } as const;
function Where() { const l = useLocation(); return <div data-testid="where">{l.pathname}</div>; }

beforeEach(() => {
  vi.stubGlobal("ResizeObserver", class { observe() {} disconnect() {} });
  vi.stubGlobal("matchMedia", () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
  installFakeDaemon({ responses: {
    "component.list": { components: [{ id: "a", name: "aeon", slug: "a", root: "/w/aeon", parentId: "", shortDescription: "" }, { id: "ui", name: "ui", slug: "ui", root: "/w/aeon/ui", parentId: "a", shortDescription: "" }] },
    "loop.list": { all: true, loops: [{ id: "l1", slug: "s52-memory-map", title: "", componentRoot: "/w/aeon/ui", status: "running", phase: "", updatedAt: "", startedAt: "", awaiting: false, mode: "standard" }] },
  } });
});
afterEach(() => { cleanup(); resetFakeDaemon(); vi.unstubAllGlobals(); });

const mount = () => render(<I18nProvider locales={APP_LOCALES}><MemoryRouter><Routes><Route path="*" element={<><CommandPalette live={live} /><Where /></>} /></Routes></MemoryRouter></I18nProvider>);

describe("CommandPalette", () => {
  it("opens on Ctrl K, finds a component with its tab, and Enter goes there", async () => {
    mount();
    expect(screen.queryByTestId("palette-input")).toBeNull();
    fireEvent.keyDown(window, { key: "k", ctrlKey: true });
    const box = await screen.findByTestId("palette-input");
    fireEvent.change(box, { target: { value: "ui know" } });
    expect(await screen.findByTestId("palette-item-c-ui")).toBeTruthy();
    fireEvent.keyDown(box, { key: "Enter" });
    expect(screen.getByTestId("where").textContent).toBe("/work/ui/knowledge");
  });

  it("finds a loop by name and says when nothing matches", async () => {
    mount();
    fireEvent.keyDown(window, { key: "k", ctrlKey: true });
    const box = await screen.findByTestId("palette-input");
    fireEvent.change(box, { target: { value: "memory-map" } });
    fireEvent.click(await screen.findByTestId("palette-item-l-l1"));
    expect(screen.getByTestId("where").textContent).toBe("/loops/l1");
    fireEvent.keyDown(window, { key: "k", ctrlKey: true });
    fireEvent.change(await screen.findByTestId("palette-input"), { target: { value: "zzzzqq" } });
    expect(await screen.findByTestId("palette-empty")).toBeTruthy();
  });
});
