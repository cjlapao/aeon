import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { I18nProvider } from "@cjlapao/ui-kit";
import { ToolsTable } from "./ToolsTable";
import { APP_LOCALES } from "../../../lib/i18n";
import type { ToolRow } from "./toolsModel";

// S74 T4: every row shows the address the policy form's patterns match against.

const row = (name: string, family: string, access: string): ToolRow => ({
  name, family, category: family, access, address: `${family}.${name}.${access}`, source: "system", description: "",
  schema: {}, schemaHash: "", rerunClass: "safe", capabilities: [], governed: [], conditional: false,
  available: true, classified: false, version: 1, origin: "system", rowCurrent: true,
} as ToolRow);

beforeEach(() => {
  window.matchMedia = vi.fn(() => ({ matches: false, media: "", onchange: null, addEventListener: () => {}, removeEventListener: () => {},
    addListener: () => {}, removeListener: () => {}, dispatchEvent: () => false })) as unknown as typeof window.matchMedia;
});
afterEach(cleanup);

describe("ToolsTable address", () => {
  it("shows the address and the access word the daemon computed", () => {
    render(
      <I18nProvider locales={APP_LOCALES}>
        <ToolsTable tools={[row("glob", "workspace", "read"), row("write", "workspace", "write")]} onSelect={() => {}} />
      </I18nProvider>,
    );
    expect(screen.getByTestId("tool-address-glob").textContent).toBe("workspace.glob.read");
    expect(screen.getByTestId("tool-access-glob").textContent).toBe("read");
    expect(screen.getByTestId("tool-access-write").textContent).toBe("write");
  });
});
