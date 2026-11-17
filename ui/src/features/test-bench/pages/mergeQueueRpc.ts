import { rpc } from "../../../lib/bridge";
import type { RpcMethods } from "../../../proto/contract";

/**
 * mergeQueueRpc — the ONLY file of the S60 bench surface that names a
 * daemon method (S60 T11). Every call the Merge queue page makes rides
 * `rpc` through one function here; the guard
 * (`mergeQueueRpc.guard.test.ts`) pins this folder's census to exactly
 * the ten `mergequeue.*` doors the daemon's own vocabulary answers,
 * plus `component.list` for the root picker — the same borrowed-door
 * note `useComponentRoots.ts` and S53's history guard carry.
 *
 * `mergequeue.proposeEnqueue` is deliberately NOT here: it is the
 * AGENT's door (the proposal kind `mergequeue_enqueue`, inert until a
 * person commits it). A person's click files rows through
 * `mergequeue.enqueue` or files nothing.
 *
 * The types are the fixture-derived contract, widened nowhere: this
 * plane's contract is byte-faithful (its 20 conformance fixtures cover
 * every field the daemon can send on these doors), so `RpcMethods[K]`
 * is the whole truth and no `as` widening lives here — unlike S53's
 * history folder, which documented three narrowing spots. If this file
 * ever needs a cast, that is the contract drifting from the wire, and
 * the fix is a fixture plus `make protogen`, never a cast.
 */

export type QueueRow = RpcMethods["mergequeue.list"]["result"][number];
export type ShowResult = RpcMethods["mergequeue.show"]["result"];
export type StatusResult = RpcMethods["mergequeue.status"]["result"];
export type Vocabulary = RpcMethods["mergequeue.vocabulary"]["result"];
export type QueueConfig = StatusResult["config"];
export type Transition = ShowResult["transitions"][number];
export type MethodEntry = Vocabulary["methods"][number];

/** The rows as `mergequeue.list` answered them. `includeTerminal` is a
 *  required wire field, which is why the page spells the toggle into
 *  every read instead of hoping the daemon defaults it. */
export function listQueue(includeTerminal: boolean): Promise<QueueRow[]> {
  return rpc<QueueRow[]>("mergequeue.list", { includeTerminal });
}

export function showQueueItem(id: string): Promise<ShowResult> {
  return rpc<ShowResult>("mergequeue.show", { id });
}

export function readQueueStatus(componentRoot: string): Promise<StatusResult> {
  return rpc<StatusResult>("mergequeue.status", { componentRoot });
}

export function readVocabulary(): Promise<Vocabulary> {
  return rpc<Vocabulary>("mergequeue.vocabulary", {});
}

/** The picker's roots. A borrowed door — owned by `component.list`, read
 *  the same `!parentId` way `ProjectsScreen` and `useComponentRoots` do;
 *  a merge queue exists only under a root component's repository. */
export async function listComponentRoots(): Promise<Array<{ id: string; name: string; slug: string; root: string }>> {
  const answer = await rpc<{ components?: unknown }>("component.list", {});
  const list = Array.isArray(answer?.components) ? (answer.components as Array<Record<string, unknown>>) : [];
  return list
    .filter((c) => typeof c.parentId !== "string" || c.parentId === "")
    .map((c) => ({ id: String(c.id ?? ""), name: String(c.name ?? ""), slug: String(c.slug ?? ""), root: String(c.root ?? "") }));
}

/** A write, sent exactly as the person aimed it; the daemon's ItemView
 *  answer is the row's NEW truth — the caller folds the response into
 *  its rows, never a hoped-for one. Refusals reject as RpcError and the
 *  page renders their parts verbatim. */
export function enqueueRow(params: RpcMethods["mergequeue.enqueue"]["params"]): Promise<QueueRow> {
  return rpc<QueueRow>("mergequeue.enqueue", params);
}

export function dequeueRow(id: string): Promise<QueueRow> {
  return rpc<QueueRow>("mergequeue.dequeue", { id });
}

export function reorderRow(id: string, beforeId: string): Promise<QueueRow> {
  return rpc<QueueRow>("mergequeue.reorder", { id, beforeId });
}

export function retryRow(id: string): Promise<QueueRow> {
  return rpc<QueueRow>("mergequeue.retry", { id });
}

export function landRow(id: string, digest: string): Promise<QueueRow> {
  return rpc<QueueRow>("mergequeue.land", { id, digest });
}

type SettingsResult = RpcMethods["mergequeue.settings"]["result"];

export function writeSettings(
  params: RpcMethods["mergequeue.settings"]["params"],
): Promise<SettingsResult> {
  return rpc<SettingsResult>("mergequeue.settings", params);
}

/**
 * The acknowledgement digest a settings refusal names, out of the
 * refusal's OWN words — the same shape the CLI door parses
 * (`cmd/aeon/mergequeue_verbs.go`'s `mqAckDigestRx`), deliberately
 * duplicated at the second client rather than smuggled through a shared
 * constant, because these two processes share only the wire.
 * A refusal that does not name a digest is not an ack-gate: the page
 * shows it and sends nothing.
 */
export function refusalAckDigest(parts: { what?: string; means?: string; todo?: string } | undefined, message: string): string | null {
  const text = `${parts?.what ?? ""}\n${parts?.means ?? ""}\n${parts?.todo ?? ""}\n${message}`;
  const match = /acknowledged "([0-9a-f]{64})"/.exec(text);
  return match?.[1] ?? null;
}
