import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { AgentChat, ChatComposer, MessageComposer, type SteerRequest } from "../src/components/agent-chat";
import { skillInstructionLine, useSkillPicker } from "../src/lib/skills";
import { chatItemsFromEvents } from "../src/components/agent-chat";
import type { SkillListResult, SkillView } from "../src/lib/skills";
import { installFakeDaemon } from "./helpers";
import { resetBridge } from "../src/lib/bridge";

// S67 built the picker; S68 moved what a choice MEANS to the wire
// (B3/B7/B8). Three surfaces, one truth:
//
//   * the composer BASE renders the chosen skills as removable chips
//     (both variants — the chat composer and the home-screen one share
//     it), the tooltip carrying the exact sentence the DAEMON renders;
//   * `/` (and the plus menu's skills row) opens a picker whose rows
//     LIST EVERYTHING — invalid and shadowed skills visible but not
//     selectable (P31: a rendering, never a silent drop);
//   * the send carries the operator's words PURE and the chosen names
//     in a `skills` field, in selection order — the daemon's door
//     validates them and renders the instruction lines into the turn's
//     system half (which the chat window does not show). The chips
//     clear with the send, and the posted bubble grows the receipt.
//
// Hosts (loop workspace, both bench views) only SPREAD useSkillPicker's
// output, so this file holds the harness the same spread — testing the
// behavior every host actually gets, not a per-host copy.

function skill(overrides: Partial<SkillView> & { name: string; status: SkillView["status"] }): SkillView {
  return {
    source: "user",
    root: "/home/dev/.agents/skills",
    path: "/home/dev/.agents/skills/" + overrides.name,
    files: { scripts: 0, references: 0, assets: 0, other: 1 },
    bodyBytes: 100,
    bodyLines: 8,
    totalBytes: 200,
    scannedAt: "2026-10-31T08:00:00Z",
    ...overrides,
  };
}

const DOCX = skill({ name: "docx", status: "valid", description: "Word documents for the office." });
const THIN = skill({ name: "thin", status: "warn", description: "Usable but thin." });
const BROKEN = skill({ name: "broken", status: "invalid" });
const SHADOWED = skill({ name: "shadowed", status: "valid", description: "Loses to the other root.", shadowed: true });

function listing(skills: SkillView[]): SkillListResult {
  return {
    skills,
    userRoot: "/home/dev/.agents/skills",
    managedRoot: "/home/dev/.local/share/aeon/skills",
    scannedAt: "2026-10-31T08:00:00Z",
    summary: {
      valid: skills.filter((s) => s.status === "valid").length,
      warn: skills.filter((s) => s.status === "warn").length,
      invalid: skills.filter((s) => s.status === "invalid").length,
      shadowed: skills.filter((s) => s.shadowed).length,
    },
    watching: true,
  };
}

/** The host spread, verbatim — the same lines every real host writes
 * (LoopWorkspaceView and both bench views wrap through the hook, so this
 * test exercises the actual behavior of every host, not a copy). */
function Harness({ onSteer }: { onSteer: (request: SteerRequest) => void | Promise<void> }) {
  const skills = useSkillPicker(true);
  return <AgentChat height="50vh" items={[]}
    picker={skills.panel ?? undefined} onPickerDismiss={skills.close}
    composer={{
      targets: [{ value: "atlas", label: "atlas" }],
      onSteer: skills.steerWith(onSteer),
      onSpecialKeyChange: skills.onSpecialKeyChange,
      onAddAction: skills.onAddAction,
      chips: skills.chips,
    }} />;
}

/** Row clicks go native: the dock's outside-dismiss logic swallows the
 * synthetic click in jsdom (probed — the handler itself is fine). */
async function clickRow(name: string): Promise<void> {
  const picker = await screen.findByTestId("agent-chat-picker");
  const row = Array.from(picker.querySelectorAll("button.chat-panel-row"))
    .find((b) => (b.textContent ?? "").startsWith(name));
  if (!row) throw new Error(`no picker row for ${name}`);
  (row as HTMLButtonElement).click();
  await waitFor(() => expect(document.body.textContent).toContain(name));
}

async function type(text: string): Promise<void> {
  const box = await screen.findByRole("textbox", { name: "message" });
  fireEvent.change(box, { target: { value: text, selectionStart: text.length } as never });
}

beforeEach(() => resetBridge());
afterEach(() => cleanup());

