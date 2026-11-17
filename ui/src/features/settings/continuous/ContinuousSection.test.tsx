import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { I18nProvider } from "@cjlapao/ui-kit";
import { ContinuousSection } from "./ContinuousSection";
import { APP_LOCALES } from "../../../lib/i18n";
import { LiveController } from "../../../lib/live";
import { notifications } from "../../../lib/notifications";
import type { LiveState } from "../../../lib/live";
import { installFakeDaemon, resetFakeDaemon, type FakeDaemon } from "../../../../test/helpers";

// S27 T10 — the panel renders what the scheduler ANSWERS and nothing else:
// state words verbatim, decision summaries verbatim, needs-you kinds
// verbatim, S55's raise instruction exactly as the daemon attached it to
// the note field, refusals verbatim with the draft kept, and writes gated
// offline while reads keep their last data. The fake daemon records every
// call, which is how these tests prove decline sends nothing and no door
// carries an actor.

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
  since: Date.now() - 4000,
  cause: "daemon restarted",
  retry: 1,
} as const satisfies LiveState;

const STATEMENT =
  "enabling lets the scheduler start loops unattended in the repositories whose rows it holds, under the permissions those loops carry; it grants nothing beyond these rows, inherits nothing down the tree, and every act is audited to the person who enabled it (S27 CN2)";

const REPO_ALPHA = {
  componentRoot: "/work/alpha",
  enabled: true,
  lastStartAt: "2026-11-05T09:00:00Z",
  minPriority: "medium",
  rootId: "r-alpha",
  state: { kind: "out-of-window", reason: "window-closed", detail: "opens Mon 09:00 America/New_York" },
  template: { blueprint: "dev-loops", tech: "go", with: { brief_path: "docs/brief.md" } },
  window: { days: "Mon-Fri", end: "18:00", start: "09:00", zone: "America/New_York" },
};

const REPO_BETA = {
  componentRoot: "/work/beta",
  enabled: false,
  lastStartAt: "",
  minPriority: "high",
  rootId: "r-beta",
  state: { kind: "off", reason: "repo-off", detail: "" },
  template: { blueprint: "dev-loops", tech: "", with: { brief_path: "docs/b.md" } },
  window: { days: "", end: "", start: "", zone: "" },
};

const DESCRIPTOR = {
  blueprintsFrom: "blueprint.list — the S48 list, names only",
  priorityStates: ["low", "medium", "high"],
  reasons: ["window-closed", "rate-limit", "repo-off", "cap-hit"],
  windowGrammar: "HH:MM on a closed-open interval, days as Mon-Fri or a comma list",
};

/** Mutable so a successful enable flips the NEXT status answer — the row
 *  must update FROM the response, never from a local guess. */
