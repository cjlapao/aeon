import { StrictMode } from "react";
import { afterEach, describe, expect, it } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { TestBench as App } from "../src/features/test-bench/TestBench";
import { installFakeDaemon, resetFakeDaemon, routesList } from "./helpers";

afterEach(() => {
  cleanup();
  resetFakeDaemon();
});

describe("the app shell", () => {
  it("uses the shared daemon session to inspect loop files in the test bench", async () => {
    const fake = installFakeDaemon({ responses: {
      "loop.list": { all: true, loops: [{ id: "l1", slug: "survey", title: "Survey", componentRoot: "/tmp/qa", status: "done" }] },
      "loop.files": { path: ".", files: [{ name: "discovery.md", path: "discovery.md", kind: "file" }] },
      "loop.read": { documents: [] },
      "loop.file.read": { path: "discovery.md", content: "# Discovery\n", bytes: 12 },
    } });
    const view = render(<App />);
    await waitFor(() => expect(screen.getByTestId("connection").getAttribute("data-state")).toBe("connected"));
    const before = fake.countOf("connect");
    view.rerender(<App activePage="files" />);
    expect(await screen.findByTestId("bench-files")).toBeTruthy();
    fireEvent.click(await screen.findByRole("button", { name: "◇ discovery.md" }));
    expect(await screen.findByRole("heading", { name: "Discovery" })).toBeTruthy();
    expect(fake.countOf("connect")).toBe(before);
    expect(fake.paramsOf("loop.file.read")).toEqual({ id: "l1", path: "discovery.md" });
  });
  // THE test. The fake answers a protocol version this code could not have
  // guessed: if the window shows 1, it is displaying a build-time constant and
  // the negotiation it claims to have done never happened.
  it("reports the negotiated protocol version, from the wire", async () => {
    installFakeDaemon({
      negotiated: { daemonVersion: "9.9.9-fake", protocolVersion: 7, endpoint: "/tmp/fake.sock" },
    });
    render(<App />);

    await waitFor(() => {
      expect(screen.getByTestId("connection").getAttribute("data-state")).toBe("connected");
    });
    const text = screen.getByTestId("protocol-version").textContent ?? "";
    expect(text).toContain("7");
    expect(text).toContain("negotiated");
    expect(screen.getByTestId("connection").textContent).toContain("9.9.9-fake");
  });

  // The negative one, which matters more.
  it("shows NO protocol number while disconnected", async () => {
    installFakeDaemon({ negotiated: new Error("connect: No such file or directory") });
    render(<App />);

    await waitFor(() => {
      expect(screen.getByTestId("connection").getAttribute("data-state")).toBe("failed");
    });
    expect(screen.queryByTestId("protocol-version")).toBeNull();
    const pill = screen.getByTestId("connection").textContent ?? "";
    expect(pill).not.toMatch(/protocol/i);
  });

  it("names the socket path it tried, connected or not", async () => {
    installFakeDaemon({
      negotiated: new Error("connect: No such file or directory"),
      endpoint: "/run/user/1000/aeon/rpc.sock",
    });
    render(<App />);

    await waitFor(() => {
      expect(screen.getByTestId("status-rows").textContent).toContain(
        "/run/user/1000/aeon/rpc.sock",
      );
    });
  });

  it("renders a connecting state before it knows", async () => {
    installFakeDaemon({});
    render(<App />);
    // Immediately, before the handshake resolves.
    expect(screen.getByTestId("connection").getAttribute("data-state")).toBe("connecting");
    await waitFor(() => {
      expect(screen.getByTestId("connection").getAttribute("data-state")).toBe("connected");
    });
  });

  // **The daemon going away must make the UI go honest.** Without this, every
  // page keeps showing its last good state and looks like it works.
  it("goes to failed when the connection is lost", async () => {
    const fake = installFakeDaemon({});
    render(<App />);
    await waitFor(() => {
      expect(screen.getByTestId("connection").getAttribute("data-state")).toBe("connected");
    });

    act(() => {
      fake.close("the daemon closed the connection");
    });
    await waitFor(() => {
      expect(screen.getByTestId("connection").getAttribute("data-state")).toBe("failed");
    });
    expect(screen.getByTestId("connection").textContent).toContain("closed");
  });

  // It says what it is, in the UI, not only in a comment.
  it("labels itself a test bench and points at S9", async () => {
    installFakeDaemon({});
    render(<App />);
    expect(screen.getByTestId("test-bench-badge").textContent).toContain("test bench");
    const note = screen.getByTestId("test-bench-note").textContent ?? "";
    expect(note).toContain("S9");
    expect(note).toMatch(/where it will be consumed/);
  });

  it("leaves navigation to the application menu", async () => {
    installFakeDaemon({});
    render(<App />);
    await waitFor(() => expect(screen.getByTestId("connection").getAttribute("data-state")).toBe("connected"));
    expect(screen.queryByRole("tablist")).toBeNull();
    expect(screen.queryByTestId("tabs")).toBeNull();
  });

  it("switches to Store and loads the store's facts", async () => {
    installFakeDaemon({
      responses: {
        "routes.list": routesList(),
        "store.info": {
          path: "/home/p/.local/state/aeon/state.db",
          bytes: 196608,
          journalMode: "wal",
          foreignKeys: true,
          applied: [{ name: "0001_core_tables", status: "applied" }],
          pending: [],
          tables: ["loop", "step"],
          vecWriterVersion: "v0.1.6",
          vecReaderVersion: "v0.1.6",
          readPoolSize: 4,
          writerPoolSize: 1,
          busyTimeoutMs: 5000,
        },
        "store.awaiting": { sql: "SELECT 1 FROM question", loops: [] },
        "loop.list": { loops: [], all: true },
      },
    });
    const view = render(<App />);
    await waitFor(() => {
      expect(screen.getByTestId("connection").getAttribute("data-state")).toBe("connected");
    });

    view.rerender(<App activePage="store" />);
    await waitFor(() => expect(screen.getByTestId("store-facts")).toBeTruthy());
    // The path is the first thing the tab answers, because every debugging
    // session starts by finding the file.
    expect(screen.getByText("/home/p/.local/state/aeon/state.db")).toBeTruthy();
  });

  it("switches to Providers and loads routes", async () => {
    installFakeDaemon({ responses: { "routes.list": routesList() } });
    const view = render(<App />);
    await waitFor(() => {
      expect(screen.getByTestId("connection").getAttribute("data-state")).toBe("connected");
    });

    view.rerender(<App activePage="providers" />);
    await waitFor(() => {
      expect(screen.getByTestId("route-anthropic")).toBeTruthy();
    });
  });

  it("refuses to show a configuration surface while disconnected", async () => {
    installFakeDaemon({ negotiated: new Error("nope") });
    const view = render(<App />);
    await waitFor(() => {
      expect(screen.getByTestId("connection").getAttribute("data-state")).toBe("failed");
    });

    view.rerender(<App activePage="providers" />);
    await waitFor(() => {
      expect(screen.getByTestId("tab-needs-connection")).toBeTruthy();
    });
    // Nothing was configured against a daemon that is not there.
    expect(screen.queryByTestId("route-form")).toBeNull();
  });

  // S34 T5 — TB12: both new tabs render the shared disconnected state
  // rather than a blank pane or a crash, same as every other tab.
  it.each(["agents", "mcp"] as const)("shows the shared disconnected state on the %s tab", async (page) => {
    installFakeDaemon({ negotiated: new Error("nope") });
    const view = render(<App />);
    await waitFor(() => {
      expect(screen.getByTestId("connection").getAttribute("data-state")).toBe("failed");
    });
    view.rerender(<App activePage={page} />);
    await waitFor(() => {
      expect(screen.getByTestId("tab-needs-connection")).toBeTruthy();
    });
    expect(screen.queryByTestId(`${page}-page`)).toBeNull();
  });

  it("has no loop list, chat or settings surface — S9 builds those", async () => {
    installFakeDaemon({ responses: { "routes.list": routesList() } });
    render(<App />);
    await waitFor(() => {
      expect(screen.getByTestId("connection").getAttribute("data-state")).toBe("connected");
    });
    // Asserted on SURFACES rather than words: prose describing the product is
    // not a loop list.
    for (const role of ["list", "table", "navigation", "menu"] as const) {
      expect(screen.queryAllByRole(role)).toHaveLength(0);
    }
  });
});

