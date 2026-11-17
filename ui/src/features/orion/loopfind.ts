export interface LoopRow { id: string; slug: string; title?: string; status: string; phase?: string; componentRoot: string }
const say = (l: LoopRow) => `${l.title || l.slug} (${l.status}${l.phase ? `, ${l.phase}` : ""})`;

/** Finds the loop a person means: an id or slug, or a few words of its title. Says so when it is ambiguous or absent. */
export function findLoop(all: readonly LoopRow[], words: string): { loop?: LoopRow; problem?: string } {
  const q = words.toLowerCase().trim();
  if (!q) {
    const running = all.filter((l) => l.status === "running");
    if (running.length === 1) return { loop: running[0] };
    return { problem: running.length === 0 ? "no loop is running; name one" : `${running.length} loops are running; say which one` };
  }
  const exact = all.filter((l) => l.id === words || l.slug === words);
  const hits = exact.length ? exact : all.filter((l) => `${l.title ?? ""} ${l.slug}`.toLowerCase().includes(q));
  if (hits.length === 1) return { loop: hits[0] };
  if (hits.length === 0) return { problem: `no loop matches "${words}"` };
  return { problem: `${hits.length} loops match "${words}": ${hits.slice(0, 4).map(say).join("; ")}` };
}

