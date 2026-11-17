import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, createEvent, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { AgentChat, ChatComposer } from "../src/components/agent-chat";

beforeEach(() => {
  vi.stubGlobal("matchMedia", vi.fn(() => ({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() })));
  vi.stubGlobal("URL", class extends URL {
    static createObjectURL = vi.fn(() => "blob:fixture-image");
    static revokeObjectURL = vi.fn();
  });
});
afterEach(() => { cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals(); });

describe("composer attachments", () => {
  it("stages files, permits attachment-only messages, and clears after a successful send", async () => {
    const onMessage = vi.fn(async () => {});
    const onAttachmentsChange = vi.fn();
    const file = new File(["context"], "notes.txt", { type: "text/plain" });
    render(<ChatComposer mode="message" onSteer={vi.fn()} onMessage={onMessage} onAttachmentsChange={onAttachmentsChange} />);
    fireEvent.change(screen.getByLabelText("Attach files"), { target: { files: [file] } });
    expect(screen.getByRole("button", { name: "Remove notes.txt" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    await waitFor(() => expect(onMessage).toHaveBeenCalledWith("", undefined, [{ id: expect.any(String), file, relativePath: undefined }]));
    await waitFor(() => expect(screen.queryByRole("button", { name: "Remove notes.txt" })).toBeNull());
    expect(onAttachmentsChange).toHaveBeenLastCalledWith([]);
  });

  it("keeps text and attachments when the host rejects a send", async () => {
    const onMessage = vi.fn().mockRejectedValue(new Error("Upload failed"));
    render(<ChatComposer mode="message" onSteer={vi.fn()} onMessage={onMessage} />);
    fireEvent.change(screen.getByLabelText("Attach files"), { target: { files: [new File(["a"], "context.txt")] } });
    fireEvent.change(screen.getByLabelText("message"), { target: { value: "Use this" } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    expect(await screen.findByRole("alert")).toHaveProperty("textContent", "Upload failed");
    expect((screen.getByLabelText("message") as HTMLTextAreaElement).value).toBe("Use this");
    expect(screen.getByRole("button", { name: "Remove context.txt" })).toBeTruthy();
  });

  it("pastes images, opens a preview, and releases preview URLs on removal", async () => {
    const file = new File(["image"], "screenshot.png", { type: "image/png" });
    render(<ChatComposer mode="message" onSteer={vi.fn()} onMessage={vi.fn()} />);
    const paste = createEvent.paste(screen.getByLabelText("message"), {
      clipboardData: { items: [{ kind: "file", type: "image/png", getAsFile: () => file }] },
    });
    fireEvent(screen.getByLabelText("message"), paste);
    expect(paste.defaultPrevented).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Preview screenshot.png" }));
    expect(await screen.findByRole("dialog")).toBeTruthy();
    fireEvent.keyDown(document, { key: "Escape" });
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    fireEvent.click(screen.getByRole("button", { name: "Remove screenshot.png" }));
    expect(URL.revokeObjectURL).toHaveBeenCalledWith("blob:fixture-image");
    expect(screen.queryByAltText("screenshot.png")).toBeNull();
  });

  it("preserves normal text paste and ignores attachments when disabled", () => {
    const onAttachmentsChange = vi.fn();
    render(<ChatComposer onSteer={vi.fn()} disabled onAttachmentsChange={onAttachmentsChange} />);
    const paste = createEvent.paste(screen.getByLabelText("message"), { clipboardData: { items: [{ kind: "string", type: "text/plain" }] } });
    fireEvent(screen.getByLabelText("message"), paste);
    expect(paste.defaultPrevented).toBe(false);
    fireEvent.change(screen.getByLabelText("Attach files"), { target: { files: [new File(["x"], "ignored.txt")] } });
    expect(onAttachmentsChange).not.toHaveBeenCalled();
  });

  it("accepts drops over the entire chat and passes files through a steer", async () => {
    const onSteer = vi.fn(async () => {});
    const file = new File(["context"], "drop.txt");
    render(<AgentChat items={[]} composer={{ fixedTarget: "builder", onSteer }} />);
    fireEvent.drop(screen.getByTestId("agent-chat-stream"), { dataTransfer: { types: ["Files"], files: [file], items: [] } });
    expect(await screen.findByRole("button", { name: "Remove drop.txt" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Steer" }));
    await waitFor(() => expect(onSteer).toHaveBeenCalledWith({ target: "builder", text: "", attachments: [expect.objectContaining({ file })] }));
  });

  it("reads every batch of a dropped folder and preserves relative paths", async () => {
    const first = new File(["a"], "a.txt");
    const second = new File(["b"], "b.txt");
    const fileEntry = (file: File) => ({ isFile: true, fullPath: `/context/${file.name}`, file: (done: (file: File) => void) => done(file) });
    const batches = [[fileEntry(first)], [fileEntry(second)], []];
    const entry = { isDirectory: true, createReader: () => ({ readEntries: (done: (entries: unknown[]) => void) => done(batches.shift()!) }) };
    const onAttachmentsChange = vi.fn();
    render(<AgentChat items={[]} composer={{ onSteer: vi.fn(), onAttachmentsChange }} />);
    fireEvent.drop(screen.getByTestId("agent-chat"), { dataTransfer: { types: ["Files"], files: [],
      items: [{ webkitGetAsEntry: () => entry, getAsFile: () => null }] } });
    await waitFor(() => expect(onAttachmentsChange).toHaveBeenCalledWith([
      expect.objectContaining({ file: first, relativePath: "context/a.txt" }),
      expect.objectContaining({ file: second, relativePath: "context/b.txt" }),
    ]));
  });

  it("opens a grouped plus menu and calls the host for context actions", async () => {
    const onAddAction = vi.fn();
    render(<ChatComposer onSteer={vi.fn()} onAddAction={onAddAction} />);
    fireEvent.click(screen.getByRole("button", { name: "Add to chat" }));
    expect(await screen.findByText("Context")).toBeTruthy();
    expect(screen.getByText("Commands")).toBeTruthy();
    fireEvent.click(screen.getByText("Skills and prompts"));
    expect(onAddAction).toHaveBeenCalledWith("skills");
  });
});
