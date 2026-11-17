import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { I18nProvider } from "@cjlapao/ui-kit";
import { APP_LOCALES } from "../../lib/i18n";
import { CodeBlock } from "../../components/CodeBlock";
import { HandoffView } from "./AgentFields";

const wrap = (node: React.ReactNode) => render(<I18nProvider locales={APP_LOCALES}>{node}</I18nProvider>);

describe("HandoffView", () => {
  it("lists each field with its purpose, and marks a `?` field optional", () => {
    wrap(<HandoffView handoff={'{"summary":"what a reader needs","tests_added?":"paths of tests"}'} />);
    const summary = screen.getByTestId("agent-handoff-summary");
    expect(summary.textContent).toContain("what a reader needs");
    expect(summary.textContent).toContain("Required");
    const tests = screen.getByTestId("agent-handoff-tests_added");
    expect(tests.textContent).toContain("Optional");
    expect(tests.textContent).not.toContain("?");
  });

  it("says an empty handoff is empty, and prints a shape it does not know as colored JSON", () => {
    const { unmount } = wrap(<HandoffView handoff="{}" />);
    expect(screen.getByTestId("agent-tab-handoff").textContent).toBe("no handoff");
    unmount();
    wrap(<HandoffView handoff={'{"a":{"b":1}}'} />);
    expect(screen.getByTestId("agent-tab-handoff").getAttribute("data-language")).toBe("json");
  });
});

describe("CodeBlock", () => {
  it("colors tokens as spans and never turns the code into markup", () => {
    render(<CodeBlock code={'{"a": "<img src=x onerror=boom()>"}'} language="json" data-testid="cb" />);
    const block = screen.getByTestId("cb");
    expect(block.querySelector("img")).toBeNull();
    expect(block.textContent).toContain("<img src=x onerror=boom()>");
    expect(block.querySelectorAll("span").length).toBeGreaterThan(3);
  });
});
