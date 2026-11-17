// gatesRpc.ts — S57 T11: the one file through which the Gates panel speaks.
//
// Every method literal the panel uses stands here spelled exactly once, so
// the guard test (`gatesRpc.guard.test.ts`) has one surface to scan besides
// the components that call these wrappers, and the DoD card's two doors
// (`dod.list`, `dod.mark`) are listed here even though their calls live in
// the loops feature — the guard asserts both directories and this is the
// record of who may call what.
//
// Params are assembled here from data the caller already holds; the actor
// is never among them (the daemon takes it from the socket peer — a param
// carrying one is REFUSED, identity-key-refused fixtures included).

import { rpcTyped } from "../../../lib/bridge";
import { useRpcQuery, type RpcQueryState } from "../../../lib/useRpcQuery";
import type { LiveController, LiveState } from "../../../lib/live";
import type { RpcMethods } from "../../../proto/contract";

/** The verify list read — daemon-side filters, exactly the params the door takes. */
export function useGateList(
  params: RpcMethods["verify.list"]["params"],
  controller: LiveController,
  live: LiveState,
): RpcQueryState<RpcMethods["verify.list"]["result"]> {
  return useRpcQuery("verify.list", params, controller, live);
}

/** The closed sets, the try statement — the form's entire field vocabulary. */
export function useVerifyVocabulary(controller: LiveController, live: LiveState) {
  return useRpcQuery("verify.vocabulary", {}, controller, live);
}

/** The seed templates, by stack. */
export function useTemplates(controller: LiveController, live: LiveState) {
  return useRpcQuery("verify.templates", {}, controller, live);
}

/** The run ledger for one component, daemon-side filters. */
export function useRuns(
  params: RpcMethods["verify.runs"]["params"],
  controller: LiveController,
  live: LiveState,
) {
  return useRpcQuery("verify.runs", params, controller, live);
}

/** The rule's closed sets + the baseline rule. */
export function useMergeRuleVocabulary(controller: LiveController, live: LiveState) {
  return useRpcQuery("mergerule.vocabulary", {}, controller, live);
}

/** The EFFECTIVE rule read — source, revision, last change, the floor. */
export function useMergeRuleGet(componentId: string, controller: LiveController, live: LiveState) {
  return useRpcQuery("mergerule.get", { componentId, effective: true }, controller, live);
}

/** The component picker's list (the S44-extra tree screen's own door — same read). */
export function useComponentList(controller: LiveController, live: LiveState) {
  return useRpcQuery("component.list", {}, controller, live);
}

// --- the writes: each returns the daemon's answer or throws its refusal ---

export const writeVerifyAdd = (payload: RpcMethods["verify.add"]["params"]) =>
  rpcTyped("verify.add", payload);

export const writeVerifyUpdate = (params: RpcMethods["verify.update"]["params"]) =>
  rpcTyped("verify.update", params);

export const writeVerifyArchive = (id: string) => rpcTyped("verify.archive", { id });

export const writeVerifyRestore = (id: string) => rpcTyped("verify.restore", { id });

export const writeApplyTemplates = (componentId: string, templateIds: string[]) =>
  rpcTyped("verify.applyTemplate", { componentId, templateIds });

export const writeVerifyTry = (componentId: string, id: string) =>
  rpcTyped("verify.try", { componentId, id });

export const writeMergeRuleSet = (componentId: string, rule: Record<string, unknown>) =>
  rpcTyped("mergerule.set", { componentId, rule } as RpcMethods["mergerule.set"]["params"]);

export const writeMergeRuleClear = (componentId: string) => rpcTyped("mergerule.clear", { componentId });

/**
 * Preview: a read that asks the forge — it evaluates the named PR against
 * the effective rule and writes nothing. A call, not a hook, because the
 * person names the PR before anything is asked.
 */
export const askMergeRulePreview = (componentId: string, pr: number) =>
  rpcTyped("mergerule.preview", { componentId, pr });

// --- the doors the DoD card opens (called from loops/DodCard.tsx; named here) ---
//
//   dod.list   — the ledger, keyed by the loop's slug
//   dod.mark   — the human mark, with the manual note as evidence
//
// The card calls them directly (`useRpcQuery("dod.list", …)` and
// `rpcTyped("dod.mark", …)`) because it lives in the loops feature and the
// direction of the import must stay loops → lib, never loops → settings.
