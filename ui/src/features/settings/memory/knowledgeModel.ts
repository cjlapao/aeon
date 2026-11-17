/**
 * knowledgeModel — the pure half of the knowledge map (the kit's `KnowledgeExplorer` fed from the
 * learnings plane).
 *
 * # What is real and what is derived
 *
 * A learning carries a body, a scope, a provenance, tags, a strength and (for retro learnings) the
 * loop it came from. It does NOT carry a kind, a title, a related-memory list or map coordinates, and
 * the daemon exposes no embedding or similarity read. So:
 *
 *  • `title`, `topic` and the explorer `status` are DISPLAY derivations from fields the row really
 *    has — nothing is invented that the row does not support.
 *  • The links on the map are the learnings that SHARE a tag or a source loop, labelled with that tag
 *    or loop. They say "these were filed alike", never "these mean alike".
 *  • The positions are a deterministic force layout over those links (stable for the same rows), not
 *    a projection of embeddings. The page says so in words beside the map.
 *
 * Everything the daemon says about a row (its `describe` sentence, provenance word, retraction stamp)
 * still renders verbatim in the detail; this file only decides how to hand rows to the explorer.
 */

import type { KnowledgePoint, KnowledgeRelationship, MemoryRecord, MemoryStatus } from "@cjlapao/ui-kit";
import type { MemoryRow } from "./memoryModel";

const TITLE_MAX = 72;
const MAX_EDGES = 800;
/** Above this the pairwise force layout is skipped and the kit's own topic layout takes over. */
export const LAYOUT_LIMIT = 700;

/** The first sentence or line of the body, cut at a word, as the memory's title. */
export function titleOf(body: string): string {
  const flat = body.replace(/\s+/g, " ").trim();
  if (flat === "") return "—";
  const stop = flat.search(/[.:;!?]\s/);
  const first = stop > 12 ? flat.slice(0, stop) : flat;
  if (first.length <= TITLE_MAX) return first;
  const cut = first.slice(0, TITLE_MAX - 1);
  const space = cut.lastIndexOf(" ");
  return `${(space > 40 ? cut.slice(0, space) : cut).trimEnd()}…`;
}

/** The explorer's status from what the row really says: a retraction wins; an assumed provenance is
 *  the unverified kind, which the explorer calls "needs review". */
export function statusOf(row: MemoryRow): MemoryStatus {
  if (row.retracted === true) return "archived";
  if (row.provenance === "assumed") return "needs-review";
  return "active";
}

/**
 * One topic per learning: the tag that groups the most learnings without being on every one of them
 * (a tag on all rows separates nothing) and that at least two share. A row with no such tag falls back
 * to its scope key's last path segment, so every point has a cluster and the map never has a lone grey blob.
 */
export function topicsOf(rows: readonly MemoryRow[]): Map<number, string> {
  const freq = new Map<string, number>();
  for (const row of rows) for (const tag of new Set(row.tags ?? [])) freq.set(tag, (freq.get(tag) ?? 0) + 1);
  const out = new Map<number, string>();
  for (const row of rows) {
    let best = "";
    let bestCount = 1;
    for (const tag of [...new Set(row.tags ?? [])].sort()) {
      const count = freq.get(tag) ?? 0;
      if (count >= 2 && count < rows.length && count > bestCount) {
        best = tag;
        bestCount = count;
      }
    }
    if (best === "") best = row.scopeKey.split("/").filter(Boolean).pop() ?? row.scope;
    out.set(row.id, best);
  }
  return out;
}

/** Links between learnings that share a tag (not a universal one) or a source loop. Each group is
 *  chained in id order rather than made a clique, so a popular tag costs N edges, not N². */
export function relationshipsOf(rows: readonly MemoryRow[]): KnowledgeRelationship[] {
  const groups = new Map<string, number[]>();
  const add = (key: string, id: number) => {
    const held = groups.get(key) ?? [];
    held.push(id);
    groups.set(key, held);
  };
  for (const row of rows) {
    for (const tag of new Set(row.tags ?? [])) add(`tag\u0000${tag}`, row.id);
    if (row.sourceLoopSlug !== undefined && row.sourceLoopSlug !== "") add(`loop\u0000${row.sourceLoopSlug}`, row.id);
  }
  const seen = new Set<string>();
  const edges: KnowledgeRelationship[] = [];
  for (const [key, ids] of [...groups.entries()].sort(([a], [b]) => a.localeCompare(b))) {
    if (ids.length < 2 || ids.length === rows.length) continue; // a label on every row relates nothing
    const label = key.split("\u0000")[1] ?? key;
    const sorted = [...ids].sort((a, b) => a - b);
    for (let index = 1; index < sorted.length; index += 1) {
      const source = String(sorted[index - 1]);
      const target = String(sorted[index]);
      const pair = `${source}-${target}`;
      if (seen.has(pair)) continue;
      seen.add(pair);
      edges.push({ id: `${pair}:${label}`, source, target, label });
      if (edges.length >= MAX_EDGES) return edges;
    }
  }
  return edges;
}

