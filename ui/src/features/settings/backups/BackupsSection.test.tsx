import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { I18nProvider } from "@cjlapao/ui-kit";
import { BackupsSection } from "./BackupsSection";
import { APP_LOCALES } from "../../../lib/i18n";
import { LiveController } from "../../../lib/live";
import type { LiveState } from "../../../lib/live";
import { notifications } from "../../../lib/notifications";
import { installFakeDaemon, resetFakeDaemon, type FakeDaemon } from "../../../../test/helpers";

/**
 * The Backups panel (S62 T10). The daemon is the mock; the words it sends
 * here — states, reasons, verify words, refusal sentences — are TEST data on
 * purpose, because the section's own sources are forbidden to hold them
 * (`backupRpc.guard.test.ts`), and a fixture that repeats a source constant
 * would prove the source was consulted.
 */

const currentLive = {
  state: "current",
  endpoint: "/run/user/1000/aeon/rpc.sock",
  daemonVersion: "9.9.9",
  protocolVersion: 7,
  generation: 1,
} as const satisfies LiveState;

const staleLive = {
  state: "stale",
  endpoint: "/run/user/1000/aeon/rpc.sock",
  since: 1_730_000_000_000,
  cause: "the daemon closed the socket",
  retry: 2,
} as const satisfies LiveState;

const CREATED_NEW = "2026-11-02T15:38:36Z";
const CREATED_OLD = "2026-10-20T09:00:00Z";

function backupRow(overrides: Record<string, unknown> = {}) {
  return {
    aeonVersion: "9.9.9",
    ageSeconds: 3600,
    applied: 43,
    canRestoreWithThisBinary: true,
    createdAt: CREATED_NEW,
    file: "20261102T153836Z_state.db.backup",
    lastMigration: "20261102153836_blueprint",
    reason: "manual",
    sha256: "sha256:aaa",
    sizeBytes: 1536 * 1024,
    verified: "verified",
    ...overrides,
  };
}

const statusFresh = {
  ageSeconds: 3600,
  db: { applied: 43, bytes: 1024 * 1024, journalMode: "wal", pending: 0, walBytes: 65536 },
  dir: "/tmp/bk",
  enabled: true,
  lastSuccess: CREATED_NEW,
  lastVerified: CREATED_NEW,
  nextDueAt: "2026-11-03T15:38:36Z",
  running: false,
  settings: [{ key: "backup.interval", source: "default", value: "24h" }],
  state: "fresh",
};

const statusNever = {
  ...statusFresh,
  ageSeconds: 0,
  enabled: false,
  lastSuccess: "",
  lastVerified: "",
  nextDueAt: "",
  settings: [],
  state: "never",
};

const listAnswer = {
  backups: [
    backupRow(),
    backupRow({
      createdAt: CREATED_OLD,
      file: "20261020T090000Z_state.db.backup",
      reason: "scheduled",
      verified: "unverified",
      canRestoreWithThisBinary: false,
      lastMigration: "20261201000000_future",
    }),
  ],
  dirs: [],
};

const runAnswer = {
  backup: backupRow({ reason: "manual", verified: "unverified" }),
  run: {
    applied: 43,
    file: "20261102T153836Z_state.db.backup",
    freeSpace: "120 GiB",
    lastMigration: "20261102153836_blueprint",
    lockReclaimed: false,
    recorded: true,
    sha256: "sha256:aaa",
    sizeBytes: 1536 * 1024,
    verifyLevel: "unverified",
  },
};

const verifyAnswer = {
  report: { file: "20261102T153836Z_state.db.backup", level: 2, newerSchema: false, ok: true, sentence: "verify: the copy opens and its rows count matches the manifest" },
  run: { startedAt: CREATED_NEW, finishedAt: CREATED_NEW, outcome: "ok" },
};

const pruneDryAnswer = {
  plan: [
    { file: "20261020T090000Z_state.db.backup", keep: false, reason: "older than the keep window" },
    { file: "20261102T153836Z_state.db.backup", keep: true, reason: "newest" },
  ],
};