describe("when events are unavailable", () => {
  // The most confusing half-working state available: every button works and
  // every stream is silent. Saying so is what makes the cause findable.
  it("says so rather than looking like it works", async () => {
    installFakeDaemon({
      subscribeFails: "event.listen not allowed. Permission core:event:allow-listen is missing",
    });
    render(<App />);

    await waitFor(() => expect(screen.getByTestId("events-unavailable")).toBeTruthy());
    const text = screen.getByTestId("events-unavailable").textContent ?? "";
    expect(text).toContain("could not install its event listener");
    // **It explains what that means for the reader** — Tauri's own message
    // says what the runtime refused and nothing about the consequence.
    expect(text).toMatch(/every call still works and every stream is silent/);
    // And it names where to look, because the symptom points nowhere.
    expect(text).toContain("capabilities");
    // And it does NOT pretend to be disconnected: calls really do work.
    await waitFor(() => {
      expect(screen.getByTestId("connection").getAttribute("data-state")).toBe("connected");
    });
  });
});

describe("one session per window", () => {
  // The daemon log's two `client connected` lines in the same second, which is
  // what this asserts against: the mount effect asked for a socket, StrictMode's
  // simulated teardown ran, the remount asked for a second one, and the shell
  // retired whichever lost. `main.tsx` is what wraps App, so the test wraps it
  // the same way — a plain `render(<App />)` would prove nothing about this bug.
  it("TestConnectsOncePerWindow", async () => {
    const fake = installFakeDaemon({});
    render(
      <StrictMode>
        <App />
      </StrictMode>,
    );
    await waitFor(() => {
      expect(screen.getByTestId("connection").getAttribute("data-state")).toBe("connected");
    });

    // **The witness that this test can fail.** "Exactly one connect" is worth
    // nothing unless the double-invoke that caused the bug actually happened, and
    // a test may not take React's word for it. Every effect in the tree runs
    // twice when StrictMode is live, and the closed-event subscription is an
    // effect: two asks, one handshake. If StrictMode ever stops applying, this
    // assertion fails and the one below is exposed as vacuous rather than green.
    expect(fake.subscribes()).toEqual(["aeon://closed", "aeon://closed"]);
    expect(fake.countOf("connect")).toBe(1);
    // Resolving the path and then handshaking, once — no second round, and no
    // `endpoint_path` from a reconnect nobody asked for.
    expect(fake.commands()).toEqual(["endpoint_path", "connect"]);
    // The remount was not handed a session torn down from under it. This is the
    // half a "fixed" version gets wrong by papering over with a second connect.
    expect(fake.countOf("disconnect")).toBe(0);

    // And the socket still goes when the window genuinely ends, which is the
    // other half of the requirement: not connected-forever, just connected once.
    act(() => {
      window.dispatchEvent(new Event("pagehide"));
    });
    await waitFor(() => {
      expect(fake.countOf("disconnect")).toBe(1);
    });
  });
});

