// environmentsRpc.ts — S59 T11: the one file through which the Environments
// panel speaks.
//
// Every method literal the panel uses stands here spelled exactly once, so the
// guard (`test/environmentsRpc.guard.test.ts`) has one surface to scan besides
// the components that call these wrappers, and the contract itself settles
// whether a name is registered at all.
//
// Params are assembled here from data the caller already holds. The actor is
// never among them: `internal/protocol/environment.go` refuses a param
// carrying an identity key (actor · actorKind · origin · legacy) before it
// looks at anything else, and the conformance fixtures `create-refuses-a-
// stolen-actor.json` and `create-refuses-a-value-field.json` are that refusal
// on the wire. A secret value has no key to arrive under either — a reference
// is a source kind plus a location plus the name the child will see.
//
// Two param types are widened by a documented assertion, and only there: the
// generated contract is fixture-derived, and no S59 fixture exercised the
// list's backend/origin/archived filters or the effective read's componentId,
// so the generated type omits fields the wire accepts (`omitempty` on each in
// `EnvironmentListParams` / `EnvironmentEffectiveParams`). The same idiom
// `gatesRpc.ts` uses for `mergerule.set`.

import { rpcTyped } from "../../../lib/bridge";
import { useRpcQuery, type RpcQueryState } from "../../../lib/useRpcQuery";
import type { LiveController, LiveState } from "../../../lib/live";
import type { RpcMethods } from "../../../proto/contract";

type ListParams = RpcMethods["environment.list"]["params"];
type EffectiveParams = RpcMethods["environment.effective"]["params"];

/** The Environments tab's read — the daemon's own filters, not a client-side sweep. */
export function useEnvironmentList(
  filters: { limit: number; backend: string; origin: string; includeArchived: boolean },
  controller: LiveController,
  live: LiveState,
): RpcQueryState<RpcMethods["environment.list"]["result"]> {
  const params = { ...filters } as unknown as ListParams;
  return useRpcQuery("environment.list", params, controller, live);
}

/** One environment in full — its canonical spec, bindings and references. */
export function useEnvironmentShow(
  slug: string,
  controller: LiveController,
  live: LiveState,
): RpcQueryState<RpcMethods["environment.show"]["result"]> {
  return useRpcQuery("environment.show", { slug }, controller, live);
}

/** The plane's audit: who changed what, and which revision the write produced. */
export function useEnvironmentChanges(
  params: { subjectKind: string; subjectId: string; action: string; limit: number },
  controller: LiveController,
  live: LiveState,
): RpcQueryState<RpcMethods["environment.changes"]["result"]> {
  return useRpcQuery("environment.changes", params, controller, live);
}

/** The provision ledger — state, loop, backend, handle, ages, the driver's sentence. */
export function useEnvironmentProvisions(
  params: { state: string; limit: number },
  controller: LiveController,
  live: LiveState,
): RpcQueryState<RpcMethods["environment.provisions"]["result"]> {
  return useRpcQuery("environment.provisions", params, controller, live);
}

/** Every closed set the panel renders: backends, allowlist, deny shapes, modes, honesty. */
export function useEnvironmentVocabulary(controller: LiveController, live: LiveState) {
  return useRpcQuery("environment.vocabulary", {}, controller, live);
}

/** What a step on that component would actually run in — names and sources only. */
export function useEnvironmentEffective(
  // No `attended` (S59 T12, G14): attendedness decides EV6's inheritance note
  // and is declared only at the three start doors, so a preview door neither
  // takes it nor lets the person choose the answer that shows more privilege
  // than the spawn will give. The daemon answers it in the view.
  params: { componentId: string },
  controller: LiveController,
  live: LiveState,
): RpcQueryState<RpcMethods["environment.effective"]["result"]> {
  const wire = { ...params } as unknown as EffectiveParams;
  return useRpcQuery("environment.effective", wire, controller, live);
}

/** The secret references — pointers (a kind, a location, an inject-as name). */
export function useSecretRefList(
  params: { sourceKind: string; limit: number },
  controller: LiveController,
  live: LiveState,
): RpcQueryState<RpcMethods["secretref.list"]["result"]> {
  return useRpcQuery("secretref.list", params, controller, live);
}

/** One reference as the store holds it, read when a row is opened. */
export function useSecretRefShow(
  name: string,
  controller: LiveController,
  live: LiveState,
): RpcQueryState<RpcMethods["secretref.show"]["result"]> {
  return useRpcQuery("secretref.show", { name }, controller, live);
}

/** The component picker's roster, and one component for the Effective tab's root. */
export function useComponentList(controller: LiveController, live: LiveState) {
  return useRpcQuery("component.list", {}, controller, live);
}

export function useComponentShow(componentId: string, controller: LiveController, live: LiveState) {
  return useRpcQuery("component.show", { component_id: componentId }, controller, live);
}

// --- the writes: each returns the daemon's answer or throws its refusal ------

export const writeEnvironmentCreate = (params: RpcMethods["environment.create"]["params"]) =>
  rpcTyped("environment.create", params);

export const writeEnvironmentUpdate = (params: RpcMethods["environment.update"]["params"]) =>
  rpcTyped("environment.update", params);

export const writeEnvironmentArchive = (id: string) => rpcTyped("environment.archive", { id });

export const writeEnvironmentRestore = (id: string) => rpcTyped("environment.restore", { id });

/**
 * The bind door, both halves. `dryRun: true` answers in the same words the
 * applied write will use (one formatter, `execenv.DescribeBind`) and applies
 * nothing; the panel shows that statement and only calls again — `dryRun:
 * false` — after the person confirms. Declining sends nothing.
 */
export const runEnvironmentBind = (params: RpcMethods["environment.bind"]["params"]) =>
  rpcTyped("environment.bind", params);

export const writeEnvironmentUnbind = (componentId: string) =>
  rpcTyped("environment.unbind", { componentId });

/** The human door off a stuck provision: the ledger is walked, never guessed. */
export const writeEnvironmentTeardown = (provisionId: string) =>
  rpcTyped("environment.teardown", { provisionId });

/** Whether a reference WOULD resolve — two booleans plus the refusal, never a value. */
export const checkSecretRef = (name: string) => rpcTyped("secretref.check", { name });

export const writeSecretRefCreate = (params: RpcMethods["secretref.create"]["params"]) =>
  rpcTyped("secretref.create", params);

// No `secretref.update` wrapper exists: this section offers create · check ·
// archive · restore, which is what the task's Secret-references tab specifies, and
// a door spelled here that no component calls would make the guard's "every
// declared door is called" assertion a formality. Add the wrapper WITH the edit
// form, never before it.

export const writeSecretRefArchive = (name: string) => rpcTyped("secretref.archive", { name });

export const writeSecretRefRestore = (name: string) => rpcTyped("secretref.restore", { name });