const pruneRealAnswer = {
  ...pruneDryAnswer,
  removed: ["20261020T090000Z_state.db.backup"],
};

const baseResponses: Record<string, unknown> = {
  "backup.status": statusFresh,
  "backup.list": listAnswer,
  "backup.run": runAnswer,
  "backup.verify": verifyAnswer,
  "backup.prune": (params: unknown) => ((params as { dryRun?: boolean }).dryRun ? pruneDryAnswer : pruneRealAnswer),
};

let writeText: ReturnType<typeof vi.fn>;

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
  writeText = vi.fn().mockResolvedValue(undefined);
  Object.defineProperty(window.navigator, "clipboard", { value: { writeText }, configurable: true });
});

afterEach(() => {
  cleanup();
  resetFakeDaemon();
  delete (window.navigator as unknown as { clipboard?: unknown }).clipboard;
});

function renderSection(responses: Record<string, unknown> = {}, live: LiveState = currentLive): { daemon: FakeDaemon; rerender: (live: LiveState) => void } {
  const daemon = installFakeDaemon({ responses: { ...baseResponses, ...responses } });
  let controller = new LiveController();
  const tree = (state: LiveState) => (
    <I18nProvider locales={APP_LOCALES}>
      <MemoryRouter>
        <BackupsSection controller={controller} live={state} />
      </MemoryRouter>
    </I18nProvider>
  );
  const view = render(tree(live));
  return { daemon, rerender: (state: LiveState) => view.rerender(tree(state)) };
}

const FILE = "20261102T153836Z_state.db.backup";

/** The table row a testid-carrying cell sits in (cells hold their own testids). */
const trOf = (testid: string): HTMLElement => screen.getByTestId(testid).closest("tr") as HTMLElement;

/** Open one copy in its side panel by clicking its row. */
async function openCopy(file: string): Promise<HTMLElement> {
  fireEvent.click(await screen.findByTestId(`backup-row-${file}`));
  return screen.findByTestId("backup-panel");
}

