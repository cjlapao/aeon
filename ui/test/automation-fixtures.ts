import type { FakeDaemon } from "./helpers";

/**
 * Fixtures for the S49 T9 automation section — written from the WIRE shapes
 * in `internal/protocol` (the conformance fixtures under
 * `internal/protocol/conformance/automation/` cover the same doors), not
 * from the components. Sentences here are the kind the daemon answers with
 * (AN11 is tested by quoting them verbatim and asserting the DOM shows the
 * SAME bytes).
 */

export const AUTHORITY = "Enabling an automation hands the daemon standing authority to start real work without asking again.";

export const VOCABULARY = {
  authorityStatement: AUTHORITY,
  events: [
    { event: "loop.finished", meaning: "A loop reached a terminal state.", applicableKeys: ["task_type", "goal_type"] },
    { event: "component.review_requested", meaning: "A component asked for review.", applicableKeys: ["task_type"] },
  ],
  goalTypes: ["ship", "stabilize"],
  limits: { auditPageMax: 100, maxValueLen: 128, maxValuesPerKey: 16, previewMax: 5, triggerIdMaxLen: 64 },
  matchKeys: ["task_type", "goal_type"],
  outcomes: [
    { family: "worked", meaning: "A loop started.", word: "started" },
    { family: "declined", meaning: "The gate said no.", word: "refused" },
    { family: "declined", meaning: "A budget limit stopped it.", word: "limited" },
    { family: "silent", meaning: "The event matched nothing to do.", word: "skipped" },
    { family: "unknown", meaning: "The engine cannot say; this is never folded into another word.", word: "could-not-say" },
  ],
  taskTypes: ["code", "docs"],
  windowGrammar: "HH:MM for start/end; days as comma lists or ranges like mon-fri",
  zones: ["UTC", "America/New_York", "Europe/Lisbon"],
};

const SCHEDULE_ROW = {
  source: "schedule", id: "morning-digest", name: "morning-digest", blueprint: "standup-digest",
  workspace: "~/code/work", goal: "Daily standup digest", expr: "0 7 * * *", zone: "America/New_York",
  window: { start: "09:00", end: "18:00", days: "mon-fri" },
  enabled: true, paused: false, archived: false, archivedReason: "", disabledReason: "",
  createdByKind: "human", createdById: "cjlapao",
  createdAt: "2026-11-01T10:00:00Z", updatedAt: "2026-11-02T10:00:00Z",
  maxRunsPerHour: 4, maxCostPerDayUsd: 5,
  lastFire: { source: "schedule", triggerId: "morning-digest", fireId: 91, at: "2026-11-02T12:00:00Z", outcome: "started", reason: "", loopId: "loop_a", loopSlug: "morning-digest-1102" },
};

const EVENT_ROW = {
  source: "event", id: "docs-on-review", name: "Docs on review", blueprint: "docs-polish",
  match: { event: "component.review_requested", task_type: ["docs"] },
  kickoff: "Polish the docs for {{subjectId}}.", componentId: "comp_1",
  enabled: true, paused: false, archived: false, archivedReason: "", disabledReason: "",
  createdByKind: "agent", createdById: "dev-loops",
  createdAt: "2026-10-20T08:00:00Z", updatedAt: "2026-10-28T08:00:00Z",
  maxRunsPerHour: 6, maxCostPerDayUsd: null,
  lastFire: null,
};

const WEBHOOK_ROW = {
  source: "webhook", id: "deploy-ping", name: "deploy-ping", blueprint: "incident-triage",
  secretEnv: "AEON_WEBHOOK_SECRET", zone: "UTC",
  workspace: "~/code/work", expr: "@on-demand",
  enabled: true, paused: false, archived: true, archivedReason: "replaced by pagerduty route", disabledReason: "",
  createdByKind: "human", createdById: "cjlapao",
  createdAt: "2026-09-01T08:00:00Z", updatedAt: "2026-10-01T08:00:00Z",
  maxRunsPerHour: 2, maxCostPerDayUsd: null,
  lastFire: { source: "webhook", triggerId: "deploy-ping", fireId: 5, at: "2026-10-01T07:00:00Z", outcome: "could-not-say", reason: "the run book was sealed before the outcome seam", loopId: "", loopSlug: "" },
};

export const AUTOMATION_ROWS = {
  schedule: SCHEDULE_ROW,
  event: EVENT_ROW,
  webhook: WEBHOOK_ROW,
  archived: WEBHOOK_ROW,
};

const HEALTH = [
  {
    source: "schedule", id: "morning-digest", canFire: true, reasons: [] as string[],
    blueprint: { state: "ok" }, scope: { exists: true }, workspace: { exists: true },
  },
  {
    source: "event", id: "docs-on-review", canFire: false,
    reasons: ["blueprint docs-polish does not resolve — nothing to start"],
    blueprint: { state: "missing" }, scope: { exists: true }, workspace: { exists: true },
  },
  {
    source: "webhook", id: "deploy-ping", canFire: true, reasons: [] as string[],
    blueprint: { state: "ok" }, scope: { exists: true }, workspace: { exists: true },
    webhook: { envName: "AEON_WEBHOOK_SECRET", set: false },
  },
];