/** The explorer's records. `related` is the symmetric closure of the derived links. */
export function recordsOf(rows: readonly MemoryRow[], relationships: readonly KnowledgeRelationship[]): MemoryRecord[] {
  const topics = topicsOf(rows);
  const related = new Map<string, Set<string>>();
  for (const edge of relationships) {
    (related.get(edge.source) ?? related.set(edge.source, new Set()).get(edge.source))?.add(edge.target);
    (related.get(edge.target) ?? related.set(edge.target, new Set()).get(edge.target))?.add(edge.source);
  }
  return rows.map((row) => {
    const id = String(row.id);
    return {
      id,
      title: titleOf(row.body),
      summary: row.describe,
      content: row.body,
      kind: "lesson",
      topic: topics.get(row.id),
      scope: `${row.scope}/${row.scopeKey}`,
      status: statusOf(row),
      sources:
        row.sourceLoopSlug !== undefined && row.sourceLoopSlug !== ""
          ? [{ id: row.sourceLoopSlug, label: row.sourceLoopSlug, href: `/loops/${encodeURIComponent(row.sourceLoopSlug)}` }]
          : [],
      relatedIds: [...(related.get(id) ?? [])].sort((a, b) => Number(a) - Number(b)),
      metadata: { provenance: row.provenance, observedCount: row.observedCount, createdAt: row.createdAt },
    } satisfies MemoryRecord;
  });
}

function hash(text: string): number {
  let h = 2166136261;
  for (let index = 0; index < text.length; index += 1) {
    h ^= text.charCodeAt(index);
    h = Math.imul(h, 16777619);
  }
  return h >>> 0;
}

/**
 * A deterministic force layout over the derived links: nodes repel, linked nodes attract, and each
 * topic pulls its members toward a centre of its own, so a topic reads as a neighbourhood. The start
 * positions come from a hash of the id — no random numbers — so the same rows always draw the same map.
 */
