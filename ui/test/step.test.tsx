import { describe, expect, it, beforeEach, afterEach } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { StepPage, lowersReasoning, outcomeTone } from "../src/features/test-bench/pages/StepPage";
import { installFakeDaemon, routeView, routesList, type FakeDaemon } from "./helpers";
import { resetBridge } from "../src/lib/bridge";

// S5's test-bench surface (AGENTS §4b).
//
// The assertions are about the three things this page exists to make visible:
// the step as it happens, the grant list as a decision somebody makes, and
// four outcomes that look different from each other.
//
// # The security-shaped one
//
// **The page must never send `bash` in the grant list unless somebody ticked
// the box.** Asserted on the recorded params rather than on the checkbox's
// state: the fake daemon records every call, so the test reads what was
// actually sent.

const okResult = {
  stepId: 7,
  kind: "complete",
  outcome: "ok",
  handoff: { status: "ok", summary: "counted 17 files" },
  usage: { inputTokens: 1200, outputTokens: 95 },
  cost: { state: "known", usd: 0.0041 },
  iterations: 3,
};

function install(responses: Record<string, unknown> = {}): FakeDaemon {
  return installFakeDaemon({
    responses: {
      // A step needs a route: the daemon refuses to guess among several, so
      // the page has to name one.
      "routes.list": routesList([
        routeView({ id: "anthropic", provider: "anthropic", model: "claude-sonnet-5" }),
      ]),
      subscribe: { subscriptionId: "sub-1", seq: 1 },
      step: okResult,
      "step.show": { id: 7, status: "done", componentRoot: "/repo", cost: okResult.cost, messages: [] },
      ...responses,
    },
  });
}

beforeEach(() => resetBridge());
afterEach(() => {
  cleanup();
  resetBridge();
});

async function fillIn(prompt = "count the files in docs/", workspace = "/repo") {
  await waitFor(() => expect(screen.getByTestId("step-submit")).toBeTruthy());
  fireEvent.change(screen.getByTestId("step-workspace"), { target: { value: workspace } });
  fireEvent.change(screen.getByTestId("step-prompt"), { target: { value: prompt } });
}