const SHOW_DESCRIPTIONS: Record<string, string> = {
  "schedule:morning-digest": "Runs standup-digest at 07:00 America/New_York, weekdays only, in ~/code/work.",
  "event:docs-on-review": "Starts docs-polish when a component asks for review and the task is docs work.",
  "webhook:deploy-ping": "Starts incident-triage when a signed POST arrives, using the secret named AEON_WEBHOOK_SECRET.",
};

function showFor(params: unknown) {
  const p = params as { source: string; id: string };
  const key = `${p.source}:${p.id}`;
  const row = AUTOMATION_ROWS[p.id === "morning-digest" ? "schedule" : p.id === "docs-on-review" ? "event" : "webhook"];
  return {
    automation: { ...row, description: SHOW_DESCRIPTIONS[key] },
    description: SHOW_DESCRIPTIONS[key],
    health: HEALTH.find((h) => h.source === p.source && h.id === p.id) ?? HEALTH[0],
    recentFires: p.id === "morning-digest"
      ? [{ source: "schedule", triggerId: "morning-digest", fireId: 91, at: "2026-11-02T12:00:00Z", outcome: "started", reason: "", loopId: "loop_a", loopSlug: "morning-digest-1102" }]
      : [],
    changes: [{ id: 3, source: p.source, triggerId: p.id, op: "update", summary: "cron moved from 0 7 * * 1-5 to 0 7 * * *", actorKind: "human", actorId: "cjlapao", occurredAt: "2026-11-02T10:00:00Z" }],
    schedule: p.source === "schedule" ? { expr: "0 7 * * *", zone: "America/New_York" } : { expr: "", zone: "UTC" },
  };
}

const FIRES = {
  counts: { started: 2, refused: 1, limited: 1, skipped: 1, couldNotSay: 1 },
  fires: [
    { source: "schedule", triggerId: "morning-digest", fireId: 92, at: "2026-11-03T12:00:00Z", outcome: "started", reason: "", loopId: "loop_b", loopSlug: "morning-digest-1103" },
    { source: "event", triggerId: "docs-on-review", fireId: 90, at: "2026-11-03T09:00:00Z", outcome: "refused", reason: "max runs per hour reached", loopId: "", loopSlug: "" },
    { source: "schedule", triggerId: "morning-digest", fireId: 89, at: "2026-11-02T12:00:00Z", outcome: "limited", reason: "daily cost ceiling $5 reached", loopId: "", loopSlug: "" },
    { source: "event", triggerId: "docs-on-review", fireId: 88, at: "2026-11-02T09:00:00Z", outcome: "skipped", reason: "", loopId: "", loopSlug: "" },
    { source: "webhook", triggerId: "deploy-ping", fireId: 5, at: "2026-10-01T07:00:00Z", outcome: "could-not-say", reason: "the run book was sealed before the outcome seam", loopId: "", loopSlug: "" },
    // A word the vocabulary does not carry — it must render verbatim, never
    // folded into one of the five (AN16 says the five are distinct; nothing
    // says a future sixth would join them).
    { source: "schedule", triggerId: "morning-digest", fireId: 87, at: "2026-11-01T12:00:00Z", outcome: "deferred", reason: "daemon test word outside the five", loopId: "", loopSlug: "" },
  ],
  nextBeforeId: "87",
};

