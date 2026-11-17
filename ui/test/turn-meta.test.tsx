import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { TurnMeta } from "../src/components/agent-chat/TurnMeta";

afterEach(cleanup);

describe("the turn line", () => {
  it("shows tokens, the call's cost, its time and the step's running total", () => {
    render(<TurnMeta item={{ id: "t", kind: "turn", agent: "a", inputTokens: 59, outputTokens: 50, reasoningTokens: 45, wallMs: 2100,
      cost: { state: "known", usd: 0.000927 }, stepCost: { state: "known", usd: 0.002 } }} />);
    const text = screen.getByTestId("agent-chat-turn").textContent ?? "";
    expect(text).toContain("59 in / 50 out / 45 thinking");
    expect(text).toContain("$0.000927");
    expect(text).toContain("step so far $0.002");
  });

  it("says a floor is a floor and an unreported cost is unreported, never $0", () => {
    render(<TurnMeta item={{ id: "t", kind: "turn", agent: "a", cost: { state: "unknown" }, stepCost: { state: "partial", usdAtLeast: 0.5 } }} />);
    const text = screen.getByTestId("agent-chat-turn").textContent ?? "";
    expect(text).toContain("cost not reported");
    expect(text).toContain("step so far ≥ $0.5");
    expect(text).not.toContain("$0.00");
  });
});
