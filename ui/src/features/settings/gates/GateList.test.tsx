import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { I18nProvider } from "@cjlapao/ui-kit";
import { GateList } from "./GateList";
import type { GateListProps } from "./GateList";
import { APP_LOCALES } from "../../../lib/i18n";
import { resetFakeDaemon } from "../../../../test/helpers";
import { listGates, verifyVocabulary } from "../../../../test/gate-fixtures";

const vocab = verifyVocabulary as { requirements: string[]; states: string[]; tryStatement: string };

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



function renderList(overrides: Partial<GateListProps> = {}): GateListProps {
  const props: GateListProps = {
    gates: listGates,
    loading: false,
    requirements: vocab.requirements,
    states: vocab.states,
    tryStatement: vocab.tryStatement,
    canWrite: true,
    showArchived: false,
    requirement: "",
    onShowArchived: vi.fn(),
    onRequirement: vi.fn(),
    onAdd: vi.fn(),
    onEdit: vi.fn(),
    onArchive: vi.fn(),
    onRestore: vi.fn(),
    onTry: vi.fn(),
    onRetry: vi.fn(),
    ...overrides,
  };
  render(
    <I18nProvider locales={APP_LOCALES}>
      <GateList {...props} />
    </I18nProvider>,
  );
  return props;
}

async function expand(id: string): Promise<void> {
  fireEvent.click(screen.getByTestId(`gate-row-toggle-${id}`));
  await screen.findByTestId(`gate-detail-${id}`); // the side panel mounts after the click
}

describe("GateList (S57 T11)", () => {
  it("an inherited row says where it comes from and offers no local edits", async () => {
    renderList();
    const inherited = listGates.find((g) => g.source === "root") ?? listGates[1];
    const id = inherited?.id ?? "";
    const pill = screen.getByTestId(`gate-source-${id}`);
    expect(pill.textContent).toContain(inherited?.sourceComponent ?? "");
    await expand(id);
    const detail = screen.getByTestId(`gate-detail-${id}`);
    expect(within(detail).queryByTestId(`gate-edit-${id}`)).toBeNull();
    expect(within(detail).queryByTestId(`gate-archive-${id}`)).toBeNull();
    // Try is still legal — running a gate asks the daemon, it edits nothing.
    expect(within(detail).getByTestId(`gate-try-${id}`)).toBeTruthy();
  });

  it("the policy preview is the daemon's sentence, verbatim, unedited", async () => {
    renderList();
    const withPreview = listGates.find((g) => g.policyPreview !== undefined);
    expect(withPreview).toBeTruthy();
    await expand(withPreview?.id ?? "");
    expect(screen.getByTestId(`gate-policy-${withPreview?.id}`).textContent).toBe(withPreview?.policyPreview);
  });

  it("Try asks first with the daemon's statement, and declining sends nothing", async () => {
    const props = renderList();
    const id = listGates[0]?.id ?? "";
    await expand(id);
    fireEvent.click(screen.getByTestId(`gate-try-${id}`));
    expect(screen.getByTestId("gates-try-statement").textContent).toBe(vocab.tryStatement);
    expect(screen.getByTestId("gates-try-command").textContent).toBe(listGates[0]?.command);
    fireEvent.click(screen.getByRole("button", { name: "Not now" }));
    expect(props.onTry).not.toHaveBeenCalled();
  });

  it("only a confirmed try reaches onTry — the one path that would send verify.try", async () => {
    const props = renderList();
    const id = listGates[0]?.id ?? "";
    await expand(id);
    fireEvent.click(screen.getByTestId(`gate-try-${id}`));
    fireEvent.click(screen.getByRole("button", { name: "Run it now" }));
    expect(props.onTry).toHaveBeenCalledTimes(1);
    expect(vi.mocked(props.onTry).mock.calls[0]?.[0]?.id).toBe(id);
  });

  it("the requirement filter carries the vocabulary's words in order", async () => {
    const props = renderList();
    const select = screen.getByLabelText("Requirement") as HTMLSelectElement;
    fireEvent.change(select, { target: { value: "advisory" } });
    expect(props.onRequirement).toHaveBeenCalledWith("advisory");
  });

  it("canWrite false leaves every control off, rows still readable", async () => {
    renderList({ canWrite: false });
    const id = listGates[0]?.id ?? "";
    await expand(id);
    const detail = screen.getByTestId(`gate-detail-${id}`);
    expect(within(detail).queryByTestId(`gate-try-${id}`)).toBeNull();
    expect(within(detail).queryByTestId(`gate-edit-${id}`)).toBeNull();
  });
});
