import type { RouteView } from "../../lib/protocol";
import { modelName } from "./modelConfig";

/** The routes that match a search term and a provider. Case-insensitive, over name, slug, id and description. */
export function filterRoutes(routes: readonly RouteView[], q: string, provider: string): RouteView[] {
  const needle = q.trim().toLowerCase();
  return routes.filter((route) => {
    if (provider !== "" && route.provider !== provider) return false;
    if (needle === "") return true;
    return [modelName(route), route.model, route.id, route.displayDescription ?? ""].some((text) => text.toLowerCase().includes(needle));
  });
}

const usd = (n: number): string => `$${Number.isInteger(n) ? n : n.toFixed(2)}`;

/**
 * The price in words, keeping the two blank states apart (S18 O8): a number says what it is, "unmetered"
 * says the person decided there is no price, and "no price set" says nobody said. A blank never reads as free.
 */
export function priceWords(route: Pick<RouteView, "priced" | "prices" | "noPrice">): string {
  if (route.noPrice) return "unmetered";
  if (!route.priced || !route.prices) return "no price set";
  return `${usd(route.prices.inputUsdPerMTok ?? 0)} in · ${usd(route.prices.outputUsdPerMTok ?? 0)} out / M`;
}

/** The distinct providers among the routes, for the filter. */
export function providersOf(routes: readonly RouteView[]): string[] {
  return [...new Set(routes.map((route) => route.provider))].sort();
}
