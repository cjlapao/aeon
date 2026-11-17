import { fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";
import { describe, expect, it } from "vitest";
import { I18nProvider } from "@cjlapao/ui-kit";
import { APP_LOCALES } from "../../lib/i18n";
import { HandoffBuilder } from "./HandoffBuilder";
import { handoffJson, handoffMap, handoffRows } from "./agentsModel";

describe("handoff rows", () => {
  it("reads the column's object, a ? on the name meaning optional, and writes it back", () => {
    const rows = handoffRows('{"summary":"what was done","links?":"where to read"}');
    expect(rows).toEqual([
      { name: "summary", purpose: "what was done", optional: false },
      { name: "links", purpose: "where to read", optional: true },
    ]);
    expect(handoffJson(rows)).toBe('{"summary":"what was done","links?":"where to read"}');
  });

  it("holds nothing for what is not an object of strings, and leaves a line with no name out", () => {
    expect(handoffRows("")).toEqual([]);
    expect(handoffRows("not json")).toEqual([]);
    expect(handoffRows("[1]")).toEqual([]);
    expect(handoffRows('{"a":3}')).toEqual([]);
    expect(handoffMap([{ name: "  ", purpose: "x", optional: false }])).toEqual({});
    expect(handoffJson([])).toBe("{}");
  });
});

function Harness({ start }: { start: string }) {
  const [json, setJson] = useState(start);
  return (
    <I18nProvider locales={APP_LOCALES}>
      <HandoffBuilder value={json} onChange={setJson} />
      <pre data-testid="json">{json}</pre>
    </I18nProvider>
  );
}

describe("HandoffBuilder", () => {
  it("adds a line that stays while it has no name, then writes the object the daemon reads", () => {
    render(<Harness start="{}" />);
    fireEvent.click(screen.getByTestId("agent-handoff-add"));
    expect(screen.getByTestId("agent-handoff-name-0")).toBeTruthy();
    expect(screen.getByTestId("json").textContent).toBe("{}");
    fireEvent.change(screen.getByTestId("agent-handoff-name-0"), { target: { value: "summary" } });
    fireEvent.change(screen.getByTestId("agent-handoff-purpose-0"), { target: { value: "what was done" } });
    expect(screen.getByTestId("json").textContent).toBe('{"summary":"what was done"}');
    fireEvent.click(screen.getByTestId("agent-handoff-optional-0").querySelector("input") as HTMLInputElement);
    expect(screen.getByTestId("json").textContent).toBe('{"summary?":"what was done"}');
  });

  it("starts from the row's fields and removes one", () => {
    render(<Harness start='{"a":"first","b?":"second"}' />);
    expect((screen.getByTestId("agent-handoff-name-1") as HTMLInputElement).value).toBe("b");
    fireEvent.click(screen.getByTestId("agent-handoff-remove-0"));
    expect(screen.getByTestId("json").textContent).toBe('{"b?":"second"}');
    fireEvent.click(screen.getByTestId("agent-handoff-remove-0"));
    expect(screen.getByTestId("json").textContent).toBe("{}");
    expect(screen.getByTestId("agent-handoff-empty")).toBeTruthy();
  });

  it("offers the fields shipped agents report, one click each, and stops offering one that is taken", () => {
    render(<Harness start="{}" />);
    fireEvent.click(screen.getByTestId("agent-handoff-suggest-tests_added"));
    expect(JSON.parse(screen.getByTestId("json").textContent ?? "")).toEqual({ "tests_added?": expect.stringContaining("tests") });
    expect(screen.queryByTestId("agent-handoff-suggest-tests_added")).toBeNull();
    expect(screen.getByTestId("agent-handoff-suggest-summary")).toBeTruthy();
  });
});
