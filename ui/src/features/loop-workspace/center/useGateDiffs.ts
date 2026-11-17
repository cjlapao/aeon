import { useEffect, useMemo, useState } from "react";
import type { ChatItem } from "../../../components/agent-chat";
import { rpc } from "../../../lib/bridge";

type Gate = Extract<ChatItem, { kind: "gate" }>;

/**
 * A gate asks "approve this?" — so it should show what "this" is. For each
 * gate in the stream, the diff of the last file written before it is fetched
 * and attached as the gate's preview. A gate with no write before it just
 * has no preview.
 */
export function useGateDiffs(items: readonly ChatItem[], componentRoot: string): ChatItem[] {
  const [extras, setExtras] = useState<Record<string, Partial<Gate>>>({});
  useEffect(() => {
    if (!componentRoot) return;
    items.forEach((item, i) => {
      if (item.kind !== "gate" || extras[item.id]) return;
      const write = [...items.slice(0, i)].reverse().find((x) => x.kind === "tool" && x.file);
      const path = write?.kind === "tool" ? write.file?.path : undefined;
      setExtras((all) => ({ ...all, [item.id]: { description: path ? `Changes to ${path} since HEAD.` : undefined } }));
      if (!path) return;
      void rpc<{ diff?: string }>("workspace.git.diff", { componentRoot, path })
        .then((r) => setExtras((all) => ({ ...all, [item.id]: { ...all[item.id], preview: r.diff || undefined, previewLines: r.diff ? r.diff.split("\n").length : undefined } })))
        .catch(() => undefined);
    });
  }, [items, componentRoot, extras]);
  return useMemo(() => items.map((item) => (item.kind === "gate" && extras[item.id] ? { ...item, ...extras[item.id] } as ChatItem : item)), [items, extras]);
}