function makeDaemon(extra: Record<string, unknown> = {}) {
  const world = { enabled: false };
  const status = () => ({
    enabled: world.enabled,
    paused: false,
    nextTickAt: "2026-11-05T15:00:00Z",
    s20Paused: false,
    attention: { open: 1 },
    descriptor: DESCRIPTOR,
    repos: [REPO_ALPHA, REPO_BETA],
    settings: [{ key: "continuous.tickInterval", value: "5m", source: "default" }],
    slots: { cap: 3, used: 1 },
    statement: STATEMENT,
  });
  const daemon = installFakeDaemon({
    responses: {
      "scheduler.status": status,
      "component.list": { components: [{ id: "c1", name: "Alpha repo", root: "/work/alpha", parentId: "", slug: "alpha" }] },
      "blueprint.list": { blueprints: [{ name: "dev-loops", version: 3, origin: "seed" }], names: ["dev-loops"] },
      "scheduler.preview": {
        now: "2026-11-05T14:55:00Z",
        wouldStart: [{ componentRoot: "/work/alpha", subjectKind: "queue", subjectId: "7", verdict: "start", reason: "admitted" }],
        decisions: [
          { componentRoot: "/work/alpha", subjectKind: "queue", subjectId: "7", verdict: "start", reason: "admitted" },
          { componentRoot: "/work/alpha", subjectKind: "roadmap", subjectId: "9", verdict: "defer", reason: "rate-limit", detail: "resets 2026-11-05T16:00:00Z" },
          { componentRoot: "/work/beta", subjectKind: "queue", subjectId: "8", verdict: "skip", reason: "repo-off", detail: "" },
        ],
      },
      "scheduler.decisions": [
        {
          id: 11,
          at: "2026-11-05T14:50:00Z",
          componentRoot: "/work/alpha",
          subjectKind: "queue",
          subjectId: 7,
          decision: "start",
          reason: "admitted",
          summary: "started loop s27-queue-7 as the row's template",
          grantRevision: "g-1",
          actorKind: "daemon",
          actorId: "master",
          loopSlug: "s27-queue-7",
        },
      ],
      "scheduler.attention": {
        open: 1,
        items: [
          {
            decisionId: 12,
            loopSlug: "s27-queue-4",
            componentRoot: "/work/alpha",
            subjectKind: "queue",
            subjectId: 4,
            kind: "cap-hit",
            note: "the loop stopped at its cost cap; raise it with aeon loop raise s27-queue-4 if the work is worth more",
            since: "2026-11-05T12:00:00Z",
          },
        ],
      },
      "scheduler.enable": () => {
        world.enabled = true;
        return { enabled: true };
      },
      "scheduler.disable": () => {
        world.enabled = false;
        return { enabled: false };
      },
      "scheduler.pause": { paused: true, runningLoops: "pause stopped picking; 1 loop keeps running" },
      "scheduler.resume": { paused: false },
      "scheduler.stopAll": { paused: true, stopped: ["s27-queue-7"], failures: [] },
      "scheduler.root.set": {
        componentRoot: REPO_ALPHA.componentRoot,
        enabled: true,
        minPriority: "medium",
        rootId: "r-alpha",
        state: { kind: "ready" },
        template: REPO_ALPHA.template,
        window: REPO_ALPHA.window,
      },
      "scheduler.root.disable": { componentRoot: REPO_BETA.componentRoot, enabled: false },
      ...extra,
    },
  });
  return { daemon, world };
}

function renderSection(
  extra: Record<string, unknown> = {},
): { daemon: FakeDaemon; world: { enabled: boolean }; view: ReturnType<typeof render> } {
  const { daemon, world } = makeDaemon(extra);
  const view = render(
    <I18nProvider locales={APP_LOCALES}>
      <MemoryRouter>
        <ContinuousSection controller={new LiveController()} live={currentLive} />
      </MemoryRouter>
    </I18nProvider>,
  );
  return { daemon, world, view };
}

afterEach(() => {
  cleanup();
  resetFakeDaemon();
});

