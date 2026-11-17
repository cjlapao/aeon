import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { I18nProvider } from "@cjlapao/ui-kit";
import { TemplatePicker } from "./TemplatePicker";
import { APP_LOCALES } from "../../../lib/i18n";
import type { LiveState } from "../../../lib/live";
import { LiveController } from "../../../lib/live";
import { installFakeDaemon, resetFakeDaemon, type FakeDaemon } from "../../../../test/helpers";
import { verifyTemplates } from "../../../../test/gate-fixtures";

const currentLive = {
  state: "current",
  endpoint: "/run/user/1000/aeon/rpc.sock",
  daemonVersion: "9.9.9",
  protocolVersion: 7,
  generation: 1,
} as const satisfies LiveState;

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


function renderPicker(responses: Record<string, unknown> = { "verify.templates": verifyTemplates }): FakeDaemon {
  const daemon = installFakeDaemon({ responses });
  render(
    <I18nProvider locales={APP_LOCALES}>
      <TemplatePicker
        controller={new LiveController()}
        live={currentLive}
        busy={false}
        canWrite
        onApply={vi.fn()}
        onClose={vi.fn()}
      />
    </I18nProvider>,
  );
  return daemon;
}

describe("TemplatePicker (S57 T11)", () => {
  it("lists the daemon\'s templates under their stacks, commands and all", async () => {
    renderPicker();
    await screen.findByTestId("gates-template-vt-go-test");
    expect(screen.getByTestId("gates-template-vt-go-test").textContent).toContain("go test ./...");
    expect(screen.getByTestId("gates-template-vt-go-vet").textContent).toContain("go vet ./...");
  });

  it("apply is dead until something is picked, and carries the count once picked", async () => {
    renderPicker();
    await screen.findByTestId("gates-template-vt-go-vet");
    expect((screen.getByTestId("gates-templates-apply") as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(screen.getByLabelText("go vet"));
    const apply = screen.getByTestId("gates-templates-apply") as HTMLButtonElement;
    expect(apply.disabled).toBe(false);
    expect(apply.textContent).toBe("Apply 1");
  });

  it("hands the chosen ids up — the parent owns the door", async () => {
    const onApply = vi.fn();
    installFakeDaemon({ responses: { "verify.templates": verifyTemplates } });
    render(
      <I18nProvider locales={APP_LOCALES}>
        <TemplatePicker
          controller={new LiveController()}
          live={currentLive}
          busy={false}
          canWrite
          onApply={onApply}
          onClose={vi.fn()}
        />
      </I18nProvider>,
    );
    await screen.findByTestId("gates-template-vt-go-test");
    fireEvent.click(screen.getByLabelText("go test (package)"));
    fireEvent.click(screen.getByTestId("gates-templates-apply"));
    expect(onApply).toHaveBeenCalledWith(["vt-go-test"]);
  });

  it("an empty template answer says the daemon offered none, without inventing one", async () => {
    renderPicker({ "verify.templates": { templates: [] } });
    expect(await screen.findByTestId("gates-templates-empty")).toBeTruthy();
  });
});
