import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { installFakeDaemon, resetFakeDaemon } from "./helpers";

// The terminal dock: the kit's TerminalTabs wired to the daemon's terminal.*
// methods. xterm needs a canvas jsdom does not have, so the kit's tabs are
// replaced by a fake that records what the dock hands it and lets a test
// drive the callbacks a real terminal would fire.

const fake = vi.hoisted(() => ({ props: undefined as undefined | Record<string, any> }));
vi.mock("../src/components/kit", async (original) => {
  const actual = await original<typeof import("../src/components/kit")>();
  return { ...actual, TerminalTabs: (props: Record<string, any>) => { fake.props = props; return <div data-testid="tabs">{props.sessions.map((s: any) => <span key={s.id}>{s.label}</span>)}</div>; } };
});
import { TerminalDock } from "../src/features/loop-workspace/center/TerminalDock";

beforeEach(() => { resetFakeDaemon(); fake.props = undefined; });
afterEach(() => { cleanup(); resetFakeDaemon(); });

const b64 = (text: string) => btoa(text);
const opened = (id: string, cwd = "/repo/aeon-qa-workspace") => ({ terminalId: id, shell: "/bin/zsh", cwd });
const handle = () => ({ write: vi.fn(), writeln: vi.fn(), clear: vi.fn(), reset: vi.fn(), focus: vi.fn(), fit: vi.fn() });

function dock(over: Record<string, unknown> = {}) {
  return render(<TerminalDock componentRoot="/repo" open height={240} onHeightChange={() => {}} onClose={() => {}} {...over} />);
}

