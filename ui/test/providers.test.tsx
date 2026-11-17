import { afterEach, describe, expect, it } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { ProvidersPage } from "../src/features/test-bench/pages/ProvidersPage";
import { installFakeDaemon, resetFakeDaemon, routesList, routeView } from "./helpers";

afterEach(() => {
  cleanup();
  resetFakeDaemon();
});

describe("the Providers page", () => {
  it("edits the route context window used by step compaction", async () => {
    const fake = installFakeDaemon({ responses: {
      "routes.list": routesList([routeView({ contextWindow: 16384 })], true),
      "routes.set": { route: routeView({ contextWindow: 8192 }) },
    } });
    render(<ProvidersPage />);
    await screen.findByTestId("route-anthropic");
    expect(screen.getByTestId("route-anthropic").textContent).toContain("context window: 16384 tokens");
    fireEvent.click(screen.getByText("Edit"));
    expect((screen.getByTestId("field-context-window") as HTMLInputElement).value).toBe("16384");
    fireEvent.change(screen.getByTestId("field-context-window"), { target: { value: "8192" } });
    fireEvent.click(screen.getByTestId("save-route"));
    await waitFor(() => expect(fake.methods()).toContain("routes.set"));
    expect((fake.paramsOf("routes.set") as { contextWindow: number }).contextWindow).toBe(8192);
  });
  it("lists routes with the key hint and never a key", async () => {
    installFakeDaemon({ responses: { "routes.list": routesList() } });
    render(<ProvidersPage />);

    await waitFor(() => {
      expect(screen.getByTestId("route-anthropic")).toBeTruthy();
    });
    expect(screen.getByTestId("key-anthropic").textContent).toContain("…wxyz");
    // The whole document must not contain anything key-shaped, because the
    // daemon never sends one and the page must not invent a place for one.
    expect(document.body.textContent ?? "").not.toMatch(/sk-[A-Za-z0-9]/);
  });

  // **The test that matters most on this page.**
  it("sends a typed key once and never reads one back", async () => {
    const fake = installFakeDaemon({
      responses: {
        "routes.list": routesList([]),
        "routes.set": { route: routeView({ id: "new-route" }) },
      },
    });
    render(<ProvidersPage />);

    await waitFor(() => expect(screen.getByTestId("no-routes")).toBeTruthy());
    fireEvent.click(screen.getByText("Add a route"));

    fireEvent.change(screen.getByTestId("field-id"), { target: { value: "new-route" } });
    fireEvent.change(screen.getByTestId("field-model"), { target: { value: "claude-sonnet-5" } });
    fireEvent.change(screen.getByTestId("field-api-key"), {
      target: { value: "sk-ant-TYPED-BY-THE-USER" },
    });
    fireEvent.click(screen.getByTestId("save-route"));

    await waitFor(() => {
      expect(fake.methods()).toContain("routes.set");
    });

    const params = fake.paramsOf("routes.set") as Record<string, unknown>;
    expect(params.apiKey).toBe("sk-ant-TYPED-BY-THE-USER");

    // The key was sent EXACTLY ONCE, and no call asked for one back.
    const keySends = fake.calls.filter((c) =>
      JSON.stringify(c.params ?? {}).includes("sk-ant-TYPED"),
    );
    expect(keySends).toHaveLength(1);

    // And it is not in the DOM afterwards — the form is gone and the list came
    // back from the daemon, which never echoes a key.
    await waitFor(() => {
      expect(screen.queryByTestId("route-form")).toBeNull();
    });
    expect(document.body.textContent ?? "").not.toContain("sk-ant-TYPED");
  });

  // S18 O8: the page can express the decision, and never invents one.
  it("sends the unmetered declaration only when the operator makes it", async () => {
    const fake = installFakeDaemon({
      responses: {
        "routes.list": routesList([]),
        "routes.set": { route: routeView({ id: "free-llama" }) },
      },
    });
    render(<ProvidersPage />);
    await waitFor(() => expect(screen.getByTestId("no-routes")).toBeTruthy());

    // Untouched: no price typed, no declaration made — the page sends
    // NEITHER. The refusal that follows is the daemon's (O8), and the page
    // shows it. Inventing `noPrice: false` here would be the page deciding.
    fireEvent.click(screen.getByText("Add a route"));
    fireEvent.change(screen.getByTestId("field-id"), { target: { value: "free-llama" } });
    fireEvent.change(screen.getByTestId("field-model"), { target: { value: "llama3.3" } });
    fireEvent.click(screen.getByTestId("save-route"));
    await waitFor(() => expect(fake.methods()).toContain("routes.set"));
    expect((fake.paramsOf("routes.set") as Record<string, unknown>).noPrice).toBeUndefined();

    // Declared: the checkbox is the decision, and it crosses the socket.
    fireEvent.click(screen.getByText("Add a route"));
    fireEvent.change(screen.getByTestId("field-id"), { target: { value: "free-llama" } });
    fireEvent.change(screen.getByTestId("field-model"), { target: { value: "llama3.3" } });
    fireEvent.click(screen.getByTestId("field-no-price"));
    fireEvent.click(screen.getByTestId("save-route"));
    await waitFor(() => {
      expect((fake.paramsOf("routes.set") as Record<string, unknown>).noPrice).toBe(true);
    });
  });

  it("writes nothing to browser storage", async () => {
    const fake = installFakeDaemon({
      responses: { "routes.list": routesList([]), "routes.set": { route: routeView() } },
    });
    localStorage.clear();
    sessionStorage.clear();

    render(<ProvidersPage />);
    await waitFor(() => expect(screen.getByTestId("no-routes")).toBeTruthy());
    fireEvent.click(screen.getByText("Add a route"));
    fireEvent.change(screen.getByTestId("field-id"), { target: { value: "r" } });
    fireEvent.change(screen.getByTestId("field-model"), { target: { value: "m" } });
    fireEvent.change(screen.getByTestId("field-api-key"), { target: { value: "sk-ant-SECRET" } });
    fireEvent.click(screen.getByTestId("save-route"));

    await waitFor(() => expect(fake.methods()).toContain("routes.set"));

    // **A credential in browser storage would survive the window, outlive the
    // daemon, and be readable by anything that can read that profile.**
    expect(localStorage.length).toBe(0);
    expect(sessionStorage.length).toBe(0);
  });

  it("leaves the key field empty when editing, and says why", async () => {
    installFakeDaemon({ responses: { "routes.list": routesList() } });
    render(<ProvidersPage />);

    await waitFor(() => expect(screen.getByTestId("route-anthropic")).toBeTruthy());
    fireEvent.click(screen.getByText("Edit"));

    const field = screen.getByTestId("field-api-key") as HTMLInputElement;
    expect(field.value).toBe("");
    // Empty means KEEP, and the hint has to say so or a user will assume the
    // key was lost and re-type it — which is the one thing this design is
    // arranged to avoid needing.
    expect(screen.getByText(/Leave this empty to keep it/)).toBeTruthy();
    expect(field.placeholder).toBe("unchanged");
  });

  it("sends no apiKey at all when the field is left empty on an edit", async () => {
    const fake = installFakeDaemon({
      responses: { "routes.list": routesList(), "routes.set": { route: routeView() } },
    });
    render(<ProvidersPage />);

    await waitFor(() => expect(screen.getByTestId("route-anthropic")).toBeTruthy());
    fireEvent.click(screen.getByText("Edit"));
    fireEvent.change(screen.getByTestId("field-model"), { target: { value: "a-new-model" } });
    fireEvent.click(screen.getByTestId("save-route"));

    await waitFor(() => expect(fake.methods()).toContain("routes.set"));
    const params = fake.paramsOf("routes.set") as Record<string, unknown>;
    expect(params.model).toBe("a-new-model");
    expect(params.apiKey).toBeUndefined();
  });

  it("offers clearing the key as a separate explicit action", async () => {
    const fake = installFakeDaemon({
      responses: { "routes.list": routesList(), "routes.set": { route: routeView() } },
    });
    render(<ProvidersPage />);

    await waitFor(() => expect(screen.getByTestId("route-anthropic")).toBeTruthy());
    fireEvent.click(screen.getByText("Edit"));
    fireEvent.click(screen.getByTestId("field-clear-key"));
    fireEvent.click(screen.getByTestId("save-route"));

    await waitFor(() => expect(fake.methods()).toContain("routes.set"));
    const params = fake.paramsOf("routes.set") as Record<string, unknown>;
    // `clearKey`, not an empty `apiKey` — which means keep.
    expect(params.clearKey).toBe(true);
  });

  // **The page says what the daemon says, in BOTH directions.**
  //
  // S3 shipped `persisted: false` honestly. S4 made routes persist and the page
  // kept claiming they did not — the same lie the other way round, and the one
  // the user actually hit: "it still says nothing is saved, and it indeed does
  // not get persisted".
  it("warns that nothing is persisted when the daemon says so", async () => {
    installFakeDaemon({ responses: { "routes.list": routesList([routeView()], false) } });
    render(<ProvidersPage />);

    await waitFor(() => {
      expect(screen.getByTestId("not-persisted")).toBeTruthy();
    });
    expect(screen.getByTestId("not-persisted").textContent).toMatch(/gone when it/i);
    // And it says what to look at, because with S4 attached this state means
    // the store failed to open rather than "the feature is not built yet".
    expect(screen.getByTestId("not-persisted").textContent).toMatch(/Store tab/i);
    expect(screen.queryByTestId("persisted")).toBeNull();
  });

  it("says routes ARE saved, and that the key is not in the database", async () => {
    installFakeDaemon({ responses: { "routes.list": routesList([routeView()], true) } });
    render(<ProvidersPage />);

    await waitFor(() => {
      expect(screen.getByTestId("persisted")).toBeTruthy();
    });
    const text = screen.getByTestId("persisted").textContent ?? "";
    // The claim, and the limit of the claim. "Saved" without saying where the
    // credential went would leave somebody assuming it is in the database —
    // which is exactly what C17 forbids and what a backup would then copy.
    expect(text).toMatch(/survives a restart/i);
    expect(text).toMatch(/0600/);
    expect(text).toMatch(/never into the database/i);
    expect(screen.queryByTestId("not-persisted")).toBeNull();
  });

  it("offers providers as a choice rather than a free-text field", async () => {
    installFakeDaemon({ responses: { "routes.list": routesList([]) } });
    render(<ProvidersPage />);

    await waitFor(() => expect(screen.getByTestId("no-routes")).toBeTruthy());
    fireEvent.click(screen.getByText("Add a route"));

    const select = screen.getByTestId("field-provider") as HTMLSelectElement;
    expect(select.tagName).toBe("SELECT");
    expect([...select.options].map((o) => o.value)).toEqual(["anthropic", "openai"]);
  });

  it("labels every price field with its unit", async () => {
    installFakeDaemon({ responses: { "routes.list": routesList([]) } });
    render(<ProvidersPage />);

    await waitFor(() => expect(screen.getByTestId("no-routes")).toBeTruthy());
    fireEvent.click(screen.getByText("Add a route"));

    // A factor-of-a-million error looks plausible in both directions, so the
    // unit is in the legend AND every label.
    expect(screen.getByText(/USD per million tokens/)).toBeTruthy();
    for (const label of ["Input $/Mtok", "Output $/Mtok"]) {
      expect(screen.getByText(label)).toBeTruthy();
    }
  });

  it("marks an unpriced route so a cost is not a surprise", async () => {
    installFakeDaemon({
      responses: { "routes.list": routesList([routeView({ priced: false, prices: undefined })]) },
    });
    render(<ProvidersPage />);

    await waitFor(() => {
      expect(screen.getByTestId("unpriced-anthropic")).toBeTruthy();
    });
    expect(screen.getByTestId("unpriced-anthropic").textContent).toMatch(/not reported/);
  });

  it("renders a refusal in three parts", async () => {
    installFakeDaemon({
      responses: {
        "routes.list": routesList([]),
        "routes.set": () => {
          throw JSON.stringify({
            code: -31103,
            message: "invalid route",
            data: {
              what: 'route "x" names provider "gemini", which has no adapter',
              means: "a route needs a provider that has an adapter",
              todo: "use one of the registered providers",
              fatal: false,
            },
          });
        },
      },
    });
    render(<ProvidersPage />);

    await waitFor(() => expect(screen.getByTestId("no-routes")).toBeTruthy());
    fireEvent.click(screen.getByText("Add a route"));
    fireEvent.change(screen.getByTestId("field-id"), { target: { value: "x" } });
    fireEvent.change(screen.getByTestId("field-model"), { target: { value: "m" } });
    fireEvent.click(screen.getByTestId("save-route"));

    await waitFor(() => expect(screen.getByTestId("error-parts")).toBeTruthy());
    const text = screen.getByTestId("error").textContent ?? "";
    expect(text).toContain("no adapter");
    expect(text).toContain("use one of the registered providers");
    expect(screen.getByTestId("error-code").textContent).toContain("-31103");
  });
});