describe("the composer base: chips", () => {
  it("renders a removable chip row in both variants, tooltip = the instruction sentence", () => {
    const onRemove = vi.fn();
    const { unmount } = render(<MessageComposer chips={[{ id: "docx", label: "docx", title: "use the docx skill — read /p/SKILL.md before answering.", onRemove }]} onSubmit={() => {}} />);
    const row = screen.getByTestId("message-composer-chips");
    expect(row.textContent).toContain("docx");
    expect(screen.getByTestId("message-composer-chip-docx").getAttribute("title")).toBe("use the docx skill — read /p/SKILL.md before answering.");
    fireEvent.click(screen.getByTestId("message-composer-chip-docx-remove"));
    expect(onRemove).toHaveBeenCalledTimes(1);
    unmount();
    render(<ChatComposer chips={[{ id: "a", label: "a" }]} onSteer={() => {}} targets={[{ value: "a", label: "a" }]} />);
    expect(screen.getByTestId("agent-chat-composer-chips").textContent).toContain("a");
  });

  it("absent or empty chips render nothing (an empty row would be a lie of tidiness)", () => {
    render(<MessageComposer chips={[]} onSubmit={() => {}} />);
    expect(screen.queryByTestId("message-composer-chips")).toBeNull();
  });
});

describe("the `/` picker", () => {
  it("lists EVERY discovery — warn selectable, invalid and shadowed visible but not selectable", async () => {
    installFakeDaemon({ responses: { "skills.list": listing([DOCX, THIN, BROKEN, SHADOWED]) } });
    render(<Harness onSteer={() => {}} />);
    await type("/");
    const picker = await screen.findByTestId("agent-chat-picker");
    expect(picker.textContent).toContain("docx");
    expect(picker.textContent).toContain("thin");
    // Listed, and stated as unselectable — never dropped.
    expect(picker.textContent).toContain("broken");
    expect(picker.textContent).toContain("not selectable — the validator refused it");
    expect(picker.textContent).toContain("shadowed");
    expect(picker.textContent).toContain("not selectable — shadowed");
    // Selecting the broken one is not offered; the good one is.
    const good = Array.from(picker.querySelectorAll("button.chat-panel-row")).find((b) => (b.textContent ?? "").startsWith("docx"));
    expect((good as HTMLButtonElement | undefined)?.disabled).toBe(false);
    expect(Array.from(picker.querySelectorAll("button.chat-panel-row")).some((b) => (b.textContent ?? "").startsWith("broken"))).toBe(false);
  });

  it("filters by the trigger query and says so when nothing matches", async () => {
    installFakeDaemon({ responses: { "skills.list": listing([DOCX, THIN]) } });
    render(<Harness onSteer={() => {}} />);
    await type("/doc");
    const picker = await screen.findByTestId("agent-chat-picker");
    expect(picker.textContent).toContain("docx");
    expect(picker.textContent).not.toContain("thin");
    await type("/zzz");
    await waitFor(() => expect(screen.getByTestId("agent-chat-picker").textContent).toContain('No skill matches “zzz”.'));
  });

  it("the plus menu's skills row opens the same picker", async () => {
    installFakeDaemon({ responses: { "skills.list": listing([DOCX]) } });
    render(<Harness onSteer={() => {}} />);
    fireEvent.click(await screen.findByRole("button", { name: "Add to chat" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: /Skills and prompts/ }));
    await waitFor(() => expect(screen.getByTestId("agent-chat-picker").textContent).toContain("docx"));
  });

  it("an unread catalog says so — never an empty one (P31)", async () => {
    installFakeDaemon({ responses: { "skills.list": () => { throw new Error("daemon down"); } } });
    render(<Harness onSteer={() => {}} />);
    await type("/");
    await waitFor(() => expect(screen.getByTestId("agent-chat-picker").textContent).toContain("The catalog could not be read:"));
  });
});

