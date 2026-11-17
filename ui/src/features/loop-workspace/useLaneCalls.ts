import { useEffect, useRef, useState } from "react";
import { chatItemsFromEvents, type ChatItem } from "../../components/agent-chat";
import { rpc } from "../../lib/bridge";
import type { AeonEvent } from "../../lib/protocol";
import type { JournalWindow } from "./journal";

type ToolItem = Extract<ChatItem, { kind: "tool" }>;
const KINDS = ["step.tool.start", "step.tool.end", "step.policy.decided", "step.tool.skipped"];
const PER_STEP = 500;

/**
 * The tool calls of steps the page has no events for: read from the journal narrowed to each step, once, when a lane
 * is opened. This is what lets the inspector list an old step's calls without paging the whole loop's history.
 */
export function useLaneCalls(loopId: string, stepIds: readonly number[], have: ReadonlySet<number>, enabled: boolean): ToolItem[] {
  const [calls, setCalls] = useState<Map<number, ToolItem[]>>(new Map());
  const asked = useRef(new Set<number>());
  useEffect(() => { asked.current = new Set(); setCalls(new Map()); }, [loopId]);
  const key = stepIds.join(",");
  useEffect(() => {
    if (!enabled || !loopId) return;
    for (const stepId of stepIds) {
      if (have.has(stepId) || asked.current.has(stepId)) continue;
      asked.current.add(stepId);
      void (async () => {
        try {
          const win = await rpc<JournalWindow>("loop.events", { id: loopId, stepId, kinds: KINDS, limit: PER_STEP });
          if (!Array.isArray(win?.events)) return;
          const events = win.events.map((r) => ({ subscriptionId: "journal", seq: r.seq, kind: r.kind, at: r.at, payload: r.payload }) as AeonEvent);
          const items = chatItemsFromEvents(events, {}).filter((i): i is ToolItem => i.kind === "tool").map((t) => ({ ...t, stepId }));
          setCalls((cur) => new Map(cur).set(stepId, items));
        } catch { asked.current.delete(stepId); }
      })();
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [enabled, loopId, key, have]);
  return [...calls.values()].flat();
}
