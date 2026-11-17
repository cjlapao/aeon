import { useCallback, useState } from "react";

/**
 * Per-viewer conveniences — panel sizes, what was collapsed — kept in
 * localStorage. Never state that must survive or be shared: storage can be
 * empty, blocked or throw (private windows, cleared data), so every read and
 * write is guarded and the page renders correctly without it.
 */
export function loadSetting<T>(key: string, fallback: T): T {
  try {
    const raw = window.localStorage.getItem(key);
    return raw === null ? fallback : (JSON.parse(raw) as T);
  } catch { return fallback; }
}

export function saveSetting(key: string, value: unknown): void {
  try { window.localStorage.setItem(key, JSON.stringify(value)); } catch { /* storage unavailable — the setting just does not persist */ }
}

export function usePersisted<T>(key: string, initial: T): [T, (value: T) => void] {
  const [value, setValue] = useState<T>(() => loadSetting(key, initial));
  const set = useCallback((next: T) => { setValue(next); saveSetting(key, next); }, [key]);
  return [value, set];
}
