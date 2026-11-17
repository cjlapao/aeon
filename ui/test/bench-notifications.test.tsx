import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { NotificationsPage } from "../src/features/test-bench/pages/NotificationsPage";
import { notifications } from "../src/lib/notifications";

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe("the bench notifications page", () => {
  it("sends a toast immediately, with no delay", () => {
    const push = vi.spyOn(notifications, "push");
    render(<NotificationsPage />);
    fireEvent.click(screen.getByRole("button", { name: "Send test toast" }));
    expect(push).toHaveBeenCalledWith(expect.objectContaining({ title: "Test toast", tone: "info" }));
  });

  it("sends the selected tone", () => {
    const push = vi.spyOn(notifications, "push");
    render(<NotificationsPage />);
    fireEvent.click(screen.getByRole("radio", { name: "Warning" }));
    fireEvent.click(screen.getByRole("button", { name: "Send test toast" }));
    expect(push).toHaveBeenCalledWith(expect.objectContaining({ tone: "warning" }));
  });

  it("delays the system notification by 5 seconds, counting down, and sends only once it elapses", async () => {
    vi.useFakeTimers();
    const push = vi.spyOn(notifications, "push");
    render(<NotificationsPage />);

    fireEvent.click(screen.getByRole("button", { name: /Send system notification/ }));
    expect(screen.getByTestId("bench-notifications-countdown").textContent).toBe("Sending in 5s…");
    expect(push).not.toHaveBeenCalled();

    await act(() => vi.advanceTimersByTimeAsync(4000));
    expect(screen.getByTestId("bench-notifications-countdown").textContent).toBe("Sending in 1s…");
    expect(push).not.toHaveBeenCalled();

    await act(() => vi.advanceTimersByTimeAsync(1000));
    expect(push).toHaveBeenCalledWith(expect.objectContaining({ title: "Test system notification", tone: "info" }));
    expect(screen.queryByTestId("bench-notifications-countdown")).toBeNull();
  });

  it("cancels a pending system notification without sending it", async () => {
    vi.useFakeTimers();
    const push = vi.spyOn(notifications, "push");
    render(<NotificationsPage />);

    fireEvent.click(screen.getByRole("button", { name: /Send system notification/ }));
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(screen.queryByTestId("bench-notifications-countdown")).toBeNull();

    await act(() => vi.advanceTimersByTimeAsync(10_000));
    expect(push).not.toHaveBeenCalled();
  });

  it("shows whether the window is currently focused, live", () => {
    vi.spyOn(document, "hasFocus").mockReturnValue(true);
    render(<NotificationsPage />);
    expect(screen.getByTestId("bench-notifications-focus").textContent).toBe("focused");
    act(() => window.dispatchEvent(new Event("blur")));
    expect(screen.getByTestId("bench-notifications-focus").textContent).toBe("unfocused");
    act(() => window.dispatchEvent(new Event("focus")));
    expect(screen.getByTestId("bench-notifications-focus").textContent).toBe("focused");
  });
});
