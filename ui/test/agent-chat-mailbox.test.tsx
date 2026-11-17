import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import {
  ChatStream,
  chatItemsFromEvents,
  mailboxItemsFromRows,
  mergeMailboxItems,
  type ChatEventLike,
  type ChatItem,
} from "../src/components/agent-chat";
import { AgentMailboxPage } from "../src/features/test-bench/pages/AgentMailboxPage";
import type { AgentMessageRowLike } from "../src/components/agent-chat";

// S70 T4 — the visible exchange (MB6), the one read door (MB7), byte
// honesty (MB8), and the third state said aloud (P31). The mapper is pure
// and the page reads through the mocked bridge, so every claim below is
// about the row in front of it — never a daemon it has to hope about.

vi.mock("../src/lib/bridge", () => ({
  rpc: vi.fn(async () => ({ loopId: "l-7", absent: false, messages: [] })),
  rpcTyped: vi.fn(async () => ({ subscriptionId: "s-1" })),
  toRpcError: (cause: unknown) => (cause instanceof Error ? cause : new Error(String(cause))),
  onEvent: vi.fn(async () => () => undefined),
}));

const ev = (seq: number, kind: string, payload: Record<string, unknown>): ChatEventLike =>
  ({ seq, kind, at: "2026-10-31T10:00:00Z", payload });

const posted = (over: Record<string, unknown> = {}) => ev(1, "agent.message.posted",
  { messageId: 12, thread: 12, from: "planner", to: "writer", body: "what framing?", expectReply: true, ...over });

const exchangeItems = (items: ChatItem[]) => items.filter((i) => i.kind === "exchange");

describe("the exchange rides the stream (MB6)", () => {
  it("draws a complete posted row verbatim — who, to whom, and the agent's own words", () => {
    const items = chatItemsFromEvents([posted()]);
    const ex = exchangeItems(items);
    expect(ex).toHaveLength(1);
    const one = ex[0];
    if (one?.kind !== "exchange") throw new Error("not an exchange");
    expect(one).toMatchObject({ messageId: 12, thread: 12, from: "planner", to: "writer",
      text: "what framing?", expectReply: true, state: "posted" });
  });

  it("draws NOTHING from a payload that half-believes the row — every field is validated", () => {
    for (const broken of [
      { messageId: undefined }, { thread: undefined }, { from: "" }, { to: undefined }, { body: undefined },
    ]) {
      expect(exchangeItems(chatItemsFromEvents([posted(broken)]))).toHaveLength(0);
    }
  });

  it("delivered and answered move the row FORWARD, and the ring replaying posted does not double it", () => {
    const items = chatItemsFromEvents([
      posted(),
      ev(2, "agent.message.delivered", { stepId: 7, messageId: 12 }),
      posted(), // the ring replayed the announcement
      ev(3, "agent.message.answered", { messageId: 12, answeredBy: "writer" }),
    ]);
    const ex = exchangeItems(items);
    expect(ex).toHaveLength(1);
    expect(ex[0]?.kind === "exchange" && ex[0].state).toBe("answered");
  });

  it("a delivery or an answer whose row the ring forgot draws no phantom (the read restores rows)", () => {
    const items = chatItemsFromEvents([
      ev(1, "agent.message.delivered", { stepId: 7, messageId: 99 }),
      ev(2, "agent.message.answered", { messageId: 99 }),
    ]);
    expect(exchangeItems(items)).toHaveLength(0);
  });
});

