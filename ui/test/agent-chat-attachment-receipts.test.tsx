import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { AgentChat, chatItemsFromEvents, type ChatEventLike, type ChatItem } from "../src/components/agent-chat";
import type { ChatAttachment } from "../src/components/agent-chat";
import { useChatActions, type ChatActionsHost, type ChatWiring } from "../src/features/loop-workspace/center/useChatActions";
import type { AttachmentReceipt } from "../src/lib/attachments";

// S69 — bytes once, refs forever, blocks at the last moment (client half).
// What is pinned here is the RULE, tested where the rule lives:
//
//   * the SEND puts each file once through `attachment.put` and carries
//     only {document, rev} references on loop.message / step.steer — the
//     field is ABSENT when nothing rode, never [];
//   * a refused put throws BEFORE the send — nothing half-attached is
//     ever queued or posted (the composer keeps the draft; the existing
//     agent-chat-attachments.test.tsx pins that half);
//   * the mapper reads receipt arrays into the items and refuses to
//     render a half-believed field;
//   * the bubble renders name · mediaType · size from the STORE's facts,
//     previews previewable images through the read door (rendering only),
//     and a failed read renders one visibly broken chip naming document
//     and revision (ruling 3) — never a blank, never a fake icon.

vi.mock("../src/lib/bridge", () => ({
  rpc: vi.fn(),
  toRpcError: (cause: unknown) => (cause instanceof Error ? cause : new Error(String(cause))),
}));

import { rpc } from "../src/lib/bridge";
const rpcMock = rpc as unknown as ReturnType<typeof vi.fn>;

/** jsdom's File in this environment may lack arrayBuffer (a Blob method the
 * DOM spec gained later than this jsdom); the fixture carries its own —
 * the put path only ever needs the bytes, and the browser File always
 * has the real method. */
const PNG_BYTES = [0x89, 0x50, 0x4e, 0x47, 1, 2, 3];
function fixtureFile(name: string, type: string, bytes: number[]): File {
  const f = new File([new Uint8Array(bytes)], name, { type });
  Object.defineProperty(f, "arrayBuffer", { value: async () => new Uint8Array(bytes).buffer });
  return f;
}
const PNG = fixtureFile("shot.png", "image/png", PNG_BYTES);

function receipt(overrides: Partial<AttachmentReceipt> = {}): AttachmentReceipt {
  return { name: "shot.png", document: "@attachments/0a1b2c3d-shot.png", rev: 1,
    mediaType: "image/png", size: 8, sha256: "0a1b2c3d".padEnd(64, "0"), ...overrides };
}

const session = { runs: [{ stepId: 42, agent: "worker", state: "running" as const }], questionStep: new Map<number, number>(), loopStatus: "running" as const, gates: new Map(), byStep: new Map(), loopIds: new Set<string>() };

let wiring!: ChatWiring;
function Host() {
  const host: ChatActionsHost = { loopId: "loop-1", session, rememberSteer: () => undefined };
  wiring = useChatActions(host, "/root", () => undefined);
  return null;
}

