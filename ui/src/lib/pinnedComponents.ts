// A client-only convenience: which component roots the user has pinned to
// the home screen. Nothing on the daemon knows about this, so it never
// travels past this browser profile (consistent with §4c: nothing that
// matters leaves the socket, and nothing that doesn't matter pretends to).
import { createPersistedStore, usePersistedStore } from "./createPersistedStore";

const store = createPersistedStore<string[]>("aeon-pinned-components-v1", [], (parsed) =>
  Array.isArray(parsed) ? parsed.filter((value): value is string => typeof value === "string") : [],
);

export const pinnedComponents = {
  getSnapshot: store.getSnapshot,
  subscribe: store.subscribe,
  pin: (root: string) => {
    const current = store.getSnapshot();
    if (!current.includes(root)) store.set([...current, root]);
  },
  unpin: (root: string) => store.set((current) => current.filter((item) => item !== root)),
};

export function usePinnedComponents(): string[] {
  return usePersistedStore(store);
}
