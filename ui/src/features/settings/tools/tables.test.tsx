import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { I18nProvider } from "@cjlapao/ui-kit";
import { ToolsTable } from "./ToolsTable";
import { AgentRosterTable, identityOf } from "./AgentRosterTable";
import { APP_LOCALES } from "../../../lib/i18n";
import type { ToolRow } from "./toolsModel";

// S75: the Tools page's two tables — paged, sorted over the whole list, with the
// agent roster searchable and quick-filterable. The rows are stand-ins for a
// daemon answer; the behaviour under test is the table's.

const tool = (name: string): ToolRow => ({
  name, family: "workspace", category: "workspace", access: "read", address: `workspace.${name}.read`, source: "system",
  description: "", schema: {}, schemaHash: "", rerunClass: "safe", capabilities: [], governed: [],
  conditional: false, available: true, classified: false, version: 1, origin: "system", rowCurrent: true,
} as ToolRow);

const agent = (name: string, provenance: string, state: string) => ({ name, provenance, state }) as never;

beforeEach(() => {
  window.matchMedia = vi.fn(() => ({ matches: false, media: "", onchange: null, addEventListener: () => {}, removeEventListener: () => {},
    addListener: () => {}, removeListener: () => {}, dispatchEvent: () => false })) as unknown as typeof window.matchMedia;
});
afterEach(cleanup);

const wrap = (node: React.ReactNode) => render(<I18nProvider locales={APP_LOCALES}>{node}</I18nProvider>);
const rows = (prefix: string) => document.querySelectorAll(`[data-testid^="${prefix}"]`).length;

describe("ToolsTable", () => {
  it("reveals a long list in steps, with the count in the footer", async () => {
    const tools = Array.from({ length: 50 }, (_, i) => tool(`t${String(i).padStart(2, "0")}`));
    wrap(<ToolsTable tools={tools} onSelect={() => {}} />);
    expect(rows("tool-row-")).toBe(40);
    expect(screen.getByTestId("tools-count").textContent).toBe("Showing 40 of 50");
  });

  it("selects the tool on a row click", async () => {
    const onSelect = vi.fn();
    wrap(<ToolsTable tools={[tool("glob")]} onSelect={onSelect} />);
    fireEvent.click(await screen.findByTestId("tool-row-glob"));
    expect(onSelect).toHaveBeenCalledWith("glob");
  });

  it("says the daemon's own sentence when the filter matched nothing", () => {
    wrap(<ToolsTable tools={[]} statement="no tool matches those filters" onSelect={() => {}} />);
    expect(screen.getByTestId("tools-empty-statement").textContent).toBe("no tool matches those filters");
  });
});

describe("AgentRosterTable", () => {
  const agents = [agent("atlas", "first-party", "active"), agent("borealis", "custom", "active"), agent("cirrus", "custom", "idle")];

  it("lists every agent and narrows by search", async () => {
    wrap(<AgentRosterTable agents={agents} loading={false} selected="" onSelect={() => {}} />);
    expect(rows("agent-row-name-")).toBe(3);
    fireEvent.change(await screen.findByPlaceholderText("Search agents by name"), { target: { value: "bore" } });
    await waitFor(() => expect(rows("agent-row-name-")).toBe(1), { timeout: 2000 });
    expect(screen.getByTestId("agent-row-name-borealis")).toBeTruthy();
  });

  it("offers quick filters built from the roster, each with its count", async () => {
    wrap(<AgentRosterTable agents={agents} loading={false} selected="" onSelect={() => {}} />);
    const bar = await screen.findByTestId("agents-filter-provenance");
    expect(bar.textContent).toContain("custom (2)");
    expect(bar.textContent).toContain("first-party (1)");
    fireEvent.click(screen.getByText("custom (2)"));
    await waitFor(() => expect(rows("agent-row-name-")).toBe(2));
  });

  it("hands the chosen agent up on a row click", async () => {
    const onSelect = vi.fn();
    wrap(<AgentRosterTable agents={agents} loading={false} selected="" onSelect={onSelect} />);
    fireEvent.click(await screen.findByTestId("agent-row-name-cirrus"));
    expect(onSelect).toHaveBeenCalledWith(identityOf(agents[2]!));
  });
});
