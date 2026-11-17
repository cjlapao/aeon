import { describe, expect, it } from "vitest";
import { filterRoutes, priceWords, providersOf } from "./modelsModel";

const route = (over: Record<string, unknown>) => ({ id: "r", model: "m", provider: "openai", hasKey: false, maxOutputTokens: 1, priced: false, source: "stored", ...over }) as never;

describe("models list", () => {
  const routes = [route({ id: "fast", model: "gpt-mini", displayName: "Fast" }), route({ id: "deep", model: "claude-x", provider: "anthropic", displayDescription: "Careful work" })];
  it("filters by text over name, slug, id and description, and by provider", () => {
    expect(filterRoutes(routes, "fast", "").map((r) => r.id)).toEqual(["fast"]);
    expect(filterRoutes(routes, "CAREFUL", "").map((r) => r.id)).toEqual(["deep"]);
    expect(filterRoutes(routes, "", "anthropic").map((r) => r.id)).toEqual(["deep"]);
    expect(filterRoutes(routes, "zzz", "")).toEqual([]);
    expect(providersOf(routes)).toEqual(["anthropic", "openai"]);
  });
  it("keeps unmetered, unpriced and priced apart", () => {
    expect(priceWords({ priced: true, prices: { inputUsdPerMTok: 3, outputUsdPerMTok: 15.5 } } as never)).toBe("$3 in · $15.50 out / M");
    expect(priceWords({ priced: false, noPrice: true } as never)).toBe("unmetered");
    expect(priceWords({ priced: false } as never)).toBe("no price set");
  });
});