beforeEach(() => {
  vi.stubGlobal("matchMedia", vi.fn(() => ({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() })));
  rpcMock.mockReset();
  rpcMock.mockImplementation(async (method: string) => {
    if (method === "loop.show") return { id: "loop-1", slug: "my-loop", componentRoot: "/root" };
    if (method === "attachment.put") return { receipt: receipt() };
    if (method === "step.steer") return { steerId: 7, state: "queued" };
    return {};
  });
  render(<Host />);
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

const callsTo = (method: string) => rpcMock.mock.calls.filter((c) => c[0] === method).map((c) => c[1] as Record<string, unknown>);

describe("the send puts bytes once and carries references only (AT2)", () => {
  it("loop.message: put runs first with the file's facts; the message carries {document, rev}", async () => {
    const files: ChatAttachment[] = [{ id: "a1", file: PNG }];
    await wiring.onMessage("look at this", undefined, undefined, files);
    const puts = callsTo("attachment.put");
    expect(puts).toHaveLength(1);
    expect(puts[0]).toMatchObject({ slug: "my-loop", componentRoot: "/root", name: "shot.png", mediaType: "image/png" });
    expect(puts[0]!.data).toBe(btoa(String.fromCharCode(0x89, 0x50, 0x4e, 0x47, 1, 2, 3)));
    const sends = callsTo("loop.message");
    expect(sends).toHaveLength(1);
    expect(sends[0]).toMatchObject({ id: "loop-1", text: "look at this", attachments: [{ document: "@attachments/0a1b2c3d-shot.png", rev: 1 }] });
    expect(sends[0]).not.toHaveProperty("skills");
  });

  it("zero files means the field is ABSENT — never []", async () => {
    await wiring.onMessage("words only");
    const sends = callsTo("loop.message");
    expect(sends[0]).not.toHaveProperty("attachments");
    expect(callsTo("attachment.put")).toHaveLength(0);
  });

  it("a refused put sends NOTHING — no half-attached turn exists", async () => {
    rpcMock.mockImplementation(async (method: string) => {
      if (method === "loop.show") return { slug: "my-loop", componentRoot: "/root" };
      if (method === "attachment.put") throw new Error("attachment shot.png: 11 MiB is over the 10 MiB cap");
      return {};
    });
    await expect(wiring.onMessage("big", undefined, undefined, [{ id: "a1", file: PNG }])).rejects.toThrow("over the 10 MiB cap");
    expect(callsTo("loop.message")).toHaveLength(0);
  });

  it("step.steer carries references beside the words, absent when none (AT3)", async () => {
    await wiring.onSteer({ target: "worker", text: "turn left", attachments: [{ id: "a1", file: PNG }] });
    const steers = callsTo("step.steer");
    expect(steers[0]).toMatchObject({ stepId: 42, text: "turn left", attachments: [{ document: "@attachments/0a1b2c3d-shot.png", rev: 1 }] });
    await wiring.onSteer({ target: "worker", text: "no files" });
    expect(callsTo("step.steer")[1]!).not.toHaveProperty("attachments");
  });

  it("the scope is the loop row's own, read once (no claim, no repeat)", async () => {
    await wiring.onMessage("one", undefined, undefined, [{ id: "a1", file: PNG }]);
    await wiring.onMessage("two", undefined, undefined, [{ id: "a1", file: PNG }]);
    expect(callsTo("loop.show")).toHaveLength(1);
    expect(callsTo("loop.show")[0]).toEqual({ id: "loop-1" });
  });
});

describe("the transcript renders the store's facts, and its absences (AT8/AT11)", () => {
  const ev = (seq: number, kind: string, payload: unknown): ChatEventLike => ({ seq, kind, at: "2026-10-31T12:00:00Z", payload });

  it("loop.message.posted receipts ride the message item; malformed receipts draw NOTHING", () => {
    const items = chatItemsFromEvents([
      ev(1, "loop.message.posted", { author: "human", body: "attached", messageId: 10, attachments: [receipt()] }),
      ev(2, "loop.message.posted", { author: "human", body: "forged", messageId: 11,
        attachments: [{ name: "x", document: "not-attachments/x", rev: 1, mediaType: "image/png", size: 1, sha256: "z" }] }),
    ]);
    const attached = items.find((i) => i.kind === "message" && i.messageId === 10);
    expect(attached?.kind === "message" && attached.attachments).toEqual([receipt()]);
    const forged = items.find((i) => i.kind === "message" && i.messageId === 11);
    expect(forged?.kind === "message" && forged.attachments).toBeUndefined();
  });

  it("step.steer.queued receipts ride the steer row", () => {
    const items = chatItemsFromEvents([ev(1, "step.steer.queued", { steerId: 9, agent: "worker", hasText: true, attachments: [receipt({ name: "spec.pdf", document: "@attachments/beef0001-spec.pdf", rev: 2, mediaType: "application/pdf", size: 2048 })] })]);
    const steer = items.find((i) => i.kind === "steer");
    expect(steer?.kind === "steer" && steer.attachments?.[0]?.document).toBe("@attachments/beef0001-spec.pdf");
  });

  function messageItem(attachments: readonly AttachmentReceipt[]): ChatItem {
    return { id: "m1", kind: "message", author: "human", text: "my words", attachments };
  }

  it("chips carry name · mediaType · human size; images preview through the read door", async () => {
    const preview = vi.fn(async () => "data:image/png;base64,AAA=");
    render(<AgentChat items={[messageItem([receipt()])]} actions={{ attachmentPreview: preview }} />);
    expect(screen.getByTestId("agent-chat-attachment-chip").textContent).toContain("shot.png");
    expect(screen.getByTestId("agent-chat-attachment-chip").textContent).toContain("8 B");
    await waitFor(() => expect(preview).toHaveBeenCalledWith("@attachments/0a1b2c3d-shot.png", 1));
    const img = await screen.findByTestId("agent-chat-attachment-preview");
    expect((img as HTMLImageElement).getAttribute("src")).toBe("data:image/png;base64,AAA=");
  });

  it("a failed read is one visibly broken chip naming document and revision — never blank (ruling 3)", async () => {
    const preview = vi.fn(async (_document: string, _rev: number) => { throw new Error("gone"); });
    render(<AgentChat items={[messageItem([receipt({ name: "gone.png", document: "@attachments/deadbeef-gone.png", rev: 4 })])]} actions={{ attachmentPreview: preview }} />);
    const broken = await screen.findByTestId("agent-chat-attachment-broken");
    expect(broken.textContent).toContain("@attachments/deadbeef-gone.png");
    expect(broken.textContent).toContain("revision 4");
    expect(screen.queryByTestId("agent-chat-attachment-chip")).toBeNull();
  });

  it("a non-preview type is a chip only — no read is asked", async () => {
    const preview = vi.fn(async () => "data:application/pdf;base64,AAA=");
    render(<AgentChat items={[messageItem([receipt({ name: "spec.pdf", mediaType: "application/pdf" })])]} actions={{ attachmentPreview: preview }} />);
    expect(screen.getByTestId("agent-chat-attachment-chip")).toBeTruthy();
    await waitFor(() => expect(preview).not.toHaveBeenCalled());
  });

  it("steer rows chip their receipts too", async () => {
    const steer: ChatItem = { id: "s1", kind: "steer", target: "worker", text: "with a file", state: "queued",
      attachments: [receipt({ mediaType: "text/plain", name: "notes.txt", document: "@attachments/aabbccdd-notes.txt" })] };
    render(<AgentChat items={[steer]} actions={{}} />);
    const chip = screen.getByTestId("agent-chat-attachment-chip");
    expect(chip.textContent).toContain("notes.txt");
    expect(chip.textContent).toContain("text/plain");
  });
});
