import { afterEach, describe, expect, it } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { ChatPage } from "../src/features/test-bench/pages/ChatPage";
import { installFakeDaemon, resetFakeDaemon, routesList, routeView } from "./helpers";

// The test-bench's multi-turn harness over `ask` (S3) — "select the provider
// per turn, watch the stream" and nothing more: no server-side transcript, no
// stop/continue, one turn in flight at a time. The Ask tab's own pinned
// behaviours (subscribe before ask, a cursor while streaming) are proven
// again here because this page keeps its own copy of that ordering, not a
// shared one.

afterEach(() => {
  cleanup();
  resetFakeDaemon();
});

const TWO_ROUTES = routesList([
  routeView({ id: "anthropic", provider: "anthropic", model: "claude-sonnet-5" }),
  routeView({ id: "openai-fast", provider: "openai", model: "gpt-fast" }),
]);

function chatEvent(kind: string, payload: unknown) {
  return { subscriptionId: "s-1", seq: 1, kind, at: "2026-10-22T00:00:00Z", payload };
}

async function send(prompt: string) {
  fireEvent.change(screen.getByTestId("chat-input"), { target: { value: prompt } });
  fireEvent.click(screen.getByTestId("chat-send"));
}

describe("the Chat test-bench tab", () => {
  it("subscribes BEFORE the first ask, same ordering rule as the Ask tab", async () => {
    const fake = installFakeDaemon({ responses: {
      "routes.list": TWO_ROUTES, subscribe: { subscriptionId: "s-1", from: 1 }, ask: { askId: "a-1" },
    } });
    render(<ChatPage />);
    await waitFor(() => expect(screen.getByTestId("chat-route")).toBeTruthy());

    await send("hello");
    await waitFor(() => expect(fake.methods()).toContain("ask"));

    const methods = fake.methods();
    expect(methods.indexOf("ask")).toBeGreaterThan(methods.indexOf("subscribe"));
    expect(fake.paramsOf("ask")).toEqual({ route: "anthropic", prompt: "hello", subscriptionId: "s-1" });
  });

  it("streams a reply into its own turn, with a cursor while running and none once done", async () => {
    const fake = installFakeDaemon({ responses: {
      "routes.list": TWO_ROUTES, subscribe: { subscriptionId: "s-1", from: 1 }, ask: { askId: "a-1" },
    } });
    render(<ChatPage />);
    await waitFor(() => expect(screen.getByTestId("chat-route")).toBeTruthy());
    await send("one sentence on monorepos");
    await waitFor(() => expect(fake.methods()).toContain("ask"));

    const turn = await screen.findByTestId(/chat-turn-\d+$/);
    const turnId = turn.getAttribute("data-testid")!.replace("chat-turn-", "");

    act(() => fake.emit(chatEvent("ask.delta", { askId: "a-1", kind: "text", text: "a mono" })));
    act(() => fake.emit(chatEvent("ask.delta", { askId: "a-1", kind: "text", text: "repo" })));
    await waitFor(() => expect(screen.getByTestId(`chat-turn-${turnId}-text`).textContent).toContain("▌"));
    expect(screen.getByTestId(`chat-turn-${turnId}-text`).textContent).toContain("a monorepo");

    act(() => fake.emit(chatEvent("ask.done", {
      askId: "a-1", usage: { inputTokens: 3, outputTokens: 2 }, cost: { state: "known", usd: 0.0001 },
    })));
    await waitFor(() => expect(screen.getByTestId(`chat-turn-${turnId}-text`).textContent).not.toContain("▌"));
    expect(await screen.findByTestId(`chat-turn-${turnId}-done`)).toBeTruthy();
    // Send is free again — exactly one turn in flight at a time, not fewer.
    // (The button is also disabled on an empty box; typing proves it is not
    // `running` that is holding it down.)
    fireEvent.change(screen.getByTestId("chat-input"), { target: { value: "a follow-up" } });
    expect((screen.getByTestId("chat-send") as HTMLButtonElement).disabled).toBe(false);
  });

  it("lets the route change between turns, and each turn remembers which one it used", async () => {
    const asks: unknown[] = [];
    const fake = installFakeDaemon({ responses: {
      "routes.list": TWO_ROUTES, subscribe: { subscriptionId: "s-1", from: 1 },
      ask: (params: unknown) => { asks.push(params); return { askId: `a-${asks.length}` }; },
    } });
    render(<ChatPage />);
    await waitFor(() => expect(screen.getByTestId("chat-route")).toBeTruthy());

    await send("first, via anthropic");
    await waitFor(() => expect(fake.methods().filter((m) => m === "ask")).toHaveLength(1));
    act(() => fake.emit(chatEvent("ask.done", { askId: "a-1", usage: {}, cost: { state: "known", usd: 0 } })));
    await screen.findByTestId("chat-turn-1-done");

    fireEvent.change(screen.getByTestId("chat-route"), { target: { value: "openai-fast" } });
    await send("second, via openai-fast");
    await waitFor(() => expect(fake.methods().filter((m) => m === "ask")).toHaveLength(2));

    expect(asks).toEqual([
      { route: "anthropic", prompt: "first, via anthropic", subscriptionId: "s-1" },
      { route: "openai-fast", prompt: "second, via openai-fast", subscriptionId: "s-1" },
    ]);

    const turns = screen.getAllByTestId(/^chat-turn-\d+$/);
    expect(turns).toHaveLength(2);
    expect(turns[0]!.querySelector('[data-testid$="-route"]')?.textContent).toBe("anthropic");
    expect(turns[1]!.querySelector('[data-testid$="-route"]')?.textContent).toBe("openai-fast");

    // The subscription is reused, never re-asked for a second turn.
    expect(fake.methods().filter((m) => m === "subscribe")).toHaveLength(1);
  });

  it("disables Send while a turn is streaming, so only one askId is ever in flight", async () => {
    const fake = installFakeDaemon({ responses: {
      "routes.list": TWO_ROUTES, subscribe: { subscriptionId: "s-1", from: 1 }, ask: { askId: "a-1" },
    } });
    render(<ChatPage />);
    await waitFor(() => expect(screen.getByTestId("chat-route")).toBeTruthy());
    await send("hello");
    await waitFor(() => expect((screen.getByTestId("chat-send") as HTMLButtonElement).disabled).toBe(true));

    fireEvent.change(screen.getByTestId("chat-input"), { target: { value: "a second message" } });
    fireEvent.click(screen.getByTestId("chat-send"));
    expect(fake.methods().filter((m) => m === "ask")).toHaveLength(1);
  });

  it("shows the structured error on a failed turn instead of a blank reply", async () => {
    const fake = installFakeDaemon({ responses: {
      "routes.list": TWO_ROUTES, subscribe: { subscriptionId: "s-1", from: 1 }, ask: { askId: "a-1" },
    } });
    render(<ChatPage />);
    await waitFor(() => expect(screen.getByTestId("chat-route")).toBeTruthy());
    await send("hello");
    await waitFor(() => expect(fake.methods()).toContain("ask"));
    const turn = await screen.findByTestId(/chat-turn-\d+$/);
    const turnId = turn.getAttribute("data-testid")!.replace("chat-turn-", "");

    act(() => fake.emit(chatEvent("ask.done", {
      askId: "a-1", usage: {}, cost: { state: "unknown", reason: "the call failed" },
      error: { code: -32000, message: "the provider refused the call", data: { what: "x", means: "y", todo: "z", fatal: false } },
    })));

    expect(await screen.findByTestId(`chat-turn-${turnId}-error`)).toBeTruthy();
    expect(screen.getByTestId(`chat-turn-${turnId}-error`).textContent).toContain("the provider refused the call");
    expect(screen.queryByTestId(`chat-turn-${turnId}-done`)).toBeNull();
  });

  it("names no history field — each turn sends only its own prompt", async () => {
    const asks: unknown[] = [];
    const fake = installFakeDaemon({ responses: {
      "routes.list": TWO_ROUTES, subscribe: { subscriptionId: "s-1", from: 1 },
      ask: (params: unknown) => { asks.push(params); return { askId: `a-${asks.length}` }; },
    } });
    render(<ChatPage />);
    await waitFor(() => expect(screen.getByTestId("chat-route")).toBeTruthy());

    await send("first");
    await waitFor(() => expect(fake.methods().filter((m) => m === "ask")).toHaveLength(1));
    act(() => fake.emit(chatEvent("ask.done", { askId: "a-1", usage: {}, cost: { state: "known", usd: 0 } })));
    await screen.findByTestId("chat-turn-1-done");

    await send("second — no mention of the first");
    await waitFor(() => expect(fake.methods().filter((m) => m === "ask")).toHaveLength(2));
    expect(asks[1]).toEqual({ route: "anthropic", prompt: "second — no mention of the first", subscriptionId: "s-1" });
  });

  it("shows an empty state naming no history is sent, and no routes as its own state", async () => {
    installFakeDaemon({ responses: { "routes.list": TWO_ROUTES } });
    render(<ChatPage />);
    expect(await screen.findByTestId("chat-empty")).toBeTruthy();

    cleanup();
    resetFakeDaemon();
    installFakeDaemon({ responses: { "routes.list": routesList([]) } });
    render(<ChatPage />);
    expect(await screen.findByTestId("no-routes-to-chat")).toBeTruthy();
  });
});
