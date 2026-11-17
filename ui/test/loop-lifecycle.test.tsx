import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { DeleteLoopDialog, LoopCard } from "../src/features/components/LoopCard";
import { DetachedNote, resetDetachedNotes } from "../src/components/DetachedNote";
import { WorkLoopsTab } from "../src/features/work/WorkLoopsTab";
import { installFakeDaemon, resetFakeDaemon } from "./helpers";

const loop = { id: "loop-1", slug: "loop-abc", title: "Broken run", phase: "plan", status: "blocked", updatedAt: "2026-11-16T10:00:00Z" };

describe("tidying a loop away", () => {
  beforeEach(() => {
    resetFakeDaemon();
    resetDetachedNotes();
    window.matchMedia = vi.fn(() => ({ matches: false, media: "", onchange: null, addEventListener: () => {}, removeEventListener: () => {}, addListener: () => {}, removeListener: () => {}, dispatchEvent: () => false })) as unknown as typeof window.matchMedia;
  });
  afterEach(() => cleanup());

  it("deletes only after the slug is typed, then says it is done and what was kept, and closes when told to", async () => {
    const daemon = installFakeDaemon({ responses: { "loop.delete": { tombstone: { slug: "loop-abc", steps: 4, toolCalls: 169, costUsd: 6.76, costState: "known" } } } });
    let deleted = 0;
    let closed = 0;
    render(<DeleteLoopDialog loop={loop} onClose={() => { closed += 1; }} onDeleted={() => { deleted += 1; }} />);
    const confirm = screen.getByTestId("loop-delete-confirm") as HTMLButtonElement;
    expect(confirm.disabled).toBe(true);
    fireEvent.change(screen.getByTestId("loop-delete-slug"), { target: { value: "loop-ab" } });
    expect(confirm.disabled).toBe(true);
    fireEvent.change(screen.getByTestId("loop-delete-slug"), { target: { value: "loop-abc" } });
    fireEvent.click(confirm);
    // The confirmation stays on screen (the list refreshing behind it cannot take it away) until it is closed.
    expect(await screen.findByTestId("loop-delete-done")).toBeTruthy();
    expect(screen.getByTestId("loop-delete-done").textContent).toContain("was deleted");
    expect(screen.getByTestId("loop-delete-done").textContent).toContain("169 calls");
    expect(deleted).toBe(1);
    expect(closed).toBe(0);
    expect(daemon.paramsOf("loop.delete")).toEqual({ id: "loop-1", confirm: "loop-abc" });
    fireEvent.click(screen.getByTestId("loop-delete-close"));
    expect(closed).toBe(1);
  });

  it("does not hand focus to the field when the explanatory text is clicked", () => {
    render(<DeleteLoopDialog loop={loop} onClose={() => undefined} onDeleted={() => undefined} />);
    const field = screen.getByTestId("loop-delete-slug");
    // The field is focused once on open; clicking the text around it is not a label activation that refocuses it.
    (field as HTMLInputElement).blur();
    fireEvent.click(screen.getByText(/will go for good/));
    expect(document.activeElement).not.toBe(field);
  });

  it("archives and restores without a confirmation, and refuses both for a running loop", async () => {
    const daemon = installFakeDaemon({ responses: { "loop.setArchived": { id: "loop-1", archived: true } } });
    let changed = 0;
    const { rerender } = render(<MemoryRouter><LoopCard loop={loop} canWrite onDelete={() => undefined} onChanged={() => { changed += 1; }} /></MemoryRouter>);
    fireEvent.click(screen.getByTestId("loop-archive"));
    await waitFor(() => expect(changed).toBe(1));
    expect(daemon.paramsOf("loop.setArchived")).toEqual({ id: "loop-1", archived: true });
    rerender(<MemoryRouter><LoopCard loop={{ ...loop, archivedAt: "2026-11-16T11:00:00Z" }} canWrite onDelete={() => undefined} onChanged={() => undefined} /></MemoryRouter>);
    expect(screen.getByTestId("loop-archive").textContent).toBe("Restore");
    rerender(<MemoryRouter><LoopCard loop={{ ...loop, status: "running" }} canWrite onDelete={() => undefined} onChanged={() => undefined} /></MemoryRouter>);
    expect((screen.getByTestId("loop-archive") as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByTestId("loop-delete") as HTMLButtonElement).disabled).toBe(true);
  });

  it("says a task lost its loop to a deletion, and says nothing for one that did not", async () => {
    installFakeDaemon({ responses: { "loop.tombstones": { tombstones: [], detached: [{ kind: "task", id: "t-1", loopSlug: "loop-abc", componentRoot: "/r", detachedAt: "2026-11-16T10:00:00Z" }] } } });
    render(<><DetachedNote kind="task" id="t-1" /><DetachedNote kind="task" id="t-2" /></>);
    const notes = await screen.findAllByTestId("detached-note");
    expect(notes).toHaveLength(1);
    expect(notes[0]!.textContent).toContain("loop-abc");
    expect(notes[0]!.textContent).toContain("only its link to the loop is gone");
  });

  it("the loops table keeps a long title to one line with the full text on hover, and always offers archive and delete", () => {
    installFakeDaemon({});
    const long = "Turn the hello program into a small, tested greeting CLI with flags, a history file and a README that explains how to use it";
    render(<MemoryRouter><WorkLoopsTab loops={[{ ...loop, title: long, componentRoot: "/r" } as never]} components={[]} showComponent={false} canWrite onChanged={() => undefined} showArchived={false} onShowArchived={() => undefined} /></MemoryRouter>);
    const title = screen.getByTestId("work-loop-loop-1");
    expect(title.className).toContain("truncate");
    expect(title.getAttribute("title")).toBe(long);
    expect(screen.getByTestId("loop-archive")).toBeTruthy();
    expect(screen.getByTestId("loop-delete")).toBeTruthy();
    expect(screen.getByTestId("show-archived-loops")).toBeTruthy();
  });

  it("says work was closed, not done, because its loop was archived, and that a restore opens it again", async () => {
    installFakeDaemon({ responses: { "loop.tombstones": { tombstones: [], detached: [], closed: [
      { kind: "task", id: "t-1", reason: "loop-archived", loopSlug: "loop-abc", closedAt: "2026-11-16T10:00:00Z", priorStatus: "in-progress" },
      { kind: "goal", id: "g-1", reason: "loop-deleted", loopSlug: "loop-abc", closedAt: "2026-11-16T10:00:00Z", priorStatus: "approved" },
    ] } } });
    render(<><DetachedNote kind="task" id="t-1" /><DetachedNote kind="goal" id="g-1" /></>);
    const notes = await screen.findAllByTestId("detached-note");
    expect(notes[0]!.textContent).toContain("Closed, not done");
    expect(notes[0]!.textContent).toContain("archived");
    expect(notes[0]!.textContent).toContain("opens again if the loop is restored");
    expect(notes[1]!.textContent).toContain("deleted");
    expect(notes[1]!.textContent).not.toContain("opens again");
  });
});
