import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { I18nProvider } from "@cjlapao/ui-kit";
import { SkillsSection } from "../src/features/settings/skills/SkillsSection";
import { APP_LOCALES } from "../src/lib/i18n";
import { LiveController } from "../src/lib/live";
import type { LiveState } from "../src/lib/live";
import { installFakeDaemon, resetFakeDaemon, type FakeDaemon } from "./helpers";

const live = { state: "current", endpoint: "/run/user/1000/aeon/rpc.sock", daemonVersion: "9.9.9", protocolVersion: 7, generation: 1 } as const satisfies LiveState;

const files = { scripts: 1, references: 2, assets: 0, other: 0 };
const base = { root: "/r", files, bodyBytes: 10, bodyLines: 3, totalBytes: 99, scannedAt: "2026-11-12T00:00:00Z" };
const LIST = {
  skills: [
    { ...base, name: "pdf", description: "Work with PDFs", status: "valid", source: "managed", path: "/m/pdf" },
    { ...base, name: "notes", description: "Take notes", status: "warn", source: "user", path: "/u/notes", issues: [{ field: "license", detail: "no license given", severity: "warning" }] },
    { ...base, name: "broken", status: "invalid", source: "user", path: "/u/broken", issues: [{ field: "name", detail: "does not match the folder", severity: "error" }] },
  ],
  userRoot: "/u",
  managedRoot: "/m",
  scannedAt: "2026-11-12T00:00:00Z",
  summary: { valid: 1, warn: 1, invalid: 1, shadowed: 0 },
  watching: true,
};

beforeEach(() => {
  window.matchMedia = vi.fn(() => ({ matches: false, media: "", onchange: null, addEventListener: () => {}, removeEventListener: () => {}, addListener: () => {}, removeListener: () => {}, dispatchEvent: () => false })) as unknown as typeof window.matchMedia;
});
afterEach(() => { cleanup(); resetFakeDaemon(); });

function renderSection(extra: Record<string, unknown> = {}): FakeDaemon {
  const daemon = installFakeDaemon({
    responses: {
      "skills.list": LIST,
      "skills.get": { skill: LIST.skills[0], body: "# PDF\nUse it for PDFs." },
      "skills.validate": { skills: LIST.skills, summary: LIST.summary, scannedAt: "2026-11-12T00:00:01Z" },
      "skills.install": { skill: { ...LIST.skills[0], name: "xlsx" } },
      "skills.remove": { name: "pdf", path: "/m/pdf" },
      ...extra,
    },
  });
  render(<I18nProvider locales={APP_LOCALES}><SkillsSection controller={new LiveController()} live={live} canWrite /></I18nProvider>);
  return daemon;
}

describe("skills", () => {
  it("lists every skill with the daemon's verdict, broken ones included", async () => {
    renderSection();
    await screen.findByTestId("skill-pdf");
    expect(screen.getByTestId("status-pdf").textContent).toBe("valid");
    expect(screen.getByTestId("status-notes").textContent).toBe("valid, with notes");
    expect(screen.getByTestId("status-broken").textContent).toBe("refused");
    expect(screen.getByTestId("source-pdf").textContent).toBe("managed");
    expect(screen.getByTestId("source-notes").textContent).toBe("yours");
    expect(screen.getByTestId("skills-summary").textContent).toContain("Watching both folders");
    expect(screen.getByTestId("skills-count").textContent).toContain("Showing 3 of 3");
  });

  it("opens a skill: issues, body as plain text, and remove only for the managed folder", async () => {
    renderSection({ "skills.get": { skill: LIST.skills[1], body: "<img src=x onerror=alert(1)>" } });
    fireEvent.click(await screen.findByTestId("skill-notes"));
    const panel = await screen.findByTestId("skill-panel");
    expect(within(panel).getByTestId("skill-issues").textContent).toContain("[warning] license: no license given");
    expect((await within(panel).findByTestId("skill-body")).textContent).toBe("<img src=x onerror=alert(1)>");
    expect(panel.querySelector("img")).toBeNull();
    expect(within(panel).queryByTestId("skill-remove")).toBeNull();
    expect(within(panel).getByTestId("skill-user-note")).toBeTruthy();
  });

  it("removing a managed skill asks first, then calls skills.remove", async () => {
    const daemon = renderSection();
    fireEvent.click(await screen.findByTestId("skill-pdf"));
    fireEvent.click(await screen.findByTestId("skill-remove"));
    expect(daemon.paramsOf("skills.remove")).toBeUndefined();
    fireEvent.click(within(await screen.findByTestId("skill-remove-ask")).getByRole("button", { name: "Remove" }));
    await waitFor(() => expect(daemon.paramsOf("skills.remove")).toEqual({ name: "pdf" }));
    await waitFor(() => expect(screen.queryByTestId("skill-panel")).toBeNull());
    expect(screen.getByTestId("skills-notice").textContent).toContain("Removed pdf");
  });

  it("installs from a source and shows the daemon's answer", async () => {
    const daemon = renderSection();
    fireEvent.click(await screen.findByTestId("skill-install"));
    const form = await screen.findByTestId("skill-install-form");
    expect((screen.getByTestId("skill-install-go") as HTMLButtonElement).disabled).toBe(true);
    fireEvent.change(within(form).getByLabelText("skill source"), { target: { value: "acme/skills" } });
    fireEvent.change(within(form).getByLabelText("skill subpath"), { target: { value: "skills/xlsx" } });
    fireEvent.click(screen.getByTestId("skill-install-go"));
    await waitFor(() => expect(daemon.paramsOf("skills.install")).toEqual({ source: "acme/skills", subpath: "skills/xlsx" }));
    await waitFor(() => expect(screen.getByTestId("skills-notice").textContent).toContain("Installed xlsx"));
  });

  it("narrows by verdict through the daemon's own filter", async () => {
    const daemon = renderSection();
    await screen.findByTestId("skill-pdf");
    fireEvent.click(screen.getByRole("radio", { name: "Refused" }));
    await waitFor(() => expect(daemon.paramsOf("skills.list")).toEqual({ status: "invalid" }));
  });
});
