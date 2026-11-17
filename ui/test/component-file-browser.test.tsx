import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { ComponentFileBrowser } from "../src/features/components/ComponentFileBrowser";
import { resetBridge, setInvoker } from "../src/lib/bridge";

beforeEach(() => {
  vi.stubGlobal("ResizeObserver", class { observe() {} disconnect() {} });
  vi.stubGlobal("matchMedia", () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
});
afterEach(() => { cleanup(); resetBridge(); vi.unstubAllGlobals(); });

describe("component file browser", () => {
  it("navigates folders and shows markdown, code, image, and unavailable files", async () => {
    const calls: { cmd: string; path: string }[] = [];
    setInvoker(async (cmd, args) => {
      const path = String(args?.path);
      calls.push({ cmd, path });
      if (cmd === "component_list") return path === "." ? { entries: [
        { name: "src", path: "src", kind: "directory" },
        { name: "README.md", path: "README.md", kind: "file" },
        { name: "logo.png", path: "logo.png", kind: "file" },
        { name: "binary.bin", path: "binary.bin", kind: "file" },
      ], truncated: false } : { entries: [{ name: "main.go", path: "src/main.go", kind: "file" }], truncated: false };
      if (cmd === "component_read") {
        if (path === "README.md") return { kind: "markdown", content: "# QA readme\n\nHello.", dataUrl: null, reason: null };
        if (path === "src/main.go") return { kind: "code", content: "package main", dataUrl: null, reason: null };
        if (path === "logo.png") return { kind: "image", content: null, dataUrl: "data:image/png;base64,iVBORw0KGgo=", reason: null };
        return { kind: "binary", content: null, dataUrl: null, reason: "This file type cannot be opened in the preview." };
      }
      throw new Error(`unexpected ${cmd}`);
    });
    render(<ComponentFileBrowser root="/tmp/aeon-qa" />);
    expect(calls).toHaveLength(0);
    fireEvent.click(screen.getByRole("button", { name: "Browse component" }));
    const tree = within(screen.getByRole("navigation", { name: "Component files" }));
    fireEvent.click(await tree.findByRole("treeitem", { name: "src" }));
    fireEvent.click(await tree.findByRole("treeitem", { name: "main.go" }));
    expect(await screen.findByText("package main")).toBeTruthy();
    fireEvent.click(tree.getByRole("treeitem", { name: "README.md" }));
    expect(await screen.findByRole("heading", { name: "QA readme" })).toBeTruthy();
    fireEvent.click(tree.getByRole("treeitem", { name: "logo.png" }));
    await waitFor(() => expect(screen.getByRole("img", { name: "logo.png" }).getAttribute("src")).toMatch(/^data:image\/png;base64,/));
    fireEvent.click(tree.getByRole("treeitem", { name: "binary.bin" }));
    expect(await screen.findByText("This file type cannot be opened in the preview.")).toBeTruthy();
    expect(calls.filter(({ cmd }) => cmd === "component_list").map(({ path }) => path)).toEqual([".", "src"]);
  });
});
