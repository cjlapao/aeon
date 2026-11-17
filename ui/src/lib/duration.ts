/**
 * Compact durations for dense table cells.
 *
 * The kit's `formatDurationFromMs` writes prose ("1 minute and 5 seconds"), which
 * is right for a sentence and far too wide for a column shown on every row.
 */
export function formatMs(ms: number | null | undefined): string {
  if (ms === null || ms === undefined || !Number.isFinite(ms)) return "—";
  if (ms < 1000) return `${Math.round(ms)}ms`;
  const s = Math.round(ms / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  const rs = s % 60;
  if (m < 60) return rs ? `${m}m ${rs}s` : `${m}m`;
  const h = Math.floor(m / 60);
  const rm = m % 60;
  return rm ? `${h}h ${rm}m` : `${h}h`;
}

/** Elapsed time since an ISO timestamp — used for steps and tasks still running. */
export function elapsedMs(since: string | null | undefined, now = Date.now()): number | null {
  if (!since) return null;
  const t = Date.parse(since);
  if (Number.isNaN(t)) return null;
  // Clock skew between the API host and this machine can make a fresh timestamp
  // look future-dated; clamping beats rendering a negative duration.
  return Math.max(0, now - t);
}
