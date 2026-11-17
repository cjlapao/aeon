import { createContext, useSyncExternalStore } from "react";

/**
 * What a page is still waiting for, counted per page.
 *
 * # The problem this answers
 *
 * A screen's own loading state is invisible from the shell. The Configuration
 * menu builds its items from the page registry and knows nothing about what
 * the mounted page is doing, so a page that spends six seconds on a daemon
 * call says so only inside itself — and while the shell's Rust command was
 * synchronous (pre-S72), "inside itself" was nowhere reachable: the whole
 * window was frozen, and a spinner in a frozen window is decoration. The
 * S72 walk recorded a user who could neither see which page was working nor
 * leave it.
 *
 * # The mechanism
 *
 * A page declares an owner id around its subtree (`PageWaitOwnerContext`, set
 * by the config shell to `config:<page id>`). Anything inside it that starts
 * waiting — `useRpcQuery`'s fetch, or a lazy chunk still on its way — takes a
 * ticket against that id and releases it when it settles. A reader (the menu)
 * asks one question per item: is anything still waiting for you?
 *
 * The ticket is a closure rather than a counter the caller decrements,
 * because the page that started a call is often not the page on screen when
 * it lands: navigating away must still release the wait it belongs to, not
 * whatever owner happens to be current at that moment.
 *
 * # What it deliberately is not
 *
 * Not a cache, not a progress estimate, not a per-method registry: one number
 * per page, incremented by whoever waits. A page with three queries in
 * flight and a spinner on its menu row is telling the truth; the shell has no
 * business knowing which of the three is slow.
 */

/** The two things a page can be waiting on: its code, or its data. */
export type WaitKind = "chunk" | "data";

/**
 * Whose subtree this component is in. `null` outside any scope — a query
 * issued from a page the shell never labelled counts nowhere, which is the
 * honest reading of "we don't know who's waiting".
 */
export const PageWaitOwnerContext = createContext<string | null>(null);

const totals = new Map<string, { chunk: number; data: number }>();
const listeners = new Set<() => void>();

function emit(): void {
  for (const listener of listeners) listener();
}

function add(owner: string, kind: WaitKind, delta: number): void {
  const row = totals.get(owner) ?? { chunk: 0, data: 0 };
  row[kind] = Math.max(0, row[kind] + delta);
  if (row.chunk === 0 && row.data === 0) totals.delete(owner);
  else totals.set(owner, row);
  emit();
}

/**
 * Take a ticket: the returned function releases it, once, however often it is
 * called. An unknown owner waits nowhere — the no-op release keeps callers
 * free of a null check they would only get wrong.
 */
export function beginWait(owner: string | null | undefined, kind: WaitKind): () => void {
  if (!owner) return () => {};
  add(owner, kind, 1);
  let released = false;
  return () => {
    if (released) return;
    released = true;
    add(owner, kind, -1);
  };
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

/** Everything still pending for one owner: the menu row's spinner is up when this is > 0. */
export function waitsFor(owner: string): number {
  const row = totals.get(owner);
  return row ? row.chunk + row.data : 0;
}

/** The hook a shell component reads to render a live indicator for one owner. */
export function useWaitsFor(owner: string): number {
  return useSyncExternalStore(
    subscribe,
    () => waitsFor(owner),
    () => 0,
  );
}