describe("the way back", () => {
  // Where the user's bug ends: the idle socket died, the pill read "not
  // connected", and the only recovery was restarting the app, because the
  // handshake was reachable only from a mount that had already happened.
  it("TestReconnectRestoresConnection", async () => {
    // The second handshake is held open by hand, so the middle state is caught
    // rather than raced past: it is the state a user stares at for the length of
    // a connect, and the one a naive implementation hides by removing the button.
    let answerSecondHandshake: (value: unknown) => void = () => {};
    const fake = installFakeDaemon({
      negotiated: (attempt: number) =>
        attempt === 1
          ? // The daemon's own words from the report, carried end to end.
            new Error("read from the daemon: Resource temporarily unavailable (os error 11)")
          : new Promise((resolve) => {
              answerSecondHandshake = resolve;
            }),
    });
    render(<App />);

    await waitFor(() => {
      expect(screen.getByTestId("connection").getAttribute("data-state")).toBe("failed");
    });
    expect(screen.getByTestId("connection").textContent).toContain("os error 11");
    // The way back is on the page with the state it leaves.
    const reconnect = screen.getByTestId("reconnect");
    expect(reconnect.textContent).toContain("Reconnect");
    expect((reconnect as HTMLButtonElement).disabled).toBe(false);

    fireEvent.click(reconnect);

    // Connecting is still its own state: the button stays visible, so the third
    // state has not collapsed into "connected", and disabled, so a second click
    // cannot race the handshake already under way.
    await waitFor(() => {
      expect(screen.getByTestId("connection").getAttribute("data-state")).toBe("connecting");
    });
    expect((screen.getByTestId("reconnect") as HTMLButtonElement).disabled).toBe(true);

    act(() => {
      answerSecondHandshake({
        daemonVersion: "9.9.9-fake",
        protocolVersion: 7,
        endpoint: "/run/user/1000/aeon/rpc.sock",
      });
    });
    await waitFor(() => {
      expect(screen.getByTestId("connection").getAttribute("data-state")).toBe("connected");
    });

    // A **fresh** handshake, not the first one's result kept on a shelf: two
    // `connect`s, and the second produced the protocol number from the wire.
    expect(fake.countOf("connect")).toBe(2);
    expect(screen.getByTestId("protocol-version").textContent).toContain("7");
    // The affordance leaves with the state it was offered for.
    expect(screen.queryByTestId("reconnect")).toBeNull();
  });
});