describe("the reasoning effort", () => {
  // The field a real provider 400 asked for: OpenAI's reasoning models refuse
  // function tools unless reasoning is off, and every Aeon step offers tools.
  // So this is the setting that decides whether the Step tab works at all.

  it("warns that a step on a chat-endpoint route will lower its reasoning", async () => {
    installFakeDaemon({ responses: { "routes.list": routesList([]) } });
    render(<ProvidersPage />);

    fireEvent.click(await screen.findByText("Add a route"));
    fireEvent.change(screen.getByTestId("field-provider"), { target: { value: "openai" } });
    fireEvent.change(screen.getByTestId("field-id"), { target: { value: "openai" } });
    fireEvent.change(screen.getByTestId("field-model"), { target: { value: "gpt-6-luna" } });

    const warning = await screen.findByTestId("reasoning-effort-warning");
    // It names both halves — Ask keeps the setting, a step lowers itself — and
    // the way to have both. That asymmetry is the confusing part.
    expect(warning.textContent).toMatch(/Ask keeps this setting/i);
    expect(warning.textContent).toMatch(/lowers itself/i);
    expect(warning.textContent).toContain("function tools");
    expect(warning.textContent).toContain("/v1/responses");
  });

  it("stops warning once the effort is none", async () => {
    installFakeDaemon({ responses: { "routes.list": routesList([]) } });
    render(<ProvidersPage />);

    fireEvent.click(await screen.findByText("Add a route"));
    fireEvent.change(screen.getByTestId("field-provider"), { target: { value: "openai" } });
    fireEvent.change(screen.getByTestId("field-id"), { target: { value: "openai" } });
    fireEvent.change(screen.getByTestId("field-model"), { target: { value: "gpt-6-luna" } });
    await screen.findByTestId("reasoning-effort-warning");

    fireEvent.change(screen.getByTestId("field-reasoning-effort"), { target: { value: "none" } });
    await waitFor(() => expect(screen.queryByTestId("reasoning-effort-warning")).toBeNull());
  });

  it("sends the chosen effort, and never sends one for Anthropic", async () => {
    const daemon = installFakeDaemon({
      responses: {
        "routes.list": routesList([]),
        "routes.set": (p: unknown) => ({ route: { ...routeView(), ...(p as object) } }),
      },
    });
    render(<ProvidersPage />);

    fireEvent.click(await screen.findByText("Add a route"));
    fireEvent.change(screen.getByTestId("field-provider"), { target: { value: "openai" } });
    fireEvent.change(screen.getByTestId("field-id"), { target: { value: "openai" } });
    fireEvent.change(screen.getByTestId("field-model"), { target: { value: "gpt-6-luna" } });
    fireEvent.change(screen.getByTestId("field-reasoning-effort"), { target: { value: "none" } });
    fireEvent.click(screen.getByTestId("save-route"));

    await waitFor(() => expect(daemon.methods()).toContain("routes.set"));
    const params = daemon.paramsOf("routes.set") as Record<string, unknown>;
    expect(params.reasoningEffort).toBe("none");
    // The warning is provider-scoped: Anthropic has no such parameter, and a
    // UI nagging about it there would be teaching a rule that does not exist.
    expect(screen.queryByTestId("reasoning-effort-warning")).toBeNull();
  });

  it("does not drop a stored effort when editing something else", async () => {
    // Empty means KEEP, like the key — a form that submitted "" for a field it
    // never showed would silently unset the one setting a step depends on.
    const daemon = installFakeDaemon({
      responses: {
        "routes.list": routesList([
          routeView({ id: "openai", provider: "openai", model: "gpt-6-luna", reasoningEffort: "none" }),
        ]),
        "routes.set": (p: unknown) => ({ route: { ...routeView(), ...(p as object) } }),
      },
    });
    render(<ProvidersPage />);

    fireEvent.click(await screen.findByText("Edit"));
    await waitFor(() =>
      expect((screen.getByTestId("field-reasoning-effort") as HTMLSelectElement).value).toBe("none"),
    );

    fireEvent.change(screen.getByTestId("field-max-tokens"), { target: { value: "8192" } });
    fireEvent.click(screen.getByTestId("save-route"));

    await waitFor(() => expect(daemon.methods()).toContain("routes.set"));
    const params = daemon.paramsOf("routes.set") as Record<string, unknown>;
    expect(params.reasoningEffort).toBe("none");
    expect(params.clearReasoningEffort).toBeUndefined();
  });
});

