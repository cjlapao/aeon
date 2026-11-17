import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { ToastProvider, ToastViewport } from "@cjlapao/ui-kit";
import { RpcError } from "./bridge";
import { useAppToast, type AppToast } from "./useAppToast";

// An outcome goes away by itself and takes no room; a failure stays until it is dismissed.

let notify: AppToast;
function Probe() {
  notify = useAppToast();
  return null;
}

function mount() {
  return render(
    <ToastProvider>
      <ToastViewport />
      <Probe />
    </ToastProvider>,
  );
}

beforeEach(() => vi.useFakeTimers());
afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

describe("useAppToast", () => {
  it("lets a success go away on its own", () => {
    mount();
    act(() => void notify.success("Saved — the daemon edited test"));
    expect(screen.getByText("Saved — the daemon edited test")).toBeTruthy();
    act(() => void vi.advanceTimersByTime(30_000));
    expect(screen.queryByText("Saved — the daemon edited test")).toBeNull();
  });

  it("keeps a failure until it is dismissed, and prints the daemon's three parts as written", () => {
    mount();
    const failure = new RpcError(-32000, "refused: nothing to delete", { what: "no such row", means: "it is already gone", todo: "refresh the list", fatal: false } as never, "rpc");
    act(() => void notify.error(failure));
    act(() => void vi.advanceTimersByTime(60_000));
    expect(screen.getByText("refused: nothing to delete")).toBeTruthy();
    expect(screen.getByTestId("toast-error-parts").textContent).toContain("refresh the list");

    fireEvent.click(screen.getAllByRole("button", { name: /close|dismiss/i })[0] as HTMLElement);
    act(() => void vi.advanceTimersByTime(1000));
    expect(screen.queryByText("refused: nothing to delete")).toBeNull();
  });

  it("keeps a plain-text failure and a warning until dismissed too", () => {
    mount();
    act(() => {
      notify.error("The daemon is not reachable");
      notify.warning("Unsaved changes");
    });
    act(() => void vi.advanceTimersByTime(60_000));
    expect(screen.getByText("The daemon is not reachable")).toBeTruthy();
    expect(screen.getByText("Unsaved changes")).toBeTruthy();
  });
});