describe("the terminal dock", () => {
  it("opening the dock opens a shell in the project root", async () => {
    const daemon = installFakeDaemon({ responses: { "terminal.list": [], "terminal.open": opened("t-1") } });
    dock();
    expect(await screen.findByText("zsh · aeon-qa-workspace")).toBeTruthy();
    expect(daemon.paramsOf("terminal.open")).toEqual({ componentRoot: "/repo", cols: 80, rows: 24 });
  });

  it("is always the dark terminal, whatever the app theme", async () => {
    installFakeDaemon({ responses: { "terminal.list": [], "terminal.open": opened("t-1") } });
    dock();
    await screen.findByText("zsh · aeon-qa-workspace");
    expect(fake.props!.scheme).toBe("dark");
  });

  it("switching to another project opens that project's shell and leaves the old one's behind", async () => {
    let n = 0;
    const daemon = installFakeDaemon({ responses: { "terminal.list": [], "terminal.open": () => (++n === 1 ? opened("t-1", "/a/one") : opened("t-2", "/b/two")) } });
    const view = dock({ componentRoot: "/a" });
    await screen.findByText("zsh · one");
    view.rerender(<TerminalDock componentRoot="/b" open height={240} onHeightChange={() => {}} onClose={() => {}} />);
    expect(await screen.findByText("zsh · two")).toBeTruthy();
    expect(screen.queryByText("zsh · one")).toBeNull();
    expect(daemon.calls.filter((c) => c.method === "terminal.open").map((c) => (c.params as { componentRoot: string }).componentRoot)).toEqual(["/a", "/b"]);
  });

  it("does nothing while closed", async () => {
    const daemon = installFakeDaemon({ responses: {} });
    dock({ open: false });
    await act(async () => { await Promise.resolve(); });
    expect(daemon.methods()).toEqual([]);
  });

  it("reattaches a terminal that outlived the window instead of opening a second one", async () => {
    const daemon = installFakeDaemon({ responses: {
      "terminal.list": [{ terminalId: "t-9", componentRoot: "/repo", cwd: "/repo/x", shell: "/bin/zsh", exited: false }],
      "terminal.attach": { scrollback: b64("hello\r\n"), seq: 3 },
    } });
    dock();
    expect(await screen.findByText("zsh · x")).toBeTruthy();
    expect(daemon.methods()).not.toContain("terminal.open");
    expect(daemon.paramsOf("terminal.attach")).toEqual({ terminalId: "t-9" });
    expect(new TextDecoder().decode(fake.props!.sessions[0].initialData)).toBe("hello\r\n");
  });

  it("sends keystrokes to terminal.input and the size to terminal.resize", async () => {
    const daemon = installFakeDaemon({ responses: { "terminal.list": [], "terminal.open": opened("t-1") } });
    dock();
    await screen.findByText("zsh · aeon-qa-workspace");
    act(() => fake.props!.onData("t-1", "ls\r"));
    act(() => fake.props!.onResize("t-1", { cols: 100, rows: 30 }));
    await waitFor(() => expect(daemon.methods()).toContain("terminal.resize"));
    expect(daemon.calls.find((c) => c.method === "terminal.input")!.params).toEqual({ terminalId: "t-1", data: "ls\r" });
    expect(daemon.paramsOf("terminal.resize")).toEqual({ terminalId: "t-1", cols: 100, rows: 30 });
  });

  it("adds a numbered tab, and closing a tab closes its terminal", async () => {
    let n = 0;
    const daemon = installFakeDaemon({ responses: { "terminal.list": [], "terminal.open": () => opened(`t-${++n}`) } });
    dock();
    await screen.findByText("zsh · aeon-qa-workspace");
    act(() => fake.props!.onAdd());
    expect(await screen.findByText("zsh 2")).toBeTruthy();
    act(() => fake.props!.onClose("t-1"));
    await waitFor(() => expect(screen.queryByText("zsh · aeon-qa-workspace")).toBeNull());
    expect(daemon.paramsOf("terminal.close")).toEqual({ terminalId: "t-1" });
  });

  it("writes output to the right tab, holding what arrives before the terminal is ready", async () => {
    const daemon = installFakeDaemon({ responses: { "terminal.list": [], "terminal.open": opened("t-1") } });
    dock();
    await screen.findByText("zsh · aeon-qa-workspace");
    await waitFor(() => expect(daemon.subscribes()).toContain("aeon://terminal"));
    act(() => daemon.emitTerminal({ terminalId: "t-1", seq: 1, data: b64("early") }));
    const h = handle();
    act(() => fake.props!.onReady("t-1", h));
    expect(new TextDecoder().decode(h.write.mock.calls[0]![0])).toBe("early");
    act(() => daemon.emitTerminal({ terminalId: "t-1", seq: 2, data: b64("later") }));
    act(() => daemon.emitTerminal({ terminalId: "other", seq: 1, data: b64("NOT MINE") }));
    expect(h.write).toHaveBeenCalledTimes(2);
    expect(new TextDecoder().decode(h.write.mock.calls[1]![0])).toBe("later");
  });

  it("says a shell exited and stops accepting input for it", async () => {
    const daemon = installFakeDaemon({ responses: { "terminal.list": [], "terminal.open": opened("t-1") } });
    dock();
    await screen.findByText("zsh · aeon-qa-workspace");
    await waitFor(() => expect(daemon.subscribes()).toContain("aeon://terminal"));
    const h = handle();
    act(() => fake.props!.onReady("t-1", h));
    act(() => daemon.emitTerminal({ terminalId: "t-1", exitCode: 3 }));
    expect(h.writeln.mock.calls.at(-1)![0]).toMatch(/exited.*3/);
    expect(fake.props!.sessions[0].readOnly).toBe(true);
  });

  it("shows why a shell could not be opened", async () => {
    installFakeDaemon({ responses: { "terminal.list": [], "terminal.open": () => { throw new Error("terminal sessions are not available on Windows"); } } });
    dock();
    expect((await screen.findByRole("alert")).textContent).toMatch(/not available on Windows/);
  });

  it("resizes from the keyboard and never below the minimum", () => {
    installFakeDaemon({ responses: { "terminal.list": [], "terminal.open": opened("t-1") } });
    const heights: number[] = [];
    dock({ height: 170, onHeightChange: (h: number) => heights.push(h) });
    const grip = screen.getByRole("separator");
    fireEvent.keyDown(grip, { key: "ArrowUp" });
    fireEvent.keyDown(grip, { key: "ArrowDown" });
    expect(heights).toEqual([194, 160]);
  });
});