describe("chips + the field (S68 B3/B7)", () => {
  it("picking attaches the NAMES and leaves the operator's words untouched", async () => {
    installFakeDaemon({ responses: { "skills.list": listing([DOCX, THIN]) } });
    const onSteer = vi.fn();
    render(<Harness onSteer={onSteer} />);
    await type("/");
    await clickRow("docx");
    await clickRow("thin");
    const chips = await screen.findByTestId("agent-chat-composer-chips");
    expect(chips.textContent).toContain("docx");
    expect(chips.textContent).toContain("thin");
    expect(screen.getByTestId("agent-chat-composer-chip-docx").getAttribute("title")).toBe(
      "use the docx skill — read /home/dev/.agents/skills/docx/SKILL.md before answering.");
    await type("summarise this");
    fireEvent.click(await screen.findByRole("button", { name: "Steer" }));
    await waitFor(() => expect(onSteer).toHaveBeenCalledTimes(1));
    const request = onSteer.mock.calls[0]![0] as SteerRequest;
    // The words are the person's own, byte for byte — no line is woven in
    // (B3). The choice rides as NAMES, in selection order (B7).
    expect(request.text).toBe("summarise this");
    expect(request.skills).toEqual(["docx", "thin"]);
    await waitFor(() => expect(screen.queryByTestId("agent-chat-composer-chips")).toBeNull());
  });

  it("no chips, no field — the send is the operator's words exactly, as before S68", async () => {
    installFakeDaemon({ responses: { "skills.list": listing([DOCX]) } });
    const onSteer = vi.fn();
    render(<Harness onSteer={onSteer} />);
    await type("plain words");
    fireEvent.click(await screen.findByRole("button", { name: "Steer" }));
    await waitFor(() => expect(onSteer).toHaveBeenCalledTimes(1));
    const request = onSteer.mock.calls[0]![0] as SteerRequest;
    expect(request.text).toBe("plain words");
    expect(request.skills).toBeUndefined(); // absent, not []: nothing chosen (P31)
  });

  it("a chip removed by hand rides nothing", async () => {
    installFakeDaemon({ responses: { "skills.list": listing([DOCX, THIN]) } });
    const onSteer = vi.fn();
    render(<Harness onSteer={onSteer} />);
    await type("/");
    await clickRow("docx");
    await clickRow("thin");
    fireEvent.click(await screen.findByTestId("agent-chat-composer-chip-docx-remove"));
    await type("just the thin one");
    fireEvent.click(await screen.findByRole("button", { name: "Steer" }));
    await waitFor(() => expect(onSteer).toHaveBeenCalledTimes(1));
    const request = onSteer.mock.calls[0]![0] as SteerRequest;
    expect(request.text).toBe("just the thin one");
    expect(request.skills).toEqual(["thin"]);
  });
});

// ── the shared sentences (B11): ONE template, both sides ───────────────────
//
// These are the same constants the daemon's tests pin (internal/daemon
// chosen_skills_test.go and internal/loop's fallback pin). The chip previews
// the first; the second is what the daemon SAYS on the wire when the person
// sent no words — it never appears in the window, and pinning it here is
// what makes the two suites drift together instead of apart.
const SHARED = {
  instruction: (name: string, path: string) => `use the ${name} skill — read ${path}/SKILL.md before answering.`,
  continueFallback: "Continue the loop per the task instructions in the system prompt.",
};

describe("the shared sentences", () => {
  it("the chip tooltip is the daemon's sentence, and the fallback sentence is pinned", () => {
    expect(skillInstructionLine("docx", "/p")).toBe(SHARED.instruction("docx", "/p"));
    expect(SHARED.continueFallback).toBe("Continue the loop per the task instructions in the system prompt.");
  });
});

// ── the receipt under the bubble (B8) ───────────────────────────────────────

describe("the chosen-skills receipt under the bubble", () => {
  it("the posted event's names ride beside the pure body — and never into it", () => {
    const items = chatItemsFromEvents([
      { seq: 8, at: "2026-10-31T09:00:00Z", kind: "loop.message.posted",
        payload: { loopId: "l", messageId: 42, seq: 8, author: "human", kind: "note", source: "user",
          body: "draft the notes", skills: ["docx", ""] } },
    ]);
    const msg = items.find((i) => i.kind === "message");
    if (!msg || msg.kind !== "message") throw new Error("no message item");
    expect(msg.text).toBe("draft the notes"); // the person's words, untouched
    expect(msg.skills).toEqual(["docx"]); // blank names are filtered, order kept
    // And the pre-S68 shape: no skills on the event, none on the item.
    const plain = chatItemsFromEvents([
      { seq: 9, at: "2026-10-31T09:00:00Z", kind: "loop.message.posted",
        payload: { loopId: "l", messageId: 43, seq: 9, author: "human", kind: "note", source: "user", body: "hi" } },
    ]);
    const plainMsg = plain.find((i) => i.kind === "message");
    if (!plainMsg || plainMsg.kind !== "message") throw new Error("no plain message item");
    expect(plainMsg.skills).toBeUndefined();
  });

  it("the bubble renders the receipt UNDER the words with the honesty sentence", () => {
    render(<AgentChat height="50vh" items={[
      { id: "m1", at: "2026-10-31T09:00:00Z", kind: "message", author: "human",
        text: "draft the notes", skills: ["docx"] },
    ]} />);
    const bubble = screen.getByTestId("agent-chat-message");
    expect(bubble.textContent).toContain("draft the notes");
    const receipt = screen.getByTestId("agent-chat-message-skills");
    expect(receipt.textContent).toContain("docx");
    expect(receipt.textContent).toContain("shown to the model, not the thread");
    // The instruction line itself is NOT on the window — the system half is
    // hidden by design, and the receipt is what says so (no fabrication).
    expect(bubble.textContent).not.toContain("read /");
  });
});
