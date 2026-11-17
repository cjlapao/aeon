import { afterEach, describe, expect, it } from "vitest";
import { createPersistedStore } from "../src/lib/createPersistedStore";

const KEY = "aeon-test-persisted-store";

afterEach(() => {
  window.localStorage.removeItem(KEY);
});

describe("createPersistedStore", () => {
  it("starts at the default when nothing is stored", () => {
    const store = createPersistedStore(KEY, ["a"]);
    expect(store.getSnapshot()).toEqual(["a"]);
  });

  it("persists a set value and reloads it in a fresh store instance", () => {
    const store = createPersistedStore<string[]>(KEY, []);
    store.set(["b", "c"]);
    expect(JSON.parse(window.localStorage.getItem(KEY) ?? "null")).toEqual(["b", "c"]);

    const reloaded = createPersistedStore<string[]>(KEY, []);
    expect(reloaded.getSnapshot()).toEqual(["b", "c"]);
  });

  it("accepts an updater function derived from the current value", () => {
    const store = createPersistedStore<number>(KEY, 0);
    store.set((current) => current + 1);
    store.set((current) => current + 1);
    expect(store.getSnapshot()).toBe(2);
  });

  it("notifies subscribers on every set, and stops after unsubscribe", () => {
    const store = createPersistedStore<number>(KEY, 0);
    let calls = 0;
    const unsubscribe = store.subscribe(() => { calls += 1; });
    store.set(1);
    store.set(2);
    unsubscribe();
    store.set(3);
    expect(calls).toBe(2);
  });

  it("sanitizes a malformed stored value instead of trusting the parse", () => {
    window.localStorage.setItem(KEY, JSON.stringify(["a", 42, null, "b"]));
    const store = createPersistedStore<string[]>(KEY, [], (parsed) =>
      Array.isArray(parsed) ? parsed.filter((v): v is string => typeof v === "string") : [],
    );
    expect(store.getSnapshot()).toEqual(["a", "b"]);
  });

  it("falls back to the default when the stored value is not valid JSON", () => {
    window.localStorage.setItem(KEY, "{not json");
    const store = createPersistedStore<string[]>(KEY, ["fallback"]);
    expect(store.getSnapshot()).toEqual(["fallback"]);
  });
});
