import { afterEach, describe, expect, it } from "vitest";
import { act, cleanup, render, renderHook, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import type { ReactNode } from "react";
import { beginWait, PageWaitOwnerContext, useWaitsFor, waitsFor } from "../src/lib/pageWait";
import { useRpcQuery } from "../src/lib/useRpcQuery";
import { resetBridge, setInvoker } from "../src/lib/bridge";
import { LiveController, type LiveState } from "../src/lib/live";
import { lazyConfigRender } from "../src/features/config/lib/lazyConfigRender";
import { registerConfigPage } from "../src/features/config/lib/configRegistry";
import { PageLayout } from "../src/app/PageLayout";

// S72: the page-level wait, counted where the shell can see it.
//
// The complaint the walk recorded was not "slow" — it was "locked": a page
// that says *Loading* while the entire window is frozen, with nothing on the
// row you clicked saying which page is working. Two halves fix that, and the
// first is what this file asserts: a page reports what it is still waiting
// for, in a register the menu reads. The second half — the shell's `rpc_call`
// no longer running on the window's thread — is Rust, out of frame in jsdom,
// and recorded with its own measurement in the task Notes.

afterEach(() => {
  cleanup();
  resetBridge();
});

const current: LiveState = {
  state: "current",
  endpoint: "/tmp/aeon-test.sock",
  daemonVersion: "1.0",
  protocolVersion: 1,
  generation: 1,
};
// One controller for every probe: `useRpcQuery` re-registers its reader when
// the controller identity changes, so building one per render would refetch
// in a loop and the test would measure its own scaffolding.
const controller = new LiveController();

function deferred<T = unknown>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((r) => {
    resolve = r;
  });
  return { promise, resolve };
}

/** An RPC that answers only when the test says so; everything else is empty. */
function hangingMethod(method: string, gate: Promise<unknown>) {
  setInvoker(async (cmd, args) => {
    if (cmd === "rpc_call" && args?.method === method) return gate;
    return {};
  });
}

/** A page component that asks the daemon one thing, like any real page does. */
function Probe({ method }: { method: string }) {
  const query = useRpcQuery(method as never, {} as never, controller, current);
  return <div data-testid="probe">{query.loading ? "loading" : "done"}</div>;
}

function underOwner(owner: string, node: ReactNode) {
  return render(
    <PageWaitOwnerContext.Provider value={owner}>
      <MemoryRouter>{node}</MemoryRouter>
    </PageWaitOwnerContext.Provider>,
  );
}

describe("the page-wait registry", () => {
  it("counts tickets against the page that took them and releases on the first call", () => {
    expect(waitsFor("config:a")).toBe(0);
    const data = beginWait("config:a", "data");
    const chunk = beginWait("config:a", "chunk");
    expect(waitsFor("config:a")).toBe(2);
    data();
    data(); // idempotent: a release called twice must not underflow the row
    expect(waitsFor("config:a")).toBe(1);
    chunk();
    expect(waitsFor("config:a")).toBe(0);
  });

  it("counts an unattributed wait nowhere, and its release is harmless", () => {
    const release = beginWait(null, "data");
    expect(waitsFor("config:a")).toBe(0);
    expect(() => release()).not.toThrow();
    expect(waitsFor("config:a")).toBe(0);
  });

  it("hands a reader a live value that comes back down", () => {
    const { result } = renderHook(() => useWaitsFor("config:live"));
    expect(result.current).toBe(0);
    let release = () => {};
    act(() => {
      release = beginWait("config:live", "data");
    });
    expect(result.current).toBe(1);
    act(() => release());
    expect(result.current).toBe(0);
  });
});

