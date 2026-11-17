import type { ReactNode } from "react";

export type UncertainValue<T> =
  | { state: "known"; value: T }
  | { state: "partial"; atLeast: T; detail?: string }
  | { state: "unknown"; reason: string };

/** The shared wording rule: uncertainty must be present in text, not colour alone. */
export function uncertainText<T>(value: UncertainValue<T>, render: (value: T) => string): string {
  switch (value.state) {
    case "known": return render(value.value);
    case "partial": return `at least ${render(value.atLeast)}`;
    case "unknown": return `unknown — ${value.reason || "no reason was given"}`;
  }
}

export function Uncertain<T>({ value, render }: { value: UncertainValue<T>; render: (value: T) => string }) {
  return <span data-uncertainty={value.state}>{uncertainText(value, render)}</span>;
}

type NoticeTone = "known" | "partial" | "unknown";

function StateNotice({ state, children }: { state: NoticeTone; children: ReactNode }) {
  return <span data-uncertainty={state}>{children}</span>;
}

export type ValidationState = "valid" | "invalid" | "unverifiable";

export function ValidationNotice({ state, message, slice }: {
  state: ValidationState; message: string; slice?: string;
}) {
  if (state === "unverifiable") {
    return <StateNotice state="unknown">cannot verify — {message || "missing registry"}{slice ? ` · ${slice}` : ""}</StateNotice>;
  }
  if (state === "invalid") return <StateNotice state="partial">invalid — {message || "validation failed"}</StateNotice>;
  return <StateNotice state="known">valid — {message || "verified"}</StateNotice>;
}

export function RecoveryNotice({ outcome, tool, receiptState, detail }: {
  outcome: "recovered" | "failed" | "unknown"; tool?: string; receiptState?: string; detail?: string;
}) {
  if (outcome === "recovered") return <StateNotice state="known">recovered{detail ? ` — ${detail}` : ""}</StateNotice>;
  const context = [tool ? `tool ${tool}` : "tool not recorded", receiptState ? `receipt ${receiptState}` : "receipt state unknown"];
  return <StateNotice state={outcome === "unknown" ? "unknown" : "partial"}>{outcome} — {context.join(" · ")}{detail ? ` · ${detail}` : ""}</StateNotice>;
}

export type LiveNotice =
  | { state: "current" }
  | { state: "stale"; ageSeconds: number; cause: string; endpoint: string }
  | { state: "disconnected"; reason: string; endpoint: string };

export function LivenessNotice({ value }: { value: LiveNotice }) {
  switch (value.state) {
    case "current": return <StateNotice state="known">current</StateNotice>;
    case "stale": return <StateNotice state="partial">stale for {Math.max(0, Math.floor(value.ageSeconds))}s — {value.cause} · {value.endpoint}</StateNotice>;
    case "disconnected": return <StateNotice state="unknown">disconnected — {value.reason} · {value.endpoint}</StateNotice>;
  }
}
