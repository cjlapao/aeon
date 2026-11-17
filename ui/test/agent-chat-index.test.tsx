import { useState } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { AgentChat, type ChatItem, type LoopMessageIndexProps } from "../src/components/agent-chat";

afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.useRealTimers(); });
const recent: ChatItem[] = [{ id: "msg:90", kind: "message", author: "human", text: "Latest message" }];
const old: ChatItem[] = [{ id: "goal:1", kind: "message", author: "human", text: "Original goal" }];
const entries = [
  { id: "goal-anchor", targetItemId: "goal:1", preview: "Original goal", kind: "goal" },
  { id: "latest-anchor", targetItemId: "msg:90", preview: "Latest message", kind: "message" },
];

describe("loop interaction index", () => {
  it("bounds requested history and aborts a stalled loader with an actionable error", async () => {
    vi.useFakeTimers();
    let signal: AbortSignal | undefined;
    const loader = vi.fn<NonNullable<LoopMessageIndexProps["onRequestJump"]>>((_entry, request) => { signal = request.signal; return new Promise<{ targetItemId: string }>(() => {}); });
    render(<AgentChat items={recent} messageIndex={{ scopeKey: "loop-a", entries, before: 500, after: -1, onRequestJump: loader }} />);
    fireEvent.click(screen.getByRole("button", { name: "Jump to goal 1: Original goal" }));
    expect(loader).toHaveBeenCalledWith(entries[0], { before: 100, after: 0, signal: expect.any(AbortSignal) });
    await act(async () => { await vi.advanceTimersByTimeAsync(10001); });
    expect(signal?.aborted).toBe(true);
    expect(screen.getByRole("alert").textContent).toContain("did not become available");
  });

  it("jumps to an already-loaded item without calling the history loader", async () => {
    const onRequestJump = vi.fn();
    const onActiveChange = vi.fn();
    const { container } = render(<AgentChat items={recent} messageIndex={{ scopeKey: "loop-a", entries, onRequestJump, onActiveChange }} />);
    const scroller = screen.getByTestId("agent-chat-stream");
    scroller.scrollTop = 0;
    const target = container.querySelector('[data-chat-item-id="msg:90"]')!;
    vi.spyOn(target, "getBoundingClientRect").mockReturnValue({ top: 200 } as DOMRect);
    vi.spyOn(scroller, "getBoundingClientRect").mockReturnValue({ top: 0 } as DOMRect);
    fireEvent.click(screen.getByRole("button", { name: "Jump to message 2: Latest message" }));
    await waitFor(() => expect(scroller.scrollTop).toBe(188));
    expect(onRequestJump).not.toHaveBeenCalled();
    expect(onActiveChange).toHaveBeenCalledWith("latest-anchor");
    expect(target.className).toContain("chat-index-target");
    expect(screen.getByRole("button", { name: "Return to latest" })).toBeTruthy();
  });

  it("loads a bounded missing window, then restores the latest window", async () => {
    const requests = vi.fn();
    const latest = vi.fn();
    function Host() {
      const [items, setItems] = useState(recent);
      return <AgentChat items={items} messageIndex={{ scopeKey: "loop-a", entries,
        onRequestJump: async (entry, request) => { requests(entry, request); setItems(old); return { targetItemId: "goal:1" }; },
        onRequestLatest: async () => { latest(); setItems(recent); return { targetItemId: "msg:90" }; },
      }} />;
    }
    const { container } = render(<Host />);
    fireEvent.click(screen.getByRole("button", { name: "Jump to goal 1: Original goal" }));
    await waitFor(() => expect(container.querySelector('[data-chat-item-id="goal:1"]')?.className).toContain("chat-index-target"));
    expect(requests).toHaveBeenCalledWith(entries[0], { before: 20, after: 20, signal: expect.any(AbortSignal) });
    fireEvent.click(screen.getByRole("button", { name: "Return to latest" }));
    await waitFor(() => expect(latest).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(screen.queryByRole("button", { name: "Return to latest" })).toBeNull());
    expect(screen.getByText("Latest message")).toBeTruthy();
  });

  it("cancels superseded requests and ignores late completions", async () => {
    const pending: Array<{ signal: AbortSignal; resolve: (result: { targetItemId: string }) => void }> = [];
    const onRequestJump: NonNullable<LoopMessageIndexProps["onRequestJump"]> = (_entry, request) => new Promise((resolve) => pending.push({ signal: request.signal, resolve }));
    const onActiveChange = vi.fn();
    const { rerender } = render(<AgentChat items={[]} messageIndex={{ scopeKey: "loop-a", entries, onRequestJump, onActiveChange }} />);
    fireEvent.click(screen.getByRole("button", { name: "Jump to goal 1: Original goal" }));
    fireEvent.click(screen.getByRole("button", { name: "Jump to message 2: Latest message" }));
    expect(pending[0]!.signal.aborted).toBe(true);
    rerender(<AgentChat items={recent} messageIndex={{ scopeKey: "loop-a", entries, onRequestJump, onActiveChange }} />);
    await act(async () => { pending[1]!.resolve({ targetItemId: "msg:90" }); });
    await act(async () => { pending[0]!.resolve({ targetItemId: "goal:1" }); });
    expect(onActiveChange).toHaveBeenLastCalledWith("latest-anchor");
  });

  it("cancels loading when the loop changes and on unmount", () => {
    const signals: AbortSignal[] = [];
    const onRequestJump: NonNullable<LoopMessageIndexProps["onRequestJump"]> = (_entry, request) => { signals.push(request.signal); return new Promise(() => {}); };
    const { rerender, unmount } = render(<AgentChat items={[]} messageIndex={{ scopeKey: "loop-a", entries, onRequestJump }} />);
    fireEvent.click(screen.getByRole("button", { name: "Jump to goal 1: Original goal" }));
    rerender(<AgentChat items={[]} messageIndex={{ scopeKey: "loop-b", entries, onRequestJump }} />);
    expect(signals[0]!.aborted).toBe(true);
    expect(screen.queryByRole("status")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Jump to goal 1: Original goal" }));
    unmount();
    expect(signals[1]!.aborted).toBe(true);
  });

  it("exposes loader errors and leaves return-to-latest available", async () => {
    render(<AgentChat items={recent} messageIndex={{ scopeKey: "loop-a", entries,
      onRequestJump: async () => { throw new Error("History unavailable"); } }} />);
    fireEvent.click(screen.getByRole("button", { name: "Jump to goal 1: Original goal" }));
    expect(await screen.findByRole("alert")).toHaveProperty("textContent", "History unavailable");
    expect(screen.getByText("Latest message")).toBeTruthy();
  });

  it("does not follow live messages or auto-load older pages after an indexed jump", async () => {
    const onLoadOlder = vi.fn();
    const config = { scopeKey: "loop-a", entries };
    const { rerender } = render(<AgentChat items={recent} messageIndex={config} hasOlder onLoadOlder={onLoadOlder} />);
    fireEvent.click(screen.getByRole("button", { name: "Jump to message 2: Latest message" }));
    const scroller = screen.getByTestId("agent-chat-stream");
    scroller.scrollTop = 25;
    Object.defineProperty(scroller, "scrollHeight", { configurable: true, value: 1000 });
    fireEvent.scroll(scroller);
    rerender(<AgentChat items={[...recent, { id: "new", kind: "text", agent: "builder", text: "New live output" }]} messageIndex={config} hasOlder onLoadOlder={onLoadOlder} />);
    await waitFor(() => expect(screen.getByText("New live output")).toBeTruthy());
    expect(scroller.scrollTop).toBe(25);
    expect(onLoadOlder).not.toHaveBeenCalled();
  });

  it("uses supplied previews, supports index pagination, and hides the rail in agent zoom", () => {
    const onLoadMore = vi.fn();
    const config = { scopeKey: "loop-a", entries: [{ ...entries[0]!, contextPreview: "A cached response excerpt" }], hasMore: true, onLoadMore };
    const { rerender } = render(<AgentChat items={recent} messageIndex={config} />);
    fireEvent.focus(screen.getByRole("button", { name: "Jump to goal 1: Original goal" }));
    expect(screen.getByRole("tooltip").textContent).toContain("A cached response excerpt");
    fireEvent.click(screen.getByRole("button", { name: "Load more interaction markers" }));
    expect(onLoadMore).toHaveBeenCalledTimes(1);
    rerender(<AgentChat items={recent} messageIndex={config} agentFilter="builder" />);
    expect(screen.queryByRole("navigation", { name: "Loop interaction index" })).toBeNull();
  });
});
