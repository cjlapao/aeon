import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { DemosPage } from "../src/features/test-bench/pages/DemosPage";
import { installFakeDaemon, resetFakeDaemon, type FakeDaemon } from "./helpers";

// S14 T12 — the "Agents · services (S14)" panel contract (QAC-25).
//
// The panel's live half spends NO model call: it reads agent.discover and
// agent.card (signature state included), and the invoke demos are refusals
// — data, not faults — because a refusal that arrives before any execution
// is the safest and most instructive thing the socket can answer. The
// success invoke is credential-gated: with no route carrying a key, no
// agent.invoke may leave this page AT ALL (the assertion below is the
// boundary; the daemon's own gates stand behind it). The suspension,
// cold-resume and third-party-label rows are LABELLED REPLAYS citing the Go
// tests — the same discipline as the MCP replay rows: a replay whose label
// you must go find is a lie waiting for its first reader.

const DISCOVER = {
  candidates: [{
    agent: "reddit-agent", name: "reddit-agent",
    description: "reads reddit and summarises sentiment", version: "1.0.0",
    defaultInputModes: ["text/plain"], defaultOutputModes: ["text/plain"],
    skills: [{
      agent: "reddit-agent", id: "summarise-sentiment", name: "Summarise sentiment",
      description: "given a topic, return what people are saying",
      tags: ["reddit", "sentiment"], examples: ["what do people think of aeon?"],
      inputModes: ["text/plain"], outputModes: ["text/plain"],
    }],
  }],
  refuses: [], count: 1,
};

const CARD = {
  agent: "reddit-agent", loaded: true, state: "loaded",
  card: { name: "reddit-agent", description: "reads reddit and summarises sentiment", version: "1.0.0", protocolVersion: "0.3", url: "local" },
};

// The sentences below are COPIED FROM THE GO SOURCE (internal/agentsvc/
// transport.go, internal/policy's default-deny formatter) — the panel must
// quote them, and the test must quote the quote, or it tests nothing.
const MISSING_TEXT = "refused: text · an ask with no words is not an ask · remedy: say what you want the callee to do";
const DEFAULT_DENY = "refused: agent:project-planner · no allow matched · default deny (PL5)";

function rpcRefusal(what: string): () => never {
  // The REAL error path: the shell serialises the structured JSON-RPC error
  // as a JSON string and toRpcError parses it back into RpcError — so the
  // fake throws exactly what the Tauri command would, code included, and
  // the sentence rides in data.what like ErrAgentRefused sends it.
  return () => {
    throw JSON.stringify({
      code: -31600, // ErrAgentRefused — refusals are DATA at this surface
      message: "the agent-services boundary refused",
      data: { what, means: "an agent ask is judged by doors with names", todo: "the what names the door", fatal: false },
      kind: "rpc",
    });
  };
}

function servicesDaemon(extra: Record<string, unknown | ((p: unknown) => unknown)> = {}): FakeDaemon {
  return installFakeDaemon({ responses: {
    "routes.list": [{ name: "openai-luna", model: "gpt", hasKey: true }],
    "agent.discover": DISCOVER,
    "agent.card": (p: unknown) => {
      const params = p as { agent: string };
      if (params.agent === "reddit-agent") return CARD;
      return { agent: params.agent, loaded: false, state: "absent" };
    },
    "agent.invoke": rpcRefusal(MISSING_TEXT),
    ...extra,
  } });
}

function typeWorkspace() {
  fireEvent.change(screen.getByTestId("demo-workspace"), { target: { value: "/home/tester/aeon-qa-workspace" } });
}

afterEach(() => { cleanup(); resetFakeDaemon(); vi.unstubAllGlobals(); });

