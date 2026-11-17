import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { I18nProvider } from "@cjlapao/ui-kit";
import { APP_LOCALES } from "../src/lib/i18n";
import { MergeQueuePage } from "../src/features/test-bench/pages/MergeQueuePage";

// S60 T11 — the bench page speaks only the daemon's words. Every claim
// below is about rows the mocked bridge actually answered: the fixtures
// here mirror the conformance files under internal/protocol/conformance/
// mergequeue/ (same shapes, same field spellings, test-local copies), and
// the tests check that the page renders THOSE and invents nothing.

interface Call { method: string; params: Record<string, unknown> }

const calls: Call[] = [];

/** Answers the mocked daemon gives per method; tests rewrite `answer`
 *  freely — one mutable table keeps every scenario legible. */
const answer: Record<string, unknown | ((params: Record<string, unknown>) => unknown)> = {};

function resolve<T>(method: string, params: Record<string, unknown>): T {
  const raw = answer[method];
  const value = typeof raw === "function" ? (raw as (p: Record<string, unknown>) => unknown)(params) : raw;
  if (value instanceof Error) throw value;
  return value as T;
}

vi.mock("../src/lib/bridge", () => ({
  rpc: vi.fn(async (method: string, params: Record<string, unknown> = {}) => {
    calls.push({ method, params });
    return resolve(method, params);
  }),
  toRpcError: (cause: unknown) => {
    const text = cause instanceof Error ? cause.message : String(cause);
    try {
      const parsed = JSON.parse(text) as { code?: number; message?: string; data?: Record<string, unknown> };
      const error = new Error(parsed.message ?? text) as Error & { code: number; data?: Record<string, unknown> };
      error.code = parsed.code ?? -1;
      error.data = parsed.data;
      return error;
    } catch {
      return new Error(text);
    }
  },
}));

const refusalError = (parts: { what: string; means?: string; todo?: string }): Error =>
  new Error(JSON.stringify({ code: -31955, message: parts.what, data: { what: parts.what, means: parts.means ?? "", todo: parts.todo ?? "", fatal: false } }));

// ── fixture shapes (mirrors of the conformance files) ─────────────────────

const row = (over: Record<string, unknown> = {}) => ({
  id: "mq-0f1e", componentRoot: "/srv/alpha", base: "main", branch: "feature/login-retry",
  loop: "loop-mq-0f1e", status: "ready", position: 1, attended: true,
  headSha: "aaaa1111", baseSha: "9999aaaa", prNumber: 412, catchUps: 1,
  enqueuedBy: { kind: "user", id: "cjlapao" }, staleness: { state: "current" },
  nextStep: "ready: the gate is green for aaaa1111", digest: "fbe6".padEnd(64, "0"),
  createdAt: "2026-11-03T10:00:00Z", updatedAt: "2026-11-04T09:00:00Z", ...over,
});

const vocabulary = (methods?: Array<{ name: string; door: string }>) => ({
  states: [
    { name: "queued", meaning: "on the lane, waiting its turn", legalNext: ["catching_up", "removed"] },
    { name: "ready", meaning: "the gate is green for this head", legalNext: ["queued", "merging", "removed"] },
  ],
  reasons: ["denied", "conflict"],
  modes: ["off", "manual", "auto"],
  conflictPolicies: ["ask", "resolve", "block"],
  landMethods: ["squash", "merge"],
  mergeActors: ["user", "cli"],
  methods: methods ?? [
    { name: "mergequeue.list", door: "read" }, { name: "mergequeue.show", door: "read" },
    { name: "mergequeue.status", door: "read" }, { name: "mergequeue.vocabulary", door: "read" },
    { name: "mergequeue.enqueue", door: "human" }, { name: "mergequeue.dequeue", door: "human" },
    { name: "mergequeue.reorder", door: "human" }, { name: "mergequeue.retry", door: "human" },
    { name: "mergequeue.land", door: "human" }, { name: "mergequeue.settings", door: "human" },
  ],
});

