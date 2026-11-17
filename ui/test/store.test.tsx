import { describe, expect, it, beforeEach, afterEach } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { StorePage } from "../src/features/test-bench/pages/StorePage";
import { installFakeDaemon, type FakeDaemon } from "./helpers";
import { resetBridge } from "../src/lib/bridge";

// S4's test-bench surface (AGENTS §4b).
//
// The assertions are about HONESTY rather than layout: that `foreign_keys` is
// shown as the answer rather than the intention, that a refused document path
// arrives with its reason, that all three cost states render, and that the
// derived awaiting query is shown AS SQL — because there is no
// `awaiting_user` column for anyone arriving from v1 to find.

// # `pending: null`, deliberately
//
// This is what the daemon ACTUALLY sent before the fix, and what an older or
// third-party one still can: Go marshals a nil slice as `null`, and `pending`
// is empty on every healthy store. The original fixture here said `pending: []`
// — a shape nothing produced — so this suite could not catch the crash that
// blanked the Store tab. The lesson is in `protocol/memory_test.go`: a
// hand-written fixture asserts what somebody BELIEVED the shape was.
const storeInfo = {
  path: "/home/p/.local/state/aeon/state.db",
  bytes: 196608,
  walBytes: 0,
  journalMode: "wal",
  foreignKeys: true,
  applied: [
    { name: "0001_core_tables", status: "applied", appliedAt: "2026-10-15T09:41:12.345Z" },
    { name: "0006_step_context_manifest", status: "applied", appliedAt: "2026-10-15T09:41:12.366Z" },
  ],
  pending: null as unknown as [],
  tables: ["event", "loop", "step", "step_message", "question", "chat_message"],
  vecWriterVersion: "v0.1.6",
  vecReaderVersion: "v0.1.6",
  readPoolSize: 4,
  writerPoolSize: 1,
  busyTimeoutMs: 5000,
};

const emptyAwaiting = {
  sql: "SELECT 1 FROM question\nWHERE project_root = ? AND loop_slug = ? AND answered_at IS NULL LIMIT 1",
  loops: [],
};

function install(responses: Record<string, unknown> = {}): FakeDaemon {
  return installFakeDaemon({
    responses: {
      "store.info": storeInfo,
      "store.awaiting": emptyAwaiting,
      "loop.list": { loops: [], all: true },
      "step.list": { steps: [] },
      ...responses,
    },
  });
}

beforeEach(() => resetBridge());
afterEach(() => {
  cleanup();
  resetBridge();
});

