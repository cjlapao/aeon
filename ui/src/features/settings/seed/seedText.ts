// seedText — the wording half of the System records page: it turns the tokens `seedModel` returns
// into the sentences the page prints. `seedModel` decides WHICH state is which; this file only picks
// the catalog key, so a state a later slice adds still reads (its raw word, never blank).

import { badgeFor, type SeedSummary } from "./seedModel";

type T = (key: string, values?: Record<string, unknown>) => string;

/** The one status line of a set: "Up to date" when every record is applied, otherwise the most severe
 *  non-applied state with its count. An unknown state the catalog has no token for still reads. */
export function statusText(t: T, summary: SeedSummary): string {
  if (summary.headline === "upToDate") return t("settings.seed.status.upToDate");
  if (badgeFor(summary.headline).labelToken !== null) return t(`settings.seed.status.${summary.headline}`, { count: summary.headlineCount });
  return `${summary.headlineCount} ${summary.headline}`;
}

/** A record state's label — the catalog's word for a known state, the daemon's raw word otherwise. */
export function stateText(t: T, state: string): string {
  const badge = badgeFor(state);
  return badge.labelToken !== null ? t(`settings.seed.state.${badge.labelToken}`) : state;
}
