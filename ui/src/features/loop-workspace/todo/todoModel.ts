import type { RpcMethods } from "../../../proto/contract";

// The loop's todo list as the page holds it (S75). Shapes are the daemon's:
// `loop.todos` answers one, `todo.updated` carries the same fields, and every
// status word is the daemon's own.

export type LoopTodos = RpcMethods["loop.todos"]["result"];

/** A `todo.updated` payload as the same shape `loop.todos` answers. */
export function todosFromEvent(p: Record<string, unknown>): LoopTodos {
  const rows = Array.isArray(p.items) ? (p.items as Record<string, unknown>[]) : [];
  const items = rows.flatMap((r) =>
    typeof r.id === "string" && typeof r.content === "string" && typeof r.status === "string"
      ? [{ id: r.id, content: r.content, status: r.status }] : []);
  const counts = (p.counts ?? { pending: 0, inProgress: 0, done: 0 }) as LoopTodos["counts"];
  const closedBy = typeof p.closedBy === "string" && p.closedBy !== "" ? p.closedBy : undefined;
  return {
    active: p.active === true,
    revision: typeof p.revision === "number" ? p.revision : 0,
    items,
    counts,
    ...(closedBy ? { closedBy } : {}),
  } as LoopTodos;
}

/** `3 of 5 done`, counted from the rows the daemon sent. */
export function progressOf(t: LoopTodos): { done: number; total: number } {
  const total = t.items.length;
  return { done: t.items.filter((i) => i.status === "done").length, total };
}

/** The daemon's status words mapped onto the chat panel's four. Only `done`
 *  and `in_progress` are named; anything else rests as pending rather than
 *  vanishing. */
export function taskStatus(status: string): "pending" | "in_progress" | "completed" {
  return status === "done" ? "completed" : status === "in_progress" ? "in_progress" : "pending";
}
