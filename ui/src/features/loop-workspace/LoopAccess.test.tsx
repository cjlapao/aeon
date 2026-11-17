import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { I18nProvider } from "@cjlapao/ui-kit";
import { APP_LOCALES } from "../../lib/i18n";
import { installFakeDaemon, resetFakeDaemon } from "../../../test/helpers";
import { LoopAccess } from "./LoopAccess";

const live = { state: "current", endpoint: "/tmp/a.sock", daemonVersion: "1", protocolVersion: 1, generation: 1 } as const;
const ASKING = ["bash", "write", "edit", "doc_write", "doc_edit", "webfetch", "ask_agent"];

beforeEach(() => {
  vi.stubGlobal("ResizeObserver", class { observe() {} disconnect() {} });
  vi.stubGlobal("matchMedia", () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
});
afterEach(() => { cleanup(); resetFakeDaemon(); vi.unstubAllGlobals(); });

function mount(canWrite = true, signal = 0) {
  let mode = "ask";
  const daemon = installFakeDaemon({ responses: {
    "loop.access": () => ({ id: "l1", mode, tools: [], askingTools: ASKING }),
    "loop.setAccess": (raw: unknown) => { mode = (raw as { mode: string }).mode; return { id: "l1", mode, tools: [], askingTools: ASKING }; },
    subscribe: { subscriptionId: "s1", from: 0 },
  } });
  render(<I18nProvider locales={APP_LOCALES}><LoopAccess loopId="l1" live={live} canWrite={canWrite} openSignal={signal} /></I18nProvider>);
  return daemon;
}

describe("LoopAccess", () => {
  it("shows that the loop asks first, and opens the choices", async () => {
    mount();
    fireEvent.click(await screen.findByTestId("loop-access"));
    expect(await screen.findByText("Ask me (default)")).toBeTruthy();
    expect(screen.getByText("Full access")).toBeTruthy();
  });

  it("full access needs the explicit warning acknowledged before it can be turned on", async () => {
    const daemon = mount();
    fireEvent.click(await screen.findByTestId("loop-access"));
    fireEvent.click(await screen.findByLabelText(/Full access/));
    expect(await screen.findByTestId("loop-access-warning")).toBeTruthy();
    expect(screen.getByText(/super powers/)).toBeTruthy();
    const apply = screen.getByTestId("loop-access-apply") as HTMLButtonElement;
    expect(apply.disabled).toBe(true);
    fireEvent.click(screen.getByLabelText("I understand"));
    expect(apply.disabled).toBe(false);
    fireEvent.click(apply);
    await vi.waitFor(() => expect(daemon.methods()).toContain("loop.setAccess"));
    expect(daemon.paramsOf("loop.setAccess")).toEqual({ id: "l1", mode: "full" });
  });

  it("pre-approving tools needs at least one, and sends the chosen names", async () => {
    const daemon = mount();
    fireEvent.click(await screen.findByTestId("loop-access"));
    fireEvent.click(await screen.findByLabelText(/Pre-approve some tools/));
    const apply = screen.getByTestId("loop-access-apply") as HTMLButtonElement;
    expect(apply.disabled).toBe(true);
    fireEvent.click(screen.getByRole("checkbox", { name: "bash" }));
    fireEvent.click(apply);
    await vi.waitFor(() => expect(daemon.methods()).toContain("loop.setAccess"));
    expect(daemon.paramsOf("loop.setAccess")).toEqual({ id: "l1", mode: "tools", tools: ["bash"] });
  });

  it("stays out of the way on a daemon without the door", async () => {
    installFakeDaemon({ responses: { "loop.access": () => { throw new Error("method not found"); } } });
    render(<I18nProvider locales={APP_LOCALES}><LoopAccess loopId="l1" live={live} canWrite /></I18nProvider>);
    await new Promise((r) => setTimeout(r, 50));
    expect(screen.queryByTestId("loop-access")).toBeNull();
  });

  it("opens on full access, warning first, when the ask card's button bumps the signal", async () => {
    const daemon = installFakeDaemon({ responses: {
      "loop.access": () => ({ id: "l1", mode: "ask", tools: [], askingTools: ASKING }),
      subscribe: { subscriptionId: "s1", from: 0 },
    } });
    expect(daemon).toBeTruthy();
    const view = render(<I18nProvider locales={APP_LOCALES}><LoopAccess loopId="l1" live={live} canWrite openSignal={0} /></I18nProvider>);
    await screen.findByTestId("loop-access");
    view.rerender(<I18nProvider locales={APP_LOCALES}><LoopAccess loopId="l1" live={live} canWrite openSignal={1} /></I18nProvider>);
    expect(await screen.findByTestId("loop-access-warning")).toBeTruthy();
    expect((screen.getByTestId("loop-access-apply") as HTMLButtonElement).disabled).toBe(true);
  });
});
