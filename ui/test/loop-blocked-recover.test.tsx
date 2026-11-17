import { afterEach, describe, expect, it } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { installFakeDaemon, resetFakeDaemon } from "./helpers";
import { LoopBlockedBanner } from "../src/features/loop-workspace/LoopBlockedBanner";

afterEach(() => { cleanup(); resetFakeDaemon(); });
const loop = { id: "L1", status: "blocked", lastReason: "invalid question: json: cannot unmarshal string" } as never;
const control = { eligibility: { resume: { eligible: false, refusal: { detail: "the loop is blocked — it has its own remedy, not a resume" } } } };

describe("a blocked loop always has a way forward", () => {
  it("offers to retry the step even when resume is refused, and asks the daemon to recover it", async () => {
    const daemon = installFakeDaemon({ responses: { "loop.control": control, "loop.recover": { recovered: true, status: "running" } } });
    let resumed = 0;
    render(<LoopBlockedBanner loop={loop} canWrite onResumed={() => { resumed += 1; }} />);
    await screen.findByTestId("loop-blocked-refusal");
    expect((screen.getByTestId("loop-blocked-resume") as HTMLButtonElement).disabled).toBe(true);
    const retry = screen.getByTestId("loop-blocked-recover") as HTMLButtonElement;
    expect(retry.disabled).toBe(false);
    fireEvent.click(retry);
    await screen.findByTestId("loop-blocked-recover");
    await new Promise((r) => setTimeout(r, 20));
    expect(daemon.paramsOf("loop.recover")).toEqual({ id: "L1" });
    expect(resumed).toBe(1);
  });
  it("shows the daemon's own words when it refuses", async () => {
    installFakeDaemon({ responses: { "loop.control": control, "loop.recover": { refused: { kind: "gate", detail: "the loop is at a gate" } } } });
    render(<LoopBlockedBanner loop={loop} canWrite onResumed={() => undefined} />);
    fireEvent.click(await screen.findByTestId("loop-blocked-recover"));
    expect((await screen.findByTestId("loop-blocked-error")).textContent).toContain("at a gate");
  });
});