describe("ContinuousSection (S27 T10)", () => {
  it("renders master off by default", async () => {
    renderSection();
    const header = await screen.findByTestId("continuous-header");
    expect((header.textContent ?? "").includes("off")).toBe(true);
    expect(screen.getByTestId("continuous-enable")).toBeTruthy();
    // Disabled master: no disable, no pause, no stop-all controls at all.
    expect(screen.queryByTestId("continuous-disable")).toBeNull();
    expect(screen.queryByTestId("continuous-pause")).toBeNull();
    expect(screen.queryByTestId("continuous-stop-all")).toBeNull();
    // No bulk-tree control anywhere (the CLI's door, not this panel's).
    expect(screen.queryByTestId(/enable-tree/)).toBeNull();
    // The settings fold open on request and read value + source word, verbatim.
    fireEvent.click(screen.getByText("Scheduler settings"));
    expect((screen.getByTestId("continuous-setting-continuous.tickInterval").textContent ?? "").includes("5m (default)")).toBe(true);
  });

  it("enable confirms with the daemon statement; decline sends nothing", async () => {
    const { daemon, world } = renderSection();
    await screen.findByTestId("continuous-enable");
    fireEvent.click(screen.getByTestId("continuous-enable"));
    await screen.findByTestId("authority-confirm");
    // The daemon's OWN sentence — the string that rode status, verbatim.
    expect((screen.getByTestId("authority-statement").textContent ?? "").includes(STATEMENT)).toBe(true);
    fireEvent.click(screen.getByTestId("authority-cancel"));
    await waitFor(() => expect(screen.queryByTestId("authority-confirm")).toBeNull());
    // Declined: the door was never knocked.
    expect(daemon.methods().filter((m) => m === "scheduler.enable").length).toBe(0);
    // Confirmed: the door takes {} — no actor, no fields we invented.
    fireEvent.click(screen.getByTestId("continuous-enable"));
    fireEvent.click(await screen.findByTestId("authority-confirm-go"));
    await waitFor(() => expect(daemon.methods()).toContain("scheduler.enable"));
    expect(daemon.paramsOf("scheduler.enable")).toEqual({});
    // The row updates FROM the response: the status refetch answers
    // enabled:true and the header flips — because the ANSWER said so.
    await waitFor(() => expect(screen.queryByTestId("continuous-enable")).toBeNull());
    expect((screen.getByTestId("continuous-header").textContent ?? "").includes("off")).toBe(false);
    expect(world.enabled).toBe(true);
  });

  it("invalid template shows the daemon sentence and keeps the draft", async () => {
    const refusal = "the blueprint named by the row does not load and validate — refuse template-invalid: dev-loops-bogus is not a known blueprint";
    renderSection({
      "scheduler.root.set": () => {
        throw new Error(refusal);
      },
    });
    const row = await screen.findByTestId(`repo-row-${REPO_ALPHA.componentRoot}`);
    fireEvent.click(row);
    await screen.findByTestId("root-form");
    const toast = vi.spyOn(notifications, "push");
    // Type into the draft; then the daemon refuses.
    fireEvent.change(screen.getByTestId("root-form-briefpath"), { target: { value: "docs/typed-by-the-person.md" } });
    fireEvent.click(screen.getByTestId("root-form-submit"));
    // The refusal, verbatim, as a toast — no panel is left behind…
    await waitFor(() => expect(toast).toHaveBeenCalled());
    expect(JSON.stringify(toast.mock.calls[0]?.[0])).toContain(refusal);
    expect(screen.queryByTestId("root-form-refusal")).toBeNull();
    // …and the draft stands — nothing wiped, nothing reset.
    expect((screen.getByTestId("root-form-briefpath") as HTMLInputElement).value).toBe("docs/typed-by-the-person.md");
    expect(screen.getByTestId("root-form")).toBeTruthy();
  });

  it("adds an unenrolled repository: save confirms with the daemon statement before it enables", async () => {
    const { daemon } = renderSection({
      "component.list": {
        components: [
          { id: "c1", name: "Alpha repo", root: "/work/alpha", parentId: "", slug: "alpha" },
          { id: "c3", name: "Gamma repo", root: "/work/gamma", parentId: "", slug: "gamma" },
        ],
      },
    });
    const add = await screen.findByTestId("repo-add");
    await waitFor(() => expect((add as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(add);
    await screen.findByTestId("root-form");
    expect((screen.getByTestId("repo-pick") as HTMLSelectElement).value).toBe("/work/gamma");
    fireEvent.change(await screen.findByLabelText("Blueprint"), { target: { value: "dev-loops" } });
    fireEvent.click(screen.getByTestId("root-form-submit"));
    // Enabling is the widening: the daemon's own statement first, and declining sends nothing.
    expect((await screen.findByTestId("authority-statement")).textContent).toContain(STATEMENT);
    expect(daemon.methods()).not.toContain("scheduler.root.set");
    fireEvent.click(screen.getByTestId("authority-confirm-go"));
    await waitFor(() => expect(daemon.methods()).toContain("scheduler.root.set"));
    expect((daemon.paramsOf("scheduler.root.set") as { componentRoot: string }).componentRoot).toBe("/work/gamma");
  });

  it("preview lists non-starts with reasons", async () => {
    renderSection();
    fireEvent.click(await screen.findByRole("tab", { name: /Next tick/ }));
    await screen.findByTestId("preview-tab");
    // The would-start half, as answered.
    expect(screen.getByTestId(`preview-start-line-${REPO_ALPHA.componentRoot}-queue-7`)).toBeTruthy();
    // Every non-start with the daemon's reason — and where the reason has
    // an instant, the instant rides in the daemon's detail, verbatim.
    const deferred = screen.getByTestId(`preview-hold-line-${REPO_ALPHA.componentRoot}-roadmap-9`);
    expect((deferred.textContent ?? "").includes("rate-limit")).toBe(true);
    expect((deferred.textContent ?? "").includes("resets 2026-11-05T16:00:00Z")).toBe(true);
    expect((screen.getByTestId(`preview-hold-line-${REPO_BETA.componentRoot}-queue-8`).textContent ?? "").includes("repo-off")).toBe(true);
    // The cap honesty line is the panel's own label — a count of slots,
    // not a memory measurement.
    expect(screen.getByTestId("preview-cap-note").textContent ?? "").toContain("not a memory measurement");
  });

  it("needs-you shows kind verbatim and the cap-hit instruction", async () => {
    renderSection();
    fireEvent.click(await screen.findByRole("tab", { name: /Needs you/ }));
    await screen.findByTestId("attention-item-12");
    // The kind, exactly as answered — no local name, no tone-word match.
    expect(screen.getByTestId("attention-kind-12").textContent).toBe("cap-hit");
    // S55's raise instruction — the daemon's OWN note, untouched (CN21).
    expect(
      (screen.getByTestId("attention-note-12").textContent ?? "").includes("aeon loop raise s27-queue-4"),
    ).toBe(true);
    expect((screen.getByTestId("attention-since-12").textContent ?? "").includes("2026-11-05T12:00:00Z")).toBe(true);
  });

  it("writes disabled offline; reads keep last data", async () => {
    const { daemon, view: online } = renderSection();
    await screen.findByTestId("continuous-enable");
    online.unmount();
    const view = render(
      <I18nProvider locales={APP_LOCALES}>
        <MemoryRouter>
          {/* A fresh mount against the SAME fake while not current: the
              panel stands down — no controls, only the honest Alert. */}
          <ContinuousSection controller={new LiveController()} live={staleLive} />
        </MemoryRouter>
      </I18nProvider>,
    );
    expect(await screen.findByTestId("continuous-reconnecting")).toBeTruthy();
    expect(view.queryByTestId("continuous-enable")).toBeNull();
    expect(view.queryByTestId("continuous-header")).toBeNull();
    // Nothing was asked of the offline daemon…
    expect(daemon.methods().filter((m) => m === "scheduler.status").length).toBeGreaterThan(0);
    const writesBefore = daemon.methods().filter((m) => m.startsWith("scheduler.") && !["scheduler.status", "scheduler.preview", "scheduler.decisions", "scheduler.attention", "subscribe"].includes(m)).length;
    expect(writesBefore).toBe(0);
    // …and when the connection is current again the reads come back from
    // the answers already held — the last data stands, no optimistic rows.
    view.rerender(
      <I18nProvider locales={APP_LOCALES}>
        <MemoryRouter>
          <ContinuousSection controller={new LiveController()} live={currentLive} />
        </MemoryRouter>
      </I18nProvider>,
    );
    await waitFor(() => expect(view.getByTestId("continuous-header")).toBeTruthy());
    expect((view.getByTestId("continuous-header").textContent ?? "").includes("2026-11-05T15:00:00Z")).toBe(true);
  });
});

describe("ContinuousSection with nothing enrolled", () => {
  it("a status with repos null is an honest empty list, not an unreadable answer", async () => {
    renderSection({
      "scheduler.status": () => ({
        enabled: false, paused: false, nextTickAt: "", s20Paused: false, attention: { open: 0 }, descriptor: DESCRIPTOR,
        repos: null, settings: null, slots: { cap: 3, used: 0 }, statement: STATEMENT,
      }),
    });
    expect(await screen.findByTestId("repositories-empty")).toBeTruthy();
    expect(screen.queryByTestId("continuous-status-unreadable")).toBeNull();
    expect(screen.getByTestId("continuous-enable")).toBeTruthy();
  });
});

describe("ContinuousSection survivability (S51's shape)", () => {
  it("a daemon answering bare {} is survivable: an honest empty, not a crash", async () => {
    // The Settings screen mounts every section with the answers each one
    // names; a half-wired daemon that answers bare {} must not take the
    // screen down (the S51 T7 lock, kept by every section since).
    renderSection({
      "scheduler.status": {},
      "scheduler.preview": {},
      "scheduler.decisions": {},
      "scheduler.attention": {},
    });
    const section = await screen.findByTestId("continuous-section");
    expect(section).toBeTruthy();
    expect(screen.getByTestId("continuous-status-unreadable")).toBeTruthy();
    // No numbers invented: no header figures, no controls against junk.
    expect(screen.queryByTestId("continuous-header")).toBeNull();
    expect(screen.queryByTestId("continuous-controls")).toBeNull();
  });
});
