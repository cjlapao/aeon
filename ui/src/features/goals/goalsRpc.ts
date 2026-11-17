/**
 * goalsRpc — the ONE file through which the goals and tasks surface speaks.
 *
 * A goal is a unit of something that needs doing; a loop is something that
 * runs to try to complete it. Goals are rows with a review state and a
 * version, they own the documents their work produced (proposal, brief, plan,
 * legal …), and their tasks hang off them. Every read here is the daemon's
 * answer as it came; the decision is a person's door (`goal.decide`), and the
 * actor is never among the params — the daemon takes the person from the
 * connection's peer.
 *
 * Params are widened by an assertion only where the generated contract is
 * fixture-derived: the fixtures always carried every filter, and every filter
 * is `omitempty` on the wire (Go: `GoalListParams` via the records catalog's
 * goal_list schema, `protocol.TaskListParams`).
 */

import { rpcTyped } from "../../lib/bridge";
import { useRpcQuery, type RpcQueryState } from "../../lib/useRpcQuery";
import type { LiveController, LiveState } from "../../lib/live";
import type { RpcMethods } from "../../proto/contract";

export type GoalRow = RpcMethods["goal.list"]["result"][number];
export type GoalPicture = RpcMethods["goal.show"]["result"];
export type GoalDocument = GoalPicture["documents"][number];
/** `goalId` is `omitempty` on the wire, so the fixture-derived row never carried it. */
export type TaskRow = RpcMethods["task.list"]["result"]["tasks"][number] & { goalId?: string };

export const REVIEW_STATES = ["in_review", "draft", "approved", "rejected", "parked"] as const;
export type ReviewState = (typeof REVIEW_STATES)[number];

/** The task ladder, in the order the engine walks it (store/machine.go). */
export const TASK_STATUSES = [
  "pending", "issue-open", "in-progress", "verified", "secure", "pr-open", "in-review",
  "changes-requested", "merged", "complete", "failed", "blocked", "skipped", "superseded",
] as const;

type GoalListParams = RpcMethods["goal.list"]["params"];
type TaskListParams = RpcMethods["task.list"]["params"];

export function useGoals(state: string, controller: LiveController, live: LiveState, rootId = ""): RpcQueryState<GoalRow[]> {
  const params: Record<string, string> = {};
  if (state) params.state = state;
  if (rootId) params.root_id = rootId;
  const query = useRpcQuery("goal.list", params as unknown as GoalListParams, controller, live);
  return { data: query.data, loading: query.loading, error: query.error, refetch: query.refetch };
}

export function useGoalPicture(id: string, controller: LiveController, live: LiveState): RpcQueryState<GoalPicture> {
  return useRpcQuery("goal.show", { id }, controller, live);
}

/** Tasks, optionally only one goal's. The daemon filters; the page sorts nothing. */
export function useTasks(goalId: string | undefined, status: string, controller: LiveController, live: LiveState): RpcQueryState<TaskRow[]> {
  const params: Record<string, string> = {};
  if (goalId) params.goal = goalId;
  if (status) params.status = status;
  const query = useRpcQuery("task.list", params as unknown as TaskListParams, controller, live);
  return { data: query.data?.tasks as TaskRow[] | undefined, loading: query.loading, error: query.error, refetch: query.refetch };
}

/** One document's text, by the loop that wrote it. */
export function readGoalDocument(doc: Pick<GoalDocument, "loopSlug" | "componentRoot" | "name">) {
  return rpcTyped("loop.read", { slug: doc.loopSlug, componentRoot: doc.componentRoot, name: doc.name });
}

export function decideGoal(id: string, version: number, decision: "approved" | "rejected" | "parked", reason: string) {
  return rpcTyped("goal.decide", { id, version, decision, reason });
}
