import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { I18nProvider } from "@cjlapao/ui-kit";
import { GateForm } from "./GateForm";
import { APP_LOCALES } from "../../../lib/i18n";
import { resetFakeDaemon } from "../../../../test/helpers";
import { verifyVocabulary } from "../../../../test/gate-fixtures";

const words = (verifyVocabulary as { requirements: string[] }).requirements;

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



function renderAdd(onSubmitAdd = vi.fn()) {
  render(
    <I18nProvider locales={APP_LOCALES}>
      <GateForm
        mode="add"
        componentId="c-aeon"
        requirements={words}
        busy={false}
        canWrite
        onSubmitAdd={onSubmitAdd}
        onClose={vi.fn()}
      />
    </I18nProvider>,
  );
  return onSubmitAdd;
}

function fill(name: string, command: string, timeout: string, memory: string): void {
  fireEvent.change(screen.getByTestId("gates-form-name"), { target: { value: name } });
  fireEvent.change(screen.getByTestId("gates-form-command"), { target: { value: command } });
  fireEvent.change(screen.getByTestId("gates-form-timeout"), { target: { value: timeout } });
  if (memory !== "") fireEvent.change(screen.getByTestId("gates-form-memory"), { target: { value: memory } });
}

describe("GateForm (S57 T11)", () => {
  it("sends the typed command verbatim with numbers as numbers", () => {
    const onSubmitAdd = renderAdd();
    fill("api-health", 'curl -s -H "X: Y" https://x', "450", "256");
    fireEvent.click(screen.getByTestId("gates-form-save"));
    expect(onSubmitAdd).toHaveBeenCalledWith({
      componentId: "c-aeon",
      name: "api-health",
      command: 'curl -s -H "X: Y" https://x',
      requirement: words[0],
      timeoutS: 450,
      enabled: true,
      memoryMb: 256,
    });
  });

  it("leaves memoryMb absent when the field is blank — declared, not invented", () => {
    const onSubmitAdd = renderAdd();
    fill("unit", "go test ./...", "600", "");
    fireEvent.click(screen.getByTestId("gates-form-save"));
    const payload = onSubmitAdd.mock.calls[0]?.[0] as Record<string, unknown>;
    expect("memoryMb" in payload).toBe(false);
  });

  it("the requirement choices ARE the vocabulary answer", () => {
    renderAdd();
    const select = screen.getByLabelText("Requirement") as HTMLSelectElement;
    const options = Array.from(select.querySelectorAll("option")).map((o) => o.value);
    expect(options).toEqual(words);
  });

  it("the memory hint says declared-not-enforced, as the wire means it", () => {
    renderAdd();
    fireEvent.change(screen.getByTestId("gates-form-memory"), { target: { value: "128" } });
    expect(screen.getByTestId("gates-form-memory-hint").textContent).toContain("not enforced");
  });

  it("a busy write disables the controls without clearing the draft", () => {
    render(
      <I18nProvider locales={APP_LOCALES}>
        <GateForm
          mode="add"
          componentId="c-aeon"
          requirements={words}
          busy
          canWrite
          onSubmitAdd={vi.fn()}
          onClose={vi.fn()}
        />
      </I18nProvider>,
    );
    fireEvent.change(screen.getByTestId("gates-form-command"), { target: { value: "make vet" } });
    expect((screen.getByTestId("gates-form-save") as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByTestId("gates-form-command") as HTMLInputElement).value).toBe("make vet");
  });
});
