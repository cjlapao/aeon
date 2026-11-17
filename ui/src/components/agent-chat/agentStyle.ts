import type { ChatAgent } from "./types";

/**
 * One colour per agent, stable across renders, sessions and windows: the
 * badge is how a reader tells two interleaved agents apart (P33), so the same
 * agent must never change colour because another one joined.
 */
const PALETTE = ["#6366f1", "#0d9488", "#d97706", "#db2777", "#2563eb", "#7c3aed", "#059669", "#dc2626", "#0891b2", "#65a30d"];

const RESERVED: Record<string, string> = { you: "#1d4ed8", orchestrator: "#334155" };

function hash(s: string): number {
  let h = 2166136261;
  for (let i = 0; i < s.length; i++) h = Math.imul(h ^ s.charCodeAt(i), 16777619);
  return h >>> 0;
}

export function agentColor(id: string, agents?: Record<string, ChatAgent>): string {
  return agents?.[id]?.color ?? RESERVED[id] ?? PALETTE[hash(id) % PALETTE.length] ?? "#6366f1";
}

export function agentName(id: string, agents?: Record<string, ChatAgent>): string {
  return agents?.[id]?.name ?? id;
}

export function agentInitial(id: string, agents?: Record<string, ChatAgent>): string {
  return (agentName(id, agents).trim()[0] ?? "?").toUpperCase();
}
