import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { I18nProvider } from "@cjlapao/ui-kit";
import { MergePreview } from "./MergePreview";
import { APP_LOCALES } from "../../../lib/i18n";
import { installFakeDaemon, resetFakeDaemon, type FakeDaemon } from "../../../../test/helpers";
import { mergerulePreview, mergerulePreviewNoPr } from "../../../../test/gate-fixtures";

const preview = mergerulePreview as {
  verdict: { allowed: boolean; refusals: Array<{ code: string; sentence: string }>; evidence: Record<string, string> };
};
const noPr = mergerulePreviewNoPr as { cannotSay?: string };


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


function renderPreview(responses: Record<string, unknown> = { "mergerule.preview": mergerulePreview }): FakeDaemon {
  const daemon = installFakeDaemon({ responses });
  render(
    <I18nProvider locales={APP_LOCALES}>
      <MergePreview componentId="c-child" />
    </I18nProvider>,
  );
  return daemon;
}

async function ask(pr: string): Promise<void> {
  fireEvent.change(screen.getByTestId("gates-preview-pr"), { target: { value: pr } });
  fireEvent.click(screen.getByTestId("gates-preview-ask"));
  await screen.findByTestId("gates-preview-answer");
}

describe("MergePreview (S57 T11)", () => {
  it("asks the daemon with an integer PR and shows the verdict as the answer says it", async () => {
    const daemon = renderPreview();
    await ask("123");
    expect(daemon.paramsOf("mergerule.preview")).toEqual({ componentId: "c-child", pr: 123 });
    expect(screen.getByTestId("gates-preview-verdict").textContent).toBe("refused");
  });

  it("refusals appear in the daemon order, byte for byte", async () => {
    renderPreview();
    await ask("123");
    const list = screen.getByTestId("gates-preview-refusals");
    const lines = within(list).getAllByRole("listitem").map((li) => li.textContent);
    expect(lines).toEqual(preview.verdict.refusals.map((r) => r.sentence));
  });

  it("the evidence grid is the daemon pairs, headed by the SHA it evaluated at", async () => {
    renderPreview();
    await ask("123");
    expect(screen.getByTestId("gates-preview-head").textContent).toContain(
      (preview.verdict.evidence.verify_head_sha ?? "").slice(0, 12),
    );
    const grid = screen.getByTestId("gates-preview-evidence").textContent ?? "";
    expect(grid).toContain("aeon_review");
    expect(grid).toContain(preview.verdict.evidence.aeon_review);
  });

  it("an answer with no verdict carries the cannotSay sentence verbatim", async () => {
    renderPreview({ "mergerule.preview": mergerulePreviewNoPr });
    await ask("123");
    expect(screen.getByTestId("gates-preview-cannotsay").textContent).toBe(noPr.cannotSay);
  });

  it("non-numeric PR text sends nothing — the door opens with a real number only", async () => {
    const daemon = renderPreview();
    fireEvent.change(screen.getByTestId("gates-preview-pr"), { target: { value: "abc" } });
    fireEvent.click(screen.getByTestId("gates-preview-ask"));
    await new Promise((r) => setTimeout(r, 20));
    expect(daemon.methods()).not.toContain("mergerule.preview");
  });

  it("keeps asking after an answer: the verdict shown is the latest one", async () => {
    let n = 0;
    const daemon = renderPreview({
      "mergerule.preview": () => {
        n += 1;
        return n === 1 ? mergerulePreview : mergerulePreviewNoPr;
      },
    });
    await ask("123");
    expect(screen.getByTestId("gates-preview-refusals")).toBeTruthy();
    fireEvent.click(screen.getByTestId("gates-preview-ask"));
    await waitFor(() => expect(screen.queryByTestId("gates-preview-refusals")).toBeNull());
    expect(daemon.methods().filter((m) => m === "mergerule.preview")).toHaveLength(2);
  });
});
