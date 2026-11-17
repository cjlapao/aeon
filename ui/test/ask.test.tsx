import { afterEach, describe, expect, it } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { AskPage } from "../src/features/test-bench/pages/AskPage";
import { installFakeDaemon, resetFakeDaemon, routesList, routeView } from "./helpers";
import type { FakeDaemon } from "./helpers";

afterEach(() => {
  cleanup();
  resetFakeDaemon();
});

const askReady = {
  "routes.list": routesList(),
  subscribe: { subscriptionId: "s-1", from: 1 },
  ask: { askId: "a-1" },
};

function askEvent(kind: string, payload: unknown) {
  return { subscriptionId: "s-1", seq: 1, kind, at: "2026-10-15T00:00:00Z", payload };
}

async function submitAsk(prompt = "hello") {
  fireEvent.change(screen.getByTestId("ask-prompt"), { target: { value: prompt } });
  fireEvent.click(screen.getByTestId("ask-submit"));
}

describe("the Ask page", () => {
  // **The ordering bug that passes every test on a slow model.**
  it("subscribes BEFORE it calls ask", async () => {
    const fake = installFakeDaemon({ responses: askReady });
    render(<AskPage />);
    await waitFor(() => expect(screen.getByTestId("ask-route")).toBeTruthy());

    await submitAsk();
    await waitFor(() => expect(fake.methods()).toContain("ask"));

    const methods = fake.methods();
    const subscribeAt = methods.indexOf("subscribe");
    const askAt = methods.indexOf("ask");
    expect(subscribeAt).toBeGreaterThanOrEqual(0);
    expect(askAt).toBeGreaterThan(subscribeAt);
  });

  it("passes the subscription id to ask, so the reply streams", async () => {
    const fake = installFakeDaemon({ responses: askReady });
    render(<AskPage />);
    await waitFor(() => expect(screen.getByTestId("ask-route")).toBeTruthy());
    await submitAsk();

    await waitFor(() => expect(fake.methods()).toContain("ask"));
    const params = fake.paramsOf("ask") as Record<string, unknown>;
    expect(params.subscriptionId).toBe("s-1");
    expect(params.route).toBe("anthropic");
    expect(params.prompt).toBe("hello");
  });

  // The cursor is an affordance, not decoration: it is how a reader can tell a
  // reply that is still arriving from one that has stopped early.
  it("shows a cursor while streaming and drops it when done", async () => {
    const fake = installFakeDaemon({ responses: askReady });
    render(<AskPage />);
    await waitFor(() => expect(screen.getByTestId("ask-route")).toBeTruthy());
    await submitAsk();
    await waitFor(() => expect(fake.methods()).toContain("ask"));

    act(() => {
      fake.emit(askEvent("ask.delta", { askId: "a-1", kind: "text", text: "partial" }));
    });
    await waitFor(() => {
      expect(screen.getByTestId("ask-text").textContent).toContain("▌");
    });

    act(() => {
      fake.emit(
        askEvent("ask.done", {
          askId: "a-1",
          usage: { inputTokens: 1, outputTokens: 1 },
          cost: { state: "known", usd: 0.00001 },
        }),
      );
    });
    await waitFor(() => {
      expect(screen.getByTestId("ask-text").textContent).not.toContain("▌");
    });
  });

  it("renders deltas as they arrive, not in one block", async () => {
    const fake = installFakeDaemon({ responses: askReady });
    render(<AskPage />);
    await waitFor(() => expect(screen.getByTestId("ask-route")).toBeTruthy());
    await submitAsk();
    await waitFor(() => expect(fake.methods()).toContain("ask"));

    act(() => {
      fake.emit(askEvent("ask.delta", { askId: "a-1", kind: "text", text: "Hello" }));
    });
    await waitFor(() => {
      expect(screen.getByTestId("ask-text").textContent).toContain("Hello");
    });
    // The text grows rather than being replaced — which is what distinguishes
    // streaming from a buffered answer arriving in pieces.
    act(() => {
      fake.emit(askEvent("ask.delta", { askId: "a-1", kind: "text", text: ", world" }));
    });
    await waitFor(() => {
      expect(screen.getByTestId("ask-text").textContent).toContain("Hello, world");
    });
  });

  it("ignores deltas belonging to another ask", async () => {
    const fake = installFakeDaemon({ responses: askReady });
    render(<AskPage />);
    await waitFor(() => expect(screen.getByTestId("ask-route")).toBeTruthy());
    await submitAsk();
    await waitFor(() => expect(fake.methods()).toContain("ask"));

    act(() => {
      fake.emit(askEvent("ask.delta", { askId: "a-1", kind: "text", text: "mine" }));
      // Another client's ask, on the same subscription — which happens, because
      // a subscription is per-connection and not per-call.
      fake.emit(askEvent("ask.delta", { askId: "a-99", kind: "text", text: "SOMEBODY ELSE" }));
    });

    await waitFor(() => {
      expect(screen.getByTestId("ask-text").textContent).toContain("mine");
    });
    expect(screen.getByTestId("ask-text").textContent).not.toContain("SOMEBODY ELSE");
  });

  it("renders thinking separately from the answer", async () => {
    const fake = installFakeDaemon({ responses: askReady });
    render(<AskPage />);
    await waitFor(() => expect(screen.getByTestId("ask-route")).toBeTruthy());
    await submitAsk();
    await waitFor(() => expect(fake.methods()).toContain("ask"));

    act(() => {
      fake.emit(askEvent("ask.delta", { askId: "a-1", kind: "thinking", text: "let me think" }));
      fake.emit(askEvent("ask.delta", { askId: "a-1", kind: "text", text: "the answer" }));
    });

    await waitFor(() => expect(screen.getByTestId("ask-thinking")).toBeTruthy());
    // Reasoning is not the answer, and inlining it makes the answer hard to
    // find.
    expect(screen.getByTestId("ask-thinking").textContent).toContain("let me think");
    const answer = screen.getByTestId("ask-text").textContent ?? "";
    expect(answer).toContain("the answer");
    // The reasoning is NOT in the answer — which is the actual claim.
    expect(answer).not.toContain("let me think");
  });

  it("shows a tool call and says nothing runs it yet", async () => {
    const fake = installFakeDaemon({ responses: askReady });
    render(<AskPage />);
    await waitFor(() => expect(screen.getByTestId("ask-route")).toBeTruthy());
    await submitAsk();
    await waitFor(() => expect(fake.methods()).toContain("ask"));

    act(() => {
      fake.emit(
        askEvent("ask.delta", {
          askId: "a-1",
          kind: "tool_use",
          block: { kind: "tool_use", id: "tu_1", name: "get_weather", input: { location: "Lisbon" } },
        }),
      );
    });

    await waitFor(() => expect(screen.getByTestId("ask-tool-call")).toBeTruthy());
    const text = screen.getByTestId("ask-tool-call").textContent ?? "";
    expect(text).toContain("get_weather");
    expect(text).toContain("Lisbon");
    // Honest about what it does not do.
    expect(text).toMatch(/nothing runs it yet/);
  });

  // --- the three cost states, each rendered distinctly ----------------------

  async function runToDone(done: unknown) {
    const fake = installFakeDaemon({ responses: askReady });
    render(<AskPage />);
    await waitFor(() => expect(screen.getByTestId("ask-route")).toBeTruthy());
    await submitAsk();
    await waitFor(() => expect(fake.methods()).toContain("ask"));
    act(() => {
      fake.emit(askEvent("ask.done", done));
    });
    await waitFor(() => expect(screen.getByTestId("ask-accounting")).toBeTruthy());
  }

  it("renders a known cost as a figure", async () => {
    await runToDone({
      askId: "a-1",
      usage: { inputTokens: 12, outputTokens: 5 },
      cost: { state: "known", usd: 0.000111 },
      stopReason: "end_turn",
    });
    // S18 T10 re-pin: this figure used to render `$0.000111` — a known cost
    // whose first five characters are the literal D9 forbids, exactly the
    // skimming-eye lie Go's MoneyString deletes with scientific notation.
    // The TS renderer is the second renderer of ONE computation (D16), so it
    // takes the same form: $1.11e-04.
    expect(screen.getByTestId("cost-line").textContent).toContain("$1.11e-04");
    expect(screen.getByTestId("cost-line").textContent).not.toContain("$0.00");
    expect(screen.getByTestId("ask-accounting").textContent).toContain("in 12");
  });

  it("renders a partial cost as a floor, naming what is missing", async () => {
    await runToDone({
      askId: "a-1",
      usage: { inputTokens: 12 },
      cost: { state: "partial", usdAtLeast: 0.00003, missing: ["outputTokens"] },
    });
    const line = screen.getByTestId("cost-line").textContent ?? "";
    expect(line).toContain("at least");
    expect(screen.getByTestId("cost-detail").textContent).toContain("outputTokens");
  });

  // **The one that matters.**
  it("renders an unknown cost as not reported, never as zero", async () => {
    await runToDone({
      askId: "a-1",
      usage: {},
      cost: { state: "unknown", reason: "provider-omitted-usage" },
    });
    const accounting = screen.getByTestId("ask-accounting").textContent ?? "";
    expect(screen.getByTestId("cost-line").textContent).toContain("not reported");
    // A zero that means "we do not know" is the specific dishonesty the three
    // states exist to prevent.
    expect(accounting).not.toContain("$0.00");
    // And the reason is a sentence, not a code.
    expect(screen.getByTestId("cost-detail").textContent).toContain(
      "the provider reported no token counts",
    );
    expect(accounting).not.toContain("provider-omitted-usage");
    // Tokens too: nothing arrived, and it says so rather than showing zeros.
    expect(accounting).toContain("tokens not reported");
  });

  it("renders a failure on ask.done in three parts", async () => {
    await runToDone({
      askId: "a-1",
      usage: {},
      cost: { state: "unknown", reason: "stream-aborted-early" },
      error: {
        code: -31102,
        message: "the model provider failed",
        data: {
          what: "anthropic answered 404",
          means: "the provider refused the request",
          todo: "check the route's model name",
          fatal: false,
        },
      },
    });
    await waitFor(() => expect(screen.getByTestId("error-parts")).toBeTruthy());
    const text = screen.getByTestId("error").textContent ?? "";
    expect(text).toContain("anthropic answered 404");
    expect(text).toContain("check the route's model name");
    // The cost line is still shown: a failed call still cost something.
    expect(screen.getByTestId("cost-line").textContent).toContain("not reported");
  });

  it("warns before the call when a route is unpriced or has no key", async () => {
    installFakeDaemon({
      responses: {
        ...askReady,
        "routes.list": routesList([routeView({ priced: false, prices: undefined, hasKey: false })]),
      },
    });
    render(<AskPage />);
    await waitFor(() => expect(screen.getByTestId("unpriced-warning")).toBeTruthy());
    expect(screen.getByTestId("no-key-warning")).toBeTruthy();
    // Before, not after: explaining an unknown cost afterwards is worse than
    // warning that it is coming.
    expect(screen.queryByTestId("ask-accounting")).toBeNull();
  });

  it("says there is nothing to call when no route exists", async () => {
    installFakeDaemon({ responses: { "routes.list": routesList([]) } });
    render(<AskPage />);
    await waitFor(() => expect(screen.getByTestId("no-routes-to-call")).toBeTruthy());
  });

  it("surfaces a refusal from ask itself", async () => {
    installFakeDaemon({
      responses: {
        ...askReady,
        ask: () => {
          throw JSON.stringify({
            code: -31100,
            message: "unknown route",
            data: {
              what: 'route "gone" is not configured; the configured routes are anthropic',
              means: "a route names a provider, a model and its prices",
              todo: "use one of the configured route ids",
              fatal: false,
            },
          });
        },
      },
    });
    render(<AskPage />);
    await waitFor(() => expect(screen.getByTestId("ask-route")).toBeTruthy());
    await submitAsk();

    await waitFor(() => expect(screen.getByTestId("error-parts")).toBeTruthy());
    expect(screen.getByTestId("error").textContent).toContain("is not configured");
    // And the button is usable again rather than stuck in a loading state.
    await waitFor(() => {
      expect((screen.getByTestId("ask-submit") as HTMLButtonElement).disabled).toBe(false);
    });
  });
});