describe("the OpenAI endpoint", () => {
  // `/v1/chat/completions` refuses function tools while reasoning is on;
  // `/v1/responses` accepts both. So the endpoint decides whether a step on
  // this route can reason at all.

  it("is offered for OpenAI and sent when chosen", async () => {
    const daemon = installFakeDaemon({
      responses: {
        "routes.list": routesList([]),
        "routes.set": (p: unknown) => ({ route: { ...routeView(), ...(p as object) } }),
      },
    });
    render(<ProvidersPage />);

    fireEvent.click(await screen.findByText("Add a route"));
    fireEvent.change(screen.getByTestId("field-provider"), { target: { value: "openai" } });
    fireEvent.change(screen.getByTestId("field-id"), { target: { value: "openai" } });
    fireEvent.change(screen.getByTestId("field-model"), { target: { value: "gpt-6.1-sol" } });
    fireEvent.change(screen.getByTestId("field-openai-endpoint"), {
      target: { value: "responses" },
    });
    fireEvent.change(screen.getByTestId("field-reasoning-effort"), { target: { value: "medium" } });

    // On `responses` the warning does NOT apply: tools and reasoning coexist,
    // and warning anyway would teach a rule that is not true of this route.
    await waitFor(() => expect(screen.queryByTestId("reasoning-effort-warning")).toBeNull());

    fireEvent.click(screen.getByTestId("save-route"));
    await waitFor(() => expect(daemon.methods()).toContain("routes.set"));

    const params = daemon.paramsOf("routes.set") as Record<string, unknown>;
    expect(params.openaiEndpoint).toBe("responses");
    expect(params.reasoningEffort).toBe("medium");
  });

  it("is not offered for a provider that has no such choice", async () => {
    installFakeDaemon({ responses: { "routes.list": routesList([]) } });
    render(<ProvidersPage />);

    fireEvent.click(await screen.findByText("Add a route"));
    fireEvent.change(screen.getByTestId("field-provider"), { target: { value: "anthropic" } });

    await waitFor(() => expect(screen.queryByTestId("field-openai-endpoint")).toBeNull());
  });

  it("defaults to chat, so an existing route is unchanged", async () => {
    const daemon = installFakeDaemon({
      responses: {
        "routes.list": routesList([
          routeView({ id: "openai", provider: "openai", model: "gpt-4o" }),
        ]),
        "routes.set": (p: unknown) => ({ route: { ...routeView(), ...(p as object) } }),
      },
    });
    render(<ProvidersPage />);

    fireEvent.click(await screen.findByText("Edit"));
    await waitFor(() =>
      expect((screen.getByTestId("field-openai-endpoint") as HTMLSelectElement).value).toBe(""),
    );

    fireEvent.click(screen.getByTestId("save-route"));
    await waitFor(() => expect(daemon.methods()).toContain("routes.set"));
    // Nothing is sent, so the daemon keeps whatever it had.
    expect((daemon.paramsOf("routes.set") as Record<string, unknown>).openaiEndpoint).toBeUndefined();
  });
});
