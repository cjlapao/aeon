import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { I18nProvider } from "@cjlapao/ui-kit";
import { MemoryRouter } from "react-router-dom";
import { notifications } from "../../../lib/notifications";
import { MergeRuleEditor } from "./MergeRuleEditor";
import { APP_LOCALES } from "../../../lib/i18n";
import type { LiveState } from "../../../lib/live";
import { LiveController } from "../../../lib/live";
import { installFakeDaemon, resetFakeDaemon, type FakeDaemon } from "../../../../test/helpers";
import { gatesBaseResponses, fixtureResult, refusedError } from "../../../../test/gate-fixtures";

const get = fixtureResult("mergerule", "get.json") as {
  rule: { revision: number; requiredChecks: string[]; method: string };
  lastChange: { summary: string };
  floor: { ok: boolean; missing: Array<{ code: string; sentence: string }> };
};

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


function renderEditor(responses: Record<string, unknown> = {}): FakeDaemon {
  const daemon = installFakeDaemon({ responses: { ...gatesBaseResponses, ...responses } });
  render(
    <I18nProvider locales={APP_LOCALES}>
      <MemoryRouter>
        <MergeRuleEditor componentId="c-child" controller={new LiveController()} live={currentLive} canWrite />
      </MemoryRouter>
    </I18nProvider>,
  );
  return daemon;
}

async function openForm(): Promise<void> {
  await screen.findByTestId("gates-merge-form");
}

describe("MergeRuleEditor (S57 T11)", () => {
  it("the header is the daemon answer: revision, the last change, the unmet floor", async () => {
    renderEditor();
    await openForm();
    expect(screen.getByTestId("gates-merge-revision").textContent).toBe(`revision ${get.rule.revision}`);
    expect(screen.getByTestId("gates-merge-lastchange").textContent).toBe(`last change: ${get.lastChange.summary}`);
    expect(screen.getByTestId(`gates-merge-floor-${get.floor.missing[0]?.code}`).textContent).toContain(
      get.floor.missing[0]?.sentence,
    );
  });

  it("a write goes through a diff of every changed field, shown BEFORE anything is sent", async () => {
    const daemon = renderEditor();
    await openForm();
    fireEvent.change(screen.getByTestId("merge-requiredChecks"), {
      target: { value: `${get.rule.requiredChecks.join(", ")}, ci/vet` },
    });
    fireEvent.click(screen.getByTestId("gates-merge-save"));
    const dialog = await screen.findByRole("dialog");
    const diff = within(dialog).getByTestId("gates-merge-confirm-diff").textContent ?? "";
    expect(diff).toContain("ci/vet");
    expect(diff).toContain(get.rule.requiredChecks[0] ?? "");
    expect(daemon.methods()).not.toContain("mergerule.set"); // not yet — the diff is the confirmation
    fireEvent.click(within(dialog).getByRole("button", { name: "Save it" }));
    await waitFor(() => expect(daemon.methods()).toContain("mergerule.set"));
    const params = daemon.paramsOf("mergerule.set") as { componentId: string; rule: { requiredChecks: string[] } };
    expect(params.componentId).toBe("c-child");
    expect(params.rule.requiredChecks).toEqual(["ci/build", "ci/lint", "ci/vet"]);
    // The revision shown afterwards is the re-read's, never a local guess.
    await waitFor(() =>
      expect(daemon.methods().filter((m) => m === "mergerule.get").length).toBeGreaterThanOrEqual(2),
    );
  });

  it("a refusal lands its parts verbatim and keeps the draft exactly as typed", async () => {
    const push = vi.spyOn(notifications, "push");
    renderEditor({
      "mergerule.set": () => {
        throw refusedError("mergerule", "set-refused.json");
      },
    });
    await openForm();
    fireEvent.change(screen.getByTestId("merge-requiredChecks"), { target: { value: "sometimes" } });
    fireEvent.click(screen.getByTestId("gates-merge-save"));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Save it" }));
    // The refusal is a toast and nothing else: no error panel stays on the page.
    await waitFor(() => expect(push).toHaveBeenCalled());
    const body = String(push.mock.calls[0]?.[0]?.body ?? "");
    expect(body).toContain('require_dod is "sometimes"');
    expect(body).toContain("the closed set is none|all_met");
    expect(screen.queryByTestId("gates-merge-error")).toBeNull();
    expect((screen.getByTestId("merge-requiredChecks") as HTMLInputElement).value).toBe("sometimes");
  });

  it("clearing asks with the honest sentence — nothing here predicts what applies next", async () => {
    const daemon = renderEditor();
    await openForm();
    fireEvent.click(screen.getByTestId("gates-merge-clear"));
    const dialog = await screen.findByRole("dialog");
    expect(dialog.textContent).toContain("whatever the daemon");
    expect(dialog.textContent).toContain("nothing here predicts it");
    fireEvent.click(within(dialog).getByRole("button", { name: "Remove this component's rule" }));
    await waitFor(() => expect(daemon.methods()).toContain("mergerule.clear"));
    expect(daemon.paramsOf("mergerule.clear")).toEqual({ componentId: "c-child" });
  });
});
