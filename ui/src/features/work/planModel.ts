// planModel — the Plan tab's two pictures, as pure rules: tasks grouped into board columns, and
// roadmap items turned into timeline bars. Every status word is the daemon's; the six columns only
// GROUP them (a card always shows its true status), and nothing is drawn that the daemon did not say.

import type { TrueColor } from "@cjlapao/ui-kit";
import type { TaskRow } from "../goals/goalsRpc";
import { TASK_STATUSES } from "../goals/goalsRpc";
import type { ItemView } from "../roadmap/roadmapModel";

export const TASK_COLUMNS = ["notStarted", "working", "checking", "inReview", "landed", "stopped"] as const;
export type TaskColumnId = (typeof TASK_COLUMNS)[number];

/** Which column each of the daemon's task statuses sits in. Every status is placed; the test pins it. */
export const COLUMN_OF_STATUS: Readonly<Record<(typeof TASK_STATUSES)[number], TaskColumnId>> = {
  pending: "notStarted",
  "issue-open": "notStarted",
  "in-progress": "working",
  verified: "checking",
  secure: "checking",
  "pr-open": "inReview",
  "in-review": "inReview",
  "changes-requested": "inReview",
  merged: "landed",
  complete: "landed",
  failed: "stopped",
  blocked: "stopped",
  skipped: "stopped",
  superseded: "stopped",
};

export const COLUMN_COLOR: Readonly<Record<TaskColumnId, TrueColor>> = {
  notStarted: "slate", working: "blue", checking: "cyan", inReview: "amber", landed: "green", stopped: "rose",
};

const STATUS_COLOR: Record<string, TrueColor> = { "changes-requested": "orange", failed: "rose", blocked: "rose" };

/** A status the daemon grows later still lands somewhere visible: unknown words go to "notStarted"'s neighbour, never vanish. */
export function columnOf(status: string): TaskColumnId {
  return (COLUMN_OF_STATUS as Record<string, TaskColumnId | undefined>)[status] ?? "notStarted";
}

export interface BoardCardData { task: TaskRow }

export function toCard(task: TaskRow): { id: string; title: string; column: string; tags: Array<{ label: string; color?: TrueColor }>; tone?: TrueColor; locked: true; data: Record<string, unknown>; assignees?: Array<{ id: string; name: string }> } {
  return {
    id: task.id,
    title: task.title || task.taskid || task.id,
    column: columnOf(task.status),
    tags: [{ label: task.status, color: STATUS_COLOR[task.status] ?? COLUMN_COLOR[columnOf(task.status)] }, ...(task.manual ? [{ label: "manual" }] : [])],
    locked: true, // the board is a picture of what agents did; nobody drags a task into "merged"
    data: { task },
    ...(task.assignee ? { assignees: [{ id: task.assignee, name: task.assignee }] } : {}),
  };
}

/** Position on the ladder, 1-based, or 0 for a word the ladder does not hold. */
export function ladderPosition(status: string): number {
  return (TASK_STATUSES as readonly string[]).indexOf(status) + 1;
}

// ── Roadmap bars ───────────────────────────────────────────────────────────────────────────────

export const ITEM_COLOR: Readonly<Record<string, TrueColor>> = {
  idea: "slate", ideating: "violet", "brief-agreed": "indigo", planned: "blue", ready: "cyan", running: "amber", done: "green", cancelled: "rose",
};
const DAY = 86_400_000;

export interface Bar {
  id: string;
  name: string;
  start: string;
  end: string;
  lane: string;
  color: TrueColor;
  progress: number;
  badge?: string;
  real: boolean;
  item: ItemView;
}

/**
 * One bar per roadmap item, on the daemon's real times: started → finished. An item that never ran
 * has no start, so its bar sits on its creation day (a planned slot, not work); a running item runs
 * to now. `real` says whether the bar is the daemon's times or the stand-in, so the page can mark it.
 */
export function span(item: ItemView, now: number): { start: number; end: number; real: boolean } {
  const created = Date.parse(item.createdAt) || now;
  const started = item.startedAt ? Date.parse(item.startedAt) : NaN;
  if (Number.isNaN(started)) return { start: created, end: created + DAY, real: false };
  const finished = item.finishedAt ? Date.parse(item.finishedAt) : NaN;
  return { start: started, end: Math.max(Number.isNaN(finished) ? now : finished, started + DAY), real: true };
}

/** Waits-for edges between the items shown: the upstream finishes before this one starts. */
export function toLinks(items: readonly ItemView[]): Array<{ id: string; source: string; target: string; type: "fs" }> {
  const shown = new Set(items.map((i) => i.id));
  return items.flatMap((item) => (item.dependencies ?? []).filter((d) => shown.has(d.id)).map((d) => ({ id: `${d.id}>${item.id}`, source: String(d.id), target: String(item.id), type: "fs" as const })));
}

export function toBars(items: readonly ItemView[], now: number, noEpic: string, notEstimated: string, axis: "epic" | "priority" = "epic"): Bar[] {
  return items.map((item) => {
    const { start, end, real } = span(item, now);
    const estimated = item.estimateMin !== null && item.estimateMax !== null;
    return {
      id: String(item.id),
      name: item.title,
      start: new Date(start).toISOString(),
      end: new Date(end).toISOString(),
      lane: axis === "priority" ? `priority ${item.priorityState}` : item.epic || noEpic,
      color: ITEM_COLOR[item.status] ?? "slate",
      progress: item.status === "done" ? 1 : 0,
      ...(estimated ? {} : { badge: notEstimated }),
      real,
      item,
    };
  });
}

export function lanesOf(bars: readonly Bar[]): Array<{ id: string; label: string }> {
  return [...new Set(bars.map((b) => b.lane))].sort((a, b) => a.localeCompare(b)).map((id) => ({ id, label: id }));
}