describe("when the window cannot receive events", () => {
  // # The bug this reproduces
  //
  // Tauri v2 gates `listen` behind its ACL; app commands are not gated. With
  // no capability granting `core:event:default`, **every call succeeded and
  // every stream was silent** — the page sat at "Streaming…" forever. The
  // listener's rejection was discarded by a `void (async () => …)()`, so there
  // was nothing on screen to explain it.
  it("refuses to start a stream, rather than hanging", async () => {
    const fake = installFakeDaemon({
      responses: askReady,
      subscribeFails: "event.listen not allowed. Permission core:event:allow-listen is missing",
    });
    render(<AskPage />);
    await waitFor(() => expect(screen.getByTestId("ask-route")).toBeTruthy());

    // The failure is on screen, with the remedy.
    await waitFor(() => expect(screen.getByTestId("error")).toBeTruthy());
    const shown = screen.getByTestId("error").textContent ?? "";
    expect(shown).toContain("could not install its event listener");
    expect(shown).toContain("capabilities");

    // And the button is disabled, so a call cannot be made that would cost
    // money and show nothing.
    const button = screen.getByTestId("ask-submit") as HTMLButtonElement;
    expect(button.disabled).toBe(true);

    fireEvent.change(screen.getByTestId("ask-prompt"), { target: { value: "hello" } });
    fireEvent.click(button);
    await new Promise((r) => setTimeout(r, 50));

    // **Nothing was called.** The daemon would otherwise do the work, bill for
    // it, and send the reply into a void.
    expect(fake.methods()).not.toContain("ask");
    expect(fake.methods()).not.toContain("subscribe");
  });

  it("does not claim to be streaming", async () => {
    installFakeDaemon({ responses: askReady, subscribeFails: "denied" });
    render(<AskPage />);
    await waitFor(() => expect(screen.getByTestId("error")).toBeTruthy());
    expect(screen.getByTestId("ask-submit").textContent).toContain("Ask");
    expect(screen.getByTestId("ask-submit").textContent).not.toContain("Streaming");
  });
});

