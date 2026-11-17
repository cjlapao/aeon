import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { I18nProvider } from "@cjlapao/ui-kit";
import { ToolAddressPicker } from "./ToolAddressPicker";
import { APP_LOCALES } from "../../../../lib/i18n";
import { LiveController } from "../../../../lib/live";
import type { LiveState } from "../../../../lib/live";
import { installFakeDaemon, resetFakeDaemon } from "../../../../../test/helpers";

const live = {
  state: "current", endpoint: "/run/user/1000/aeon/rpc.sock", daemonVersion: "9.9.9", protocolVersion: 7, generation: 1,
} as const satisfies LiveState;

const tool = (name: string, category: string, access: string) => ({
  name, family: category, category, access, address: `${category}.${name}.${access}`, source: "system",
  description: "", schema: {}, schemaHash: "", rerunClass: "safe", capabilities: [], governed: [],
  conditional: false, available: true, classified: false, version: 1, origin: "system", rowCurrent: true,
});

beforeEach(() => {
  window.matchMedia = vi.fn(() => ({ matches: false, media: "", onchange: null, addEventListener: () => {}, removeEventListener: () => {},
    addListener: () => {}, removeListener: () => {}, dispatchEvent: () => false })) as unknown as typeof window.matchMedia;
});
afterEach(() => { cleanup(); resetFakeDaemon(); });

function mount(value: string, onChange = vi.fn()) {
  const daemon = installFakeDaemon({
    responses: {
      "tools.list": { tools: [tool("glob", "workspace", "read"), tool("write", "workspace", "write")], total: 2 },
      // The preview is the DAEMON's answer; the fake stands in for it.
      "tools.matchPattern": (p: unknown) => {
        const pattern = (p as { pattern: string }).pattern;
        if (pattern.includes("*") && pattern.split(".").some((s) => s.length > 1 && s.includes("*"))) {
          throw new Error('segment 1 of toolName "' + pattern + '" is a partial wildcard; use a whole * or a literal');
        }
        return pattern === "workspace.*.read"
          ? { pattern, matches: ["workspace.glob.read"], count: 1 }
          : { pattern, matches: [], count: 0 };
      },
    },
  });
  render(
    <I18nProvider locales={APP_LOCALES}>
      <ToolAddressPicker value={value} onChange={onChange} controller={new LiveController()} live={live} />
    </I18nProvider>,
  );
  return { daemon, onChange };
}

describe("ToolAddressPicker", () => {
  it("offers the catalog grouped by category with the wildcard rows, and choosing one fills the pattern", async () => {
    const { onChange } = mount("");
    const picker = (await screen.findByLabelText("Pick a tool or a pattern")) as HTMLSelectElement;
    await waitFor(() => expect(picker.disabled).toBe(false));
    const values = Array.from(picker.querySelectorAll("option")).map((o) => o.value);
    expect(values).toContain("*.*.read");
    expect(values).toContain("workspace.*.read");
    expect(values).toContain("workspace.glob.read");
    fireEvent.change(picker, { target: { value: "workspace.*.read" } });
    expect(onChange).toHaveBeenCalledWith("workspace.*.read");
  });

  it("shows what the daemon says the pattern matches today", async () => {
    mount("workspace.*.read");
    const line = await screen.findByTestId("tool-match-count", {}, { timeout: 2000 });
    expect(line.textContent).toContain("Matches 1 tools");
    expect(line.textContent).toContain("workspace.glob.read");
  });

  it("says plainly when a pattern matches no tool today", async () => {
    mount("process.*.read");
    expect(await screen.findByTestId("tool-match-none", {}, { timeout: 2000 })).toBeTruthy();
  });

  it("shows the boundary's refusal for a malformed pattern, not a count", async () => {
    mount("wo*.*.read");
    const refused = await screen.findByTestId("tool-match-refused", {}, { timeout: 2000 });
    expect(refused.textContent).toContain("segment 1");
  });
});
