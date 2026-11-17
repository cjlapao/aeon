import { useSyncExternalStore } from "react";

/**
 * The localStorage-subscribe-save shape that `appSettings.ts`,
 * `pinnedComponents.ts`, and `notifications.ts` each wrote by hand — pulled
 * out once it was written a third time. Nothing on the daemon ever sees
 * this: every caller is a client-only convenience, which is why "failed to
 * save" is swallowed rather than surfaced (a local preference losing a
 * write is not worth alarming anyone over).
 */
export interface PersistedStore<T> {
  getSnapshot(): T;
  subscribe(listener: () => void): () => void;
  /** Replace the value, or derive the next one from the current one. */
  set(next: T | ((current: T) => T)): void;
}

/**
 * `sanitize` turns whatever `JSON.parse` handed back into a real `T` —
 * filtering a malformed array, rejecting a shape that no longer matches a
 * stored version, etc. Defaults to trusting the parse, which is fine for a
 * plain value but wrong for anything a hand-edited or stale file could
 * violate (an array of a specific element shape, for instance).
 */
export function createPersistedStore<T>(
  key: string,
  defaultValue: T,
  sanitize: (parsed: unknown) => T = (parsed) => parsed as T,
): PersistedStore<T> {
  function load(): T {
    try {
      const raw = window.localStorage.getItem(key);
      if (raw === null) return defaultValue;
      return sanitize(JSON.parse(raw));
    } catch {
      return defaultValue;
    }
  }

  let current = load();
  const listeners = new Set<() => void>();

  return {
    getSnapshot: () => current,
    subscribe: (listener) => {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
    set: (next) => {
      current = typeof next === "function" ? (next as (c: T) => T)(current) : next;
      try {
        window.localStorage.setItem(key, JSON.stringify(current));
      } catch {
        // See the module doc comment: a client-only preference that failed
        // to persist is not worth surfacing.
      }
      for (const listener of listeners) listener();
    },
  };
}

export function usePersistedStore<T>(store: PersistedStore<T>): T {
  return useSyncExternalStore(store.subscribe, store.getSnapshot, store.getSnapshot);
}
