/**
 * variablesRpc — the ONE file through which the Variables & secrets page speaks.
 *
 * `vault.list` and `vault.changes` are the reads; `vault.set` and `vault.delete`
 * are the person's writes (human door: the daemon takes the actor from the
 * connection's peer, never from params). A secret's value is never in an answer —
 * the wire type has no field for it — so this page can only SEND a value, never
 * show one back. The generated contract is fixture-derived (the set fixture
 * carried a value and a description only), so the params are widened here by one
 * documented assertion: every other field is `omitempty` on the wire
 * (`protocol.VaultSetParams`).
 */

import { rpcTyped } from "../../../lib/bridge";
import { useRpcQuery, type RpcQueryState } from "../../../lib/useRpcQuery";
import type { LiveController, LiveState } from "../../../lib/live";
import type { RpcMethods } from "../../../proto/contract";

export type VaultVariable = RpcMethods["vault.list"]["result"]["variables"][number];
export type VaultChange = RpcMethods["vault.changes"]["result"]["changes"][number];

/** What the form sends. Absent fields keep what is stored (or take the daemon's default on a new variable). */
export interface VariableWrite {
  name: string;
  project?: string;
  description?: string;
  value?: string;
  secret?: boolean;
  agentVisible?: boolean;
}

type SetParams = RpcMethods["vault.set"]["params"];
type ChangesParams = RpcMethods["vault.changes"]["params"];

export function useVariables(controller: LiveController, live: LiveState): RpcQueryState<VaultVariable[]> {
  const query = useRpcQuery("vault.list", {} as Record<string, never>, controller, live);
  return { data: query.data?.variables, loading: query.loading, error: query.error, refetch: query.refetch };
}

export function useVariableChanges(name: string, controller: LiveController, live: LiveState): RpcQueryState<VaultChange[]> {
  const params = (name ? { name, limit: 20 } : { limit: 20 }) as unknown as ChangesParams;
  const query = useRpcQuery("vault.changes", params, controller, live);
  return { data: query.data?.changes, loading: query.loading, error: query.error, refetch: query.refetch };
}

export function setVariable(write: VariableWrite) {
  return rpcTyped("vault.set", write as unknown as SetParams);
}

export function deleteVariable(name: string, project: string | undefined) {
  return rpcTyped("vault.delete", (project ? { name, project } : { name }) as unknown as RpcMethods["vault.delete"]["params"]);
}