describe("the step page", () => {
  it("grants question only on request and sends declared context files", async () => {
    const daemon = install();
    render(<StepPage />);
    await fillIn("read and ask", "/repo");
    fireEvent.change(screen.getByTestId("step-context-files"), { target: { value: "README.md, docs/plan.md\nnotes.txt" } });
    fireEvent.click(screen.getByTestId("step-allow-question"));
    fireEvent.click(screen.getByTestId("step-submit"));
    await waitFor(() => expect(daemon.methods()).toContain("step"));
    const params = daemon.paramsOf("step") as { tools: string[]; contextFiles: string[] };
    expect(params.tools).toEqual(["read", "list", "write_file", "question"]);
    expect(params.tools).not.toContain("bash");
    expect(params.contextFiles).toEqual(["README.md", "docs/plan.md", "notes.txt"]);
  });

  it("reopens a suspended step and answers through the existing subscription", async () => {
    let answered = false;
    const question = { id: 9, stepId: 42, kind: "choice", body: "Small edit or rewrite?", options: ["small edit", "rewrite"], toolUseId: "q1" };
    const before = { id: 42, status: "suspended", componentRoot: "/repo", cost: { state: "known", usd: 0.01 }, question,
      messages: [{ role: "assistant", content: [{ kind: "tool_use", id: "q1", name: "question" }] }] };
    const after = { ...before, status: "done", question: undefined,
      messages: [...before.messages, { role: "user", content: [{ kind: "tool_result", toolUseId: "q1", content: "small edit" }] }] };
    const daemon = install({
      "step.show": () => answered ? after : before,
      answer: () => { answered = true; return { stepId: 42, questionId: 9, answeredAt: "2026-10-17T12:00:00Z", status: "running" }; },
      "step.verify": { ok: true, messages: 2, signatureLength: 0, digest: "abc" },
    });
    const page = render(<StepPage />);
    await waitFor(() => expect(screen.getByTestId("step-load")).toBeTruthy());
    fireEvent.change(screen.getByTestId("step-lookup-id"), { target: { value: "42" } });
    fireEvent.click(screen.getByTestId("step-load"));
    await screen.findByTestId("step-question");
    expect(screen.getByTestId("step-question").textContent).toContain("Small edit or rewrite?");
    expect(screen.getByTestId("step-saved").textContent).toContain("question results0");
    fireEvent.change(screen.getByTestId("step-answer-choice"), { target: { value: "small edit" } });
    fireEvent.click(screen.getByTestId("step-answer-submit"));
    await waitFor(() => expect(daemon.methods()).toContain("answer"));
    expect(daemon.methods().indexOf("subscribe")).toBeLessThan(daemon.methods().indexOf("answer"));
    expect(daemon.paramsOf("answer")).toEqual({ stepId: 42, questionId: 9, answer: "small edit" });
    await waitFor(() => expect(screen.getByTestId("step-saved").textContent).toContain("question results1"));
    fireEvent.click(screen.getByTestId("step-verify"));
    expect((await screen.findByTestId("step-verify-result")).textContent).toContain("verified");
    page.unmount();
    await waitFor(() => expect(daemon.methods()).toContain("unsubscribe"));
    render(<StepPage />);
    await waitFor(() => expect(screen.getByTestId("step-load")).toBeTruthy());
    fireEvent.change(screen.getByTestId("step-lookup-id"), { target: { value: "42" } });
    fireEvent.click(screen.getByTestId("step-load"));
    await waitFor(() => expect(screen.getByTestId("step-saved").textContent).toContain("question results1"));
    expect(screen.queryByTestId("step-question")).toBeNull();
  });

  it("shows changed-file notes and compaction records from a saved step", async () => {
    install({ "step.show": { id: 51, status: "done", componentRoot: "/repo", cost: { state: "partial", usdAtLeast: 0.02, missing: ["cacheRead"] },
      contextManifest: { compactions: [{ start: 2, end: 5, summary: "the earlier work", estimateBefore: 300, estimateAfter: 60, cost: { usd: 0.001 } }] },
      messages: [{ role: "user", content: [{ kind: "text", text: "Changed while this step waited: README.md" }] }] } });
    render(<StepPage />);
    await waitFor(() => expect(screen.getByTestId("step-load")).toBeTruthy());
    fireEvent.change(screen.getByTestId("step-lookup-id"), { target: { value: "51" } });
    fireEvent.click(screen.getByTestId("step-load"));
    expect((await screen.findByTestId("step-file-note")).textContent).toContain("README.md");
    expect(screen.getByTestId("step-compactions").textContent).toContain("the earlier work");
    expect(screen.getByTestId("step-saved").textContent).toContain("at least");
  });

  it("accepts an empty free-form answer and does not offer it twice", async () => {
    let answered = false;
    const daemon = install({
      "step.show": () => ({ id: 52, status: answered ? "done" : "suspended", componentRoot: "/repo", cost: { state: "unknown" }, messages: [],
        question: answered ? undefined : { id: 12, stepId: 52, kind: "free", body: "Any other context?" } }),
      answer: () => { answered = true; return { stepId: 52, questionId: 12, answeredAt: "2026-10-17T12:00:00Z", status: "running" }; },
    });
    render(<StepPage />);
    await waitFor(() => expect(screen.getByTestId("step-load")).toBeTruthy());
    fireEvent.change(screen.getByTestId("step-lookup-id"), { target: { value: "52" } });
    fireEvent.click(screen.getByTestId("step-load"));
    await screen.findByTestId("step-question");
    expect((screen.getByTestId("step-answer-submit") as HTMLButtonElement).disabled).toBe(false);
    fireEvent.click(screen.getByTestId("step-answer-submit"));
    await waitFor(() => expect(daemon.methods()).toContain("answer"));
    expect((daemon.paramsOf("answer") as { answer: string }).answer).toBe("");
    await waitFor(() => expect(screen.queryByTestId("step-question")).toBeNull());
  });

  it("refreshes an asynchronous continuation when the daemon saves it", async () => {
    let state: "suspended" | "running" | "done" = "suspended";
    const daemon = install({
      "step.show": () => ({ id: 53, status: state, componentRoot: "/repo", cost: { state: "known", usd: 0.02 }, messages: [],
        question: state === "suspended" ? { id: 13, stepId: 53, kind: "free", body: "Continue?" } : undefined }),
      answer: () => { state = "running"; return { stepId: 53, questionId: 13, answeredAt: "2026-10-17T12:00:00Z", status: "running" }; },
    });
    render(<StepPage />);
    await waitFor(() => expect(screen.getByTestId("step-load")).toBeTruthy());
    fireEvent.change(screen.getByTestId("step-lookup-id"), { target: { value: "53" } });
    fireEvent.click(screen.getByTestId("step-load"));
    await screen.findByTestId("step-question");
    fireEvent.click(screen.getByTestId("step-answer-submit"));
    await screen.findByTestId("step-resuming");
    act(() => {
      daemon.emit({ subscriptionId: "sub-1", seq: 90, kind: "model.delta", payload: { stepId: 53, text: "Continuing." } });
      state = "done";
      daemon.emit({ subscriptionId: "sub-1", seq: 91, kind: "step.saved", payload: { stepId: 53, outcome: "ok" } });
    });
    await waitFor(() => expect(screen.getByTestId("step-saved").textContent).toContain("done"));
    expect(screen.getByTestId("step-stream").textContent).toContain("Continuing.");
    expect(screen.queryByTestId("step-resuming")).toBeNull();
  });
  it("sends the workspace, the prompt and workspace file tools", async () => {
    const daemon = install();
    render(<StepPage />);
    await fillIn();

    fireEvent.click(screen.getByTestId("step-submit"));
    await waitFor(() => expect(daemon.methods()).toContain("step"));

    const params = daemon.paramsOf("step") as Record<string, unknown>;
    expect(params.prompt).toBe("count the files in docs/");
    expect(params.workspaceRoot).toBe("/repo");
    // The grant, as sent. `read` and nothing else.
    expect(params.tools).toEqual(["read", "list", "write_file"]);
  });

  it("never sends bash unless somebody ticked the box", async () => {
    // D1 at the surface a person touches. Asserted on what was SENT, because
    // the checkbox being off is a claim about the page and the params are a
    // claim about the daemon.
    const daemon = install();
    render(<StepPage />);
    await fillIn();

    fireEvent.click(screen.getByTestId("step-submit"));
    await waitFor(() => expect(daemon.methods()).toContain("step"));

    const params = daemon.paramsOf("step") as { tools?: string[] };
    expect(params.tools).not.toContain("bash");

    // And no warning is shown, because a warning on every run is a warning
    // nobody reads.
    expect(screen.queryByTestId("bash-warning")).toBeNull();
  });

  it("warns before the run when the shell is granted, and then sends it", async () => {
    const daemon = install();
    render(<StepPage />);
    await fillIn();

    fireEvent.click(screen.getByTestId("step-allow-bash"));

    // The warning appears BEFORE the step runs. A warning in the result would
    // be a warning after the command.
    const warning = await screen.findByTestId("bash-warning");
    expect(warning.textContent).toContain("no command policy");
    expect(warning.textContent).toContain("bounded by a timeout");
    expect(daemon.methods()).not.toContain("step");

    fireEvent.click(screen.getByTestId("step-submit"));
    await waitFor(() => expect(daemon.methods()).toContain("step"));
    expect((daemon.paramsOf("step") as { tools: string[] }).tools).toEqual(["read", "list", "write_file", "bash"]);
  });

  it("subscribes before it calls step", async () => {
    // The subscription must exist before the call that streams into it.
    // Doing it the other way round works almost always and fails on a fast
    // step — which is the worst kind of ordering bug.
    const daemon = install();
    render(<StepPage />);
    await fillIn();

    fireEvent.click(screen.getByTestId("step-submit"));
    await waitFor(() => expect(daemon.methods()).toContain("step"));

    const order = daemon.methods();
    expect(order.indexOf("subscribe")).toBeLessThan(order.indexOf("step"));
    expect((daemon.paramsOf("step") as { subscriptionId: string }).subscriptionId).toBe("sub-1");
  });

  it("requires a workspace, because the daemon has no default for it", async () => {
    install();
    render(<StepPage />);
    await waitFor(() => expect(screen.getByTestId("step-submit")).toBeTruthy());

    fireEvent.change(screen.getByTestId("step-prompt"), { target: { value: "do it" } });
    expect((screen.getByTestId("step-submit") as HTMLButtonElement).disabled).toBe(true);

    fireEvent.change(screen.getByTestId("step-workspace"), { target: { value: "/repo" } });
    await waitFor(() =>
      expect((screen.getByTestId("step-submit") as HTMLButtonElement).disabled).toBe(false),
    );
  });

  it("shows the step as it happens rather than at the end", async () => {
    // K19. The whole reason events are emitted as they happen is that
    // somebody is watching.
    const daemon = installFakeDaemon({
      responses: {
        "routes.list": routesList([routeView({ id: "anthropic" })]),
        subscribe: { subscriptionId: "sub-1", seq: 1 },
        // A step that never answers, so the only thing on screen is the
        // stream — which is what a long step looks like.
        step: () => new Promise(() => {}),
      },
    });
    render(<StepPage />);
    await fillIn();
    fireEvent.click(screen.getByTestId("step-submit"));
    await waitFor(() => expect(daemon.methods()).toContain("step"));

    act(() => {
      daemon.emit({
        kind: "step.started",
        payload: { summary: "1 context section(s), 1 tool(s) offered" },
      });
      daemon.emit({ kind: "model.delta", payload: { text: "I will read " } });
      daemon.emit({ kind: "model.delta", payload: { text: "the docs." } });
      daemon.emit({ kind: "step.tool.start", payload: { tool: "read", summary: "docs/plan.md" } });
      daemon.emit({ kind: "step.tool.end", payload: { tool: "read", result: "docs/plan.md\n2.0 KiB" } });
    });

    const stream = await screen.findByTestId("step-stream");
    // Consecutive deltas are joined, so the model's prose reads as prose.
    expect(stream.textContent).toContain("I will read the docs.");
    expect(stream.textContent).toContain("→ read docs/plan.md");
    expect(stream.textContent).toContain("← docs/plan.md");
    // And the result is not on screen, because the step has not finished.
    expect(screen.queryByTestId("step-outcome")).toBeNull();
  });

  it("shows each subscription event once, even when delivered twice", async () => {
    const daemon = installFakeDaemon({ responses: {
      "routes.list": routesList([routeView()]),
      subscribe: { subscriptionId: "sub-1", seq: 1 },
      step: () => new Promise(() => {}),
    } });
    render(<StepPage />);
    await fillIn();
    fireEvent.click(screen.getByTestId("step-submit"));
    await waitFor(() => expect(daemon.methods()).toContain("step"));
    act(() => {
      const event = { subscriptionId: "sub-1", seq: 10, kind: "model.delta", payload: { text: "Once." } };
      daemon.emit(event);
      daemon.emit(event);
      daemon.emit({ ...event, subscriptionId: "old-sub", seq: 11, payload: { text: "Old." } });
    });
    expect(screen.getByTestId("step-stream").textContent).toContain("Once.");
    expect(screen.getByTestId("step-stream").textContent).not.toContain("Once.Once.");
    expect(screen.getByTestId("step-stream").textContent).not.toContain("Old.");
  });

  it("releases its daemon subscription when the tab closes", async () => {
    const daemon = install();
    const page = render(<StepPage />);
    await fillIn();
    fireEvent.click(screen.getByTestId("step-submit"));
    await waitFor(() => expect(daemon.methods()).toContain("step"));
    page.unmount();
    await waitFor(() => expect(daemon.methods()).toContain("unsubscribe"));
    expect(daemon.paramsOf("unsubscribe")).toEqual({ subscriptionId: "sub-1" });
  });

  it("marks a failed tool result as an error without ending the step", async () => {
    // O5/O6: a tool that failed is a result the model can recover from. The
    // page shows it as an error line and keeps streaming.
    const daemon = installFakeDaemon({
      responses: {
        "routes.list": routesList([routeView({ id: "anthropic" })]),
        subscribe: { subscriptionId: "sub-1", seq: 1 },
        step: () => new Promise(() => {}),
      },
    });
    render(<StepPage />);
    await fillIn();
    fireEvent.click(screen.getByTestId("step-submit"));
    await waitFor(() => expect(daemon.methods()).toContain("step"));

    act(() => {
      daemon.emit({
        kind: "step.tool.end",
        payload: { tool: "read", isError: true, result: "read refused \"/etc/passwd\": absolute" },
      });
      daemon.emit({ kind: "model.delta", payload: { text: "I will try a relative path." } });
    });

    const stream = await screen.findByTestId("step-stream");
    expect(stream.textContent).toContain("✗ read refused");
    expect(stream.textContent).toContain("I will try a relative path.");
  });

  it("truncates a long tool result to one line", async () => {
    // A tool result with 400 lines of output would bury everything after it.
    const daemon = installFakeDaemon({
      responses: {
        "routes.list": routesList([routeView({ id: "anthropic" })]),
        subscribe: { subscriptionId: "sub-1", seq: 1 },
        step: () => new Promise(() => {}),
      },
    });
    render(<StepPage />);
    await fillIn();
    fireEvent.click(screen.getByTestId("step-submit"));
    await waitFor(() => expect(daemon.methods()).toContain("step"));

    act(() => {
      daemon.emit({
        kind: "step.tool.end",
        payload: { tool: "bash", result: `${"x".repeat(500)}\nand more` },
      });
    });

    const trace = await screen.findAllByTestId("step-trace");
    for (const line of trace) {
      expect((line.textContent ?? "").length).toBeLessThan(200);
      expect(line.textContent).not.toContain("and more");
    }
  });

  it("shows the outcome, the handoff and the accounting", async () => {
    install();
    render(<StepPage />);
    await fillIn();
    fireEvent.click(screen.getByTestId("step-submit"));

    const outcome = await screen.findByTestId("step-outcome");
    expect(screen.getByTestId("step-outcome-pill").textContent).toBe("ok");
    expect(outcome.textContent).toContain("step 7");
    expect(screen.getByTestId("step-handoff").textContent).toContain("counted 17 files");
    expect(outcome.textContent).toContain("3");
    // And it says where the transcript is, because the step is the thing a
    // person will want to read afterwards.
    expect(screen.getByTestId("step-where-stored").textContent).toContain("step 7");
  });

  it("renders a partial cost as `at least` rather than as a figure", async () => {
    // S3's D12, at the last surface. An unknown cost shown as `$0.00` is the
    // dishonesty the three-state design exists to prevent.
    install({
      step: {
        ...okResult,
        cost: { state: "partial", usdAtLeast: 0.0041, missing: ["cacheWrite"] },
      },
    });
    render(<StepPage />);
    await fillIn();
    fireEvent.click(screen.getByTestId("step-submit"));

    const line = await screen.findByTestId("step-cost-line");
    expect(line.textContent).toMatch(/at least/i);
    expect(line.textContent).toContain("cacheWrite");
  });

  it("renders an unknown cost without inventing a number", async () => {
    install({
      step: {
        ...okResult,
        cost: { state: "unknown", reason: "this route has no prices" },
      },
    });
    render(<StepPage />);
    await fillIn();
    fireEvent.click(screen.getByTestId("step-submit"));

    const line = await screen.findByTestId("step-cost-line");
    expect(line.textContent).not.toContain("$0.00");
    expect(line.textContent).toMatch(/not reported|unknown/i);
  });

  it("distinguishes the four outcomes, and a reject is not an alarm", async () => {
    // A handler that considered the question and answered no did its job. A
    // UI that painted that red would teach a person to treat a considered no
    // as a bug.
    expect(outcomeTone("ok")).toBe("green");
    expect(outcomeTone("reject")).toBe("slate");
    expect(outcomeTone("blocked")).toBe("amber");
    expect(outcomeTone("failed")).toBe("red");

    // Four distinct tones, so the four are actually distinguishable.
    const tones = new Set(["ok", "reject", "blocked", "failed"].map(outcomeTone));
    expect(tones.size).toBe(4);
  });

  it("shows a blocked step's reason, which names the bound it hit", async () => {
    install({
      step: {
        ...okResult,
        outcome: "blocked",
        handoff: null,
        reason: "exceeded MaxIterations (12)",
      },
    });
    render(<StepPage />);
    await fillIn();
    fireEvent.click(screen.getByTestId("step-submit"));

    const reason = await screen.findByTestId("step-reason");
    expect(reason.textContent).toContain("MaxIterations (12)");
    expect(screen.queryByTestId("step-handoff")).toBeNull();
  });

  it("reports undelivered events rather than hiding them", async () => {
    // A window that saw no events and a successful result would otherwise
    // have no way to tell which half broke.
    install({ step: { ...okResult, sinkFailures: 4 } });
    render(<StepPage />);
    await fillIn();
    fireEvent.click(screen.getByTestId("step-submit"));

    const outcome = await screen.findByTestId("step-outcome");
    expect(outcome.textContent).toContain("4 could not be delivered");
  });

  it("refuses to run when the event listener could not be installed", async () => {
    // The shape of the real bug: every `invoke` succeeds and every stream is
    // silent, because app commands are not ACL-gated and `listen` is. A page
    // that ran anyway would have the daemon do the work, bill for it, and
    // stream it into a void.
    const daemon = installFakeDaemon({
      subscribeFails: "event.listen not allowed on window main",
      responses: {
        "routes.list": routesList([routeView({ id: "anthropic" })]),
        subscribe: { subscriptionId: "s", seq: 1 },
        step: okResult,
      },
    });
    render(<StepPage />);

    await waitFor(() => expect(screen.getByTestId("step-submit")).toBeTruthy());
    fireEvent.change(screen.getByTestId("step-workspace"), { target: { value: "/repo" } });
    fireEvent.change(screen.getByTestId("step-prompt"), { target: { value: "do it" } });

    // Disabled, and the reason is on screen.
    await waitFor(() =>
      expect((screen.getByTestId("step-submit") as HTMLButtonElement).disabled).toBe(true),
    );
    expect(daemon.methods()).not.toContain("step");
  });

  it("surfaces a daemon refusal as its three parts", async () => {
    // `step` refuses a missing workspace, an unknown tool and an uncompilable
    // step with named errors. Each must reach the person as what happened,
    // what it means and what to do.
    install({
      step: () => {
        throw JSON.stringify({
          code: -31201,
          message: "a step arrived without a workspace root",
          data: {
            what: "the step named no workspaceRoot",
            means: "a step with no workspace would read the daemon's own files",
            todo: "send the client's working directory as workspaceRoot",
            fatal: false,
          },
        });
      },
    });
    render(<StepPage />);
    await fillIn();
    fireEvent.click(screen.getByTestId("step-submit"));

    await waitFor(() => expect(screen.getByText(/no workspaceRoot/i)).toBeTruthy());
    expect(screen.getByText(/read the daemon's own files/i)).toBeTruthy();
    expect(screen.getByText(/as workspaceRoot/i)).toBeTruthy();
  });

  it("carries an iteration bound when one is given, and omits it when not", async () => {
    const daemon = install();
    render(<StepPage />);
    await fillIn();

    fireEvent.change(screen.getByTestId("step-max-iterations"), { target: { value: "3" } });
    fireEvent.click(screen.getByTestId("step-submit"));
    await waitFor(() => expect(daemon.methods()).toContain("step"));
    expect((daemon.paramsOf("step") as { maxIterations?: number }).maxIterations).toBe(3);

    fireEvent.change(screen.getByTestId("step-max-iterations"), { target: { value: "" } });
    fireEvent.click(screen.getByTestId("step-submit"));
    await waitFor(() => expect(daemon.calls.filter((c) => c.method === "step").length).toBe(2));
    expect((daemon.paramsOf("step") as { maxIterations?: number }).maxIterations).toBeUndefined();
  });

  it("never asks the daemon for a credential and never sends one", async () => {
    // AGENTS §4c. A credential goes IN and never OUT, and this page has no
    // business with one at all.
    const daemon = install();
    render(<StepPage />);
    await fillIn();
    fireEvent.click(screen.getByTestId("step-submit"));
    await waitFor(() => expect(daemon.methods()).toContain("step"));

    const sent = JSON.stringify(daemon.calls);
    for (const needle of ["apiKey", "api_key", "ANTHROPIC_API_KEY", "sk-"]) {
      expect(sent).not.toContain(needle);
    }
  });
});

describe("the step page's subscription", () => {
  // # The bug these are the guard for
  //
  // The page subscribed on every run and never unsubscribed. The daemon fans
  // each event out to EVERY matching subscription on the connection, so by the
  // fourth run every event arrived four times — the stream showed four
  // "step.started" lines and four outcomes, and it read as the daemon emitting
  // duplicates.
  //
  // No test caught it because every test ran ONE step. The assertion that
  // matters is therefore about the second run, not the first.

  it("subscribes once, however many steps are run", async () => {
    const daemon = install();
    render(<StepPage />);
    await fillIn();

    for (let i = 0; i < 3; i++) {
      fireEvent.click(screen.getByTestId("step-submit"));
      await waitFor(() =>
        expect(daemon.calls.filter((c) => c.method === "step").length).toBe(i + 1),
      );
    }

    const subscribes = daemon.calls.filter((c) => c.method === "subscribe").length;
    if (subscribes !== 1) {
      throw new Error(
        `the page subscribed ${subscribes} times for 3 runs. Each extra subscription means the ` +
          `daemon delivers every event one more time, so run 3 would show 3 copies of each line`,
      );
    }
  });

  it("reuses the same subscription id on every run", async () => {
    const daemon = install();
    render(<StepPage />);
    await fillIn();

    fireEvent.click(screen.getByTestId("step-submit"));
    await waitFor(() => expect(daemon.methods()).toContain("step"));
    const first = (daemon.paramsOf("step") as { subscriptionId: string }).subscriptionId;

    fireEvent.click(screen.getByTestId("step-submit"));
    await waitFor(() => expect(daemon.calls.filter((c) => c.method === "step").length).toBe(2));
    const second = (daemon.paramsOf("step") as { subscriptionId: string }).subscriptionId;

    expect(second).toBe(first);
    expect(second).toBeTruthy();
  });

  it("clears the previous run's stream rather than appending to it", async () => {
    // The other half of what looked like duplication: a second run must not
    // show the first one's events. Both runs are held open deliberately, so
    // there is a window in which to emit.
    let release: (() => void) | undefined;
    const daemon = installFakeDaemon({
      responses: {
        "routes.list": routesList([routeView({ id: "anthropic" })]),
        subscribe: { subscriptionId: "sub-1", seq: 1 },
        step: () =>
          new Promise((resolve) => {
            release = () => resolve(okResult);
          }),
      },
    });
    render(<StepPage />);
    await fillIn();

    fireEvent.click(screen.getByTestId("step-submit"));
    await waitFor(() => expect(daemon.methods()).toContain("step"));
    act(() => {
      daemon.emit({ kind: "step.started", payload: { summary: "FIRST-RUN" } });
    });
    expect((await screen.findByTestId("step-stream")).textContent).toContain("FIRST-RUN");

    await act(async () => {
      release?.();
      await Promise.resolve();
    });
    await waitFor(() => expect(screen.getByTestId("step-outcome")).toBeTruthy());

    fireEvent.click(screen.getByTestId("step-submit"));
    await waitFor(() => expect(daemon.calls.filter((c) => c.method === "step").length).toBe(2));
    act(() => {
      daemon.emit({ kind: "step.started", payload: { summary: "SECOND-RUN" } });
    });

    const stream = await screen.findByTestId("step-stream");
    expect(stream.textContent).toContain("SECOND-RUN");
    expect(stream.textContent).not.toContain("FIRST-RUN");
  });
});

describe("the step page's reasoning effort", () => {
  it("sends nothing by default, so the step decides", async () => {
    // The step lowers a reasoning route to `none` by itself. A page that sent
    // a value would be overriding a decision it has no information for.
    const daemon = install();
    render(<StepPage />);
    await fillIn();

    fireEvent.click(screen.getByTestId("step-submit"));
    await waitFor(() => expect(daemon.methods()).toContain("step"));

    const params = daemon.paramsOf("step") as Record<string, unknown>;
    expect(params.reasoningEffort).toBeUndefined();
  });

  it("sends an explicit choice", async () => {
    const daemon = install();
    render(<StepPage />);
    await fillIn();

    fireEvent.change(screen.getByTestId("step-reasoning-effort"), { target: { value: "none" } });
    fireEvent.click(screen.getByTestId("step-submit"));
    await waitFor(() => expect(daemon.methods()).toContain("step"));

    expect((daemon.paramsOf("step") as Record<string, unknown>).reasoningEffort).toBe("none");
  });

  it("shows what the step decided, in its opening line", async () => {
    // The note rides on `step.started`, so the page needs no knowledge of what
    // a note is about — which is what keeps the next one free.
    const daemon = installFakeDaemon({
      responses: {
        "routes.list": routesList([routeView({ id: "anthropic" })]),
        subscribe: { subscriptionId: "sub-1", seq: 1 },
        step: () => new Promise(() => {}),
      },
    });
    render(<StepPage />);
    await fillIn();
    fireEvent.click(screen.getByTestId("step-submit"));
    await waitFor(() => expect(daemon.methods()).toContain("step"));

    act(() => {
      daemon.emit({
        kind: "step.started",
        payload: {
          summary:
            '0 declared file(s), 1 tool(s) offered · reasoning lowered from "medium" to `none` ' +
            "for this step, because it offers 1 tool(s) and models in this family refuse tools " +
            "with reasoning on",
        },
      });
    });

    const stream = await screen.findByTestId("step-stream");
    expect(stream.textContent).toContain("reasoning lowered");
    expect(stream.textContent).toContain("medium");
  });
});

describe("the step page's route", () => {
  // The daemon takes the only configured route when there is exactly one and
  // **refuses when there are several** — picking one would spend money on a
  // model nobody chose. A page that cannot name a route therefore works on a
  // single-route daemon and fails on everybody else's.

  it("names the chosen route in the step", async () => {
    const daemon = install({
      "routes.list": routesList([
        routeView({ id: "anthropic", provider: "anthropic", model: "claude-sonnet-5" }),
        routeView({ id: "openai", provider: "openai", model: "gpt-6-luna" }),
      ]),
    });
    render(<StepPage />);
    await fillIn();

    // The first is selected by default, and both are offered.
    const select = screen.getByTestId("step-route") as HTMLSelectElement;
    expect(select.value).toBe("anthropic");
    expect(select.querySelectorAll("option").length).toBe(2);

    fireEvent.change(select, { target: { value: "openai" } });
    fireEvent.click(screen.getByTestId("step-submit"));
    await waitFor(() => expect(daemon.methods()).toContain("step"));

    expect((daemon.paramsOf("step") as Record<string, unknown>).route).toBe("openai");
  });

  it("says so when there is nothing to run a step with", async () => {
    install({ "routes.list": routesList([]) });
    render(<StepPage />);

    const empty = await screen.findByTestId("step-no-routes");
    expect(empty.textContent).toMatch(/Providers/);
    expect(screen.queryByTestId("step-submit")).toBeNull();
  });

  it("survives a routes.list answer with no routes field", async () => {
    // The crash this is the guard for: `result.routes[0]` inside a state
    // updater throws during RENDER, so a missing field killed the whole tab
    // and the console showed a React error rather than a bad response. Same
    // lesson as S4's `pending: null`.
    install({ "routes.list": {} });
    render(<StepPage />);

    // The page is still there — either asking for a route or saying it has
    // none — rather than blank.
    await waitFor(() =>
      expect(
        screen.queryByTestId("step-no-routes") ?? screen.queryByTestId("step-prompt"),
      ).toBeTruthy(),
    );
  });

  it("warns when the chosen route has no key, before anything is spent", async () => {
    install({
      "routes.list": routesList([
        routeView({ id: "openai", provider: "openai", hasKey: false, keyHint: undefined }),
      ]),
    });
    render(<StepPage />);
    await fillIn();

    const warning = await screen.findByTestId("step-no-key-warning");
    expect(warning.textContent).toMatch(/first model call/);
  });

  it("explains that a reasoning route will be lowered for this step", async () => {
    // Worked out on the page rather than left for the person to deduce from a
    // provider's refusal — and scoped to the route they actually picked.
    install({
      "routes.list": routesList([
        routeView({ id: "openai", provider: "openai", model: "gpt-6-luna", reasoningEffort: "medium" }),
      ]),
    });
    render(<StepPage />);
    await fillIn();

    const note = await screen.findByTestId("step-reasoning-lowered");
    expect(note.textContent).toContain("medium");
    expect(note.textContent).toMatch(/only steps are lowered/i);
  });

  it("says nothing about reasoning when the route has none set", async () => {
    install({
      "routes.list": routesList([routeView({ id: "anthropic", provider: "anthropic" })]),
    });
    render(<StepPage />);
    await fillIn();

    expect(screen.queryByTestId("step-reasoning-lowered")).toBeNull();
  });
});

describe("the step page and the OpenAI endpoint", () => {
  it("says nothing about lowering on a /v1/responses route", async () => {
    // That endpoint accepts tools and reasoning together, so nothing is
    // lowered — and the page must not teach a rule that does not apply to the
    // route in front of it.
    install({
      "routes.list": routesList([
        routeView({
          id: "openai",
          provider: "openai",
          model: "gpt-6.1-sol",
          reasoningEffort: "medium",
          openaiEndpoint: "responses",
        }),
      ]),
    });
    render(<StepPage />);
    await fillIn();

    expect(screen.queryByTestId("step-reasoning-lowered")).toBeNull();
  });

  it("knows which routes lower reasoning and which do not", async () => {
    expect(lowersReasoning(routeView({ reasoningEffort: "medium" }) as never)).toBe(true);
    expect(lowersReasoning(routeView({ reasoningEffort: "none" }) as never)).toBe(false);
    expect(lowersReasoning(routeView({}) as never)).toBe(false);
    expect(
      lowersReasoning(
        routeView({ reasoningEffort: "medium", openaiEndpoint: "responses" }) as never,
      ),
    ).toBe(false);
  });
});
