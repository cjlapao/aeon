import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { I18nProvider } from "@cjlapao/ui-kit";
import { APP_LOCALES } from "../../lib/i18n";
import { installFakeDaemon, resetFakeDaemon } from "../../../test/helpers";
import { ModelTestPanel } from "./ModelTestPanel";

const route = { id: "research", model: "gpt-x", provider: "openai", displayName: "Research", capabilities: ["text", "image"], hasKey: true, maxOutputTokens: 4096, priced: false, source: "stored" } as never;

beforeEach(() => {
  vi.stubGlobal("ResizeObserver", class { observe() {} disconnect() {} });
  vi.stubGlobal("matchMedia", () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
});
afterEach(() => { cleanup(); resetFakeDaemon(); vi.unstubAllGlobals(); });

const mount = (canRun = true) => render(<I18nProvider locales={APP_LOCALES}><ModelTestPanel route={route} canRun={canRun} onClose={() => {}} /></I18nProvider>);

describe("ModelTestPanel", () => {
  it("sends nothing until asked, then shows the answer with time, tokens and the cost in its own state", async () => {
    const daemon = installFakeDaemon({ responses: { ask: { askId: "a1", text: "I am Research.", usage: { inputTokens: 12, outputTokens: 5 }, cost: { state: "known", usd: 0.0003 }, stopReason: "end_turn" } } });
    mount();
    await screen.findByTestId("model-test-panel");
    expect(daemon.methods()).not.toContain("ask");
    fireEvent.click(screen.getByTestId("model-test-run"));
    expect(await screen.findByText("I am Research.")).toBeTruthy();
    expect(daemon.paramsOf("ask")).toMatchObject({ route: "research" });
    expect(screen.getByText(/12 in · 5 out tokens/)).toBeTruthy();
    expect(screen.getByTestId("model-test-cost").textContent).not.toMatch(/^\$0\.00$/);
  });

  it("an unknown cost is not shown as free, and a refusal is shown in the daemon's words", async () => {
    installFakeDaemon({ responses: { ask: { askId: "a1", text: "ok", cost: { state: "unknown", usd: 0 } } } });
    mount();
    fireEvent.click(await screen.findByTestId("model-test-run"));
    expect((await screen.findByTestId("model-test-cost")).textContent).not.toContain("$0.00");
    cleanup(); resetFakeDaemon();
    installFakeDaemon({ responses: { ask: () => { throw Object.assign(new Error("route has no key"), { code: -32000 }); } } });
    mount();
    fireEvent.click(await screen.findByTestId("model-test-run"));
    expect(await screen.findByTestId("model-test-error")).toBeTruthy();
  });

  it("checks capabilities only on request, and keeps verified, contradicted and declared apart", async () => {
    const daemon = installFakeDaemon({ responses: { "routes.probe": { route: "research", saved: true, probes: [
      { capability: "text", state: "verified", detail: "answered" },
      { capability: "image", state: "contradicted", detail: "the provider refused image input" },
    ] } } });
    mount();
    fireEvent.click(await screen.findByRole("tab", { name: "Capabilities" }));
    expect(daemon.methods()).not.toContain("routes.probe");
    expect(screen.getAllByText("declared")).toHaveLength(2); // nothing checked yet
    fireEvent.click(screen.getByTestId("model-probe-run"));
    expect(await screen.findByText("the provider refused image input")).toBeTruthy();
    expect(screen.getByTestId("probe-text").textContent).toContain("verified");
    expect(screen.getByTestId("probe-image").textContent).toContain("contradicted");
  });

  it("is off while the daemon is not current", async () => {
    installFakeDaemon({ responses: {} });
    mount(false);
    expect((await screen.findByTestId("model-test-run") as HTMLButtonElement).disabled).toBe(true);
  });
});