describe("the store page", () => {
  it("shows the path first, and copyable", async () => {
    install();
    render(<StorePage />);
    await waitFor(() => expect(screen.getByTestId("store-facts")).toBeTruthy());
    // Every debugging session starts by finding the file, and comparing this
    // against the daemon's own startup line is how a mismatch gets found.
    expect(screen.getByText(storeInfo.path)).toBeTruthy();
  });

  it("reports foreign_keys as the answer, not the intention", async () => {
    install();
    render(<StorePage />);
    await waitFor(() => expect(screen.getByTestId("foreign-keys")).toBeTruthy());
    expect(screen.getByTestId("foreign-keys").textContent).toContain("ON");
  });

  it("names the consequence when foreign_keys is off", async () => {
    // The wrong DSN spelling produces a store that LOOKS configured and
    // performs no cascades (measured). A page that showed `OFF` without saying
    // what it means would leave somebody to diagnose it as a data-modelling
    // bug, which is what actually happens.
    install({ "store.info": { ...storeInfo, foreignKeys: false } });
    render(<StorePage />);
    await waitFor(() => expect(screen.getByTestId("foreign-keys")).toBeTruthy());
    expect(screen.getByTestId("foreign-keys").textContent).toContain("cascades");
  });

  it("marks the extension as statically linked, with no install step to offer", async () => {
    install();
    render(<StorePage />);
    await waitFor(() => expect(screen.getByTestId("vec-version")).toBeTruthy());
    const text = screen.getByTestId("vec-version").textContent ?? "";
    expect(text).toContain("v0.1.6");
    expect(text).toContain("statically linked");
  });

  it("says when the two pools report different extension versions", async () => {
    // An extension registered for one pool is a retrieval slice that works in
    // tests and fails under load. It should be impossible; the page says so
    // rather than showing one of the two.
    install({ "store.info": { ...storeInfo, vecReaderVersion: "v0.0.1" } });
    render(<StorePage />);
    await waitFor(() => expect(screen.getByTestId("vec-version")).toBeTruthy());
    expect(screen.getByTestId("vec-version").textContent).toContain("disagree");
  });

  it("surfaces a failed migration rather than letting the store look fine", async () => {
    install({
      "store.info": {
        ...storeInfo,
        applied: [
          { name: "0001_core_tables", status: "applied" },
          { name: "0002_steps_and_events", status: "failed" },
        ],
      },
    });
    render(<StorePage />);
    await waitFor(() => expect(screen.getByTestId("migration-failed")).toBeTruthy());
    const alert = screen.getByTestId("migration-failed");
    expect(alert.textContent).toContain("0002_steps_and_events");
    expect(alert.textContent).toContain("stopped part-way");
  });

  it("survives every collection arriving as null", async () => {
    // THE regression test. Go marshals a nil slice as `null`, so a daemon that
    // has not been fixed — or an older one — sends `null` for every empty
    // list. The page must render, not crash: a crash took the whole tab blank
    // and put the reason in the console, where nobody looking at the window
    // can see it.
    install({
      "store.info": {
        ...storeInfo,
        applied: null,
        pending: null,
        tables: null,
      },
      "store.awaiting": { sql: "SELECT 1 FROM question", loops: null },
      "loop.list": { loops: null, all: true },
      "step.list": { steps: null },
    });
    render(<StorePage />);
    await waitFor(() => expect(screen.getByTestId("store-facts")).toBeTruthy());

    // It rendered, and the counts read as zero rather than as a crash.
    expect(screen.getByTestId("migration-count").textContent).toContain("0 applied");
    expect(screen.getByTestId("loops-empty")).toBeTruthy();
    expect(screen.getByTestId("awaiting-none")).toBeTruthy();
    // And no pending pill, because null is not "some pending".
    expect(screen.getByTestId("migration-count").textContent).not.toContain("pending");
  });

  it("shows pending migrations", async () => {
    install({ "store.info": { ...storeInfo, pending: [{ name: "0007_future" }] } });
    render(<StorePage />);
    await waitFor(() => expect(screen.getByTestId("migration-count")).toBeTruthy());
    expect(screen.getByTestId("migration-count").textContent).toContain("pending");
  });

  it("shows the derived awaiting query as SQL", async () => {
    // Anyone arriving from v1 will look for an `awaiting_user` column. Showing
    // the query is how they learn the answer is derived (DB6).
    install();
    render(<StorePage />);
    await waitFor(() => expect(screen.getByTestId("awaiting-sql")).toBeTruthy());
    const sql = screen.getByTestId("awaiting-sql").textContent ?? "";
    expect(sql).toContain("FROM question");
    expect(sql).toContain("answered_at IS NULL");
    expect(screen.getByTestId("awaiting-none")).toBeTruthy();
  });

  it("asks for every project when listing loops, explicitly", async () => {
    const fake = install();
    render(<StorePage />);
    await waitFor(() => expect(fake.methods()).toContain("loop.list"));
    // Explicit rather than defaulted: the daemon serves every project on the
    // machine, so "every loop" is a choice. The REQUEST must say so, because
    // the daemon refuses a list with neither a root nor `all`.
    expect(fake.paramsOf("loop.list")).toMatchObject({ all: true });
  });

  it("shows a loop's project root beside its slug", async () => {
    // The same slug in two roots is TWO loops (DB4). A list showing the slug
    // alone is one that will eventually show two rows that look identical.
    install({
      "loop.list": {
        all: true,
        loops: [
          {
            id: "a",
            slug: "refactor",
            componentRoot: "/home/p/repo-a",
            title: "refactor",
            mode: "standard",
            phase: "build",
            status: "active",
            startedAt: "t",
            updatedAt: "t",
            awaiting: false,
          },
          {
            id: "b",
            slug: "refactor",
            componentRoot: "/home/p/repo-b",
            title: "refactor",
            mode: "standard",
            phase: "plan",
            status: "active",
            startedAt: "t",
            updatedAt: "t",
            awaiting: true,
          },
        ],
      },
    });
    render(<StorePage />);
    await waitFor(() => expect(screen.getByTestId("loops-list")).toBeTruthy());
    expect(screen.getByText("/home/p/repo-a")).toBeTruthy();
    expect(screen.getByText("/home/p/repo-b")).toBeTruthy();
    // And the derived awaiting marker, on the one that has an open question.
    expect(screen.getAllByTestId("loop-awaiting")).toHaveLength(1);
  });

  it("refuses to create a loop without both halves of its identity", async () => {
    install();
    render(<StorePage />);
    // Waiting for the empty list to land first: the panel loads on mount, and
    // typing before that resolves produces a state update outside act().
    await waitFor(() => expect(screen.getByTestId("loops-empty")).toBeTruthy());
    const button = screen.getByTestId("loop-create") as HTMLButtonElement;
    // Disabled with only a slug: there is no default project, because the
    // daemon's working directory is not the user's.
    expect(button.disabled).toBe(true);
    fireEvent.change(screen.getByTestId("loop-slug"), { target: { value: "refactor" } });
    expect((screen.getByTestId("loop-create") as HTMLButtonElement).disabled).toBe(true);
    fireEvent.change(screen.getByTestId("loop-project"), { target: { value: "/home/p/repo" } });
    expect((screen.getByTestId("loop-create") as HTMLButtonElement).disabled).toBe(false);
  });

  it("renders a refused document path with all three parts of its reason", async () => {
    // THE test for this page. A refusal that reads as a success is the failure
    // mode that matters: a document name comes from a model, and a silent clamp
    // turns an escape attempt into a write somewhere unexpected.
    const refusal = JSON.stringify({
      code: -31106,
      message: "the document name was refused",
      data: {
        what: "a document name cannot contain a .. segment (got '../../etc/passwd')",
        means: "names are confined to the loop",
        todo: "use a known name or a path like `tasks/T1.md`",
        fatal: false,
      },
    });
    install({
      "loop.list": {
        all: true,
        loops: [
          {
            id: "a",
            slug: "refusals",
            componentRoot: "/home/p/repo",
            title: "refusals",
            mode: "standard",
            phase: "build",
            status: "active",
            startedAt: "t",
            updatedAt: "t",
            awaiting: false,
          },
        ],
      },
      "loop.write": () => {
        throw refusal;
      },
    });
    render(<StorePage />);
    await waitFor(() => expect(screen.getByTestId("loops-list")).toBeTruthy());

    fireEvent.click(screen.getByText("write a document"));
    const name = screen.getByTestId("doc-name");
    fireEvent.change(name, { target: { value: "../../etc/passwd" } });
    fireEvent.click(screen.getByTestId("doc-write"));

    await waitFor(() => expect(screen.getByTestId("doc-error")).toBeTruthy());
    const text = screen.getByTestId("doc-error").textContent ?? "";
    expect(text).toContain(".. segment");
    expect(text).toContain("confined to the loop");
    expect(text).toContain("tasks/T1.md");
    // And nothing was reported as written.
    expect(screen.queryByTestId("doc-result")).toBeNull();
  });

  it("reports an unchanged write as creating no revision", async () => {
    install({
      "loop.list": {
        all: true,
        loops: [
          {
            id: "a",
            slug: "same",
            componentRoot: "/home/p/repo",
            title: "same",
            mode: "standard",
            phase: "build",
            status: "active",
            startedAt: "t",
            updatedAt: "t",
            awaiting: false,
          },
        ],
      },
      "loop.write": {
        unchanged: true,
        document: { file: "plan.md", rev: 1, bytes: 7, sha256: "abc", writtenAt: "t" },
      },
    });
    render(<StorePage />);
    await waitFor(() => expect(screen.getByTestId("loops-list")).toBeTruthy());
    fireEvent.click(screen.getByText("write a document"));
    fireEvent.click(screen.getByTestId("doc-write"));
    await waitFor(() => expect(screen.getByTestId("doc-result")).toBeTruthy());
    expect(screen.getByTestId("doc-result").textContent).toContain("unchanged");
  });

  it("renders all three cost states on a step list", async () => {
    // A surface that cannot show `unknown` is a surface that will show a
    // confident wrong answer. All three, through the same renderer the CLI
    // uses, so the two cannot disagree about what a cost means.
    install({
      "step.list": {
        steps: [
          { id: 1, loopSlug: "l", componentRoot: "/r", phase: "build", agent: "a", status: "done",
            startedAt: "t", cost: { state: "known", usd: 0.00081 } },
          { id: 2, loopSlug: "l", componentRoot: "/r", phase: "build", agent: "a", status: "done",
            startedAt: "t", cost: { state: "partial", usdAtLeast: 0.0004, missing: ["cacheWriteUsdPerMTok"] } },
          { id: 3, loopSlug: "l", componentRoot: "/r", phase: "build", agent: "a", status: "suspended",
            startedAt: "t", cost: { state: "unknown", reason: "the stream aborted before usage arrived" } },
        ],
      },
    });
    render(<StorePage />);
    fireEvent.change(screen.getByTestId("steps-slug"), { target: { value: "l" } });
    fireEvent.change(screen.getByTestId("steps-project"), { target: { value: "/r" } });
    fireEvent.click(screen.getByTestId("steps-list"));

    await waitFor(() => expect(screen.getByTestId("steps-rows")).toBeTruthy());
    const rows = screen.getByTestId("steps-rows").textContent ?? "";
    // S18 T10 re-pin: a sub-cent known figure is Go's scientific form
    // ($8.10e-04), not "$0.00081" — five decimals under a cent start "$0.00x"
    // and read as free to the skimming eye, the D9 class MoneyString deletes.
    expect(rows).toContain("$8.10e-04");
    expect(rows).not.toContain("$0.00");
    // "at least" is the whole claim: a floor presented as a total is a number
    // somebody will add up.
    expect(rows).toContain("at least");
    expect(rows).toContain("the stream aborted");
    // And the suspended step is shown as suspended, not as orphaned: a
    // suspended step has no process BY DESIGN.
    expect(rows).toContain("suspended");
  });

  it("reports a verified transcript with the signature length", async () => {
    install({
      "step.list": {
        steps: [{ id: 4, loopSlug: "l", componentRoot: "/r", phase: "build", agent: "a",
          status: "done", startedAt: "t", cost: { state: "known", usd: 0.1 } }],
      },
      "step.show": {
        id: 4, loopSlug: "l", componentRoot: "/r", phase: "build", agent: "a", status: "done",
        startedAt: "t", cost: { state: "known", usd: 0.1 },
        messages: [
          { role: "user", content: [{ kind: "text", text: "what is 17 × 23?" }] },
          { role: "assistant", content: [
            { kind: "thinking", text: "17 × 20 = 340…", signature: "x".repeat(92) },
            { kind: "text", text: "391." },
          ] },
        ],
      },
      "step.verify": { ok: true, messages: 2, signatureLength: 92, digest: "deadbeefdeadbeef" },
    });
    render(<StorePage />);
    fireEvent.change(screen.getByTestId("steps-slug"), { target: { value: "l" } });
    fireEvent.change(screen.getByTestId("steps-project"), { target: { value: "/r" } });
    fireEvent.click(screen.getByTestId("steps-list"));
    await waitFor(() => expect(screen.getByTestId("step-4")).toBeTruthy());
    fireEvent.click(screen.getByTestId("step-4"));

    await waitFor(() => expect(screen.getByTestId("step-verify")).toBeTruthy());
    const verify = screen.getByTestId("step-verify").textContent ?? "";
    expect(verify).toContain("transcript verified");
    // The LENGTH, because "present" and "the right length" are different
    // claims and a truncated signature is the failure that reads as present.
    expect(verify).toContain("92 bytes");
    // And the transcript renders a signed thinking block as signed.
    expect(screen.getByTestId("step-detail").textContent).toContain("signed, 92 bytes");
  });

  it("marks an unsigned thinking block as unsigned", async () => {
    // `ask --save-step` cannot save a signature — S3's streaming surface
    // carries none — so a saved transcript holds an UNSIGNED thinking block.
    // The page says so rather than rendering it as if it were signed, because
    // S7 cannot replay an unsigned one.
    install({
      "step.list": {
        steps: [{ id: 5, loopSlug: "l", componentRoot: "/r", phase: "build", agent: "a",
          status: "done", startedAt: "t", cost: { state: "known", usd: 0.1 } }],
      },
      "step.show": {
        id: 5, loopSlug: "l", componentRoot: "/r", phase: "build", agent: "a", status: "done",
        startedAt: "t", cost: { state: "known", usd: 0.1 },
        messages: [{ role: "assistant", content: [{ kind: "thinking", text: "reasoning with no signature" }] }],
      },
      "step.verify": { ok: true, messages: 1, signatureLength: 0, digest: "abc" },
    });
    render(<StorePage />);
    fireEvent.change(screen.getByTestId("steps-slug"), { target: { value: "l" } });
    fireEvent.change(screen.getByTestId("steps-project"), { target: { value: "/r" } });
    fireEvent.click(screen.getByTestId("steps-list"));
    await waitFor(() => expect(screen.getByTestId("step-5")).toBeTruthy());
    fireEvent.click(screen.getByTestId("step-5"));

    await waitFor(() => expect(screen.getByTestId("step-detail")).toBeTruthy());
    expect(screen.getByTestId("step-detail").textContent).toContain("UNSIGNED");
    expect(screen.getByTestId("step-verify").textContent).toContain("no thinking signature");
  });

  it("renders store.info failing as an error with three parts, not an empty page", async () => {
    installFakeDaemon({
      responses: {
        "store.info": () => {
          throw JSON.stringify({
            code: -31104,
            message: "no store is available",
            data: {
              what: "this daemon has no store",
              means: "the store opens before the daemon registers its methods",
              todo: "check `aeon serve`'s first lines",
              fatal: true,
            },
          });
        },
        "store.awaiting": emptyAwaiting,
        "loop.list": { loops: [], all: true },
      },
    });
    render(<StorePage />);
    await waitFor(() => expect(screen.getByTestId("store-info-error")).toBeTruthy());
    const text = screen.getByTestId("store-info-error").textContent ?? "";
    expect(text).toContain("no store");
    expect(text).toContain("aeon serve");
    // An empty page would read as "the store is fine and empty", which is the
    // one thing it must not say.
    expect(screen.queryByTestId("store-facts")).toBeNull();
  });

  it("says that nothing here runs a step", async () => {
    // A bench that looks finished invites somebody to rely on it. The runtime
    // is S5, and the page says so next to the steps it cannot create.
    install();
    render(<StorePage />);
    await waitFor(() => expect(screen.getByTestId("store-note")).toBeTruthy());
    expect(screen.getByTestId("store-note").textContent).toContain("S5");
  });

  it("never asks the daemon for anything beyond the store's methods", async () => {
    // The generic bridge means a page can call ANY method, so the guard is a
    // test: this page must not reach for `routes.list` and start holding
    // credentials (AGENTS §4c).
    const fake = install();
    render(<StorePage />);
    await waitFor(() => expect(fake.methods().length).toBeGreaterThan(0));
    const allowed = new Set([
      "store.info", "store.awaiting", "loop.list", "loop.create", "loop.write",
      "loop.read", "step.list", "step.show", "step.verify",
    ]);
    for (const method of fake.methods()) {
      expect(allowed.has(method), `the store page called ${method}`).toBe(true);
    }
  });
});
