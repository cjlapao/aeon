import type { Cost, Usage } from "./protocol";
import { uncertainText, type UncertainValue } from "./uncertain";

/**
 * A cost, rendered in **three distinct forms**.
 *
 * # Why this is a function and not a template
 *
 * The three states are the whole point of the cost design, and the way they get
 * lost is a renderer that formats `usd` and hopes. An `unknown` cost shown as
 * `$0.00` is the exact failure the design exists to prevent; a bare `—` is
 * barely better, because it says nothing about why.
 *
 * Mirrors `cmd/aeon/client_verbs.go`'s `costLine` deliberately: the CLI and the
 * app must not disagree about what a cost means.
 */
export interface CostLine {
  /** The headline, always safe to show. */
  text: string;
  /** A sentence explaining an `unknown`, or what is missing from a `partial`. */
  detail?: string;
  /** For styling. Never the only signal — `text` carries the fact in words. */
  tone: "green" | "amber" | "red";
}

export function costLine(cost: Cost | undefined): CostLine {
  if (!cost) {
    return { text: "cost not reported", detail: "no accounting arrived at all", tone: "red" };
  }

  switch (cost.state) {
    case "known":
      if (cost.usd === undefined) return { text: "unknown — known cost has no amount", tone: "red" };
      return { text: uncertainText({ state: "known", value: cost.usd }, money), tone: "green" };

    case "partial":
      if (cost.usdAtLeast === undefined) return { text: "unknown — partial cost has no measured floor", tone: "red" };
      return {
        // "at least" is the whole claim. A floor presented as a total is a
        // number somebody will add up.
        text: uncertainText({ state: "partial", atLeast: cost.usdAtLeast }, money),
        detail:
          cost.missing && cost.missing.length > 0
            ? `missing: ${cost.missing.join(", ")}`
            : "some token counts were not reported",
        tone: "amber",
      };

    case "unknown": {
      const detail = explainReason(cost.reason);
      const measured =
        cost.usdAtLeast && cost.usdAtLeast > 0
          // The floor goes through `money` — Go renders `at least
          // <MoneyString(floor)>` and a floor too small to survive four
          // decimals would print as the forbidden literal here too (D9).
          ? ` — at least ${money(cost.usdAtLeast)} was measured`
          : "";
      return { text: `not reported — ${uncertainText({ state: "unknown", reason: detail }, money)}${measured}`, detail, tone: "red" };
    }

    default:
      // A state this build does not know. Said plainly rather than rendered as
      // one of the three, because guessing which would be a lie.
      return {
        text: `unrecognised cost state "${cost.state}"`,
        detail: "this app is older than the daemon",
        tone: "red",
      };
  }
}

function money(usd: number): string {
  // Mirrors Go's MoneyString EXACTLY (one computation, two renderers — D16).
  // Three forms, none of which can be read as free by an eye that is skimming:
  //   - a reported zero is SPOKEN, not printed as $0.00 (D9): a real zero and
  //     a coerced one must never share a rendering;
  //   - anything under a cent goes to scientific notation, because five
  //     decimals of a token-cheap price start "$0.00x" — digits that read as
  //     free (the same argument Go's %.2e makes in MoneyString);
  //   - the rest keeps five decimals, the CLI's form.
  if (usd === 0) return "zero cost — usage was reported and priced to nothing";
  if (usd < 0.01) return `$${usd.toExponential(2).replace(/e([+-])(\d)$/, "e$10$2")}`; // e-6 → e-06, byte-for-byte Go
  return `$${usd.toFixed(5)}`;
}

/** A measured sum remains a floor when any component is partial or unknown. */
export function totalCost(costs: readonly (Cost | undefined)[]): UncertainValue<number> {
  if (costs.length === 0) return { state: "unknown", reason: "no steps have reported cost" };
  let floor = 0;
  let uncertain = false;
  let hasUnmeasuredPart = false;
  for (const cost of costs) {
    if (!cost) { uncertain = true; hasUnmeasuredPart = true; continue; }
    if (cost.state === "known") {
      if (cost.usd === undefined) { uncertain = true; hasUnmeasuredPart = true; continue; }
      floor += cost.usd;
    } else if (cost.state === "partial" || cost.state === "unknown") {
      uncertain = true;
      if (cost.state === "unknown" || cost.usdAtLeast === undefined) hasUnmeasuredPart = true;
      floor += cost.usdAtLeast ?? 0;
    } else {
      uncertain = true;
      hasUnmeasuredPart = true;
    }
  }
  if (hasUnmeasuredPart && floor === 0) return { state: "unknown", reason: "one or more steps have no measured cost" };
  return uncertain ? { state: "partial", atLeast: floor, detail: "one or more steps have incomplete accounting" }
    : { state: "known", value: floor };
}

/** Turn a reason code into a sentence a person can act on. */
export function explainReason(reason: string | undefined): string {
  switch (reason) {
    case "no-price-for-route":
      return "the route has no prices configured";
    case "provider-omitted-usage":
      return "the provider reported no token counts";
    case "stream-aborted-early":
      return "the stream ended before any usage arrived";
    case undefined:
    case "":
      return "no reason was given";
    default:
      return reason;
  }
}

/**
 * Six decimals below a cent, because one token of a cheap model is ~$0.000003
 * and two decimals would print a real cost as free.
 */
export function formatUSD(usd: number): string {
  if (usd === 0) return "0.00";
  if (usd < 0.01) return String(Number(usd.toFixed(6)));
  return usd.toFixed(4);
}

/** Token counts as a line, saying so when none arrived. */
export function usageLine(usage: Usage | undefined): string {
  if (!usage) return "tokens not reported";
  const parts: string[] = [];
  if (usage.inputTokens !== undefined) parts.push(`in ${usage.inputTokens}`);
  if (usage.outputTokens !== undefined) parts.push(`out ${usage.outputTokens}`);
  if (usage.cacheReadTokens !== undefined) parts.push(`cache-read ${usage.cacheReadTokens}`);
  if (usage.cacheWriteTokens !== undefined) parts.push(`cache-write ${usage.cacheWriteTokens}`);
  if (usage.reasoningTokens !== undefined) parts.push(`reasoning ${usage.reasoningTokens}`);
  // Said explicitly. Printing nothing would read as zero tokens.
  return parts.length > 0 ? parts.join(" · ") : "tokens not reported";
}