describe("the ask page's subscription", () => {
  // The same defect the Step tab had: one subscription per ask, never
  // released. The daemon fans each event out to every matching subscription on
  // the connection, so the fourth ask streamed every delta four times — which
  // renders as the model repeating itself and sends somebody to the provider.
  //
  // Filtering by `askId` HID it: a delta for the current ask arrived N times
  // and all N passed the filter.

  // finishAsk emits the done event, which is what re-enables the button — a
  // second ask is impossible until the first one ends.
  function finishAsk(fake: FakeDaemon, askId: string) {
    act(() => {
      fake.emit(
        askEvent("ask.done", {
          askId,
          usage: { inputTokens: 1, outputTokens: 1 },
          cost: { state: "known", usd: 0.00001 },
        }),
      );
    });
  }

  it("subscribes once, however many asks are made", async () => {
    const fake = installFakeDaemon({ responses: askReady });
    render(<AskPage />);
    await waitFor(() => expect(screen.getByTestId("ask-route")).toBeTruthy());

    for (let i = 0; i < 3; i++) {
      await submitAsk();
      await waitFor(() => expect(fake.calls.filter((c) => c.method === "ask").length).toBe(i + 1));
      finishAsk(fake, "a-1");
      await waitFor(() =>
        expect((screen.getByTestId("ask-submit") as HTMLButtonElement).disabled).toBe(false),
      );
    }

    const subscribes = fake.calls.filter((c) => c.method === "subscribe").length;
    if (subscribes !== 1) {
      throw new Error(
        `the page subscribed ${subscribes} times for 3 asks. Each extra subscription means every ` +
          `delta is delivered one more time, so the reply appears ${subscribes} times`,
      );
    }
  });

  it("reuses the same subscription id", async () => {
    const fake = installFakeDaemon({ responses: askReady });
    render(<AskPage />);
    await waitFor(() => expect(screen.getByTestId("ask-route")).toBeTruthy());

    await submitAsk();
    await waitFor(() => expect(fake.methods()).toContain("ask"));
    const first = (fake.paramsOf("ask") as { subscriptionId: string }).subscriptionId;
    finishAsk(fake, "a-1");

    await waitFor(() =>
      expect((screen.getByTestId("ask-submit") as HTMLButtonElement).disabled).toBe(false),
    );
    await submitAsk();
    await waitFor(() => expect(fake.calls.filter((c) => c.method === "ask").length).toBe(2));

    expect((fake.paramsOf("ask") as { subscriptionId: string }).subscriptionId).toBe(first);
  });

  it("sends a per-call reasoning effort when one is chosen, and nothing otherwise", async () => {
    // The per-chat half of the dial: a chat offers no tools, so it can reason
    // freely — and asking somebody to keep a second route for that is asking
    // them to maintain a workaround.
    const fake = installFakeDaemon({ responses: askReady });
    render(<AskPage />);
    await waitFor(() => expect(screen.getByTestId("ask-route")).toBeTruthy());

    await submitAsk();
    await waitFor(() => expect(fake.methods()).toContain("ask"));
    expect((fake.paramsOf("ask") as Record<string, unknown>).reasoningEffort).toBeUndefined();
    finishAsk(fake, "a-1");

    await waitFor(() =>
      expect((screen.getByTestId("ask-submit") as HTMLButtonElement).disabled).toBe(false),
    );
    fireEvent.change(screen.getByTestId("ask-reasoning-effort"), { target: { value: "high" } });
    await submitAsk();
    await waitFor(() => expect(fake.calls.filter((c) => c.method === "ask").length).toBe(2));
    expect((fake.paramsOf("ask") as Record<string, unknown>).reasoningEffort).toBe("high");
  });
});