describe("a page's queries are that page's wait", () => {
  it("is up while the call is in flight and down when the answer lands", async () => {
    const gate = deferred();
    hangingMethod("policy.list", gate.promise);

    const view = underOwner("config:policy", <Probe method="policy.list" />);
    expect(screen.getByTestId("probe").textContent).toBe("loading");
    expect(waitsFor("config:policy")).toBe(1);

    act(() => gate.resolve({ policies: [] }));
    await waitFor(() => expect(screen.getByTestId("probe").textContent).toBe("done"));
    expect(waitsFor("config:policy")).toBe(0);
    view.unmount();
  });

  it("counts each page's own waits, never the other page's", async () => {
    const gate = deferred();
    hangingMethod("seed.status", gate.promise);

    const slow = underOwner("config:slow", <Probe method="seed.status" />);
    expect(waitsFor("config:slow")).toBe(1);

    // The user gave up and opened another page while the first call hung. The
    // new page counts its own call — and when that one lands, its row goes
    // quiet while the hung one keeps saying "still working".
    const fast = underOwner("config:fast", <Probe method="policy.vocabulary" />);
    expect(waitsFor("config:fast")).toBe(1);
    await waitFor(() => expect(waitsFor("config:fast")).toBe(0));
    expect(waitsFor("config:slow")).toBe(1);

    act(() => gate.resolve({ sets: [] }));
    // The release runs in the query's `finally`, one microtask after the
    // answer — the same instant the page's own `loading` clears.
    await waitFor(() => expect(waitsFor("config:slow")).toBe(0));
    slow.unmount();
    fast.unmount();
  });

  it("counts nothing outside a page scope", async () => {
    const gate = deferred();
    hangingMethod("policy.list", gate.promise);
    const view = render(
      <MemoryRouter>
        <Probe method="policy.list" />
      </MemoryRouter>,
    );
    expect(waitsFor("config:policy")).toBe(0);
    act(() => gate.resolve({ policies: [] }));
    expect(waitsFor("config:policy")).toBe(0);
    view.unmount();
  });
});

describe("the chunk a page arrives in", () => {
  it("waits while the code is still on its way and stops the moment it lands", async () => {
    const gate = deferred<{ default: () => ReactNode }>();
    const renderPage = lazyConfigRender(() => gate.promise);
    const view = underOwner(
      "config:chunked",
      renderPage({ controller, live: current, canWrite: true }),
    );

    expect(screen.getByText("Opening…")).toBeTruthy();
    expect(waitsFor("config:chunked")).toBe(1);

    act(() => gate.resolve({ default: () => <div data-testid="chunked-body">here</div> }));
    await waitFor(() => expect(screen.getByTestId("chunked-body")).toBeTruthy());
    expect(waitsFor("config:chunked")).toBe(0);
    view.unmount();
  });
});

describe("the shell row wears the wait", () => {
  it("marks the configuration row busy while its page is waiting", async () => {
    registerConfigPage({
      id: "waited-for",
      label: "Waited for",
      group: "general",
      order: 5,
      status: "ready",
      summary: "a page that keeps the shell waiting",
      render: () => <Probe method="policy.list" />,
    });
    const gate = deferred();
    hangingMethod("policy.list", gate.promise);

    // The shell itself is disconnected (it reads nothing), and the children it
    // renders are the page — the same pairing the router makes on
    // `/config/waited-for`. What matters is that both halves use the row's
    // own slug as the owner id.
    const idleController = { registerReader: () => () => {} } as unknown as LiveController;
    const disconnected: LiveState = { state: "disconnected", endpoint: "test", reason: "test" };
    const view = render(
      <MemoryRouter initialEntries={["/config/waited-for"]}>
        <PageLayout controller={idleController} live={disconnected}>
          <PageWaitOwnerContext.Provider value="config:waited-for">
            <Probe method="policy.list" />
          </PageWaitOwnerContext.Provider>
        </PageLayout>
      </MemoryRouter>,
    );

    const row = await waitFor(() => {
      const found = view.container.querySelector('a[href="/config/waited-for"]');
      if (!found) throw new Error("the configuration row has not rendered yet");
      return found;
    });
    expect(row.getAttribute("aria-busy")).toBe("true");

    act(() => gate.resolve({ policies: [] }));
    await waitFor(() => expect(row.getAttribute("aria-busy")).not.toBe("true"));
    view.unmount();
  });
});
