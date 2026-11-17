import { useRef } from "react";
import type { ChatItem } from "./types";

/** Equal field by field. Primitives by `===`; a nested object or array (a question list, a verdict) by its JSON. */
function sameItem(a: ChatItem, b: ChatItem): boolean {
  const x = a as unknown as Record<string, unknown>;
  const y = b as unknown as Record<string, unknown>;
  const keys = Object.keys(x);
  if (keys.length !== Object.keys(y).length) return false;
  for (const k of keys) {
    const u = x[k];
    const v = y[k];
    if (u === v) continue;
    if (typeof u === "function" || typeof v === "function" || typeof u !== "object" || typeof v !== "object" || u === null || v === null) return false;
    if (JSON.stringify(u) !== JSON.stringify(v)) return false;
  }
  return true;
}

/**
 * The items, with every row that did not change kept as the SAME object it was last time. The chat is rebuilt from the
 * event list on every streamed chunk, which makes a new object for every row; rows that look at object identity (the
 * memoised ones) would then redraw all of them, thinking blocks included, for each word one agent writes. Keeping the
 * unchanged rows' identity means only the row that is growing is drawn again.
 */
export function useStableItems(items: readonly ChatItem[]): readonly ChatItem[] {
  const previous = useRef<{ byId: Map<string, ChatItem>; list: readonly ChatItem[] }>({ byId: new Map(), list: [] });
  const { byId, list } = previous.current;
  const next = items.map((item) => {
    const old = byId.get(item.id);
    return old && sameItem(old, item) ? old : item;
  });
  const unchanged = next.length === list.length && next.every((item, i) => item === list[i]);
  const result = unchanged ? list : next;
  previous.current = { byId: new Map(result.map((item) => [item.id, item])), list: result };
  return result;
}
