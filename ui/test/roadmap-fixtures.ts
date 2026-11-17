import type { FakeDaemon } from "./helpers";

/**
 * Fixtures for the S50 T7 roadmap management surface — written from the
 * WIRE shapes in `internal/protocol` (`roadmap.go`, `roadmap_manage.go`,
 * `roadmap_reads.go`, `roadmap_dispatch*.go`, the conformance fixtures under
 * `internal/protocol/conformance/roadmap/`), not from the components. The
 * sentences here are the kind the daemon answers with — `Describe` is the
 * byte shape `roadmapview.Describe` composes, the blocker names the missing
 * thing, the vocabulary carries the real dispatchStatement slot ("It is
 * about: <item>") — and the tests assert the DOM carries the SAME bytes.
 */

export const ROADMAP_ROOT = "/srv/repos/ledger";

export const ROADMAP_DISPATCH_STATEMENT =
  "This starts agents with no one present unless the loop is declared attended. Outward actions follow " +
  "the standing policy — nothing here widens it. The rung moves only when the dispatched work itself " +
  "lands; a failed run leaves the item exactly where it stands. It is about: <item>";

export const ROADMAP_VOCABULARY = {
  statuses: [
    { status: "idea", next: ["ideating", "cancelled"], terminal: false },
    { status: "ideating", next: ["brief-agreed", "cancelled"], terminal: false },
    { status: "brief-agreed", next: ["planned", "cancelled"], terminal: false },
    { status: "planned", next: ["ready", "cancelled"], terminal: false },
    { status: "ready", next: ["running", "cancelled"], terminal: false },
    { status: "running", next: ["done"], terminal: false },
    { status: "done", next: null, terminal: true },
    { status: "cancelled", next: null, terminal: true },
  ],
  priorities: ["known", "assumed", "unknown"],
  provenances: ["authored", "triggered", "triaged"],
  estimateRule: "an estimate is the whole range and its basis together, or absent — never a bare number (D3/D8)",
  pathRule:
    "artifact paths must be absolute under the item's component_root, and are editable only while the item has not started (statuses idea, ideating, brief-agreed, planned, ready); from running they are history",
  pathEditableStatuses: ["idea", "ideating", "brief-agreed", "planned", "ready"],
  titleMaxBytes: 200,
  epicMaxBytes: 64,
  dispatchStatement: ROADMAP_DISPATCH_STATEMENT,
};

/** The item view as `RoadmapItemView` emits it: the estimate trio is
 *  all-present-or-all-absent, `priorityLabel` rides only when stated. */
export function roadmapItem(over: Record<string, unknown> = {}) {
  return {
    id: 7,
    componentRoot: ROADMAP_ROOT,
    title: "Weekly burn-down export",
    status: "idea",
    priorityState: "unknown",
    priorityLabel: null,
    estimateMin: null,
    estimateMax: null,
    estimateBasis: null,
    provenance: "authored",
    createdAt: "2026-11-01T10:00:00Z",
    updatedAt: "2026-11-02T10:00:00Z",
    ...over,
  };
}

export const IDEA_ITEM = roadmapItem();

export const RUNNING_ITEM = roadmapItem({
  id: 8,
  title: "Reconcile the ledger on import",
  status: "running",
  priorityState: "known",
  priorityLabel: "high",
  estimateMin: 2,
  estimateMax: 3,
  estimateBasis: "t-shirt mapped from S40's import work",
  briefPath: "/srv/repos/ledger/docs/briefs/reconcile.md",
  planPath: "/srv/repos/ledger/docs/plans/reconcile.md",
  loopSlug: "ledger-reconcile-1102",
});

/** The `describe` sentences, in the shape the ONE daemon formatter
 *  composes (three-state priority, estimate-with-basis, loop word). */
export const IDEA_DESCRIBE = "idea · priority unknown · no estimate yet · no loop";
export const RUNNING_DESCRIBE =
  "running · priority high (known) · estimate 2–3 (t-shirt mapped from S40's import work) · loop ledger-reconcile-1102";

export const IDEA_BLOCKER = "not ready: brief path missing";

export const ROADMAP_BOARD = {
  rows: [
    { item: IDEA_ITEM, describe: IDEA_DESCRIBE, ready: false, blocker: IDEA_BLOCKER },
    { item: RUNNING_ITEM, describe: RUNNING_DESCRIBE, ready: true, loop: { slug: "ledger-reconcile-1102", status: "running" } },
  ],
  groups: { idea: 1, running: 1 },
};

/** Newest first, as the door answers: a person row (the CLI's own `cli:`
 *  id) above a reactor transition — `system` rows carry the reactor's id. */
export const ROADMAP_HISTORY = {
  changes: [
    {
      id: 41,
      itemId: 7,
      op: "transition",
      summary: "reactor: idea → ideating (loop ledger-ideate-1103 started)",
      actorKind: "system",
      actorId: "system:reactor",
      occurredAt: "2026-11-03T09:05:00Z",
    },
    {
      id: 40,
      itemId: 7,
      op: "update",
      summary: "title set to “Weekly burn-down export”",
      actorKind: "person",
      actorId: "cli:cjlapao",
      occurredAt: "2026-11-02T10:00:00Z",
    },
  ],
};