export function automationBaseResponses(): Record<string, unknown> {
  return {
    "automation.vocabulary": VOCABULARY,
    "automation.list": { automations: [SCHEDULE_ROW, EVENT_ROW, WEBHOOK_ROW] },
    "automation.health": { evaluatedAt: "2026-11-03T12:00:00Z", health: HEALTH },
    "automation.show": (params: unknown) => showFor(params),
    "automation.fires": FIRES,
    "automation.preview": { instants: [
      { at: "2026-11-04T12:00:00Z", kind: "next", wall: "Wed Nov 4 07:00 AM EST" },
      { at: "2026-11-05T12:00:00Z", kind: "after", wall: "Thu Nov 5 07:00 AM EST" },
    ] },
    "automation.test": { canonical: "event=component.review_requested task_type=docs", matches: true, perKey: [
      { key: "task_type", ok: true, want: ["docs"], have: "docs" },
    ] },
    "automation.dryrun": { from: 200, scanned: 47, wouldMatch: [{ outboxId: 12, at: "2026-11-02T09:00:00Z", event: "component.review_requested", subjectKind: "component", subjectId: "comp_1" }] },
    "automation.changes": { changes: [{ id: 3, source: "schedule", triggerId: "morning-digest", op: "update", summary: "cron moved from 0 7 * * 1-5 to 0 7 * * *", actorKind: "human", actorId: "cjlapao", occurredAt: "2026-11-02T10:00:00Z" }] },
    "automation.runs": { total: 41, loops: [{ id: "loop_b", slug: "morning-digest-1103", startedAt: "2026-11-03T12:00:00Z", status: "running" }] },
    "blueprint.list": { blueprints: [{ name: "standup-digest", origin: "builtin", version: 2, revision: 4, contentHash: "aa", usedByCount: 1, updatedAt: "2026-11-01T00:00:00Z", updatedBy: "shipped" }], names: ["standup-digest"] },
    "component.list": { components: [{ id: "comp_1", name: "Aeon", slug: "aeon", description: "", shortDescription: "", icon: "", branchPrefix: "", defaultBranch: "main", owner: "cjlapao", parentId: "", remote: "", remoteKey: "", repoKey: "", root: "", maxParallelAgents: 1, tags: [], createdAt: "2026-01-01T00:00:00Z", updatedAt: "2026-01-01T00:00:00Z" }] },
    "proposal.list": { proposals: [
      { id: 12, kind: "automation_disable", payload: { source: "event", id: "docs-on-review", reason: "storm of review requests" }, command: "aeon automation trigger disable docs-on-review --reason 'storm of review requests'", status: "pending", createdByKind: "agent", createdById: "dev-loops", createdAt: "2026-11-03T08:00:00Z" },
      { id: 13, kind: "environment_restrict", payload: { target: { id: "env_1", kind: "environment" }, action: "deny" }, command: "aeon environment update env_1 --deny 'AWS_*'", status: "pending", createdByKind: "agent", createdById: "dev-loops", createdAt: "2026-11-03T09:00:00Z" },
    ] },
    "proposal.get": () => ({
      proposal: { id: 12, kind: "automation_disable", payload: { source: "event", id: "docs-on-review", reason: "storm of review requests" }, command: "aeon proposal commit 12", status: "proposed", createdByKind: "agent", createdById: "dev-loops", createdAt: "2026-11-03T08:00:00Z" },
      digest: "ff00ff0011223344",
      preview: { kind: "automation_disable", target: "event:docs-on-review", describe: "disable the docs-on-review trigger — storm of review requests", inForce: [] },
    }),
    "proposal.commit": { applied: true, id: 12, note: "applied" },
    "proposal.reject": { id: 12, status: "rejected" },
    "trigger.add": { found: true, reason: "created", trigger: { id: "nightly" } },
    "trigger.update": { changed: true, before: "0 7 * * *", after: "30 6 * * *" },
    "trigger.pause": { changed: true, paused: true },
    "trigger.resume": { changed: true, paused: false },
    "trigger.fireNow": { found: true, loopId: "loop_z", outcome: "started", paused: false, triggerId: "morning-digest" },
    "automation.trigger.create": { id: "new-event", blueprint: "standup-digest", componentId: "", createdAt: "2026-11-03T10:00:00Z", createdById: "cjlapao", createdByKind: "human", disabledReason: "", enabled: true, kickoff: "", lastFire: null, match: { event: "loop.finished" }, maxCostPerDayUsd: null, maxRunsPerHour: 6, name: "New event", rootId: "", source: "event", updatedAt: "2026-11-03T10:00:00Z" },
    "automation.trigger.update": { blueprint: "standup-digest", componentId: "", createdAt: "2026-11-03T10:00:00Z", createdById: "cjlapao", createdByKind: "human", disabledReason: "", enabled: true, id: "docs-on-review", kickoff: "", lastFire: null, match: { event: "loop.finished" }, maxCostPerDayUsd: null, maxRunsPerHour: 6, name: "Docs on review", rootId: "", source: "event", updatedAt: "2026-11-03T10:00:00Z" },
    "automation.trigger.enable": { blueprint: "docs-polish", componentId: "", createdAt: "2026-10-20T08:00:00Z", createdById: "dev-loops", createdByKind: "agent", disabledReason: "", enabled: true, id: "docs-on-review", kickoff: "", lastFire: null, match: { event: "component.review_requested", task_type: ["docs"] }, maxCostPerDayUsd: null, maxRunsPerHour: 6, name: "Docs on review", rootId: "", source: "event", updatedAt: "2026-11-03T10:30:00Z" },
    "automation.trigger.disable": { blueprint: "docs-polish", componentId: "", createdAt: "2026-10-20T08:00:00Z", createdById: "dev-loops", createdByKind: "agent", disabledReason: "storm of review requests", enabled: false, id: "docs-on-review", kickoff: "", lastFire: null, match: { event: "component.review_requested" }, maxCostPerDayUsd: null, maxRunsPerHour: 6, name: "Docs on review", rootId: "", source: "event", updatedAt: "2026-11-03T10:31:00Z" },
    "automation.archive": { before: { archived: false }, after: { archived: true, archivedReason: "superseded" }, changed: true },
    "automation.unarchive": { before: { archived: true, archivedReason: "replaced by pagerduty route" }, after: { archived: false, enabled: false, paused: false }, changed: true, health: HEALTH[0] },
  };
}

export type { FakeDaemon };