const status = {
  componentRoot: "/srv/alpha",
  config: { componentRoot: "/srv/alpha", mode: "manual", conflictPolicy: "ask", landMethod: "squash", maxCatchUps: 3, updatedAt: "2026-11-01T12:00:00Z", updatedBy: "user/cjlapao" },
  counts: {},
  lanes: [{ base: "main", headItem: "mq-0f1e" }],
  items: [],
};

const shown = {
  item: row(),
  transitions: [
    { kind: "transition", toStatus: "queued", actorKind: "user", actorId: "cjlapao", detail: "appended at position 1", at: "2026-11-03T10:00:00Z" },
    { kind: "transition", fromStatus: "queued", toStatus: "catching_up", actorKind: "daemon", actorId: "driver", at: "2026-11-03T10:05:00Z" },
    { kind: "transition", fromStatus: "catching_up", toStatus: "verifying", actorKind: "daemon", actorId: "driver", at: "2026-11-03T10:20:00Z" },
    { kind: "transition", fromStatus: "verifying", toStatus: "ready", actorKind: "daemon", actorId: "driver", at: "2026-11-04T09:00:00Z" },
  ],
  worktree: "/srv/alpha/.aeon/integrate/mq-0f1e",
};

function setDefaultAnswers(): void {
  answer["component.list"] = { components: [
    { id: "c-alpha", name: "Alpha", slug: "alpha", parentId: "", root: "/srv/alpha" },
    { id: "c-child", name: "Child", slug: "child", parentId: "c-alpha", root: "/srv/alpha/child" },
  ] };
  answer["mergequeue.vocabulary"] = vocabulary();
  answer["mergequeue.list"] = [row(), row({
    id: "mq-1a2b", branch: "feature/export-csv", status: "queued", position: 2, attended: false,
    loop: undefined, prNumber: undefined, catchUps: 0,
    enqueuedBy: { kind: "cli", id: "aeon-cli" },
    staleness: { state: "not-yet", reason: "the queue has not read its base for this state yet" },
    nextStep: "queued: waiting its turn on main behind 1 item",
  }), row({
    id: "mq-3c4d", branch: "feature/no-fetch", status: "queued", position: 3, prNumber: undefined, loop: undefined,
    staleness: { state: "unknown", reason: "the base cannot be read (no fetch has run, or the root is not a repository)" },
  })];
  answer["mergequeue.status"] = status;
  answer["mergequeue.show"] = shown;
  answer["mergequeue.enqueue"] = row({ id: "mq-new", status: "queued", position: 4 });
  answer["mergequeue.dequeue"] = row({ status: "removed" });
  answer["mergequeue.retry"] = row({ status: "queued" });
  answer["mergequeue.reorder"] = row({ position: 1 });
  answer["mergequeue.land"] = row({ status: "merging" });
  answer["mergequeue.settings"] = { config: status.config, statement: "MERGE QUEUE — settings as held." };
}

function renderPage(connected = true) {
  return render(<I18nProvider locales={APP_LOCALES}><MergeQueuePage connected={connected} /></I18nProvider>);
}

const methodCalls = (method: string): Call[] => calls.filter((call) => call.method === method);

beforeEach(() => {
  calls.length = 0;
  for (const key of Object.keys(answer)) delete answer[key];
  setDefaultAnswers();
});