export const ROADMAP_HEALTH = {
  settings: [
    { key: "roadmap.ideationBlueprint", name: "Ideation blueprint", state: "ok", reportCounts: { valid: 4, invalid: 0, unverifiable: 1 } },
    { key: "roadmap.planBlueprint", name: "Plan blueprint", state: "missing" },
  ],
  evaluatedAt: "2026-11-03T12:00:00Z",
};

/** `settings.get`'s View for each dispatch key (S44's shape): the ideation
 *  one answers the declared DEFAULT, the plan one a stored row. */
export function roadmapSettingView(key: string, over: Record<string, unknown> = {}) {
  const ideation = key === "roadmap.ideationBlueprint";
  return {
    setting: {
      area: "roadmap",
      default: ideation ? "brainstorm" : "roadmap-plan",
      description: ideation ? "The blueprint roadmap.ideate starts when no blueprint is named." : "The blueprint the plan dispatch runs.",
      envInvalid: "",
      key,
      sensitive: false,
      shadowedBy: "",
      source: ideation ? "default" : "setting",
      stored: ideation ? "" : "roadmap-plan",
      storedInvalid: false,
      type: "string",
      updatedAt: "2026-11-01T09:00:00Z",
      updatedBy: "user:cjlapao",
      value: ideation ? "brainstorm" : "roadmap-plan",
      ...over,
    },
  };
}

/** Proposal rows as the shared desk answers them (S45's row shape): two
 *  `roadmap_idea` rows and one other kind the strip must NOT render. */
export const IDEA_PROPOSAL = {
  id: 12,
  command: "aeon proposal commit 12",
  createdAt: "2026-11-03T09:00:00Z",
  createdById: "dev-loops",
  createdByKind: "agent",
  decidedAt: "",
  decidedById: "",
  decidedByKind: "",
  kind: "roadmap_idea",
  payload: { componentRoot: ROADMAP_ROOT, epic: "reporting", note: "weekly export from the effort rows", title: "Weekly burn-down export" },
  status: "proposed",
};

export const OTHER_PROPOSAL = {
  id: 13,
  command: "aeon proposal commit 13",
  createdAt: "2026-11-03T09:30:00Z",
  createdById: "dev-loops",
  createdByKind: "agent",
  decidedAt: "",
  decidedById: "",
  decidedByKind: "",
  kind: "mcp_server_add",
  payload: { server: { name: "ledger-mcp" } },
  status: "proposed",
};

export const PROPOSAL_DIGEST = "sha256:1f7be0c4d2a94c1e8d3f5a6b7c8d9e0f1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6";

/** The base answers for the management surface; tests spread overrides
 *  (a refusal, an empty board, a changed:false answer) over this. */
export function roadmapBaseResponses(): Record<string, unknown> {
  return {
    "roadmap.vocabulary": ROADMAP_VOCABULARY,
    "roadmap.board": ROADMAP_BOARD,
    "roadmap.show": (params: unknown) => {
      const id = (params as { id?: number }).id;
      return id === 8
        ? { item: RUNNING_ITEM, describe: RUNNING_DESCRIBE }
        : { item: IDEA_ITEM, describe: IDEA_DESCRIBE };
    },
    "roadmap.history": ROADMAP_HISTORY,
    "roadmap.health": ROADMAP_HEALTH,
    "roadmap.ideate": { loopSlug: "ledger-ideate-1103", started: true, status: "ideating" },
    "roadmap.plan": { dispatched: true, hostLoop: "ledger-reconcile-1102" },
    "roadmap.materialize": { loopSlug: "ledger-reconcile-1104", stamped: true, started: true, status: "running" },
    "roadmap.update": () => ({ item: IDEA_ITEM, changed: true }),
    "settings.get": (params: unknown) => roadmapSettingView(String((params as { key?: string }).key)),
    "settings.set": (params: unknown) => {
      const p = (params ?? {}) as { key?: string; value?: string };
      return { changed: true, setting: { ...roadmapSettingView(String(p.key)).setting, value: p.value, source: "setting", stored: p.value } };
    },
    "proposal.list": { proposals: [IDEA_PROPOSAL, OTHER_PROPOSAL] },
    "proposal.get": { proposal: IDEA_PROPOSAL, digest: PROPOSAL_DIGEST, preview: { describe: "add a roadmap idea: Weekly burn-down export" } },
    "proposal.commit": () => ({ proposal: { ...IDEA_PROPOSAL, status: "committed" }, effectApplied: true }),
    "proposal.reject": (params: unknown) => ({
      proposal: { ...IDEA_PROPOSAL, status: "rejected", decidedByKind: "user", decideReason: (params as { reason?: string }).reason },
    }),
  };
}

/** A daemon refusal in the shape the shell actually serialises (the same
 *  helper idiom the automation tests carry): message verbatim on the wire. */
export function roadmapRefusal(code: number, message: string): () => never {
  return () => {
    throw JSON.stringify({ code, message });
  };
}

export type RoadmapFake = FakeDaemon;
