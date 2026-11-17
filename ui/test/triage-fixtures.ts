import type { FakeDaemon } from "./helpers";
import { fixtureResult } from "./environment-fixtures";

/**
 * Fixtures for the S51 T7 triage management surface — read from the REAL
 * conformance answers under `internal/protocol/conformance/triage/` (the
 * discipline `environment-fixtures.ts` set: a UI test that hand-typed its
 * own daemon answer drifts from the wire; these answer with the fixtures,
 * so every assertion is made against what the daemon actually says). Only
 * the SECOND board row is authored here, and it is authored in the exact
 * wire shape `TriageBoardRow` emits, to carry the states the fixture board
 * does not: `could-not-tell`, an `assumed` priority, and a landed hand-off.
 */

/** The fixture board row — bug #7, `unvalidated`, `known`, zero attempts,
 *  one scored candidate (#9). The bytes the T2 conformance test round-trips. */
export const BOARD_ROW = (fixtureResult("triage", "board.json") as Array<Record<string, unknown>>)[0]!;
export const BOARD_BUG = BOARD_ROW.bug as { id: number; title: string; componentRoot: string };

/** A second row in the same wire shape: the OTHER honest states. */
export const BOARD_ROW_10 = {
  bug: {
    id: 10,
    componentRoot: "/srv/projects/ledger",
    title: "webhook retries double-post after 429",
    body: "the handler reschedules twice when the provider rate-limits",
    validation: "could-not-tell",
    priorityState: "assumed",
    priorityLabel: "P3",
    filedBy: "golang-engineer",
    validated: false,
    roadmapItemId: 42,
    createdAt: "2026-10-31T09:00:00Z",
    updatedAt: "2026-11-03T15:00:00Z",
  },
  describe: {
    validationSentence: "validation: \"could-not-tell\" — an attempt ran and could not tell (D1)",
    prioritySentence: "priority: \"assumed\" (\"P3\")",
    handoff: "handed off to roadmap item 42 (provenance triaged, D8)",
  },
  attempts: 1,
  lastAttemptAt: "2026-11-02T08:00:00Z",
  roadmapStatus: "ideating",
  candidates: [],
};

export const TRIAGE_VOCABULARY = fixtureResult("triage", "vocabulary.json") as {
  validationStates: string[];
  priorityStates: string[];
  validateStatement: string;
  handoffStatement: string;
};

export const TRIAGE_HISTORY = fixtureResult("triage", "history.json");
export const TRIAGE_VALIDATE_ANSWER = fixtureResult("triage", "validate.json");
export const TRIAGE_HANDOFF_ANSWER = fixtureResult("triage", "handoff.json") as { roadmapItem: number; note: string };

export const TRIAGE_HEALTH = {
  settings: [{ key: "triage.validationBlueprint", name: "Validation blueprint", state: "ok" }],
  evaluatedAt: "2026-11-04T12:00:00Z",
};

/** `settings.get`'s View for the plane's one declared key (S44's shape),
 *  answering the declared default until a test's save stores a row. */
export function triageSettingView(value: string, source: string) {
  return {
    setting: {
      area: "triage",
      default: "triage-repro",
      description: "The blueprint a triage validation attempt runs.",
      envInvalid: "",
      key: "triage.validationBlueprint",
      sensitive: false,
      shadowedBy: "",
      source,
      stored: source === "setting" ? value : "",
      storedInvalid: false,
      type: "string",
      updatedAt: "2026-11-04T12:00:00Z",
      updatedBy: "user:cjlapao",
      value,
    },
  };
}

/** The base answers for the management surface; tests spread overrides
 *  (a refusal, an empty board, a changed:false answer) over this. */
export function triageBaseResponses(): Record<string, unknown> {
  return {
    "triage.board": [BOARD_ROW, BOARD_ROW_10],
    "triage.show": (params: unknown) => {
      const id = (params as { id?: number }).id;
      return id === 10 ? BOARD_ROW_10.bug : BOARD_ROW.bug;
    },
    "triage.history": TRIAGE_HISTORY,
    "triage.vocabulary": TRIAGE_VOCABULARY,
    "triage.health": TRIAGE_HEALTH,
    "triage.update": (params: unknown) => {
      const p = (params ?? {}) as { title?: string; body?: string };
      const bug = BOARD_ROW.bug as Record<string, unknown>;
      return { changed: true, bug: { ...bug, title: p.title ?? bug.title, body: p.body ?? bug.body } };
    },
    "triage.validate": TRIAGE_VALIDATE_ANSWER,
    "triage.handoff": TRIAGE_HANDOFF_ANSWER,
    "triage.confirm-duplicate": (params: unknown) => {
      const p = (params ?? {}) as { of?: number };
      return { ...(BOARD_ROW.bug as Record<string, unknown>), duplicateOf: p.of };
    },
    "settings.get": () => triageSettingView("triage-repro", "default"),
    "settings.set": (params: unknown) => {
      const p = (params ?? {}) as { value?: string };
      return { changed: true, setting: { ...triageSettingView(String(p.value), "setting").setting } };
    },
    "blueprint.list": {
      blueprints: [
        { name: "triage-repro", origin: "seed", revision: 1, version: 1, contentHash: "sha256:ab12cd34" },
        { name: "brainstorm", origin: "seed", revision: 1, version: 1, contentHash: "sha256:9f8e7d6c" },
      ],
    },
  };
}

/** A daemon refusal in the shape the shell actually serialises (the same
 *  helper idiom the roadmap tests carry): message verbatim on the wire. */
export function triageRefusal(code: number, message: string): () => never {
  return () => {
    throw JSON.stringify({ code, message });
  };
}

export type TriageFake = FakeDaemon;