describe("rows are the daemon's rows (verbatim)", () => {
  it("renders position, status, branch → base, PR, loop, staleness and enqueued-by from the list answer", async () => {
    renderPage();
    await waitFor(() => expect(screen.getByTestId("mq-row-mq-0f1e")).toBeTruthy());
    const one = screen.getByTestId("mq-row-mq-0f1e");
    expect(one.textContent).toContain("feature/login-retry");
    expect(one.textContent).toContain("main");
    expect(one.textContent).toContain("412");
    expect(one.textContent).toContain("loop-mq-0f1e");
    expect(one.textContent).toContain("cjlapao");
    expect(screen.getByTestId("mq-row-mq-1a2b").textContent).toContain("aeon-cli");
    // the picker offered only roots.
    const picker = screen.getByTestId("mq-root") as HTMLSelectElement;
    expect(picker.textContent).toContain("Alpha");
    expect(picker.textContent).not.toContain("Child");
  });

  it("renders the staleness looks distinctly: a state, a state with reason, and the unread — which never shows a number", async () => {
    renderPage();
    await waitFor(() => expect(screen.getByTestId("mq-row-mq-0f1e")).toBeTruthy());
    const shapes = screen.getAllByTestId("mq-staleness").map((node) => ({ shape: node.getAttribute("data-shape"), text: node.textContent ?? "" }));
    expect(shapes.map((entry) => entry.shape)).toEqual(["state", "state-reason", "state-reason"]);
    expect(shapes[0]?.text).toContain("current");
    expect(shapes[1]?.text).toContain("the queue has not read its base for this state yet");
    const unread = shapes.find((entry) => entry.text.includes("the base cannot be read"));
    expect(unread).toBeTruthy();
    // the second row's not-yet keeps its own reason; the third row shows
    // its reason and NO digit count — "0" would be a lie about freshness.
    expect(unread?.text).not.toMatch(/\b0\b/);
  });

  it("clicking a row asks mergequeue.show for exactly that id and renders the answer verbatim", async () => {
    renderPage();
    await waitFor(() => expect(screen.getByTestId("mq-row-mq-0f1e")).toBeTruthy());
    fireEvent.click(screen.getByTestId("mq-row-mq-0f1e"));
    await waitFor(() => expect(screen.getByTestId("mq-detail")).toBeTruthy());
    expect(methodCalls("mergequeue.show").some((call) => call.params.id === "mq-0f1e")).toBe(true);
    expect(screen.getByTestId("mq-next-step").textContent).toBe("ready: the gate is green for aaaa1111");
    expect(screen.getByTestId("mq-transitions").children).toHaveLength(4);
    expect(screen.getByTestId("mq-detail").textContent).toContain("/srv/alpha/.aeon/integrate/mq-0f1e");
  });
});

describe("actions are the vocabulary's methods, not this page's list", () => {
  it("offers a button per human door the vocabulary answers", async () => {
    renderPage();
    // settings-apply is the LAST of the six to render (it needs the full
    // roots → root → status → config → draft chain, while the row buttons
    // and enqueue need only the vocabulary), so settling it settles them all.
    await waitFor(() => expect(screen.getByTestId("mq-settings-apply")).toBeTruthy());
    for (const id of ["mq-retry", "mq-dequeue", "mq-reorder", "mq-land", "mq-enqueue", "mq-settings-apply"]) {
      expect(screen.getByTestId(id)).toBeTruthy();
    }
  });

  it("a vocabulary without the land door draws no land button (the buttons come from the answer)", async () => {
    answer["mergequeue.vocabulary"] = vocabulary(vocabulary().methods.filter((m) => m.name !== "mergequeue.land"));
    renderPage();
    await waitFor(() => expect(screen.getByTestId("mq-retry")).toBeTruthy());
    expect(screen.queryByTestId("mq-land")).toBeNull();
  });

  it("the vocabulary table renders the daemon's states, meanings and legal-next words", async () => {
    renderPage();
    await waitFor(() => expect(screen.getByTestId("mq-vocab-queued")).toBeTruthy());
    const queued = screen.getByTestId("mq-vocab-queued");
    expect(queued.textContent).toContain("on the lane, waiting its turn");
    expect(queued.textContent).toContain("catching_up");
  });
});