describe("S14 services panel", () => {
  it("enumerates cards and skills with signature state, spending no model call", async () => {
    const daemon = servicesDaemon();
    render(<DemosPage />);
    fireEvent.click(screen.getByRole("button", { name: /Run services demo/ }));
    const rows = await screen.findAllByTestId("services-step");
    await waitFor(() => {
      expect(rows.filter((r) => !r.textContent?.includes("met")).map((r) => (r.textContent ?? "").slice(0, 160))).toEqual([]);
    });
    const joined = rows.map((r) => r.textContent ?? "").join("\n");
    // The listing shows what a reader judges fit FROM — tags and examples —
    // and the card row shows the SIGNATURE STATE, three-valued.
    expect(joined).toContain("summarise-sentiment");
    expect(joined).toContain("reddit");
    expect(joined).toContain("what do people think of aeon?");
    expect(joined).toContain("loaded");
    // No model call, no loop, no ask: the live half is reads only.
    const methods = daemon.methods();
    expect(methods).toContain("agent.discover");
    expect(methods).toContain("agent.card");
    expect(methods.some((m) => m === "ask" || m.startsWith("loop.") || m === "agent.invoke" || m.startsWith("agent.task."))).toBe(false);
  });

  it("renders an absent card as the ANSWER it is — absent, not error", async () => {
    servicesDaemon({
      "agent.card": () => ({ agent: "nobody", loaded: false, state: "absent" }),
    });
    render(<DemosPage />);
    fireEvent.click(screen.getByRole("button", { name: /Run services demo/ }));
    const rows = await screen.findAllByTestId("services-step");
    const cardRow = rows.find((r) => r.textContent?.includes("card"));
    expect(cardRow?.textContent).toContain("absent");
    expect(cardRow?.textContent).not.toContain("error (");
  });

  it("the invoke demos are refusals-as-data, quoting the daemon verbatim", async () => {
    servicesDaemon(); // invoke answers -31600 with the missing-text sentence
    render(<DemosPage />);
    typeWorkspace();
    // The routes probe is async: wait for the gate to open before pressing.
    const run = await screen.findByRole("button", { name: /Run invoke demos/ });
    await waitFor(() => expect((run as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(run);
    const rows = await screen.findAllByTestId("services-invoke-step");
    await waitFor(() => {
      const joined = rows.map((r) => r.textContent ?? "").join("\n");
      expect(joined).toContain(MISSING_TEXT); // verbatim, not paraphrased
      expect(joined).not.toContain("error ("); // a refusal is not a fault
    });
  });

  it("the SkillAccess denial renders its S10 sentence verbatim", async () => {
    servicesDaemon({ "agent.invoke": rpcRefusal(DEFAULT_DENY) });
    render(<DemosPage />);
    const run = await screen.findByRole("button", { name: /Run invoke demos/ });
    await waitFor(() => expect((run as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(run);
    const rows = await screen.findAllByTestId("services-invoke-step");
    await waitFor(() => {
      expect(rows.map((r) => r.textContent ?? "").join("\n")).toContain("default deny (PL5)");
    });
  });

  it("greys the invoke demos with the operator route when no route carries a key — and NO invoke ever leaves", async () => {
    const daemon = servicesDaemon({ "routes.list": [{ name: "openai-luna", model: "gpt", hasKey: false }] });
    render(<DemosPage />);
    typeWorkspace();
    const run = await screen.findByRole("button", { name: /Run invoke demos/ });
    expect((run as HTMLButtonElement).disabled).toBe(true);
    const note = await screen.findByTestId("services-invoke-unavailable");
    expect(note.textContent).toContain("DEMO.md"); // the walk is the remedy
    // Run everything the page allows in this state: reads and replays.
    fireEvent.click(screen.getByRole("button", { name: /Run services demo/ }));
    await screen.findAllByTestId("services-step");
    fireEvent.click(screen.getByRole("button", { name: /Run services replay/ }));
    await screen.findAllByTestId("services-replay-step");
    expect(daemon.methods().some((m) => m === "agent.invoke" || m.startsWith("agent.task."))).toBe(false);
  });

  it("replays the suspension, cold resume, cycle and third-party label — labeled, cited, and never live", async () => {
    const daemon = servicesDaemon();
    render(<DemosPage />);
    fireEvent.click(screen.getByRole("button", { name: /Run services replay/ }));
    const rows = await screen.findAllByTestId("services-replay-step");
    expect(rows.length).toBeGreaterThanOrEqual(4);
    expect(rows.every((r) => r.textContent?.includes("replay"))).toBe(true);
    const joined = rows.map((r) => r.textContent ?? "").join(" ");
    // Each verdict cites the Go test behind it — the panel claims nothing
    // the suites have not already proven.
    expect(joined).toMatch(/task_test|invoke_test|chain_test|untrusted_test|transport_test/);
    expect(joined).toMatch(/suspend/i);
    expect(joined).toMatch(/cold/i);
    expect(joined).toMatch(/cycle/i);
    expect(joined).toMatch(/untrusted/i);
    // And a replay never reached for the socket.
    expect(daemon.methods().some((m) => m.startsWith("agent."))).toBe(false);
  });
});
