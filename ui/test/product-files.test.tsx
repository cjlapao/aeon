import { afterEach, beforeEach, expect, it } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { StepScreen } from "../src/features/loops/pages/StepScreen";
import { LoopFiles } from "../src/features/loops/components/LoopFiles";
import { LiveController } from "../src/lib/live";
import { installFakeDaemon, resetFakeDaemon } from "./helpers";

beforeEach(() => { vi.stubGlobal("ResizeObserver", class { observe() {} disconnect() {} }); window.location.hash = "#/loops"; });
afterEach(() => { cleanup(); resetFakeDaemon(); window.location.hash = "#/"; vi.unstubAllGlobals(); });

describe("S9 product surfaces", () => {
  it("opens generated Markdown and stored documents from the loop file panel", async () => {
    const daemon = installFakeDaemon({ responses: {
      "loop.files": { path: ".", files: [{ name: "discovery.md", path: "discovery.md", kind: "file" }] },
      "loop.file.read": { path: "discovery.md", content: "# Discovery\n\nThe workspace finding.", bytes: 35 },
      "loop.read": (params: unknown) => (params as { name?: string }).name ?
        { file: "security.md", content: "# Security\n" } : { documents: [{ file: "security.md", bytes: 20, writtenBy: "reviewer" }] },
    } });
    render(<LoopFiles id="l1" slug="l1" root="/tmp/repo" produced={[]} live={{ state: "current", endpoint: "test", daemonVersion: "1", protocolVersion: 1, generation: 1 }} />);
    fireEvent.click(await screen.findByRole("button", { name: "◇ discovery.md" }));
    expect(await screen.findByRole("heading", { name: "Discovery" })).toBeTruthy();
    expect(screen.getByText("The workspace finding.")).toBeTruthy();
    expect(daemon.paramsOf("loop.file.read")).toEqual({ id: "l1", path: "discovery.md" });
    fireEvent.click(screen.getByRole("button", { name: "security.md" }));
    await waitFor(() => expect(daemon.paramsOf("loop.read")).toEqual({ slug: "l1", componentRoot: "/tmp/repo", name: "security.md" }));
  });
  it("shows changed and missing files distinctly and locates a compacted summary", async () => {
    installFakeDaemon({ responses: { "step.show": { id: 4, agent: "surveyor", phase: "build", status: "done",
      cost: { state: "partial", usdAtLeast: 0.23 }, contextManifest: { files: [{ path: "README.md", hash: "sha256:old" }] },
      messages: [
        { role: "user", content: [{ kind: "text", text: "Original request" }] },
        { role: "user", content: [{ kind: "text", text: "Earlier step context, compacted: summary of prior work" }] },
        { role: "user", content: [{ kind: "text", text: "Changed while this step waited: README.md\nMissing while this step waited: PLAN.md. The original file content remains in the earlier transcript." }] },
      ], recovery: { case: "C", reissued: false, failure: { kind: "receipt-unknown", tool: "bash", detail: "outcome unknown" } } },
      "subscribe": { subscriptionId: "s1", from: 0 } } });
    render(<MemoryRouter initialEntries={["/steps/4"]}><Routes><Route path="/steps/:id" element={<StepScreen
      controller={new LiveController()} live={{ state: "current", endpoint: "/tmp/aeon.sock", daemonVersion: "1", protocolVersion: 1, generation: 1 }} />}
      /></Routes></MemoryRouter>);
    await screen.findByText(/Earlier context compacted at transcript entry 2/);
    expect(document.querySelector('[data-change-kind="changed"]')?.textContent).toContain("README.md");
    expect(document.querySelector('[data-change-kind="missing"]')?.textContent).toContain("PLAN.md");
    expect(screen.getByText(/Earlier context compacted at transcript entry 2/)).toBeTruthy();
    expect(screen.getByText(/tool bash · receipt receipt-unknown/)).toBeTruthy();
  });

  it("marks files from structured resume observations while keeping original hashes", async () => {
    installFakeDaemon({ responses: { "step.show": { id: 4, agent: "surveyor", phase: "build", status: "done",
      cost: { state: "unknown", reason: "price unavailable" }, contextManifest: {
        files: [{ path: "README.md", hash: "sha256:old" }, { path: "PLAN.md", hash: "sha256:original" }],
        observedChanges: [{ path: "README.md", kind: "changed" }, { path: "PLAN.md", kind: "missing" }],
      }, messages: [] }, "subscribe": { subscriptionId: "s1", from: 0 } } });
    render(<MemoryRouter initialEntries={["/steps/4"]}><Routes><Route path="/steps/:id" element={<StepScreen
      controller={new LiveController()} live={{ state: "current", endpoint: "/tmp/aeon.sock", daemonVersion: "1", protocolVersion: 1, generation: 1 }} />}
      /></Routes></MemoryRouter>);
    await screen.findByText(/Changed while suspended: README.md/);
    expect(document.querySelector('[data-change-kind="missing"]')?.textContent).toContain("PLAN.md");
    expect(screen.getByText(/README.md · sha256:old/)).toBeTruthy();
    expect(screen.getByText(/PLAN.md · sha256:original/)).toBeTruthy();
  });
});
