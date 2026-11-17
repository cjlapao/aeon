import { describe, expect, it } from "vitest";
import { elapsedMs, formatMs } from "../src/lib/duration";

describe("formatMs", () => {
  it("scales the unit to the magnitude", () => {
    expect(formatMs(450)).toBe("450ms");
    expect(formatMs(9_000)).toBe("9s");
    expect(formatMs(65_000)).toBe("1m 5s");
    expect(formatMs(120_000)).toBe("2m");
    expect(formatMs(3_600_000)).toBe("1h");
    expect(formatMs(5_400_000)).toBe("1h 30m");
  });

  it("renders a dash rather than NaN for missing values", () => {
    // A task that never ran has no duration; "—" reads as absent, "NaNms" as broken.
    expect(formatMs(null)).toBe("—");
    expect(formatMs(undefined)).toBe("—");
    expect(formatMs(Number.NaN)).toBe("—");
  });
});

describe("elapsedMs", () => {
  it("measures from an ISO timestamp", () => {
    const now = Date.parse("2026-08-19T12:00:00.000Z");
    expect(elapsedMs("2026-08-19T11:59:00.000Z", now)).toBe(60_000);
  });

  it("clamps a future timestamp to zero", () => {
    // Clock skew between the API host and this machine must not render "-3s".
    const now = Date.parse("2026-08-19T12:00:00.000Z");
    expect(elapsedMs("2026-08-19T12:00:05.000Z", now)).toBe(0);
  });

  it("returns null for absent or unparseable input", () => {
    expect(elapsedMs(null)).toBeNull();
    expect(elapsedMs("not a date")).toBeNull();
  });
});
