import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { I18nProvider } from "@cjlapao/ui-kit";
import { AutomationSection } from "./AutomationSection";
import { APP_LOCALES } from "../../../lib/i18n";
import { LiveController } from "../../../lib/live";
import type { LiveState } from "../../../lib/live";
import { installFakeDaemon, resetFakeDaemon, type FakeDaemon } from "../../../../test/helpers";
import { AUTHORITY, AUTOMATION_ROWS, automationBaseResponses } from "../../../../test/automation-fixtures";

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

function renderSection(
  responses: Record<string, unknown> = automationBaseResponses(),
  live: LiveState = currentLive,
): FakeDaemon {
  const daemon = installFakeDaemon({ responses });
  render(
    <I18nProvider locales={APP_LOCALES}>
      <MemoryRouter>
        <AutomationSection controller={new LiveController()} live={live} />
      </MemoryRouter>
    </I18nProvider>,
  );
  return daemon;
}

describe("AutomationSection — the list (AN5, AN11)", () => {
  it("asks the daemon for list + health + vocabulary once, and renders the daemon's sentences verbatim", async () => {
    const daemon = renderSection();
    const list = await screen.findByTestId("automation-section");
    expect(list).toBeTruthy();
    await waitFor(() => expect(daemon.methods()).toContain("automation.list"));
    expect(daemon.methods()).toContain("automation.health");
    expect(daemon.methods()).toContain("automation.vocabulary");
    expect(daemon.calls.find((c) => c.method === "automation.vocabulary")?.params).toEqual({});

    // The per-row description is the DAEMON's sentence fetched per row —
    // quoted byte for byte, not re-derived by the UI (AN11).
    await screen.findByTestId("row-description-morning-digest");
    expect(screen.getByTestId("row-description-morning-digest").textContent).toBe(
      "Runs standup-digest at 07:00 America/New_York, weekdays only, in ~/code/work.",
    );
    // Health dot: the daemon's state word, as text too.
    expect(screen.getByTestId("row-blueprint-morning-digest").textContent).toContain("ok");
    expect(screen.getByTestId("row-blueprint-docs-on-review").textContent).toContain("missing");
  });

  it("counts needs-attention from the daemon's canFire answer only", async () => {
    renderSection();
    const count = await screen.findByTestId("attention-count");
    // morning-digest can fire; docs-on-review is live but cannot (1);
    // deploy-ping is archived — its state is not an alarm.
    expect(count.textContent).toContain("1");
  });

  it("the two empties stay distinct: the daemon's statement renders verbatim, no fallback", async () => {
    renderSection({ ...automationBaseResponses(), "automation.list": { automations: [], statement: "no automations. nothing starts agents on its own." } });
    const empty = await screen.findByTestId("automation-list-empty");
    expect(empty.textContent).toContain("no automations. nothing starts agents on its own.");
  });

  it("a bare {} answer is survivable and honest — not a crash, not a fake sentence", async () => {
    renderSection({ "automation.list": {}, "automation.health": {}, "automation.vocabulary": {} });
    await screen.findByTestId("automation-section");
    // No throw, no rows. The fallback copy admits the daemon said nothing.
    expect(screen.queryAllByTestId(/^automation-row-/)).toHaveLength(0);
  });

  it("a stale connection says so and asks nothing", async () => {
    const daemon = renderSection(automationBaseResponses(), staleLive);
    expect(await screen.findByTestId("automation-reconnecting")).toBeTruthy();
    expect(daemon.methods()).not.toContain("automation.list");
    expect(daemon.methods()).not.toContain("automation.vocabulary");
  });

  it("filters travel as automation.list PARAMS, not as client-side hiding", async () => {
    const daemon = renderSection();
    await screen.findByTestId("automation-row-morning-digest");
    fireEvent.change(screen.getByTestId("filter-source"), { target: { value: "event" } });
    fireEvent.change(screen.getByPlaceholderText("name, id or the plain-words sentence"), { target: { value: "docs" } }); // the search box debounces
    await waitFor(() => {
      const call = [...daemon.calls].reverse().find((c) => c.method === "automation.list");
      expect(call?.params).toMatchObject({ source: "event", q: "docs" });
    });
    // The default list never sends the archived word at all — exclude is the
    // daemon's default and the UI does not restate defaults as params.
    const first = daemon.calls.find((c) => c.method === "automation.list");
    expect(first?.params).not.toHaveProperty("archived");
  });
});