describe("the seatbelts: declines send nothing", () => {
  it("land asks with the daemon's own words first and sends mergequeue.land only on the press, with the shown digest", async () => {
    renderPage();
    await waitFor(() => expect(screen.getByTestId("mq-row-mq-0f1e")).toBeTruthy());
    fireEvent.click(screen.getByTestId("mq-row-mq-0f1e"));
    await waitFor(() => expect(screen.getByTestId("mq-detail")).toBeTruthy());
    fireEvent.click(screen.getByTestId("mq-land"));
    await waitFor(() => expect(screen.getByTestId("mq-pending")).toBeTruthy());
    expect(screen.getByTestId("mq-pending-statement").textContent).toContain("feature/login-retry");
    expect(screen.getByTestId("mq-pending-statement").textContent).toContain("ready: the gate is green for aaaa1111");
    expect(methodCalls("mergequeue.land")).toHaveLength(0);
    fireEvent.click(screen.getByTestId("mq-pending-decline"));
    await waitFor(() => expect(screen.queryByTestId("mq-pending")).toBeNull());
    expect(methodCalls("mergequeue.land")).toHaveLength(0);

    fireEvent.click(screen.getByTestId("mq-land"));
    await waitFor(() => expect(screen.getByTestId("mq-pending-press")).toBeTruthy());
    fireEvent.click(screen.getByTestId("mq-pending-press"));
    await waitFor(() => expect(methodCalls("mergequeue.land").length).toBe(1));
    const landed = methodCalls("mergequeue.land")[0];
    expect(landed?.params.id).toBe("mq-0f1e");
    expect(landed?.params.digest).toBe("fbe6".padEnd(64, "0"));
  });

  it("a settings widening refuses first; the digest rides back ONLY from the press", async () => {
    const digest = "a".repeat(64);
    let second = false;
    answer["mergequeue.settings"] = (params: Record<string, unknown>) => {
      if (params.acknowledged !== undefined) {
        second = true;
        return { config: { ...status.config, mode: params.mode }, statement: "APPLIED." };
      }
      throw refusalError({ what: `widening the queue's authority needs acknowledged "${digest}" from this refusal echoed back`, means: "auto lands rows without a person", todo: "press apply again with the acknowledgement" });
    };
    renderPage();
    await waitFor(() => expect(screen.getByTestId("mq-set-mode")).toBeTruthy());
    fireEvent.change(screen.getByTestId("mq-set-mode"), { target: { value: "auto" } });
    fireEvent.click(screen.getByTestId("mq-settings-apply"));
    await waitFor(() => expect(screen.getByTestId("mq-ack-gate")).toBeTruthy());
    // the refusal is shown verbatim, and the draft kept its aim:
    expect(screen.getByTestId("mq-refusal").textContent).toContain("widening the queue's authority");
    expect((screen.getByTestId("mq-set-mode") as HTMLSelectElement).value).toBe("auto");
    expect(methodCalls("mergequeue.settings").filter((c) => c.params.acknowledged !== undefined)).toHaveLength(0);

    fireEvent.click(screen.getByTestId("mq-ack-press"));
    await waitFor(() => expect(second).toBe(true));
    const acked = methodCalls("mergequeue.settings").find((c) => c.params.acknowledged !== undefined);
    expect(acked?.params.acknowledged).toBe(digest);
  });

  it("declining the widening gate sends nothing further and keeps the refusal on screen", async () => {
    answer["mergequeue.settings"] = () => {
      throw refusalError({ what: `widening needs acknowledged "${"b".repeat(64)}" back`, means: "wider authority", todo: "" });
    };
    renderPage();
    await waitFor(() => expect(screen.getByTestId("mq-set-mode")).toBeTruthy());
    fireEvent.click(screen.getByTestId("mq-settings-apply"));
    await waitFor(() => expect(screen.getByTestId("mq-ack-gate")).toBeTruthy());
    const before = calls.length;
    fireEvent.click(screen.getByTestId("mq-ack-decline"));
    await waitFor(() => expect(screen.queryByTestId("mq-ack-gate")).toBeNull());
    expect(calls.length).toBe(before);
    expect(screen.getByTestId("mq-refusal").textContent).toContain("wider authority");
  });

  it("a plain refusal (retry on a row that did not fail) is shown verbatim and moves no row", async () => {
    answer["mergequeue.retry"] = () => {
      throw refusalError({ what: "retry is for rows that ended short; this row is mid-flight", means: "nothing to retry yet", todo: "wait for it to end, or dequeue it" });
    };
    renderPage();
    await waitFor(() => expect(screen.getByTestId("mq-row-mq-0f1e")).toBeTruthy());
    fireEvent.click(screen.getByTestId("mq-row-mq-0f1e"));
    await waitFor(() => expect((screen.getByTestId("mq-retry") as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(screen.getByTestId("mq-retry"));
    await waitFor(() => expect(screen.getByTestId("mq-refusal")).toBeTruthy());
    expect(screen.getByTestId("mq-refusal").textContent).toContain("retry is for rows that ended short");
    // the row still shows the daemon's last truth, not a hoped-for move:
    expect(screen.getByTestId("mq-status-mq-0f1e").textContent).toBe("ready");
  });
});

describe("rows move when the response says so — never before", () => {
  it("the row keeps its old status until dequeue ANSWERS, then shows the answer", async () => {
    let release: ((value: unknown) => void) | null = null;
    answer["mergequeue.dequeue"] = () => new Promise((done) => {
      release = done;
    });
    renderPage();
    await waitFor(() => expect(screen.getByTestId("mq-row-mq-0f1e")).toBeTruthy());
    fireEvent.click(screen.getByTestId("mq-row-mq-0f1e"));
    await waitFor(() => expect((screen.getByTestId("mq-dequeue") as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(screen.getByTestId("mq-dequeue"));
    await waitFor(() => expect(methodCalls("mergequeue.dequeue").length).toBe(1));
    // in flight: the old truth still stands.
    expect(screen.getByTestId("mq-status-mq-0f1e").textContent).toBe("ready");
    (release as unknown as (value: unknown) => void)(row({ status: "removed" }));
    await waitFor(() => expect(screen.getByTestId("mq-status-mq-0f1e").textContent).toBe("removed"));
  });
});

describe("offline: writes disabled, last reads kept", () => {
  it("going offline disables every write and keeps the rows the last read answered", async () => {
    const view = renderPage(true);
    await waitFor(() => expect(screen.getByTestId("mq-row-mq-0f1e")).toBeTruthy());
    view.rerender(<I18nProvider locales={APP_LOCALES}><MergeQueuePage connected={false} /></I18nProvider>);
    expect((screen.getByTestId("mq-retry") as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByTestId("mq-dequeue") as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByTestId("mq-land") as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByTestId("mq-enqueue") as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByTestId("mq-settings-apply") as HTMLButtonElement).disabled).toBe(true);
    // the rows are the last data read, not a wipe:
    expect(screen.getByTestId("mq-row-mq-0f1e").textContent).toContain("feature/login-retry");
  });

  it("never connected draws the notice and asks the queue nothing", async () => {
    renderPage(false);
    await waitFor(() => expect(screen.getByTestId("mq-empty")).toBeTruthy());
    expect(methodCalls("mergequeue.list")).toHaveLength(0);
    expect(calls.filter((c) => c.method === "component.list" || c.method === "mergequeue.vocabulary")).toHaveLength(0);
  });
});

describe("the enqueue door is the person's; the proposal twin is untouched", () => {
  it("enqueues with the git facts aimed at the selected root and never names the agent's proposal door", async () => {
    renderPage();
    await waitFor(() => expect(screen.getByTestId("mq-enqueue")).toBeTruthy());
    fireEvent.change(screen.getByTestId("mq-enqueue-base"), { target: { value: "main" } });
    fireEvent.change(screen.getByTestId("mq-enqueue-branch"), { target: { value: "feature/nightly-sweep" } });
    fireEvent.change(screen.getByTestId("mq-enqueue-head"), { target: { value: "cccc3333" } });
    fireEvent.click(screen.getByTestId("mq-enqueue"));
    await waitFor(() => expect(methodCalls("mergequeue.enqueue").length).toBe(1));
    expect(methodCalls("mergequeue.enqueue")[0]?.params).toMatchObject({
      componentRoot: "/srv/alpha", baseBranch: "main", featureBranch: "feature/nightly-sweep", headSha: "cccc3333",
    });
    expect(calls.some((c) => c.method.includes("propose"))).toBe(false);
  });

  it("sends nothing half-aimed: an empty head keeps the click off the wire", async () => {
    renderPage();
    await waitFor(() => expect(screen.getByTestId("mq-enqueue")).toBeTruthy());
    fireEvent.click(screen.getByTestId("mq-enqueue"));
    expect(methodCalls("mergequeue.enqueue")).toHaveLength(0);
    expect(screen.getByTestId("mq-refusal").textContent).toContain("sends nothing half-aimed");
  });
});
