import { readFileSync } from "node:fs";
import { join } from "node:path";

/**
 * S57 T11 test fixtures — the REAL conformance answers, read from the same
 * JSON files the Go suite round-trips. A UI test that hand-typed its own
 * daemon answer drifts from the wire; these tests answer with the fixtures,
 * so every assertion is made against what the daemon actually says.
 *
 * Lives in ui/test/ (not in the gates folder) on purpose: the production
 * sources are the guarded ones; fixture data is allowed to name protocol
 * words because naming them IS its job.
 */

const CONFORMANCE = join(__dirname, "../../internal/protocol/conformance");

export function fixtureResult(tree: string, file: string): unknown {
  const doc = JSON.parse(readFileSync(join(CONFORMANCE, tree, file), "utf8")) as {
    response: { result: unknown };
  };
  return doc.response.result;
}

export const verifyVocabulary = fixtureResult("verify", "vocabulary.json");
export const verifyList = fixtureResult("verify", "list-effective.json");

/** The effective-list rows, typed by the UI model (a cast over fixture JSON). */
export const listGates = (verifyList as { gates: unknown[] }).gates as unknown as import("../src/features/settings/gates/gatesModel").GateView[];
export const verifyTemplates = fixtureResult("verify", "templates.json");
export const verifyRuns = fixtureResult("verify", "runs-summary.json");
export const verifyRunShow = fixtureResult("verify", "run-show.json");
export const verifyTry = fixtureResult("verify", "try-truncated.json");
export const mergeruleVocabulary = fixtureResult("mergerule", "vocabulary.json");
export const mergeruleGet = fixtureResult("mergerule", "get.json");
export const mergerulePreview = fixtureResult("mergerule", "preview.json");
export const mergerulePreviewNoPr = fixtureResult("mergerule", "preview-no-pr.json");
export const mergeruleSet = fixtureResult("mergerule", "set.json");
export const mergeruleClear = fixtureResult("mergerule", "clear.json");
export const dodList = fixtureResult("dod", "list.json");
export const dodMark = fixtureResult("dod", "mark.json");

/** The component roster the picker reads; two rows so ordering is visible. */
export const componentList = {
  components: [
    { id: "c-aeon", name: "aeon", root: "/home/cjlapao/src/aeon", origin: "registered", createdAt: "2026-10-01T00:00:00Z" },
    { id: "c-web", name: "web", root: "/home/cjlapao/src/web", origin: "registered", createdAt: "2026-10-02T00:00:00Z" },
  ],
};

/** Every door a mounted GatesSection opens, answered with the fixtures. */
export const gatesBaseResponses: Record<string, unknown> = {
  "component.list": componentList,
  "verify.vocabulary": verifyVocabulary,
  "verify.list": verifyList,
  "verify.templates": verifyTemplates,
  "verify.runs": verifyRuns,
  "verify.runShow": verifyRunShow,
  "verify.try": verifyTry,
  "mergerule.vocabulary": mergeruleVocabulary,
  "mergerule.get": mergeruleGet,
  "mergerule.preview": mergerulePreview,
  "mergerule.set": mergeruleSet,
  "mergerule.clear": mergeruleClear,
  "dod.list": dodList,
  "dod.mark": dodMark,
};

/** The whole fixture document, when a test wants the refusal parts. */
export function fixtureDoc(tree: string, file: string): { response: { result?: unknown; error?: { code: number; message: string; data?: Record<string, unknown> } } } {
  return JSON.parse(readFileSync(join(CONFORMANCE, tree, file), "utf8"));
}

export function refusedError(tree: string, file: string): unknown {
  const doc = fixtureDoc(tree, file);
  // The fake daemon re-throws an Error answer as its bare MESSAGE, and a
  // message is too small a bag for a refusal: the parts live in `data`. So
  // the throw is the raw JSON string — the same text the real socket hands
  // the bridge, where toRpcError parses it back into code+message+data.
  return JSON.stringify(doc.response.error);
}
