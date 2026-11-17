import { useEffect, useState } from "react";
import { rpc } from "../lib/bridge";

interface Detached { kind: string; id: string; loopSlug: string; detachedAt: string }
interface Tombstone { slug: string; componentRoot: string; deletedAt: string; title: string }
interface Closed { kind: string; id: string; reason: string; loopSlug: string; closedAt: string; priorStatus: string }
interface Records { detached: Detached[]; tombstones: Tombstone[]; closed: Closed[] }

// One read shared by every note on screen, refreshed at most every few seconds: a page full of tasks asks the daemon
// once, not once per row.
let cache: { at: number; load: Promise<Records> } | undefined;
const FRESH_MS = 5000;

function records(): Promise<Records> {
  if (!cache || Date.now() - cache.at > FRESH_MS) {
    cache = {
      at: Date.now(),
      load: rpc<{ detached?: Detached[]; tombstones?: Tombstone[]; closed?: Closed[] }>("loop.tombstones", {})
        .then((r) => ({ detached: Array.isArray(r?.detached) ? r.detached : [], tombstones: Array.isArray(r?.tombstones) ? r.tombstones : [], closed: Array.isArray(r?.closed) ? r.closed : [] }),
          () => ({ detached: [], tombstones: [], closed: [] })),
    };
  }
  return cache.load;
}

/** Forget the shared read, so the next note asks the daemon again (after a delete or archive, and in tests). */
export function resetDetachedNotes(): void { cache = undefined; }

const when = (iso: string) => { const d = new Date(iso); return Number.isNaN(d.getTime()) ? iso : d.toLocaleDateString(); };

/**
 * Says that the loop an item was worked in has been deleted. A task, roadmap item or queue entry keeps its work when its
 * loop is deleted and loses only the link; this is where that is stated. `kind` is task, roadmap or queue with the row's
 * `id`; `kind="goal"` says a goal was parked because the only loop working it went; `kind="loop"` takes a loop `slug` instead (a goal's source loop). Nothing is drawn when there is nothing to say.
 */
export function DetachedNote({ kind, id, slug, className = "" }: { kind: "task" | "roadmap" | "queue" | "goal" | "loop"; id?: string; slug?: string; className?: string }) {
  const [text, setText] = useState("");
  useEffect(() => {
    let on = true;
    void records().then((r) => {
      if (!on) return;
      if (kind === "loop") {
        const t = r.tombstones.find((x) => x.slug === slug);
        setText(t ? `Its source loop ${t.slug} was deleted on ${when(t.deletedAt)}.` : "");
        return;
      }
      // Work closed because its loop was archived or deleted says so first: it is closed, not done.
      const c = r.closed.find((x) => x.kind === kind && x.id === id);
      const closedText = c ? `Closed, not done: its loop ${c.loopSlug} was ${c.reason === "loop-deleted" ? "deleted" : "archived"} on ${when(c.closedAt)} while this was still ${c.priorStatus}.${c.reason === "loop-archived" ? " It opens again if the loop is restored." : ""}` : "";
      if (kind === "goal") { setText(closedText); return; }
      const d = r.detached.find((x) => x.kind === kind && x.id === id);
      const detachedText = d ? `The loop ${d.loopSlug} that worked on this was deleted on ${when(d.detachedAt)}. The ${kind} was kept; only its link to the loop is gone.` : "";
      setText([closedText, detachedText].filter(Boolean).join(" "));
    });
    return () => { on = false; };
  }, [kind, id, slug]);
  if (!text) return null;
  return <p data-testid="detached-note" className={`text-xs text-amber-700 dark:text-amber-300 ${className}`}>{text}</p>;
}