describe("the read folds the SAME rows (S39's reload rule, MB7/MB8)", () => {
  const row = (over: Partial<AgentMessageRowLike> = {}): AgentMessageRowLike => ({
    id: 12, thread: 12, fromAgent: "planner", toAgent: "writer", body: "what framing?",
    expectReply: true, createdAt: "2026-10-31T10:00:00Z", ...over,
  });

  it("maps a row to an item verbatim, and the ask a reply linked to is ANSWERED", () => {
    const items = mailboxItemsFromRows([
      row(),
      row({ id: 13, fromAgent: "writer", toAgent: "planner", body: "the framing is this", expectReply: false, resultOf: 12, deliveredAt: "2026-10-31T10:02:00Z" }),
    ]);
    expect(items).toHaveLength(2);
    const ask = items.find((i) => i.kind === "exchange" && i.messageId === 12);
    const reply = items.find((i) => i.kind === "exchange" && i.messageId === 13);
    expect(ask?.kind === "exchange" && ask.state).toBe("answered");
    expect(reply?.kind === "exchange" && reply.state).toBe("delivered");
  });

  it("refuses rows that half-believe themselves — no invented fields, no half rows", () => {
    const bad = [row({ id: 0 }), row({ fromAgent: "" }), row({ createdAt: "" }), row({ thread: 0 })];
    expect(mailboxItemsFromRows(bad)).toHaveLength(0);
    expect(mailboxItemsFromRows([])).toHaveLength(0);
  });

  it("merge keeps one row one item, moves state only FORWARD, and appends what the ring forgot", () => {
    const live = chatItemsFromEvents([posted()]); // state: posted
    const merged = mergeMailboxItems(live, mailboxItemsFromRows([row({ deliveredAt: "x" })]));
    const ex = exchangeItems(merged);
    expect(ex).toHaveLength(1);
    expect(ex[0]?.kind === "exchange" && ex[0].state).toBe("delivered");
    // The further state wins, the earlier one never rewinds it.
    const again = mergeMailboxItems(merged, mailboxItemsFromRows([row()]));
    expect(exchangeItems(again)[0]?.kind === "exchange" && exchangeItems(again)[0]?.state === "posted").toBe(false);
    // A row the ring forgot rides in beside the live items.
    const restored = mergeMailboxItems(live, mailboxItemsFromRows([row({ id: 44 })]));
    expect(exchangeItems(restored)).toHaveLength(2);
  });
});

describe("the exchange renders as the exchange it is", () => {
  it("names both agents, the verbatim words, the state, and the thread by its header row", () => {
    const items = chatItemsFromEvents([posted(), ev(2, "agent.message.answered", { messageId: 12 })]);
    render(<ChatStream items={items} />);
    const row = screen.getByTestId("agent-chat-exchange");
    expect(row.textContent).toContain("planner");
    expect(row.textContent).toContain("writer");
    expect(row.textContent).toContain("what framing?");
    expect(row.textContent).toContain("thread 12");
    expect(row.textContent).toContain("answered");
  });

  it("belongs to BOTH agents in a zoomed view — it was authored by one and addressed to the other", () => {
    const items = chatItemsFromEvents([posted()]);
    const both = render(<ChatStream items={items} agentFilter="writer" />);
    expect(screen.getByTestId("agent-chat-exchange").textContent).toContain("what framing?");
    both.unmount();
    const none = render(<ChatStream items={items} agentFilter="nobody" />);
    expect(screen.queryByTestId("agent-chat-exchange")).toBeNull();
    none.unmount();
  });
});

describe("the bench page reads the one door and says what it does not have", () => {
  it("renders rows from agent.messages and never offers a hand that writes", () => {
    render(<AgentMailboxPage connected={true} />);
    // The ruling is visible on the page: no compose box, no send.
    expect(screen.queryByRole("button", { name: /send/i })).toBeNull();
    expect(screen.getByText(/agent.messages/)).toBeTruthy();
    expect(screen.getByText(/door to a loop is/i).textContent).toMatch(/steer/);
  });

  it("scripts its gallery honestly: the states show, and the banner says the rows are fixtures", async () => {
    const { fireEvent } = await import("@testing-library/react");
    render(<AgentMailboxPage connected={true} />);
    fireEvent.click(screen.getByRole("button", { name: /scripted exchange/i }));
    const rows = screen.getAllByTestId("mailbox-row");
    // The gallery shows the settlement (ask 12 answered by its reply) AND
    // the open state (ask 21 awaiting a reply) — the three states the
    // stream speaks, in one honest screen.
    expect(rows[0]?.textContent).toContain("answered");
    expect(rows[1]?.textContent).toContain("delivered");
    expect(rows[2]?.textContent).toContain("awaits a reply");
    // The banner's honesty is the point: a scripted screen must SAY so.
    expect(screen.getByText(/Scripted gallery/i)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: /said absence/i }));
    expect(screen.getByText(/does not exist in this loop/)).toBeTruthy();
  });
});
