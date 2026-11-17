// protogen infers a JSON object's shape from its fixture EXAMPLES, with no
// notion of "this Go field is a map[string]X" (it is pure fixture-shape
// inference, never Go reflection) — so the generated type for
// `mcp.status.result.probes` is a struct with one optional key per server
// NAME the fixtures happened to use ("github"), not `Record<string, X>`.
// That is wrong for a field whose real keys are whatever servers are
// configured, so this is hand-typed here rather than read off the contract,
// and callers reading `result.probes` cast to this shape (see McpPage/
// AgentsPage) rather than trusting the generated one for this one field.
export interface ProbeSummary {
  ok: boolean;
  tools?: number;
  error?: string;
  ms: number;
  checkedAt: string;
}

/**
 * The chip vocabulary design.md §4 states verbatim — shared by AgentsPage's
 * MCP-declares strip (T6) and McpPage's server table (T7), so a probe's
 * result reads identically wherever it appears. Two states carry no number
 * where a number would be invented (TB2): "not checked" and "checking…".
 */
export function ProbeChip({ probe, checking }: { probe?: ProbeSummary; checking?: boolean }) {
  if (checking) {
    return <span className="text-xs text-slate-500" data-testid="probe-chip-checking">checking…</span>;
  }
  if (!probe) {
    return <span className="text-xs text-slate-500" data-testid="probe-chip-not-checked">not checked</span>;
  }
  if (probe.ok) {
    const n = probe.tools ?? 0;
    return (
      <span className="text-xs text-emerald-700 dark:text-emerald-300!" data-testid="probe-chip-ok">
        ok · {n} tool{n === 1 ? "" : "s"} · {probe.ms}ms · checked {formatTime(probe.checkedAt)}
      </span>
    );
  }
  return (
    <span className="text-xs text-red-700 dark:text-red-300!" data-testid="probe-chip-failed">
      {probe.error ?? "failed"} · checked {formatTime(probe.checkedAt)}
    </span>
  );
}

function formatTime(iso: string): string {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}
