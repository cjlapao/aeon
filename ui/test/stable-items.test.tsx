import { describe, expect, it } from "vitest";
import { renderHook } from "@testing-library/react";
import { useStableItems } from "../src/components/agent-chat/useStableItems";
import type { ChatItem } from "../src/components/agent-chat";

const think = (id: string, text: string) => ({ id, agent: "a", kind: "thinking", text }) as ChatItem;

describe("useStableItems", () => {
  it("keeps unchanged rows as the same object, and only the changed row is new", () => {
    const first = [think("t1", "done thought"), think("t2", "growing")];
    const { result, rerender } = renderHook(({ items }) => useStableItems(items), { initialProps: { items: first } });
    const a = result.current;
    // The chat is rebuilt: every row is a fresh object, one has more text.
    rerender({ items: [think("t1", "done thought"), think("t2", "growing more")] });
    const b = result.current;
    expect(b[0]).toBe(a[0]);
    expect(b[1]).not.toBe(a[1]);
    // A rebuild with nothing changed hands back the very same list.
    rerender({ items: [think("t1", "done thought"), think("t2", "growing more")] });
    expect(result.current).toBe(b);
  });
});
