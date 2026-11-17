import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { I18nProvider } from "@cjlapao/ui-kit";
import { DodCard } from "./DodCard";
import { APP_LOCALES } from "../../../lib/i18n";
import type { LiveState } from "../../../lib/live";
import { LiveController } from "../../../lib/live";
import { installFakeDaemon, resetFakeDaemon, type FakeDaemon } from "../../../../test/helpers";
import { dodList, dodMark, refusedError } from "../../../../test/gate-fixtures";

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


function renderCard(responses: Record<string, unknown> = {}, canWrite = true): FakeDaemon {
  const daemon = installFakeDaemon({ responses: { "dod.list": dodList, "dod.mark": dodMark, ...responses } });
  render(
    <I18nProvider locales={APP_LOCALES}>
      <DodCard slug="fix-the-login" controller={new LiveController()} live={currentLive} canWrite={canWrite} />
    </I18nProvider>,
  );
  return daemon;
}

describe("DodCard (S57 T11)", () => {
  it("renders the ledger with state captions and the evidence a met item names", async () => {
    renderCard();
    await screen.findByTestId("dod-item-dod-1");
    expect(screen.getByTestId("dod-state-dod-1").textContent).toBe("met");
    expect(screen.getByTestId("dod-state-dod-2").textContent).toBe("unchecked");
    expect(screen.getByTestId("dod-evidence-dod-1").textContent).toContain("vr-101");
  });

  it("no write controls at all when the person cannot write", async () => {
    renderCard({}, false);
    await screen.findByTestId("dod-item-dod-1");
    expect(screen.queryByTestId("dod-actions-dod-2")).toBeNull();
  });

  it("a met mark WITHOUT a note is refused locally — nothing crosses the wire", async () => {
    const daemon = renderCard();
    await screen.findByTestId("dod-item-dod-2");
    fireEvent.click(screen.getByTestId("dod-mark-met-dod-2"));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByTestId("dod-confirm-note-required").textContent).toContain("needs the note");
    const confirm = within(dialog).getByRole("button", { name: "Mark it" }) as HTMLButtonElement;
    expect(confirm.disabled).toBe(true);
    fireEvent.click(confirm); // the click a habit could make
    expect(daemon.methods()).not.toContain("dod.mark");
  });

  it("a met mark WITH a note sends the daemon its own evidence pair, capitalised as Go marshals it", async () => {
    const daemon = renderCard();
    await screen.findByTestId("dod-item-dod-2");
    fireEvent.click(screen.getByTestId("dod-mark-met-dod-2"));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByTestId("dod-confirm-note"), { target: { value: "checked by hand" } });
    const confirm = within(dialog).getByRole("button", { name: "Mark it" }) as HTMLButtonElement;
    expect(confirm.disabled).toBe(false);
    fireEvent.click(confirm);
    await new Promise((r) => setTimeout(r, 20));
    expect(daemon.paramsOf("dod.mark")).toEqual({
      id: "dod-2",
      state: "met",
      evidence: { Kind: "manual", Ref: "checked by hand" },
    });
  });

  it("unmet needs no note — the params carry no evidence at all", async () => {
    const daemon = renderCard();
    await screen.findByTestId("dod-item-dod-2");
    fireEvent.click(screen.getByTestId("dod-mark-unmet-dod-2"));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Mark it" }));
    await new Promise((r) => setTimeout(r, 20));
    expect(daemon.paramsOf("dod.mark")).toEqual({ id: "dod-2", state: "unmet" });
  });

  it("the row updates from the ANSWER: the daemon\'s item replaces the daemon\'s row", async () => {
    renderCard();
    await screen.findByTestId("dod-item-dod-3");
    expect(screen.getByTestId("dod-state-dod-3").textContent).toBe("unmet");
    fireEvent.click(screen.getByTestId("dod-mark-met-dod-3"));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByTestId("dod-confirm-note"), { target: { value: "e2e green" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Mark it" }));
    await screen.findByTestId("dod-item-dod-3");
    // mark.json answers with dod-3 now MET with the fixture's own evidence.
    await waitForText("dod-state-dod-3", "met");
  });

  it("a refused mark renders the daemon\'s parts — the what sentence is the repair", async () => {
    renderCard({ "dod.mark": () => { throw refusedError("dod", "mark-met-without-evidence.json"); } });
    await screen.findByTestId("dod-item-dod-2");
    fireEvent.click(screen.getByTestId("dod-mark-met-dod-2"));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByTestId("dod-confirm-note"), { target: { value: "a note" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Mark it" }));
    const error = await screen.findByTestId("dod-error");
    expect(error.textContent).toContain("a met item names its evidence");
  });
});

async function waitForText(testid: string, text: string): Promise<void> {
  for (let i = 0; i < 50; i += 1) {
    const el = document.querySelector(`[data-testid="${testid}"]`);
    if (el?.textContent === text) return;
    await new Promise((r) => setTimeout(r, 20));
  }
  throw new Error(`${testid} never became ${text}`);
}
