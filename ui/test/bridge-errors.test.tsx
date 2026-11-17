import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { ErrorPanel } from "../src/components/ErrorPanel";
import { toRpcError } from "../src/lib/bridge";
import { onEvent, resetBridge, setSubscriber } from "../src/lib/bridge";

afterEach(() => { cleanup(); resetBridge(); });

describe("bridge error kinds", () => {
  it.each([
    ["transport", "error-transport"],
    ["timeout", "error-timeout"],
    ["rpc", "error-rpc"],
  ] as const)("renders %s distinctly without fabricated detail", (kind, testId) => {
    const error = toRpcError(JSON.stringify({ kind, code: kind === "rpc" ? -32601 : 1, message: "failed" }));
    expect(error.kind).toBe(kind);
    render(<ErrorPanel error={error} />);
    expect(screen.getByTestId(testId)).toBeTruthy();
  });

  it("preserves a daemon code even without error data", () => {
    const error = toRpcError(JSON.stringify({ kind: "rpc", code: -32601, message: "method not found" }));
    render(<ErrorPanel error={error} />);
    expect(screen.getByTestId("error-code").textContent).toContain("-32601");
  });
});

it("ignores unknown additive event kinds without disturbing known events", async () => {
  let callback: ((payload: unknown) => void) | undefined;
  setSubscriber(async (_channel, handler) => { callback = handler; return () => {}; });
  const received: unknown[] = [];
  await onEvent((event) => received.push(event));
  callback?.({ kind: "future.event", seq: 1 });
  callback?.({ kind: "loop.advanced", seq: 3 });
  expect(received).toEqual([{ kind: "loop.advanced", seq: 3 }]);
});
