import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { costLine, formatUSD, totalCost } from "../src/lib/cost";
import { LivenessNotice, RecoveryNotice, Uncertain, ValidationNotice } from "../src/lib/uncertain";

afterEach(cleanup);

describe("honest uncertainty", () => {
  it("never renders unknown cost as zero, a dash, or blank", () => {
    const cost = costLine({ state: "unknown", reason: "provider-omitted-usage" });
    render(<Uncertain<number> value={{ state: "unknown", reason: cost.detail ?? "" }} render={(usd) => `$${formatUSD(usd)}`} />);
    const output = screen.getByText(/unknown/).textContent ?? "";
    expect(output).toContain("provider reported no token counts");
    expect(output).not.toContain("$0.00");
    expect(output.trim()).not.toBe("—");
    expect(cost.text).not.toContain("$0.00");
    expect(costLine({ state: "known" }).text).toContain("unknown");
  });

  it("spells a KNOWN zero instead of printing the forbidden literal (D9’s TS mirror, S18 T10)", () => {
    // Go's MoneyString spells `usd == 0` out rather than print $0.00; the TS
    // renderer is the second renderer of the same computation (D16) and must
    // not be the surface where a real zero and a coerced one become
    // indistinguishable. A free-tier route is the row that finds the hole.
    const cost = costLine({ state: "known", usd: 0 });
    expect(cost.text).not.toContain("$0.00");
    expect(cost.text).toContain("zero cost — usage was reported and priced to nothing");
    // And a small known cost keeps its digits honest — Go's form, scientific,
    // because five decimals of a token-cheap price starts "$0.00x".
    expect(costLine({ state: "known", usd: 0.0000031 }).text).toContain("$3.10e-06");
    expect(costLine({ state: "known", usd: 0.0000031 }).text).not.toContain("$0.00");
  });

  it("words a partial cost and a total over uncertain parts as floors", () => {
    expect(costLine({ state: "partial", usdAtLeast: 0.018 }).text).toContain("at least $0.0180");
    const total = totalCost([
      { state: "known", usd: 0.0312 },
      { state: "partial", usdAtLeast: 0.018 },
      { state: "unknown", reason: "no-usage" },
    ]);
    expect(total.state).toBe("partial");
    if (total.state === "partial") {
      expect(total.atLeast).toBeCloseTo(0.0492);
      render(<Uncertain value={total} render={(usd) => `$${formatUSD(usd)}`} />);
      expect(screen.getByText(/at least \$0\.0492/)).toBeTruthy();
    }
  });

  it("does not show a zero floor when every cost is unmeasured", () => {
    const total = totalCost([{ state: "unknown", reason: "provider-omitted-usage" }, undefined]);
    expect(total.state).toBe("unknown");
    render(<Uncertain value={total} render={(usd) => `$${formatUSD(usd)}`} />);
    const output = screen.getByText(/unknown/).textContent ?? "";
    expect(output).toContain("no measured cost");
    expect(output).not.toContain("$0.00");
    expect(totalCost([{ state: "partial", usdAtLeast: 0 }]).state).toBe("partial");
  });

  it("keeps validation's unverifiable state distinct and names its slice", () => {
    render(<div>
      <ValidationNotice state="valid" message="ok" />
      <ValidationNotice state="invalid" message="bad reference" />
      <ValidationNotice state="unverifiable" message="code action registry missing" slice="S16" />
    </div>);
    expect(screen.getByText(/cannot verify/).getAttribute("data-uncertainty")).toBe("unknown");
    expect(screen.getByText(/cannot verify/).textContent).toContain("S16");
    expect(screen.getByText(/invalid/).getAttribute("data-uncertainty")).toBe("partial");
    expect(screen.getByText(/^valid —/).getAttribute("data-uncertainty")).toBe("known");
  });

  it("names the tool and receipt in a failed recovery", () => {
    render(<RecoveryNotice outcome="unknown" tool="bash" receiptState="started-without-completion" />);
    expect(screen.getByText(/tool bash/).textContent).toContain("receipt started-without-completion");
  });

  it("keeps stale and disconnected liveness explicit", () => {
    render(<div>
      <LivenessNotice value={{ state: "current" }} />
      <LivenessNotice value={{ state: "stale", ageSeconds: 40, cause: "reconnecting", endpoint: "/tmp/rpc.sock" }} />
      <LivenessNotice value={{ state: "disconnected", reason: "gave up", endpoint: "/tmp/rpc.sock" }} />
    </div>);
    expect(screen.getByText(/stale for 40s/).textContent).toContain("/tmp/rpc.sock");
    expect(screen.getByText(/disconnected/).textContent).toContain("gave up");
    expect(screen.getByText("current")).toBeTruthy();
  });
});
