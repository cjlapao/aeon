import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { useState } from "react";
import { HeaderPortal, HeaderSlot, HeaderSlotProvider } from "../src/app/HeaderSlot";
import { PageLayout } from "../src/app/PageLayout";
import { layoutFor, type AppRoute } from "../src/app/routes";
import type { LiveController, LiveState } from "../src/lib/live";

// The app header's slot: a page puts its own controls into the fixed header
// without the header knowing about pages, and takes them out on unmount.

// The shell reads projects and loops for its panel only while the socket is
// current; a disconnected shell renders the chrome alone, which is what the
// layout assertions below need.
const idleController = { registerReader: () => () => {} } as unknown as LiveController;
const idleLive = { state: "disconnected", endpoint: "test", reason: "test" } satisfies LiveState;

afterEach(() => cleanup());

function Harness() {
  const [on, setOn] = useState(true);
  return <HeaderSlotProvider>
    <header><HeaderSlot side="start" /><HeaderSlot side="end" /></header>
    <button onClick={() => setOn((v) => !v)}>toggle</button>
    {on && <HeaderPortal side="end"><span>engine ready</span></HeaderPortal>}
    {on && <HeaderPortal side="start"><span>breadcrumb</span></HeaderPortal>}
  </HeaderSlotProvider>;
}

describe("the header slot", () => {
  it("a page puts content in the app header and takes it away when it unmounts", async () => {
    const { container } = render(<Harness />);
    const end = container.querySelector('[data-header-slot="end"]') as HTMLElement;
    const start = container.querySelector('[data-header-slot="start"]') as HTMLElement;
    expect(await within(end).findByText("engine ready")).toBeTruthy();
    expect(within(start).getByText("breadcrumb")).toBeTruthy();
    screen.getByText("toggle").click();
    await Promise.resolve();
    expect(container.textContent).not.toContain("engine ready");
    expect(container.textContent).not.toContain("breadcrumb");
  });

  it("renders nothing, and does not throw, outside a provider", () => {
    expect(() => render(<HeaderPortal side="end"><span>orphan</span></HeaderPortal>)).not.toThrow();
    expect(screen.queryByText("orphan")).toBeNull();
  });
});

describe("full-bleed layout", () => {
  it("gives a normal page its padded, width-capped wrapper and a workspace page none", () => {
    const normal = render(<MemoryRouter><PageLayout controller={idleController} live={idleLive}><p>page</p></PageLayout></MemoryRouter>);
    expect(normal.container.querySelector(".max-w-375")).toBeTruthy();
    normal.unmount();
    const bleed = render(<MemoryRouter><PageLayout fullBleed controller={idleController} live={idleLive}><p>page</p></PageLayout></MemoryRouter>);
    expect(bleed.container.querySelector(".max-w-375")).toBeNull();
    expect(bleed.getByText("page")).toBeTruthy();
  });

  it("knows which routes are workspace pages", () => {
    const routes: AppRoute[] = [
      { id: "loop", path: "/loops/:id", title: "Loop", access: "open", layout: "workspace" },
      { id: "step", path: "/steps/:id", title: "Step", access: "open" },
    ];
    expect(layoutFor("/loops/loop-1", routes)).toBe("workspace");
    expect(layoutFor("/loops/loop-1/classic", routes)).toBe("page"); // not matched: end-anchored
    expect(layoutFor("/steps/4", routes)).toBe("page");
    expect(layoutFor("/nope", routes)).toBe("page");
  });
});
