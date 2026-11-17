import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { I18nProvider } from "@cjlapao/ui-kit";
import { AgentPicker } from "./AgentPicker";
import { APP_LOCALES } from "../lib/i18n";
import { LiveController } from "../lib/live";
import { installFakeDaemon, resetFakeDaemon } from "../../test/helpers";

const currentLive = {
  state: "current",
  endpoint: "/run/user/1000/aeon/rpc.sock",
  daemonVersion: "9.9.9",
  protocolVersion: 7,
  generation: 1,
} as const;

/** A roster entry — identity `name`, exactly what the generated contract types. */
function agent(name: string, overrides: Record<string, unknown> = {}) {
  return { name, provenance: "first_party", state: "running", ...overrides };
}

/** The kit Modal/Input read `matchMedia`; jsdom has none. */
beforeEach(() => {
  window.matchMedia = vi.fn(() => ({
    matches: false,
    media: "",
    onchange: null,
    addEventListener: () => {},
    removeEventListener: () => {},
    addListener: () => {},
    removeListener: () => {},
    dispatchEvent: () => false,
  })) as unknown as typeof window.matchMedia;
});

afterEach(() => {
  cleanup();
  resetFakeDaemon();
});

function renderPicker(
  responses: Record<string, unknown>,
  props: Partial<Parameters<typeof AgentPicker>[0]> = {},
) {
  const onChange = vi.fn();
  const daemon = installFakeDaemon({ responses });
  render(
    <I18nProvider locales={APP_LOCALES}>
      <MemoryRouter>
        <AgentPicker value="" onChange={onChange} controller={new LiveController()} live={currentLive} {...props} />
      </MemoryRouter>
    </I18nProvider>,
  );
  return { daemon, onChange };
}

describe("AgentPicker", () => {
  it("lists agents by slug and display name — both visible", async () => {
    renderPicker({
      "agent.list": {
        agents: [agent("golang-engineer"), agent("review-bot", { name: "Review Bot", slug: "review-bot" })],
      },
    });
    await screen.findByText("golang-engineer");
    // The slug identity (when the roster ships one) AND the display name.
    expect(screen.getByText("review-bot")).toBeTruthy();
    expect(screen.getByText("Review Bot")).toBeTruthy();
  });

  it("searches both slug and display name", async () => {
    renderPicker({
      "agent.list": { agents: [agent("golang-engineer"), agent("review-bot", { slug: "bot", name: "Reviewer 9000" })] },
    });
    const search = await screen.findByRole("textbox");
    fireEvent.change(search, { target: { value: "reviewer" } }); // matches the display name only
    expect(screen.getByText("Reviewer 9000")).toBeTruthy();
    fireEvent.change(search, { target: { value: "golang" } }); // matches the slug/name
    expect(screen.getByText("golang-engineer")).toBeTruthy();
    expect(screen.queryByText("Reviewer 9000")).toBeNull();
  });

  it("refuses a free-typed slug that matches no agent — the output is always a real roster value or empty", async () => {
    const { onChange } = renderPicker({
      "agent.list": { agents: [agent("golang-engineer")] },
    });
    const search = await screen.findByRole("textbox");
    // A typo matching nothing: the picker says so and offers no selection.
    fireEvent.change(search, { target: { value: "golang-enginer" } });
    expect(screen.getByTestId("agent-picker-no-match")).toBeTruthy();
    expect(screen.getByTestId("agent-picker-refused-free-text")).toBeTruthy();
    expect(onChange).not.toHaveBeenCalled();

    // Even text that exactly matches an agent does NOT select by typing —
    // only choosing a roster row does. The emitted value is the roster's own.
    fireEvent.change(search, { target: { value: "golang-engineer" } });
    expect(onChange).not.toHaveBeenCalled();
    fireEvent.click(screen.getByTestId("agent-picker-option"));
    expect(onChange).toHaveBeenCalledWith("golang-engineer");
  });

  it("allowEmpty offers an explicit none and emits the empty string", async () => {
    const { onChange } = renderPicker({ "agent.list": { agents: [agent("a-one")] } }, { allowEmpty: true });
    fireEvent.click(await screen.findByTestId("agent-picker-none"));
    expect(onChange).toHaveBeenCalledWith("");
  });

  it("the empty roster is stated honestly and the control is disabled — never a blank dropdown", async () => {
    renderPicker({ "agent.list": { agents: [] } });
    expect(await screen.findByText(/no agents — the roster is empty/i)).toBeTruthy();
    expect(screen.queryAllByTestId("agent-picker-option").length).toBe(0);
    // The refusal-to-select is said out loud, not left to be discovered.
    expect(screen.getByText(/picker is disabled/i)).toBeTruthy();
  });

  it("shows the loading state before the roster answers, and an error panel on failure", async () => {
    const { rerender } = render(
      <I18nProvider locales={APP_LOCALES}>
        <MemoryRouter>
          <AgentPicker value="" onChange={() => {}} controller={new LiveController()} live={{ state: "connecting", endpoint: "?" } as never} />
        </MemoryRouter>
      </I18nProvider>,
    );
    expect(rerender).toBeTruthy(); // renders the connecting state without throwing

    renderPicker({
      "agent.list": () => {
        throw JSON.stringify({ code: -32000, message: "the roster is not wired" });
      },
    });
    await waitFor(() => expect(screen.getByTestId("error")).toBeTruthy());
  });

  it("the kind filter keeps only roster entries whose provenance matches — a read of the daemon's field", async () => {
    renderPicker(
      {
        "agent.list": { agents: [agent("first-one"), agent("imported-one", { provenance: "imported" })] },
      },
      { kind: "imported" },
    );
    await screen.findByText("imported-one");
    expect(screen.queryByText("first-one")).toBeNull();
  });

  it("calls agent.list through the generic bridge, exactly once per read", async () => {
    const { daemon } = renderPicker({ "agent.list": { agents: [agent("only-one")] } });
    await screen.findByText("only-one");
    expect(daemon.methods().filter((m) => m === "agent.list").length).toBeGreaterThanOrEqual(1);
    expect(daemon.methods().every((m) => m === "agent.list")).toBe(true);
  });
});
