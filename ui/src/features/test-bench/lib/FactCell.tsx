import type { ReactNode } from "react";
import type { Fact } from "./fact";

/**
 * The one renderer every strip on both trust-bench pages uses for its
 * `Fact<T>` (TB2) — so "unwired" and "error" read identically everywhere
 * instead of each strip inventing its own empty/error copy.
 */
export function FactCell<T>({ fact, children }: { fact: Fact<T>; children: (value: T) => ReactNode }) {
  switch (fact.state) {
    case "loading":
      return <p className="text-sm text-slate-500" data-testid="fact-loading">loading…</p>;
    case "unwired":
      return (
        <p className="text-sm text-amber-700 dark:text-amber-300!" data-testid="fact-unwired">
          not wired on this daemon — the method is not registered yet
        </p>
      );
    case "error":
      return (
        <p className="text-sm text-red-700 dark:text-red-300!" data-testid="fact-error">
          {fact.message}
        </p>
      );
    case "ok":
      return <>{children(fact.value)}</>;
  }
}
