import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { installFakeDaemon, resetFakeDaemon } from "./helpers";
import { ProductApp } from "../src/app/router";
import { applyTokenInsert, mergeSpecialKeyHandlers } from "../src/lib/contextPickers";
import { attachmentLaunchGate, declaredType } from "../src/lib/attachments";
import { mintLaunchSlug } from "../src/features/home/pages/HomeLaunchComposer";

// S77 home integration: the composer's panels are CONTEXT-AWARE — `@`
// speaks only once a component is chosen (and then about ITS files), `//`
// lists the loops that live there and navigating IS the pick, `$` names
// agents — and a staged attachment the chosen route cannot carry DISABLES
// the launch with the gate's own sentence before any byte is put.

URL.createObjectURL = vi.fn(() => "blob:fixture-image");
URL.revokeObjectURL = vi.fn();
// jsdom's File has no arrayBuffer — the reader jsdom DOES have stands in,
// so the put path runs the real `toBase64(await file.arrayBuffer())` bytes.
if (!File.prototype.arrayBuffer) {
  (File.prototype as unknown as { arrayBuffer: () => Promise<ArrayBuffer> }).arrayBuffer = function (this: Blob) {
    return new Promise<ArrayBuffer>((resolve, reject) => {
      const reader = new FileReader();
      reader.onload = () => resolve(reader.result as ArrayBuffer);
      reader.onerror = () => reject(reader.error);
      reader.readAsArrayBuffer(this);
    });
  };
}

function component(overrides: Record<string, unknown> = {}) {
  return {
    id: "c1", name: "aeon-core", slug: "aeon-core", root: "/work/aeon",
    icon: "", owner: "", remote: "", remoteKey: "", branchPrefix: "", defaultBranch: "main",
    description: "", shortDescription: "", tags: [], projectId: "p1", createdAt: "2026-01-01T00:00:00Z",
    updatedAt: "2026-01-01T00:00:00Z", ...overrides,
  };
}
function loop(overrides: Record<string, unknown> = {}) {
  return {
    id: "l9", slug: "ship-auth", title: "", status: "running", phase: "build", mode: "standard",
    componentRoot: "/work/aeon", awaiting: false, startedAt: "2026-01-01T00:00:00Z", updatedAt: "2026-01-02T00:00:00Z",
    ...overrides,
  };
}
function route(id: string, capabilities?: string[]) {
  return {
    id, provider: id, model: "m", hasKey: true, maxOutputTokens: 100, priced: false, source: "db",
    ...(capabilities ? { capabilities } : {}),
  };
}

const baseResponses = (routes: unknown[]) => ({
  "loop.list": { loops: [loop()], all: true },
  "component.list": { components: [component()] },
  "blueprint.list": { names: ["two-step"] },
  "blueprint.validate": { diagnostics: [] },
  "routes.list": { routes, providers: routes.map((r) => (r as { id: string }).id), persisted: true },
  "skills.list": { skills: [], userRoot: "/u", managedRoot: "/m", scannedAt: "2026-01-01T00:00:00Z", summary: { valid: 0, warn: 0, invalid: 0, shadowed: 0 } },
  "subscribe": { subscriptionId: "s1", from: 0 },
  "workspace.git.status": { branch: "main", ahead: 0, behind: 0, detached: false, state: "clean", upstream: "", changed: [
    { path: "src/a.ts", index: "modified", worktree: "modified" }, { path: "src/b_test.ts", index: "", worktree: "modified" },
  ] },
  "search.query": { hits: [{ source: "src/a.ts", lineStart: 12, lineEnd: 20, score: 0.8 }] },
  "agent.list": { agents: [{ name: "read-only", provenance: "builtin", state: "ready" }] },
  "loop.show": { id: "l9", slug: "ship-auth", blueprint: "two-step", goal: "Ship auth", root: "/work/aeon", status: "running", position: { lane: "build" }, lanes: [] },
  "loop.start": { id: "l1", slug: "l1", status: "running", blueprint: "two-step" },
  "attachment.put": { receipt: { name: "diagram.png", document: "doc-1", rev: 1, mediaType: "image/png", size: 4, sha256: "aa" } },
});

