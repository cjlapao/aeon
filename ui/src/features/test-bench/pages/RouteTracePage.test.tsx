import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { I18nProvider } from "@cjlapao/ui-kit";
import { APP_LOCALES } from "../../../lib/i18n";
import { installFakeDaemon, resetFakeDaemon } from "../../../../test/helpers";
import { RouteTracePage } from "./RouteTracePage";

beforeEach(() => {
  vi.stubGlobal("ResizeObserver", class { observe() {} disconnect() {} });
  vi.stubGlobal("matchMedia", () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
});
afterEach(() => { cleanup(); resetFakeDaemon(); vi.unstubAllGlobals(); });

const ROUTE = { capabilities: ["thinking"], reasoningEfforts: ["low", "high"], id: "qwen", provider: "openai", model: "qwen-flash", displayName: "Qwen", hasKey: true, source: "stored", maxOutputTokens: 4096, priced: false, noPrice: true, extraBody: '{"a":1}' };
const RAW = 'data: {"choices":[{"delta":{"reasoning":"hmm"}}]}\n\ndata: [DONE]\n';

function mount(trace: unknown) {
  const calls: { method: string; params: unknown }[] = [];
  installFakeDaemon({ responses: {
    "routes.list": () => ({ routes: [ROUTE], providers: ["openai"], persisted: true }),
    "routes.trace": (p: unknown) => { calls.push({ method: "routes.trace", params: p }); return trace; },
    "routes.set": (p: unknown) => { calls.push({ method: "routes.set", params: p }); return { route: ROUTE }; },
  } });
  render(<I18nProvider locales={APP_LOCALES}><RouteTracePage connected /></I18nProvider>);
  return calls;
}

describe("Route trace page", () => {
  it("offers thinking and effort only when the route enables them, and builds the options from them", async () => {
    mount({});
    await screen.findByTestId("route-trace-saved");
    fireEvent.change(screen.getByLabelText("thinking"), { target: { value: "off" } });
    expect((screen.getByLabelText("extra body") as HTMLTextAreaElement).value).toContain('"enable_thinking": false');
    fireEvent.change(screen.getByLabelText("reasoning effort"), { target: { value: "high" } });
    const preview = screen.getByTestId("route-trace-willsend").textContent ?? "";
    expect(preview).toContain('"reasoning_effort": "high"');
    expect(preview).toContain('"enable_thinking": false');
    fireEvent.change(screen.getByLabelText("thinking"), { target: { value: "" } });
    expect((screen.getByLabelText("extra body") as HTMLTextAreaElement).value).toBe("");
  });

  it("hides both controls on a route that enables neither", async () => {
    installFakeDaemon({ responses: { "routes.list": () => ({ routes: [{ id: "plain", provider: "openai", model: "m", hasKey: true, source: "stored", maxOutputTokens: 1, priced: false }], providers: [], persisted: true }) } });
    render(<I18nProvider locales={APP_LOCALES}><RouteTracePage connected /></I18nProvider>);
    await screen.findByLabelText("route");
    expect(screen.queryByLabelText("thinking")).toBeNull();
    expect(screen.queryByLabelText("reasoning effort")).toBeNull();
  });

  it("sends the chosen options and shows the body sent, the raw reply, and a warning when reasoning goes unread", async () => {
    const calls = mount({ route: "qwen", effort: "low", url: "http://x/v1/chat/completions", requestBody: { reasoning_effort: "low" }, status: 200, raw: RAW, thinkingChars: 0, textChars: 0, toolCalls: 0, durationMs: 12 });
    await screen.findByTestId("route-trace-saved");
    fireEvent.change(screen.getByLabelText("reasoning effort"), { target: { value: "low" } });
    fireEvent.click(screen.getByText("enable_thinking: false"));
    fireEvent.click(screen.getByLabelText("offer a tool"));
    fireEvent.click(screen.getByTestId("route-trace-send"));
    await screen.findByTestId("route-trace-result");
    const sent = calls.find((c) => c.method === "routes.trace")!.params as Record<string, unknown>;
    expect(sent).toMatchObject({ route: "qwen", reasoningEffort: "low", offerTool: true, extraBody: { chat_template_kwargs: { enable_thinking: false } } });
    expect(screen.getByTestId("route-trace-sent").textContent).toContain("reasoning_effort");
    expect(screen.getByTestId("route-trace-raw").textContent).toContain("reasoning");
    expect(screen.getByTestId("route-trace-silent")).toBeTruthy();
  });

  it("refuses to send invalid JSON and says why next to the box", async () => {
    mount({});
    await screen.findByTestId("route-trace-saved");
    fireEvent.change(screen.getByLabelText("extra body"), { target: { value: "{oops" } });
    expect((screen.getByTestId("route-trace-send") as HTMLButtonElement).disabled).toBe(true);
    expect(screen.getByText("Extra body is not valid")).toBeTruthy();
  });

  it("saves the box as the route's extra body, touching nothing else", async () => {
    const calls = mount({});
    await screen.findByTestId("route-trace-saved");
    fireEvent.change(screen.getByLabelText("extra body"), { target: { value: '{"reasoning_effort":"low"}' } });
    fireEvent.click(screen.getByTestId("route-trace-save"));
    await waitFor(() => expect(calls.some((c) => c.method === "routes.set")).toBe(true));
    const set = calls.find((c) => c.method === "routes.set")!.params as Record<string, unknown>;
    expect(set).toMatchObject({ id: "qwen", provider: "openai", model: "qwen-flash", extraBody: '{"reasoning_effort":"low"}' });
    expect(set).not.toHaveProperty("apiKey");
    expect(set).not.toHaveProperty("prices");
  });
});
