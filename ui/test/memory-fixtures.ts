import { fixtureResult } from "./environment-fixtures";

/**
 * Fixtures for the S52 T6 memory management surface — read from the REAL
 * conformance answers under `internal/protocol/conformance/learnings/`
 * (the discipline `environment-fixtures.ts` set: a UI test that hand-typed
 * its own daemon answer drifts from the wire; these answer with the
 * fixtures, so every assertion is made against what the daemon actually
 * says). Only the rows the T2 fixtures do not carry are authored here, and
 * they are authored in the exact wire shape the Go `LearningsView`/board
 * row emits: a RETRACTED row (the retraction stamp the board answers only
 * under `include-retracted`), a `relayed` row (the channel fact LM10 wants
 * visibly distinct), and one pending `learning_retract` proposal for the
 * desk.
 */

export const MEMORY_BOARD_ROWS = (fixtureResult("learnings", "board.json") as {
  learnings: Array<Record<string, unknown>>;
}).learnings;

/** A second board row in the same wire shape: the states the fixture board
 *  does not carry — a retraction that has already landed, with its stamp.
 *  The board answers it ONLY under `include-retracted` (LM2), which the
 *  test proves by reading the params back. */
export const MEMORY_BOARD_RETRACTED_ROW = {
  id: 12,
  scope: "person",
  scopeKey: "person",
  body: "the staging host still runs the 2019 TLS stack",
  provenance: "told",
  observedCount: 1,
  tags: ["staging"],
  sourceLoopSlug: "",
  createdAt: "2026-10-29T18:00:00Z",
  describe:
    'told by a person — the person\'s own word; retract moves it behind the retraction line, never out of the file (S52 LM2)',
  retracted: true,
  retractedAt: "2026-11-03T10:00:00Z",
  retractedBy: "user:1000",
  retractedReason: "the staging host was rebuilt in October; the memory is stale",
};

/** The channel fact: `relayed` rows are a fact ABOUT the channel (S20 T7),
 *  and LM10 wants that word visibly distinct everywhere. */
export const MEMORY_BOARD_RELAYED_ROW = {
  id: 13,
  scope: "project",
  scopeKey: "/srv/repos/ledger",
  body: "the provider documents a 24h retry window",
  provenance: "relayed",
  observedCount: 1,
  tags: ["docs"],
  sourceLoopSlug: "loop-cgo-fix",
  createdAt: "2026-11-01T08:30:00Z",
  describe:
    "relayed — a fact about the channel, never a person's word (M1: it ranks below)",
};

export const MEMORY_VOCABULARY = fixtureResult("learnings", "vocabulary.json") as {
  scopes: string[];
  provenances: string[];
  actorKinds: string[];
  m1Ordering: string;
  retractLaw: string;
  limits: { reasonMin: number; reasonMax: number; forAgentLimitDefault: number; forAgentLimitMax: number; summaryMax: number };
  tagGrammar: { exact: boolean; prefix: boolean };
};

export const MEMORY_SHOW = fixtureResult("learnings", "show.json");
export const MEMORY_HISTORY = fixtureResult("learnings", "history.json");
export const MEMORY_FORAGENT = fixtureResult("learnings", "foragent.json");
export const MEMORY_RETRACT_ANSWER = fixtureResult("learnings", "retract.json");
export const MEMORY_RESTORE_ANSWER = fixtureResult("learnings", "restore.json");

/** One pending `learning_retract` proposal — the shape `proposal.list`
 *  carries for the desk (S45's wire, S52 T4's kind): the agent filed it,
 *  nothing moves until a person commits it. */
export const MEMORY_RETRACT_PROPOSAL = {
  id: 41,
  kind: "learning_retract",
  status: "proposed",
  createdById: "golang-engineer",
  createdByKind: "agent",
  createdAt: "2026-11-04T10:00:00Z",
  payload: { id: 12, reason: "the staging memory names a host that no longer exists" },
};

/** The answers a mounted MemorySection needs to render fully. */
export function memoryBaseResponses(): Record<string, unknown> {
  return {
    "learnings.vocabulary": MEMORY_VOCABULARY,
    "learnings.board": {
      learnings: [...MEMORY_BOARD_ROWS, MEMORY_BOARD_RELAYED_ROW, MEMORY_BOARD_RETRACTED_ROW],
    },
    "learnings.show": MEMORY_SHOW,
    "learnings.history": MEMORY_HISTORY,
    "learnings.forAgent": MEMORY_FORAGENT,
    "learnings.retract": MEMORY_RETRACT_ANSWER,
    "learnings.restore": MEMORY_RESTORE_ANSWER,
    "loop.list": {
      all: true,
      loops: [
        {
          id: "7",
          slug: "loop-cgo-fix",
          title: "fix the CGO shim",
          blueprint: "dev-loops",
          phase: "running",
          status: "active",
          mode: "attended",
          componentRoot: "/srv/repos/ledger",
          startedAt: "2026-10-30T06:00:00Z",
          updatedAt: "2026-11-01T09:00:00Z",
          awaiting: false,
        },
      ],
    },
    "agent.list": {
      agents: [
        { id: 3, name: "golang-engineer", slug: "golang-engineer", provenance: "imported", enabled: true },
      ],
    },
    "proposal.list": { proposals: [MEMORY_RETRACT_PROPOSAL] },
    "proposal.get": MEMORY_RETRACT_PROPOSAL,
  };
}