function composerInput() {
  return screen.getByPlaceholderText(/Describe a task/) as HTMLTextAreaElement;
}
function pickerButton(fieldTestId: string) {
  return within(screen.getByTestId(fieldTestId)).getByRole("button");
}
function panel() {
  return screen.getByTestId("agent-chat-picker");
}
async function pickComponent() {
  fireEvent.click(pickerButton("composer-component-field"));
  fireEvent.click(within(screen.getByRole("listbox")).getByRole("option", { name: /aeon-core/ }));
}

beforeEach(() => {
  vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
  vi.stubGlobal("matchMedia", () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
  window.location.hash = "#/";
});
afterEach(() => { cleanup(); resetFakeDaemon(); window.location.hash = "#/"; vi.unstubAllGlobals(); });

async function openHome(responses: Record<string, unknown>) {
  const daemon = installFakeDaemon({
    negotiated: { daemonVersion: "1.0", protocolVersion: 1, endpoint: "/tmp/aeon.sock" },
    responses,
  });
  render(<ProductApp />);
  await screen.findByTestId("product-home");
  return daemon;
}

describe("the context-aware @ panel", () => {
  it("gates @ until a component is chosen, then lists and inserts ITS files", async () => {
    const daemon = await openHome(baseResponses([route("plain")]));

    // Gated: the panel speaks, names the missing context, and NO workspace
    // door is called for a context that does not exist. The typed token
    // stays exactly where the person left it — a gated panel deletes nothing.
    fireEvent.change(composerInput(), { target: { value: "@src" } });
    await waitFor(() => expect(panel()).toBeTruthy());
    expect(within(panel()).getByText("Choose a component first — @ then targets its files.")).toBeTruthy();
    expect(daemon.calls.some((call) => call.method === "workspace.git.status")).toBe(false);
    expect(composerInput().value).toBe("@src");

    // Chosen: the same token now answers with THAT component's changed files.
    await pickComponent();
    fireEvent.change(composerInput(), { target: { value: "@src" } });
    await waitFor(() => expect(within(panel()).getByText("src/a.ts")).toBeTruthy());

    // Picking inserts the path where the token stood, sigil and all.
    fireEvent.click(within(panel()).getByRole("button", { name: /src\/a\.ts/ }));
    await waitFor(() => expect(composerInput().value).toBe("@src/a.ts "));
    expect(screen.queryByTestId("agent-chat-picker")).toBeNull();
  });
});

describe("the // and $ panels", () => {
  it("lists this component's loops and navigating IS the pick", async () => {
    await openHome(baseResponses([route("plain")]));
    await pickComponent();
    fireEvent.change(composerInput(), { target: { value: "//ship" } });
    await waitFor(() => expect(panel()).toBeTruthy());
    fireEvent.click(within(panel()).getByRole("button", { name: /ship-auth/ }));
    await waitFor(() => expect(window.location.hash).toBe("#/loops/l9"));
  });

  it("lists agents for $ and inserts the chosen name into the draft", async () => {
    await openHome(baseResponses([route("plain")]));
    fireEvent.change(composerInput(), { target: { value: "$re" } });
    await waitFor(() => expect(within(panel()).getByText("read-only")).toBeTruthy());
    fireEvent.click(within(panel()).getByRole("button", { name: /read-only/ }));
    await waitFor(() => expect(composerInput().value).toBe("$read-only "));
  });
});

describe("the attachment capability gate", () => {
  it("disables the launch while the route cannot carry the file, then puts once and starts with the receipt", async () => {
    const daemon = await openHome(baseResponses([route("plain"), route("vision", ["image", "file", "tool-calling"])]));

    fireEvent.change(composerInput(), { target: { value: "Ship it" } });
    await pickComponent();

    const png = new File([new Uint8Array([0x89, 0x50, 0x4e, 0x47])], "diagram.png", { type: "image/png" });
    fireEvent.change(screen.getByLabelText("Attach files"), { target: { files: [png] } });

    // The route is the FIRST one — it never declared `image`, and absent is
    // not yes (P31). The launch is disabled and the reason is spoken.
    const launch = await screen.findByRole("button", { name: "Launch loop" });
    await waitFor(() => expect(launch.hasAttribute("disabled")).toBe(true));
    expect(screen.getByText(/needs the "image" capability, which route 'plain' does not declare/)).toBeTruthy();

    // Removing the staged file re-enables — nothing staged is un-removable.
    fireEvent.click(screen.getByRole("button", { name: "Remove diagram.png" }));
    await waitFor(() => expect(launch.hasAttribute("disabled")).toBe(false));

    // Stage it again and choose the vision route: the gate clears, and the
    // launch PUTS the bytes first (under the claimed slug + component), then
    // starts carrying the receipt — refs, never bytes, on loop.start.
    fireEvent.change(screen.getByLabelText("Attach files"), { target: { files: [png] } });
    await waitFor(() => expect(launch.hasAttribute("disabled")).toBe(true));
    fireEvent.click(screen.getByRole("button", { name: "Advanced options ▾" }));
    fireEvent.click(pickerButton("composer-provider-field"));
    const vision = await within(screen.getByRole("listbox")).findByRole("option", { name: /vision/ });
    fireEvent.click(vision);
    await waitFor(() => expect(launch.hasAttribute("disabled")).toBe(false));
    fireEvent.click(launch);

    await waitFor(() => expect(daemon.methods()).toContain("attachment.put"));
    await waitFor(() => expect(daemon.paramsOf("loop.start")).toMatchObject({ attachments: [{ document: "doc-1", rev: 1 }] }));
    const methods = daemon.methods();
    expect(methods.indexOf("attachment.put")).toBeLessThan(methods.indexOf("loop.start"));
    expect(methods.filter((m) => m === "attachment.put")).toHaveLength(1); // bytes once, refs forever
    const put = daemon.calls.find((call) => call.method === "attachment.put")?.params as Record<string, unknown>;
    expect(String(put.slug)).toMatch(/^ship-it-[a-z0-9]+$/); // minted from the goal
    expect(put.componentRoot).toBe("/work/aeon");
    expect(put.mediaType).toBe("image/png");
    const started = daemon.paramsOf("loop.start") as Record<string, unknown>;
    expect(started.slug).toBe(put.slug); // receipts checked against the scope they were put under
    expect(started.goal).toBe("Ship it"); // the person's words, undecorated
    expect(started.route).toBe("vision");
  });
});

describe("the pure helpers", () => {
  it("applyTokenInsert splices at the token and keeps the rest of the draft", () => {
    const text = "ping @src/se";
    const request = { key: "@", query: "src/se", start: 5, end: 12 };
    expect(applyTokenInsert(text, request, "@src/server.ts ")).toBe("ping @src/server.ts ");
  });

  it("mergeSpecialKeyHandlers lets every picker see every request", () => {
    const seen: Array<string | null> = [];
    const merged = mergeSpecialKeyHandlers([
      (request) => { if (request?.key === "/") seen.push("/"); },
      (request) => { seen.push(request && request.key === "@" ? "@" : null); },
    ]);
    merged({ key: "@", query: "", start: 0, end: 1 });
    merged({ key: "/", query: "", start: 0, end: 1 });
    merged(null);
    expect(seen).toEqual(["@", "/", null, null]); // skills took "/", the context handler closed on it
  });

  it("the gate speaks per file: undeclared capability and off-allow-list bytes", () => {
    const png = new File([new Uint8Array()], "a.png", { type: "image/png" });
    expect(attachmentLaunchGate([png], { id: "r1" })).toHaveLength(1); // absent capabilities ≠ yes
    expect(attachmentLaunchGate([png], { id: "r1", capabilities: ["image"] })).toEqual([]);
    const blob = new File([new Uint8Array()], "mystery", { type: "" });
    expect(declaredType(blob)).toBe("application/octet-stream");
    expect(attachmentLaunchGate([blob], { id: "r1", capabilities: ["image"] })[0]).toMatch(/none of those/);
    expect(mintLaunchSlug("Ship it!")).toMatch(/^ship-it-[a-z0-9]+$/);
    expect(mintLaunchSlug("?!")).toMatch(/^launch-[a-z0-9]+$/);
  });
});