describe("AutomationSection — the drawer (AN3, AN12, AN6)", () => {
  it("enable passes the authority confirmation carrying the daemon's statement — and nothing fires without the click", async () => {
    const responses = automationBaseResponses();
    // The same row, disarmed: the drawer must offer ENABLE and route it
    // through the authority confirmation (AN3/AN12).
    responses["automation.list"] = { automations: [{ ...AUTOMATION_ROWS.event, enabled: false }] };
    const baseShow = responses["automation.show"] as (p: unknown) => Record<string, unknown>;
    responses["automation.show"] = (params: unknown) => {
      const answer = baseShow(params);
      return { ...answer, automation: { ...(answer.automation as Record<string, unknown>), enabled: false, disabledReason: "created disarmed by the automation form — enabling is a separate deliberate act (AN3)" } };
    };
    const daemon = renderSection(responses);
    fireEvent.click(await screen.findByTestId("automation-row-docs-on-review"));
    await screen.findByTestId("automation-drawer");

    fireEvent.click(screen.getByTestId("drawer-enable"));
    // The confirmation is up BEFORE any write leaves the app:
    const modal = await screen.findByTestId("authority-confirm");
    expect(modal.textContent).toContain(AUTHORITY);
    expect(daemon.methods()).not.toContain("automation.trigger.enable");

    fireEvent.click(screen.getByTestId("authority-confirm-go"));
    await waitFor(() => expect(daemon.methods()).toContain("automation.trigger.enable"));
    const call = daemon.calls.find((c) => c.method === "automation.trigger.enable");
    expect(call?.params).toEqual({ id: "docs-on-review" });
  });

  it("disable REQUIRES a reason — the door is not even called while the field is blank", async () => {
    const daemon = renderSection();
    fireEvent.click(await screen.findByTestId("automation-row-docs-on-review"));
    await screen.findByTestId("automation-drawer");

    fireEvent.click(screen.getByTestId("drawer-disable"));
    await screen.findByTestId("reason-modal");
    const submit = screen.getByTestId("reason-submit") as HTMLButtonElement;
    expect(submit.disabled).toBe(true);

    fireEvent.change(screen.getByLabelText(/Reason/i), { target: { value: "storm of review requests" } });
    expect((screen.getByTestId("reason-submit") as HTMLButtonElement).disabled).toBe(false);
    fireEvent.click(screen.getByTestId("reason-submit"));
    await waitFor(() => expect(daemon.methods()).toContain("automation.trigger.disable"));
    expect(daemon.calls.find((c) => c.method === "automation.trigger.disable")?.params).toEqual({
      id: "docs-on-review",
      reason: "storm of review requests",
    });
  });

  it("fire-now is an armed act: authority first, and the answer keeps the door's outcome word", async () => {
    const daemon = renderSection();
    fireEvent.click(await screen.findByTestId("automation-row-morning-digest"));
    await screen.findByTestId("automation-drawer");
    fireEvent.click(screen.getByTestId("drawer-fire-now"));
    expect(await screen.findByTestId("authority-confirm")).toBeTruthy();
    expect(daemon.methods()).not.toContain("trigger.fireNow");
    fireEvent.click(screen.getByTestId("authority-confirm-go"));
    await waitFor(() => expect(daemon.methods()).toContain("trigger.fireNow"));
    expect(await screen.findByTestId("drawer-fire-result")).toBeTruthy();
  });

  it("archive asks a reason and there is NO delete control anywhere (AN6)", async () => {
    renderSection();
    fireEvent.click(await screen.findByTestId("automation-row-morning-digest"));
    const drawer = await screen.findByTestId("automation-drawer");
    expect(screen.queryByTestId("drawer-delete")).toBeNull();
    expect(within(drawer).queryByText(/delete/i)).toBeNull();
    fireEvent.click(screen.getByTestId("drawer-archive"));
    await screen.findByTestId("reason-modal");
    fireEvent.change(screen.getByLabelText(/Reason/i), { target: { value: "superseded" } });
    fireEvent.click(screen.getByTestId("reason-submit"));
  });

  it("unarchive also passes the authority confirmation and says the row comes back disarmed", async () => {
    const daemon = renderSection();
    // deploy-ping is archived — open it (the default list hides archived
    // rows, so widen the filter exactly as the acceptance walk does).
    fireEvent.change(screen.getByTestId("filter-archived"), { target: { value: "include" } });
    fireEvent.click(await screen.findByTestId("automation-row-deploy-ping"));
    await screen.findByTestId("automation-drawer");
    fireEvent.click(screen.getByTestId("drawer-unarchive"));
    const modal = await screen.findByTestId("authority-confirm");
    expect(modal.textContent).toContain("disarmed");
    fireEvent.click(screen.getByTestId("authority-confirm-go"));
    await waitFor(() => expect(daemon.methods()).toContain("automation.unarchive"));
  });
});

