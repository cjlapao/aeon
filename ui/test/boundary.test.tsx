import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { TabBoundary } from "../src/components/TabBoundary";

// A render error in one tab must not blank the app — and the boundary must
// SHOW the error rather than apologise for it.
//
// This exists because the Store tab crashed on a fresh store (one null field)
// and the whole window went empty, with the reason only in the console.

// The exact error the Store tab threw on a fresh store.
function Throws(): never {
  throw new TypeError("null is not an object (evaluating 'info.pending.length')");
}

afterEach(() => cleanup());

describe("a tab boundary", () => {
  it("renders its children when nothing throws", () => {
    render(
      <TabBoundary name="Store">
        <p data-testid="child">the page</p>
      </TabBoundary>,
    );
    expect(screen.getByTestId("child")).toBeTruthy();
    expect(screen.queryByTestId("tab-boundary")).toBeNull();
  });

  it("shows the error message rather than a friendly sentence", () => {
    // React logs the error itself; silenced so the suite's output stays
    // readable, NOT so the boundary can hide anything.
    const spy = vi.spyOn(console, "error").mockImplementation(() => {});
    render(
      <TabBoundary name="Store">
        <Throws />
      </TabBoundary>,
    );

    expect(screen.getByTestId("tab-boundary")).toBeTruthy();
    const detail = screen.getByTestId("tab-boundary-error").textContent ?? "";
    // The actual message, verbatim: the person looking at it is the person who
    // has to report it.
    expect(detail).toContain("info.pending.length");

    const whole = screen.getByTestId("tab-boundary").textContent ?? "";
    // It names which tab, and says the daemon is not the suspect.
    expect(whole).toContain("Store");
    expect(whole).toContain("not in the daemon");
    // It points at the tab that answers "is the daemon there at all".
    expect(whole).toContain("Status");
    // And it does not offer a reload, which cannot fix a render error.
    expect(whole.toLowerCase()).not.toContain("try again");

    // The console still got it: the boundary re-logs rather than swallowing.
    expect(spy).toHaveBeenCalled();
    spy.mockRestore();
  });
});