export function layoutOf(records: readonly MemoryRecord[], relationships: readonly KnowledgeRelationship[]): KnowledgePoint[] | undefined {
  const n = records.length;
  if (n === 0 || n > LAYOUT_LIMIT) return undefined;
  const topics = [...new Set(records.map((record) => record.topic ?? ""))].sort();
  const centre = new Map<string, { x: number; y: number }>();
  topics.forEach((topic, index) => {
    const angle = (index / Math.max(1, topics.length)) * Math.PI * 2;
    const radius = topics.length === 1 ? 0 : 180 + (hash(topic) % 60);
    centre.set(topic, { x: Math.cos(angle) * radius, y: Math.sin(angle) * radius });
  });
  const x = new Float64Array(n);
  const y = new Float64Array(n);
  const index = new Map<string, number>();
  records.forEach((record, at) => {
    index.set(record.id, at);
    const c = centre.get(record.topic ?? "") ?? { x: 0, y: 0 };
    const angle = (hash(record.id) / 4294967296) * Math.PI * 2;
    const radius = 20 + (hash(`${record.id}:r`) % 60);
    x[at] = c.x + Math.cos(angle) * radius;
    y[at] = c.y + Math.sin(angle) * radius;
  });
  const links = relationships.flatMap((edge) => {
    const a = index.get(edge.source);
    const b = index.get(edge.target);
    return a === undefined || b === undefined ? [] : [[a, b] as const];
  });
  const dx = new Float64Array(n);
  const dy = new Float64Array(n);
  for (let step = 0; step < 140; step += 1) {
    const cool = 1 - step / 140;
    dx.fill(0);
    dy.fill(0);
    for (let i = 0; i < n; i += 1) {
      for (let j = i + 1; j < n; j += 1) {
        let vx = (x[i] ?? 0) - (x[j] ?? 0);
        let vy = (y[i] ?? 0) - (y[j] ?? 0);
        let d2 = vx * vx + vy * vy;
        if (d2 < 0.01) {
          vx = (i % 7) - 3 + 0.1;
          vy = (j % 5) - 2 + 0.1;
          d2 = vx * vx + vy * vy;
        }
        if (d2 > 90000) continue;
        const force = 1800 / d2;
        dx[i] = (dx[i] ?? 0) + vx * force;
        dy[i] = (dy[i] ?? 0) + vy * force;
        dx[j] = (dx[j] ?? 0) - vx * force;
        dy[j] = (dy[j] ?? 0) - vy * force;
      }
    }
    for (const [a, b] of links) {
      const vx = (x[b] ?? 0) - (x[a] ?? 0);
      const vy = (y[b] ?? 0) - (y[a] ?? 0);
      dx[a] = (dx[a] ?? 0) + vx * 0.02;
      dy[a] = (dy[a] ?? 0) + vy * 0.02;
      dx[b] = (dx[b] ?? 0) - vx * 0.02;
      dy[b] = (dy[b] ?? 0) - vy * 0.02;
    }
    records.forEach((record, at) => {
      const c = centre.get(record.topic ?? "") ?? { x: 0, y: 0 };
      dx[at] = (dx[at] ?? 0) + (c.x - (x[at] ?? 0)) * 0.03;
      dy[at] = (dy[at] ?? 0) + (c.y - (y[at] ?? 0)) * 0.03;
      const move = Math.min(12, Math.hypot(dx[at] ?? 0, dy[at] ?? 0)) * cool;
      const len = Math.hypot(dx[at] ?? 0, dy[at] ?? 0) || 1;
      x[at] = (x[at] ?? 0) + ((dx[at] ?? 0) / len) * move;
      y[at] = (y[at] ?? 0) + ((dy[at] ?? 0) / len) * move;
    });
  }
  return records.map((record, at) => ({ memoryId: record.id, x: x[at] ?? 0, y: y[at] ?? 0, clusterId: record.topic ?? "" }));
}

/** The wire answer of `learnings.map`, as far as this file reads it. */
export interface DaemonMap {
  projected: boolean;
  total: number;
  embedded: number;
  omitted: number;
  canEmbed: boolean;
  basis: string;
  points: ReadonlyArray<{ id: number; x: number; y: number }>;
  links: ReadonlyArray<{ a: number; b: number; similarity: number }>;
}

/** Read the daemon's map answer defensively: a bare `{}` (an old daemon, a refusal) is an honest "no map". */
export function daemonMapOf(answer: unknown): DaemonMap | undefined {
  if (typeof answer !== "object" || answer === null) return undefined;
  const a = answer as Partial<DaemonMap>;
  if (typeof a.projected !== "boolean") return undefined;
  return {
    projected: a.projected,
    total: typeof a.total === "number" ? a.total : 0,
    embedded: typeof a.embedded === "number" ? a.embedded : 0,
    omitted: typeof a.omitted === "number" ? a.omitted : 0,
    canEmbed: a.canEmbed === true,
    basis: typeof a.basis === "string" ? a.basis : "",
    points: Array.isArray(a.points) ? a.points : [],
    links: Array.isArray(a.links) ? a.links : [],
  };
}

/**
 * When the daemon placed the learnings by meaning, those positions and links replace the derived ones:
 * the points are its projection of the stored vectors, and each link is a nearest-neighbour pair labelled
 * with its cosine similarity. Memories the daemon did not place (no vector yet, or left off to stay
 * bounded) keep their place in the library and simply have no dot — they are never given a made-up one.
 */
export function semanticGraphOf(
  map: DaemonMap,
  records: readonly MemoryRecord[],
): { points: KnowledgePoint[]; relationships: KnowledgeRelationship[] } {
  const topics = new Map(records.map((record) => [record.id, record.topic ?? ""]));
  const points: KnowledgePoint[] = map.points
    .filter((point) => topics.has(String(point.id)))
    .map((point) => ({ memoryId: String(point.id), x: point.x, y: point.y, clusterId: topics.get(String(point.id)) ?? "" }));
  const placed = new Set(points.map((point) => point.memoryId));
  const relationships: KnowledgeRelationship[] = map.links
    .filter((link) => placed.has(String(link.a)) && placed.has(String(link.b)))
    .map((link) => ({ id: `${link.a}-${link.b}`, source: String(link.a), target: String(link.b), label: link.similarity.toFixed(2) }));
  return { points, relationships };
}
