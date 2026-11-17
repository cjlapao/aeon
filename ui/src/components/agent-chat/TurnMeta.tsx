import type { ChatItem, TurnCost } from "./types";
import { formatDuration, Mono } from "./parts";
import { formatUSD } from "../../lib/cost";

/**
 * What one model call spent, as a quiet line after its thinking, text and tool calls: tokens in and out, the call's
 * cost, how long it took, and the step's running total. The cost keeps its state: a floor reads "at least", and a
 * cost nobody reported reads "cost not reported", never $0.
 */
function money(c: TurnCost | undefined): string | undefined {
  if (!c) return undefined;
  if (c.state === "known" && c.usd !== undefined) return `$${formatUSD(c.usd)}`;
  if (c.state === "partial" && c.usdAtLeast !== undefined) return `≥ $${formatUSD(c.usdAtLeast)}`;
  return "cost not reported";
}

export function TurnMeta({ item }: { item: Extract<ChatItem, { kind: "turn" }> }) {
  const tokens = [
    item.inputTokens !== undefined ? `${item.inputTokens} in` : undefined,
    item.outputTokens !== undefined ? `${item.outputTokens} out` : undefined,
    item.reasoningTokens ? `${item.reasoningTokens} thinking` : undefined,
  ].filter(Boolean).join(" / ");
  const parts = [
    tokens || "tokens not reported",
    money(item.cost),
    item.wallMs !== undefined ? formatDuration(item.wallMs) : undefined,
    item.stepCost ? `step so far ${money(item.stepCost)}` : undefined,
  ].filter(Boolean);
  return (
    <div data-testid="agent-chat-turn" className="flex justify-end py-0.5">
      <Mono className="text-[11px] text-slate-400 dark:text-slate-500!">{parts.join(" · ")}</Mono>
    </div>
  );
}
