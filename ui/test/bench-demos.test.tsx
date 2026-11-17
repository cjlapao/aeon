import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { DemosPage } from "../src/features/test-bench/pages/DemosPage";
import { installFakeDaemon, resetFakeDaemon, type FakeDaemon } from "./helpers";

// The bench demo runner (spec 2026-10-25-bench-demo-runner): scenarios are
// DATA the page runs against the socket, and every step lands in one of the
// honest states — met, refused-as-data VERBATIM, not runnable here (with the
// operator command) — never a silent pass. The MCP half is a labeled replay:
// the page must never send a tools.* call (absent_test.go's boundary on the
// daemon side has its UI mirror here).

const FORBIDDEN = [
  { handler: "golang-engineer-loop", kind: "forbidden-tool", ref: "loop_get", msg: "no such tool exists or will — the capability it names has an Aeon mechanism named in the drop record" },
  { handler: "golang-engineer-loop", kind: "forbidden-tool", ref: "memory_append", msg: "no such tool exists or will" },
  { handler: "golang-engineer-loop", kind: "forbidden-tool", ref: "event_append", msg: "no such tool exists or will" },
  { handler: "golang-engineer-loop", kind: "skill", ref: "dev-loop-protocol", msg: "nothing loads skills here; instructions the model needs are in the body or nowhere" },
];

function registryAnswer() {
  const handlers = [{ name: "default", kind: "preset", tools: ["read", "list", "write", "question"] },
    { name: "read-only", kind: "preset", tools: ["read", "list"] }];
  for (let i = 0; i < 24; i++) handlers.push({ name: `h${String(i).padStart(2, "0")}-engineer`, kind: "body", tools: ["read"] });
  handlers.push({ name: "golang-engineer", kind: "body", tools: ["read", "list", "write", "edit", "bash"] });
  return { handlers, resolvers: ["by-tech-tag"] };
}

interface ShowAnswer { [id: string]: unknown }

function healthyDaemon(extra: Record<string, unknown | ((p: unknown) => unknown)> = {}): FakeDaemon {
  const started: Record<string, string> = {}; // tech -> id
  const shows: ShowAnswer = {};
  return installFakeDaemon({ responses: {
    "routes.list": [{ name: "openai-luna", model: "gpt", hasKey: true }],
    "handlers.list": registryAnswer(),
    "handlers.verify": (p: unknown) => {
      const params = p as { scope: string; name?: string };
      if (params.scope === "corpus") return { scope: "corpus", passed: 25, refused: false };
      return { scope: "original", name: params.name, refused: true, findings: FORBIDDEN };
    },
    "loop.start": (p: unknown) => {
      const params = p as { blueprint: string; slug: string; tech?: string; goal: string; workspaceRoot: string };
      const id = `loop-${params.tech ?? "plain"}`;
      started[params.tech ?? "plain"] = id;
      if (params.tech === "elixir") {
        shows[id] = { id, slug: params.slug, blueprint: "tag-only", goal: params.goal, root: params.workspaceRoot,
          status: "blocked", position: { lane: "implement" }, lastReason: 'no handler for tech tag "elixir"\navailable: go, javascript, python, react-ts, rust, sql, tauri-react, tauri-vue, vue-ts, websocket',
          lanes: [{ id: "implement", structure: "direct", status: "blocked", produces: ["OUT.md"],
            steps: [{ id: "implement", kind: "model-step", status: "blocked" }] }] };
      } else {
        shows[id] = { id, slug: params.slug, blueprint: "tag-only", goal: params.goal, root: params.workspaceRoot,
          status: "blocked", position: { lane: "implement" }, lastReason: "aeon -31100: unknown route",
          // no step row yet: the measured no-credential reality — the route
          // refuses before CreateStep, so the proof is the SENTENCE
          // (unknown route, downstream of resolution), not an agent field.
          lanes: [{ id: "implement", structure: "direct", status: "blocked", produces: ["OUT.md"],
            steps: [{ id: "implement", kind: "model-step", status: "blocked" }] }] };
      }
      return { id, slug: params.slug, status: "running" };
    },
    "loop.show": (p: unknown) => {
      const params = p as { id: string };
      if (params.id in shows) return shows[params.id];
      for (const v of Object.values(shows)) {
        const view = v as { slug: string };
        if (view.slug === params.id) return v;
      }
      throw new Error(`no such loop ${params.id}`);
    },
    ...extra,
  } });
}

