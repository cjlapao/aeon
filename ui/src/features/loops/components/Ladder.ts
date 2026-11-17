import type { Cost } from "../../../lib/protocol";

export interface LadderStep {
  id: string; kind: string; status: string; outcome?: string; reason?: string; stepId?: number; cost?: Cost;
  startedAt?: string; endedAt?: string; wallMs?: number;
}
export interface LadderLane {
  id: string; structure?: "direct" | "ladder" | "iterate"; status: string; condition?: string; iterate?: string; itemCount?: number;
  produces?: string[]; steps?: LadderStep[]; items?: Array<{ id: string; status: string; steps: LadderStep[] }>;
}
export interface BlockedReason { kind: string; detail: string; action?: string; slice?: string }

export function blockedLine(reason: string | BlockedReason): string {
  if (typeof reason === "string") return reason;
  if (reason.kind === "unimplemented-action") return `${reason.action || "Code action"} is not built yet — arrives at ${reason.slice || "a later slice"}`;
  return `${reason.kind}: ${reason.detail}`;
}