describe("AutomationSection — forms (AN3, AN11)", () => {
  it("a new schedule is created and then PAUSED unless armed: the AN3 pair on the wire", async () => {
    const daemon = renderSection();
    fireEvent.click(await screen.findByTestId("create-schedule"));
    await screen.findByTestId("automation-form-schedule");
    fireEvent.change(screen.getByLabelText(/^Id$/i), { target: { value: "nightly" } });

    // The blueprint picker lists what blueprint.list answered, the zone
    // select offers ONLY vocabulary zones, and the cron field carries the
    // daemon's preview — never a client-side parse.
    const blueprint = screen.getByLabelText(/Blueprint/i) as HTMLSelectElement;
    await waitFor(() => expect(Array.from(blueprint.options).map((o) => o.value)).toContain("standup-digest"));
    fireEvent.change(blueprint, { target: { value: "standup-digest" } });
    fireEvent.change(screen.getByLabelText(/Cron/i), { target: { value: "0 7 * * *" } });
    const preview = await screen.findByTestId("preview-instant-next");
    expect(preview.textContent).toContain("Wed Nov 4 07:00 AM EST"); // daemon's wall word, verbatim
    const zone = screen.getByLabelText(/Timezone/i) as HTMLSelectElement;
    fireEvent.change(zone, { target: { value: "America/New_York" } });
    expect(zone.options.length).toBe(3); // exactly the vocabulary's zones — no free-text timezone is offered

    fireEvent.change(screen.getByLabelText(/Workspace/i), { target: { value: "~/code/work" } });
    fireEvent.change(screen.getByLabelText(/Goal/i), { target: { value: "Nightly digest" } });

    const save = screen.getByTestId("automation-form-save") as HTMLButtonElement;
    await waitFor(() => expect(save.disabled).toBe(false));
    fireEvent.click(save); // NOT armed — the checkbox stays unchecked
    await waitFor(() => expect(daemon.methods()).toContain("trigger.add"));
    expect(daemon.methods()).not.toContain("authority-confirm");
    await waitFor(() => expect(daemon.methods()).toContain("trigger.pause"));

    const add = daemon.calls.find((c) => c.method === "trigger.add")?.params as Record<string, unknown>;
    expect(add).toMatchObject({ kind: "schedule", id: "nightly", expr: "0 7 * * *", zone: "America/New_York" });
    // The section closed the form and reloaded the list on success — the
    // disarmed note lived inside the form; the WIRE pair above is the proof.
    await screen.findByTestId("automation-section");
  });

  it("creating ARMED passes the daemon's authority statement before anything is sent", async () => {
    const daemon = renderSection();
    fireEvent.click(await screen.findByTestId("create-schedule"));
    await screen.findByTestId("automation-form-schedule");
    fireEvent.change(screen.getByLabelText(/^Id$/i), { target: { value: "nightly" } });
    fireEvent.change(screen.getByLabelText(/Blueprint/i), { target: { value: "standup-digest" } });
    fireEvent.change(screen.getByLabelText(/Cron/i), { target: { value: "0 7 * * *" } });
    fireEvent.change(screen.getByLabelText(/Workspace/i), { target: { value: "~/code/work" } });
    fireEvent.change(screen.getByLabelText(/Goal/i), { target: { value: "Nightly digest" } });
    fireEvent.click(screen.getByLabelText(/Create armed/i));
    fireEvent.click(screen.getByTestId("automation-form-save"));
    // Nothing left the app until the confirmation is clicked.
    expect(daemon.methods()).not.toContain("trigger.add");
    const modal = await screen.findByTestId("authority-confirm");
    expect(modal.textContent).toContain(AUTHORITY);
    fireEvent.click(screen.getByTestId("authority-confirm-go"));
    await waitFor(() => expect(daemon.methods()).toContain("trigger.add"));
    expect(daemon.methods()).not.toContain("trigger.pause"); // armed means armed
  });

  it("the webhook form asks for the secret's NAME and refuses a value-shaped entry without a socket call", async () => {
    const daemon = renderSection();
    fireEvent.click(await screen.findByTestId("create-webhook"));
    await screen.findByTestId("automation-form-webhook");
    // No cron field on the webhook form — the POST is its clock.
    expect(screen.queryByLabelText(/Cron/i)).toBeNull();
    fireEvent.change(screen.getByLabelText(/^Id$/i), { target: { value: "ping" } });
    fireEvent.change(screen.getByLabelText(/Blueprint/i), { target: { value: "standup-digest" } });
    fireEvent.change(screen.getByLabelText(/Workspace/i), { target: { value: "~/code/work" } });
    fireEvent.change(screen.getByLabelText(/Secret env var NAME/i), { target: { value: "sk=abc123" } });
    expect(await screen.findByTestId("secret-env-refusal")).toBeTruthy();
    expect((screen.getByTestId("automation-form-save") as HTMLButtonElement).disabled).toBe(true);
    fireEvent.change(screen.getByLabelText(/Secret env var NAME/i), { target: { value: "AEON_WEBHOOK_SECRET" } });
    await waitFor(() => expect((screen.getByTestId("automation-form-save") as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(screen.getByTestId("automation-form-save"));
    await waitFor(() => expect(daemon.methods()).toContain("trigger.add"));
    const add = daemon.calls.find((c) => c.method === "trigger.add")?.params as Record<string, unknown>;
    expect(add).toMatchObject({ kind: "webhook", secretEnv: "AEON_WEBHOOK_SECRET" });
    // Only the NAME crossed the wire. The value field the form refuses to
    // hold never appears anywhere on the params.
    expect(JSON.stringify(add)).not.toContain("abc123");
  });

  it("the event form tests a sample through the daemon and shows the per-key answer verbatim", async () => {
    const daemon = renderSection();
    fireEvent.click(await screen.findByTestId("create-event"));
    await screen.findByTestId("automation-form-event");
    const eventSelect = screen.getByLabelText(/Event/i) as HTMLSelectElement;
    fireEvent.change(eventSelect, { target: { value: "component.review_requested" } });
    // applicableKeys: this event applies to task_type ONLY — goal_type must
    // not be offered (AN9: an inapplicable key is a match that can never fire).
    expect(await screen.findByTestId("match-key-task_type")).toBeTruthy();
    expect(screen.queryByTestId("match-key-goal_type")).toBeNull();

    fireEvent.click(screen.getByTestId("match-test-run"));
    const answer = await screen.findByTestId("match-test-answer");
    expect(answer.textContent).toContain("would start a loop");
    expect(answer.textContent).toContain("event=component.review_requested task_type=docs"); // canonical, verbatim
    expect(daemon.methods()).toContain("automation.test");
  });
});

describe("AutomationSection — fire log, audit, proposals (AN16, PM4)", () => {
  it("the fire log keeps all five counts distinct, filters by the vocabulary's outcomes, and renders a sixth word verbatim", async () => {
    const daemon = renderSection();
    fireEvent.click(await screen.findByRole("tab", { name: "Fire log" }));
    await screen.findByTestId("fire-log");
    for (const word of ["started", "refused", "limited", "skipped", "could-not-say"]) {
      expect(screen.getByTestId(`fire-count-${word}`)).toBeTruthy();
    }
    expect(screen.getByTestId("fire-count-could-not-say").textContent).toContain("1");
    const filter = screen.getByTestId("firelog-outcome-filter") as HTMLSelectElement;
    expect(filter.options.length).toBe(1 + 5); // all + the vocabulary's five words only

    // The daemon's sixth word — the one the vocabulary does NOT carry — reads
    // verbatim, purple, and un-folded into could-not-say.
    const deferred = await screen.findByTestId("fire-outcome-87");
    expect(deferred.textContent).toBe("deferred");

    fireEvent.change(filter, { target: { value: "refused" } });
    await waitFor(() => {
      const call = [...daemon.calls].reverse().find((c) => c.method === "automation.fires");
      expect(call?.params).toMatchObject({ outcome: "refused" });
    });
  });

  it("the audit tab walks automation.changes with the actor the daemon stamped", async () => {
    renderSection();
    fireEvent.click(await screen.findByRole("tab", { name: "Audit" }));
    const row = await screen.findByTestId("audit-row-3");
    expect(row.textContent).toContain("cron moved from 0 7 * * 1-5 to 0 7 * * *");
    expect(row.textContent).toContain("human:cjlapao");
  });

  it("the proposals tab filters the shared queue to automation kinds and reuses the policy card", async () => {
    const daemon = renderSection();
    fireEvent.click(await screen.findByRole("tab", { name: "Proposals" }));
    await waitFor(() => expect(daemon.methods()).toContain("proposal.list"));
    await screen.findByTestId("automation-proposals");
    // The one automation_disable proposal shows; the environment_restrict
    // one belongs to the policy tab and must not appear here.
    expect(screen.getAllByTestId(/proposal-card-12/).length).toBeGreaterThan(0);
    expect(screen.queryAllByTestId(/proposal-card-13/)).toHaveLength(0);
  });

  it("nothing in this section can reach a door the plane does not have", async () => {
    const daemon = renderSection();
    await screen.findByTestId("automation-section");
    fireEvent.click(screen.getByRole("tab", { name: "Fire log" }));
    fireEvent.click(screen.getByRole("tab", { name: "Audit" }));
    fireEvent.click(screen.getByRole("tab", { name: "Proposals" }));
    await waitFor(() => expect(daemon.methods()).toContain("proposal.list"));
    for (const method of daemon.methods()) {
      expect(
        method.startsWith("automation.") ||
        method.startsWith("trigger.") ||
        method === "blueprint.list" ||
        method === "component.list" ||
        method === "proposal.list" ||
        method === "proposal.get" ||
        method === "proposal.commit" ||
        method === "proposal.reject",
      ).toBe(true);
    }
  });
});
