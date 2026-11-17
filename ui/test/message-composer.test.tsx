import { useRef, useState } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MessageComposer, type ComposerDraft } from "../src/components/agent-chat";

afterEach(() => { cleanup(); vi.restoreAllMocks(); });

describe("shared MessageComposer", () => {
  it("submits a draft without chat callbacks and clears only after success", async () => {
    const onSubmit = vi.fn(async () => {});
    render(<MessageComposer onSubmit={onSubmit} defaultValue={{ text: "  First message  ", attachments: [] }} submitLabel="Start loop" />);
    fireEvent.click(screen.getByRole("button", { name: "Start loop" }));
    await waitFor(() => expect(onSubmit).toHaveBeenCalledWith({ text: "First message", attachments: [] }));
    await waitFor(() => expect((screen.getByLabelText("message") as HTMLTextAreaElement).value).toBe(""));
    expect(screen.queryByTestId("agent-chat-stream")).toBeNull();
  });

  it("supports controlled quick starters, custom selectors, focus, and a footer", async () => {
    const onSubmit = vi.fn(async () => {});
    function Host() {
      const [draft, setDraft] = useState<ComposerDraft>({ text: "", attachments: [] });
      const [selection, setSelection] = useState("");
      const inputRef = useRef<HTMLTextAreaElement>(null);
      return <>
        <button onClick={() => { setDraft({ text: "Review this change", attachments: [] }); inputRef.current?.focus(); }}>Quick starter</button>
        <MessageComposer value={draft} onChange={setDraft} onSubmit={onSubmit} inputRef={inputRef}
          submitDisabled={!selection} submitLabel="Launch"
          selectors={({ disabled }) => <select aria-label="Blueprint" value={selection} disabled={disabled} onChange={(event) => setSelection(event.target.value)}>
            <option value="">Choose</option><option value="review">Review</option>
          </select>} footer={<p>Blueprint validation belongs to the host.</p>} />
      </>;
    }
    render(<Host />);
    fireEvent.click(screen.getByRole("button", { name: "Quick starter" }));
    expect(document.activeElement).toBe(screen.getByLabelText("message"));
    expect((screen.getByRole("button", { name: "Launch" }) as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByLabelText("message") as HTMLTextAreaElement).disabled).toBe(false);
    fireEvent.change(screen.getByLabelText("Blueprint"), { target: { value: "review" } });
    fireEvent.click(screen.getByRole("button", { name: "Launch" }));
    await waitFor(() => expect(onSubmit).toHaveBeenCalledWith({ text: "Review this change", attachments: [] }));
    await waitFor(() => expect((screen.getByLabelText("message") as HTMLTextAreaElement).value).toBe(""));
    expect(screen.getByText("Blueprint validation belongs to the host.")).toBeTruthy();
  });

  it("retains a controlled text/file draft after upload or submission failure", async () => {
    const file = new File(["script"], "task.sh", { type: "text/plain" });
    function Host() {
      const [draft, setDraft] = useState<ComposerDraft>({ text: "Run this script", attachments: [] });
      return <MessageComposer value={draft} onChange={setDraft} onSubmit={async () => { throw new Error("Upload unavailable"); }} />;
    }
    render(<Host />);
    fireEvent.change(screen.getByLabelText("Attach files"), { target: { files: [file] } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    expect(await screen.findByRole("alert")).toHaveProperty("textContent", "Upload unavailable");
    expect((screen.getByLabelText("message") as HTMLTextAreaElement).value).toBe("Run this script");
    expect(screen.getByRole("button", { name: "Remove task.sh" })).toBeTruthy();
  });

  it("locks repeated submits and custom selectors while pending, and preserves a newer host draft", async () => {
    let resolve!: () => void;
    const pending = new Promise<void>((done) => { resolve = done; });
    const onSubmit = vi.fn(() => pending);
    const original: ComposerDraft = { text: "Original", attachments: [] };
    const onChange = vi.fn();
    const { rerender } = render(<MessageComposer value={original} onChange={onChange} onSubmit={onSubmit}
      selectors={({ disabled }) => <select aria-label="Selection" disabled={disabled}><option>One</option></select>} />);
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    fireEvent.keyDown(screen.getByLabelText("message"), { key: "Enter" });
    expect(onSubmit).toHaveBeenCalledTimes(1);
    expect((screen.getByLabelText("Selection") as HTMLSelectElement).disabled).toBe(true);
    rerender(<MessageComposer value={{ text: "Next request", attachments: [] }} onChange={onChange} onSubmit={onSubmit} />);
    resolve();
    await waitFor(() => expect(screen.queryByRole("button", { name: "Sending…" })).toBeNull());
    expect(onChange).not.toHaveBeenCalled();
    expect((screen.getByLabelText("message") as HTMLTextAreaElement).value).toBe("Next request");
  });

  it("renders its picker and special-key callbacks without AgentChat", () => {
    const onSpecialKeyChange = vi.fn();
    const onPickerDismiss = vi.fn();
    render(<MessageComposer onSubmit={vi.fn()} onSpecialKeyChange={onSpecialKeyChange}
      picker={{ id: "loops", title: "Loops", items: [] }} onPickerDismiss={onPickerDismiss} />);
    fireEvent.change(screen.getByLabelText("message"), { target: { value: "//review" } });
    expect(onSpecialKeyChange).toHaveBeenLastCalledWith({ key: "//", query: "review", start: 0, end: 8 });
    expect(screen.getByRole("button", { name: "Close Loops" })).toBeTruthy();
    fireEvent.keyDown(screen.getByLabelText("message"), { key: "Escape" });
    expect(onPickerDismiss).toHaveBeenCalledTimes(1);
  });

  it("shares pending/error handling with custom asynchronous actions", async () => {
    render(<MessageComposer onSubmit={vi.fn()} actions={({ runAction, disabled }) =>
      <button disabled={disabled} onClick={() => void runAction("save", async () => { throw new Error("Could not save draft"); })}>Save draft</button>} />);
    fireEvent.click(screen.getByRole("button", { name: "Save draft" }));
    expect(await screen.findByRole("alert")).toHaveProperty("textContent", "Could not save draft");
  });
});