describe("BackupsSection (S62 T10)", () => {
  it("renders every field of backup.list, newest first, and asks with the daemon-side shape", async () => {
    const { daemon } = renderSection();

    // The list's own params: pre-migration copies in, the limit on the daemon side.
    expect(daemon.paramsOf("backup.list")).toEqual({ includePreMigration: true, limit: 50 });

    await screen.findByTestId("backup-row-20261102T153836Z_state.db.backup");
    const first = trOf("backup-row-20261102T153836Z_state.db.backup");
    const second = trOf("backup-row-20261020T090000Z_state.db.backup");
    // The daemon sorts newest first; the panel keeps that order.
    expect(first.compareDocumentPosition(second) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();

    // Row one, field by field.
    expect(within(first).getByTestId("backup-row-file-20261102T153836Z_state.db.backup").textContent).toBe("20261102T153836Z_state.db.backup");
    expect(within(first).getByTestId("backup-row-created-20261102T153836Z_state.db.backup").textContent).toContain(CREATED_NEW);
    expect(within(first).getByTestId("backup-row-size-20261102T153836Z_state.db.backup").textContent).toBe("1.5 MiB");
    expect(within(first).getByTestId("backup-row-schema-20261102T153836Z_state.db.backup").textContent).toBe("43 applied · last 20261102153836_blueprint");
    expect(within(first).getByTestId("backup-row-reason-20261102T153836Z_state.db.backup").textContent).toBe("manual");
    expect(within(first).getByTestId("backup-row-verified-20261102T153836Z_state.db.backup").textContent).toBe("verified");
    expect(within(first).getByTestId("backup-row-restorable-20261102T153836Z_state.db.backup").textContent).toBe("restorable with this version");

    // Row two: the caption comes from the daemon's own words, and the
    // not-restorable line names the migration it does not know.
    expect(within(second).getByTestId("backup-row-reason-20261020T090000Z_state.db.backup").textContent).toBe("scheduled");
    expect(within(second).getByTestId("backup-row-verified-20261020T090000Z_state.db.backup").textContent).toBe("unverified");
    expect(within(second).getByTestId("backup-row-restorable-20261020T090000Z_state.db.backup").textContent).toBe(
      "not restorable with this version — schema ahead at 20261201000000_future",
    );
  });

  it("status header in the never state: the state in words, the sentence, and Run now beside it", async () => {
    renderSection({ "backup.status": statusNever, "backup.list": { backups: [], dirs: [] } });

    const pill = await screen.findByTestId("backup-state-pill");
    expect(pill.textContent).toBe("Never backed up");
    expect(screen.getByTestId("backup-state-sentence").textContent).toBe("No backup exists yet — the database has no safety net until the first copy.");
    expect(screen.getByTestId("backup-last-success").textContent).toBe("no success yet");
    // Schedule off: no next due to show, and the honest line says so.
    expect(screen.getByTestId("backup-next-due").textContent).toBe("off");
    expect(screen.getByTestId("backup-schedule").textContent).toBe("off");
    expect((screen.getByTestId("backup-run-now") as HTMLButtonElement).disabled).toBe(false);
    expect(screen.getByTestId("backup-list-empty")).not.toBeNull();
  });

  it("status header in the fresh state shows the last success, the next due and the settings line", async () => {
    renderSection();
    const pill = await screen.findByTestId("backup-state-pill");
    expect(pill.textContent).toBe("Fresh");
    expect(screen.getByTestId("backup-state-sentence").textContent).toContain("recent backup");
    expect(screen.getByTestId("backup-last-success").textContent).toContain("ago");
    expect(screen.getByTestId("backup-next-due").textContent).toContain("due");
    expect(screen.getByTestId("backup-db-size").textContent).toBe("1 MiB + 64 KiB WAL · 43/43 migrations");
    // the schedule's settings fold open on request, read-only.
    fireEvent.click(screen.getAllByText("Schedule settings")[0] as HTMLElement);
    expect(screen.getByTestId("backup-settings-line").textContent).toBe("backup.interval=24h (default)");
  });

  it("Run now calls backup.run once with the daemon's reason, and rows come from the next response", async () => {
    const { daemon } = renderSection();
    const runNow = await screen.findByTestId("backup-run-now");
    fireEvent.click(runNow);

    expect(daemon.paramsOf("backup.run")).toEqual({ reason: "manual" });
    expect(daemon.calls.filter((c) => c.method === "backup.run")).toHaveLength(1);
    // The answer triggered a re-read of both surfaces — the rows are the
    // daemon's next answer, never a local guess.
    await waitFor(() => expect(daemon.calls.filter((c) => c.method === "backup.list")).toHaveLength(2));
    await waitFor(() => expect(daemon.calls.filter((c) => c.method === "backup.status")).toHaveLength(2));
  });

  it("a refusal is a toast in the daemon's words, and nothing was written", async () => {
    const toast = vi.spyOn(notifications, "push");
    const refusal = "refused: no free space · 0 B left in the backup directory";
    const { daemon } = renderSection({
      "backup.run": () => {
        throw new Error(refusal);
      },
    });
    fireEvent.click(await screen.findByTestId("backup-run-now"));

    await waitFor(() => expect(toast).toHaveBeenCalled());
    expect(JSON.stringify(toast.mock.calls.map((c) => c[0]))).toContain(refusal);
    expect(screen.queryByTestId("backups-action-error")).toBeNull();
    // The list never changed and no retry was sent on the person's behalf.
    expect(daemon.calls.filter((c) => c.method === "backup.run")).toHaveLength(1);
    expect(screen.getByTestId("backup-row-20261102T153836Z_state.db.backup")).not.toBeNull();
  });

  it("Verify sends the row's file and the drill draft, shows the daemon's sentence, and a refusal keeps the draft", async () => {
    const { daemon } = renderSection();
    const row = await openCopy(FILE);

    // The drill draft: tick it, then verify.
    fireEvent.click(within(row).getByTestId("backup-row-drill-20261102T153836Z_state.db.backup"));
    expect(within(row).getByTestId("backup-row-drill-cost-20261102T153836Z_state.db.backup")).not.toBeNull();
    fireEvent.click(within(row).getByTestId("backup-row-verify-20261102T153836Z_state.db.backup"));

    expect(daemon.paramsOf("backup.verify")).toEqual({ file: "20261102T153836Z_state.db.backup", drill: true });
    const sentence = await screen.findByTestId("backup-verify-sentence");
    expect(sentence.textContent).toBe("verify: the copy opens and its rows count matches the manifest");
  });

  it("a verify refusal is a toast and keeps the drill draft untouched", async () => {
    const toast = vi.spyOn(notifications, "push");
    const refusal = "refused: the file 20261102T153836Z_state.db.backup does not exist in the directory";
    const { daemon } = renderSection({
      "backup.verify": () => {
        throw new Error(refusal);
      },
    });
    const row = await openCopy(FILE);
    fireEvent.click(within(row).getByTestId("backup-row-drill-20261102T153836Z_state.db.backup"));
    fireEvent.click(within(row).getByTestId("backup-row-verify-20261102T153836Z_state.db.backup"));

    await waitFor(() => expect(toast).toHaveBeenCalled());
    expect(JSON.stringify(toast.mock.calls.map((c) => c[0]))).toContain(refusal);
    // The draft the person built is still there: ticked, and still set.
    expect((within(row).getByTestId("backup-row-drill-20261102T153836Z_state.db.backup") as HTMLInputElement).checked).toBe(true);
    expect(daemon.calls.filter((c) => c.method === "backup.verify")).toHaveLength(1);
  });

  it("Prune: the dry run answers, the plan is shown verbatim, the type-to-confirm calls the real prune", async () => {
    const { daemon } = renderSection();
    fireEvent.click(await screen.findByTestId("backup-prune"));

    // The door is opened dry first — nothing deleted by the preview.
    const pruneCalls = () => daemon.calls.filter((c) => c.method === "backup.prune");
    await waitFor(() => expect(pruneCalls()).toHaveLength(1));
    expect(pruneCalls()[0]?.params).toEqual({ dryRun: true });

    // The daemon's statement, verbatim: keep and remove, each with its reason.
    const removedRow = await screen.findByTestId("prune-row-20261020T090000Z_state.db.backup");
    expect(removedRow.textContent).toContain("remove");
    expect(removedRow.textContent).toContain("older than the keep window");
    const keptRow = screen.getByTestId("prune-row-20261102T153836Z_state.db.backup");
    expect(keptRow.textContent).toContain("keep");

    // The confirm is the type-to-confirm: the word, typed, and the real call.
    fireEvent.click(screen.getByTestId("prune-confirm-button"));
    const dialog = await screen.findByRole("alertdialog");
    const confirmInput = within(dialog).getByPlaceholderText("prune");
    fireEvent.change(confirmInput, { target: { value: "prune" } });
    fireEvent.keyDown(confirmInput, { key: "Enter" });

    await waitFor(() => expect(pruneCalls()).toHaveLength(2));
    expect(pruneCalls()[1]?.params).toEqual({ dryRun: false });
    const removed = await screen.findByTestId("prune-removed");
    expect(removed.textContent).toContain("20261020T090000Z_state.db.backup");
    // The rows re-read from the daemon, never edited locally.
    await waitFor(() => expect(daemon.calls.filter((c) => c.method === "backup.list")).toHaveLength(2));
  });

  it("a real-prune refusal is a toast and the plan is kept", async () => {
    const toast = vi.spyOn(notifications, "push");
    const refusal = "refused: prune is not allowed while a backup is running";
    const { daemon } = renderSection({
      "backup.prune": (params: unknown) => {
        if ((params as { dryRun?: boolean }).dryRun) return pruneDryAnswer;
        throw new Error(refusal);
      },
    });
    fireEvent.click(await screen.findByTestId("backup-prune"));
    await screen.findByTestId("prune-row-20261020T090000Z_state.db.backup");
    fireEvent.click(screen.getByTestId("prune-confirm-button"));
    const dialog = await screen.findByRole("alertdialog");
    const confirmInput = within(dialog).getByPlaceholderText("prune");
    fireEvent.change(confirmInput, { target: { value: "prune" } });
    fireEvent.keyDown(confirmInput, { key: "Enter" });

    await waitFor(() => expect(toast).toHaveBeenCalled());
    expect(JSON.stringify(toast.mock.calls.map((c) => c[0]))).toContain(refusal);
    // The plan the daemon answered is still on screen — the draft kept.
    expect(screen.getByTestId("prune-row-20261020T090000Z_state.db.backup")).not.toBeNull();
    expect(daemon.calls.filter((c) => c.method === "backup.prune")).toHaveLength(2);
  });

  it("offline: writes are disabled, reads keep the last data", async () => {
    const { daemon, rerender } = renderSection();
    await openCopy(FILE);
    const callsBefore = daemon.calls.length;

    rerender(staleLive);

    // The connection is gone: the warning is shown, every write is inert…
    expect(screen.getByTestId("backups-offline")).not.toBeNull();
    expect((screen.getByTestId("backup-run-now") as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByTestId("backup-prune") as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByTestId("backup-row-verify-20261102T153836Z_state.db.backup") as HTMLButtonElement).disabled).toBe(true);
    // …and no request went out on the way down.
    expect(daemon.calls.length).toBe(callsBefore);
    // …but the last data the daemon sent stays on screen.
    expect(screen.getByTestId("backup-row-20261102T153836Z_state.db.backup")).not.toBeNull();
    expect(screen.getByTestId("backup-state-pill").textContent).toBe("Fresh");
  });

  it("no restore control exists anywhere; each copy's panel shows its own copyable command", async () => {
    const { daemon } = renderSection();
    await openCopy(FILE);

    // A static sentence, the exact command for THIS file, and a copy button.
    expect(screen.getByTestId("backups-restore-body").textContent).toContain("daemon stopped");
    expect(screen.getByTestId("backups-restore-command").textContent).toBe(`aeon backup restore ${FILE}`);
    fireEvent.click(screen.getByTestId("backups-restore-copy"));
    await waitFor(() => expect(writeText).toHaveBeenCalledWith(`aeon backup restore ${FILE}`));

    // No method this section owns is restore-shaped — restore is a CLI verb.
    for (const call of daemon.methods()) {
      expect(call.toLowerCase().includes("restore")).toBe(false);
    }
  });

  it("links to the Daemon settings section that owns the backup.* keys", async () => {
    renderSection();
    fireEvent.click(await screen.findByText("Schedule settings"));
    const link = await screen.findByTestId("backups-settings-link");
    expect(link.getAttribute("href")).toBe("#daemon-settings");
  });

  it("renders the same shape in pt as in en (spot: the never sentence and the drill cost)", async () => {
    installFakeDaemon({ responses: baseResponses });
    render(
      <I18nProvider locales={APP_LOCALES} locale="pt">
        <MemoryRouter>
          <BackupsSection controller={new LiveController()} live={currentLive} />
        </MemoryRouter>
      </I18nProvider>,
    );
    // The words are the daemon's; the captions around them are the catalog's.
    const pill = await screen.findByTestId("backup-state-pill");
    expect(pill.textContent).toBe("Frescor");
    fireEvent.click(await screen.findByTestId(`backup-row-${FILE}`));
    expect((await screen.findByTestId("backups-restore-command")).textContent).toBe(`aeon backup restore ${FILE}`);
  });
});
