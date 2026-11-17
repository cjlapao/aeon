import { useEffect, useState } from "react";

const MINUTE = 60_000;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

export function formatRelative(iso: string, now = Date.now()): string {
  const then = Date.parse(iso);
  if (Number.isNaN(then)) return "";
  const delta = now - then;
  // Clock skew between the API host and this machine can make a timestamp look
  // slightly future-dated; "just now" beats "in -3 seconds".
  if (delta < MINUTE) return "just now";
  if (delta < HOUR) return `${Math.floor(delta / MINUTE)}m ago`;
  if (delta < DAY) return `${Math.floor(delta / HOUR)}h ago`;
  return `${Math.floor(delta / DAY)}d ago`;
}

/**
 * A timestamp that ages without a refetch.
 *
 * Re-renders on a shared-ish 30s interval so "2m ago" does not sit lying on a
 * dashboard left open. `title` keeps the exact value one hover away.
 */
export function RelativeTime({ iso, prefix }: { iso: string; prefix?: string }) {
  const [, tick] = useState(0);

  useEffect(() => {
    const t = setInterval(() => tick((n) => n + 1), 30_000);
    return () => clearInterval(t);
  }, []);

  if (!iso) return null;
  return (
    <span title={iso}>
      {prefix ? `${prefix} ` : ""}
      {formatRelative(iso)}
    </span>
  );
}
