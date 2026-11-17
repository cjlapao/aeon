import { componentList, fixtureResult, refusedFixture } from "./environment-fixtures";

/**
 * Fixtures for the S53 T5 History surface — the REAL conformance answers, read
 * from `internal/protocol/conformance/history/` (the discipline
 * `environment-fixtures.ts` and `triage-fixtures.ts` set: a UI test that
 * hand-typed its own daemon answer drifts from the wire, so these answer with
 * the fixtures and every assertion is made against what the daemon actually
 * says).
 *
 * Only two answers are authored here, and both are authored in the exact wire
 * shape the Go structs emit, to carry the states the fixture set does not:
 * the coverage answer with something UNDECODABLE (the fixture answers
 * `count: 0`, so the warning state — P31's third state — would otherwise
 * never be rendered in a test), and the honest empty (P31's `statement`, which
 * the populated `list.json` deliberately does not carry — the fixture's own
 * description says the caption belongs to the empty case only).
 *
 * Lives in `ui/test/`, not in the history folder: the production sources are
 * the guarded ones, and fixture data is ALLOWED to name protocol words —
 * naming them is its job.
 */

const coverageFixture = fixtureResult("history", "coverage.json") as {
  scope: string;
  live: { loops: number; oldest?: string; newest?: string };
  archived: { loops: number; oldest?: string; newest?: string };
  undecodable: { count: number; ids: string[] };
  floor: { statement: string };
  notShown: Array<{ reason: string; count: number }>;
};

/** The floor sentence the daemon carries — the fixture's own bytes, so a test
 *  that asserts verbatim rendering asserts the daemon's sentence and not a
 *  copy this file made up. */
export const FLOOR_STATEMENT = coverageFixture.floor.statement;

/** The fixture coverage answer: everything decodable, nothing hidden. */
export const HISTORY_COVERAGE_HEALTHY = coverageFixture;

/** The same answer with the states the fixture cannot carry: three archives
 *  the probe could not prove (named), and one reason the view does not show
 *  something. Same struct, `history.CoverageView`, nothing invented. */
export const HISTORY_COVERAGE_WARNING = {
  ...coverageFixture,
  undecodable: { count: 3, ids: ["loop_ghost_1", "loop_ghost_2", "loop_ghost_3"] },
  notShown: [{ reason: "archived loops carry no steps", count: 4 }],
};

/** The populated list answer (one live row, `costState: partial`). */
export const HISTORY_ROWS = fixtureResult("history", "list.json");

/** The export answer (one archived row, `costState: unknown`) — a DIFFERENT
 *  row on purpose, so a test can prove which door the download came from. */
export const HISTORY_EXPORT_ROWS = fixtureResult("history", "export.json");

/** The drill-down answer (a live entry's transcript). */
export const HISTORY_SHOW = fixtureResult("history", "show.json");

/** The wrong-door / unknown-id refusal, as the wire sends it (the raw JSON the
 *  bridge parses back into its parts). */
export const HISTORY_REFUSAL = refusedFixture("history", "refused.json");

/** The vocabulary the daemon serves — the ONLY source of lane and outcome
 *  words the surface may render. */
export const HISTORY_VOCABULARY = fixtureResult("history", "vocabulary.json") as {
  lanes: string[];
  outcomes: string[];
  filters: string[];
};

/** The honest empty, in the wire shape `HistoryListResult` emits it
 *  (protocol/history.go:73-76): no rows and the daemon's OWN statement. The
 *  sentence is the daemon's P31 caption; the test asserts the screen quotes it
 *  rather than writing one. */
export const HISTORY_EMPTY_WITH_STATEMENT = {
  rows: [],
  statement: "no history — no live or archived loops in scope",
};

/** The empty with NO statement, the other honest shape: the daemon said
 *  nothing, so the screen says it said nothing. */
export const HISTORY_EMPTY_BLANK = { rows: [] };

/** Every door a mounted HistoryScreen opens, answered with the fixtures. */
export function historyBaseResponses(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    // The projects folder's own roots read, which the scope picker borrows.
    "component.list": componentList,
    "history.vocabulary": HISTORY_VOCABULARY,
    "history.coverage": HISTORY_COVERAGE_HEALTHY,
    "history.list": HISTORY_ROWS,
    "history.show": HISTORY_SHOW,
    "history.export": HISTORY_EXPORT_ROWS,
    ...overrides,
  };
}