function runScenario(name: RegExp) {
  fireEvent.click(screen.getByRole("button", { name }));
}

afterEach(() => { cleanup(); resetFakeDaemon(); vi.unstubAllGlobals(); });

describe("bench demo runner", () => {
  it("runs the agents resolver demo and every step reads met", async () => {
    const daemon = healthyDaemon();
    render(<DemosPage />);
    // The workspace starts EMPTY on purpose (a `~` default was a bug the
    // 2026-10-25 walk found: the daemon refuses tilde paths), so type the
    // absolute path the way an operator must.
    fireEvent.change(screen.getByTestId("demo-workspace"), { target: { value: "/home/tester/aeon-qa-workspace" } });
    runScenario(/Run agents demo/);
    const rows = await screen.findAllByTestId("demo-step");
    await waitFor(() => {
      const missing = rows.filter((r) => !r.textContent?.includes("met")).map((r) => (r.textContent ?? "").slice(0, 200));
      expect(missing).toEqual([]);
    });
    // The refusal half is the demo's proof: the elixir step must QUOTE the
    // daemon's sentence, not paraphrase it.
    const elixir = rows.find((r) => r.textContent?.includes("elixir"));
    expect(elixir?.textContent).toContain('no handler for tech tag "elixir"');
    // The resolution half: the loop view's step agent is the citation.
    const go = rows.find((r) => r.textContent?.includes("golang"));
    expect(go?.textContent).toContain("golang-engineer");
    // Started as demo loops: slugs carry the demo- prefix, and the workspace
    // root traveled — the loops stay in the store as the S9 tradition says.
    const starts = daemon.calls.filter((c) => c.method === "loop.start");
    expect(starts.length).toBe(2);
    for (const c of starts) {
      const p = c.params as { slug: string; workspaceRoot: string; blueprint: string };
      expect(p.slug).toMatch(/^demo-/);
      expect(p.blueprint).toBe("tag-only");
      expect(p.workspaceRoot).toBeTruthy();
    }
  });

  it("renders the original-verify refusal VERBATIM, basename-only", async () => {
    healthyDaemon();
    render(<DemosPage />);
    runScenario(/Run agents demo/);
    const rows = await screen.findAllByTestId("demo-step");
    const refusal = rows.find((r) => r.textContent?.includes("golang-engineer-loop"));
    expect(refusal?.textContent).toContain(
      "no such tool exists or will — the capability it names has an Aeon mechanism named in the drop record");
    expect(refusal?.textContent).toContain("dev-loop-protocol");
    expect(refusal?.textContent).not.toContain("/home/"); // no daemon paths, ever
  });

  it("answers not-runnable-here with the operator command when originals are unavailable", async () => {
    healthyDaemon({
      "handlers.verify": (p: unknown) => {
        const params = p as { scope: string; name?: string };
        if (params.scope === "corpus") return { scope: "corpus", passed: 25, refused: false };
        return { scope: "original", name: params.name, reason: { kind: "originals-unavailable",
          detail: "the originals ship with the source tree, not the daemon — set AEON_OPENCODE_DIR to the directory of original bodies (or run where the tree is above you) to verify a negative control from here" } };
      },
    });
    render(<DemosPage />);
    runScenario(/Run agents demo/);
    const rows = await screen.findAllByTestId("demo-step");
    const original = rows.find((r) => r.textContent?.includes("original"));
    expect(original?.textContent).toContain("not runnable here");
    expect(original?.textContent).toContain("AEON_OPENCODE_DIR"); // the remedy, verbatim
  });

  it("greys the model-build scenario with commands when no route has a key", async () => {
    healthyDaemon({ "routes.list": [{ name: "openai-luna", model: "gpt", hasKey: false }] });
    render(<DemosPage />);
    const run = await screen.findByRole("button", { name: /Run model demo/ });
    expect((run as HTMLButtonElement).disabled).toBe(true);
    const note = await screen.findByTestId("model-demo-unavailable");
    expect(note.textContent).toContain("aeon loop start --blueprint dev-loop");
    expect(note.textContent).toContain("--tech go");
  });

  it("replays the MCP demo without ever calling a tools method", async () => {
    const daemon = healthyDaemon();
    render(<DemosPage />);
    runScenario(/Run MCP replay/);
    const rows = await screen.findAllByTestId("mcp-step");
    await waitFor(() => expect(rows.length).toBeGreaterThan(0));
    expect(rows.every((r) => r.textContent?.includes("replay"))).toBe(true);
    // The replay label is not decoration: the page never sends tools.* —
    // absent_test.go pins the daemon away from the MCP layer; this pins the UI.
    expect(daemon.methods().some((m) => m.startsWith("tools."))).toBe(false);
    // And it cites where the envelopes came from, rather than asserting a live call.
    const joined = rows.map((r) => r.textContent ?? "").join(" ");
    expect(joined).toMatch(/TestCall|fixture|spec-literal/);
  });

  it("refuses tilde and empty workspaces BEFORE loop.start, naming the remedy", async () => {
    // The 2026-10-25 walk's -32603: a `~` path reaches the daemon only to be
    // refused. The runner answers in the daemon's own register, first.
    const daemon = healthyDaemon();
    render(<DemosPage />);
    fireEvent.change(screen.getByTestId("demo-workspace"), { target: { value: "~/aeon-qa-workspace" } });
    runScenario(/Run agents demo/);
    const rows = await screen.findAllByTestId("demo-step");
    const tagRows = rows.filter((r) => r.textContent?.includes("tag-only"));
    expect(tagRows.length).toBe(2);
    for (const r of tagRows) {
      expect(r.textContent).toContain("not runnable here");
      expect(r.textContent).toContain("workspace path must be absolute");
      expect(r.textContent).toContain("/home/you/aeon-qa-workspace"); // the remedy
    }
    expect(daemon.calls.filter((c) => c.method === "loop.start").length).toBe(0);
    // The other steps still run — the guard is the two tag steps, not a veto.
    expect(rows.find((r) => r.textContent?.includes("handlers.list"))?.textContent).toContain("met");
  });

  it("an unregistered method reads as not-runnable-here with the restart remedy, not a red error", async () => {
    // The 2026-10-25 screenshot's -32601s: the app was pointed at a daemon
    // older than handlers.list/handlers.verify. That is a capability gap to
    // name, not a fault to dress in red.
    const notRegistered = (method: string) => () => {
      const err = new Error(`method "${method}" is not registered`) as Error & { code?: number };
      err.code = -32601;
      throw err;
    };
    healthyDaemon({
      "handlers.list": notRegistered("handlers.list"),
      "handlers.verify": notRegistered("handlers.verify"),
    });
    render(<DemosPage />);
    fireEvent.change(screen.getByTestId("demo-workspace"), { target: { value: "/home/tester/aeon-qa-workspace" } });
    runScenario(/Run agents demo/);
    const rows = await screen.findAllByTestId("demo-step");
    const registry = rows.find((r) => r.textContent?.includes("handlers.list"));
    expect(registry?.textContent).toContain("not runnable here");
    expect(registry?.textContent).toContain("is not registered"); // verbatim, the daemon's own sentence
    expect(registry?.textContent).toContain("Restart");            // the remedy
    expect(registry?.textContent).not.toMatch(/error \(-32601\)/); // no red-error register
    // The tag steps never touched the unregistered methods — they still met.
    expect(rows.find((r) => r.textContent?.includes("elixir"))?.textContent).toContain("met");
  });

  it("a step whose call errors shows the error, not a pass", async () => {
    healthyDaemon({ "handlers.list": () => { throw new Error("daemon is napping"); } });
    render(<DemosPage />);
    runScenario(/Run agents demo/);
    const rows = await screen.findAllByTestId("demo-step");
    const failed = rows.find((r) => r.textContent?.includes("daemon is napping"));
    expect(failed).toBeTruthy();
    expect(failed?.textContent).not.toContain("met");
  });
});
