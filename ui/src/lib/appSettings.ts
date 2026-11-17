import { useSyncExternalStore } from "react";
import { TRUE_COLORS, type TrueColor } from "@cjlapao/ui-kit";

export type AeonTheme = "light" | "dark" | "system";
export type AppSettings = { theme: AeonTheme; controlTone: TrueColor; textTone: TrueColor; surfaceTone: TrueColor };
const KEY = "aeon-app-settings-v1";
const LEGACY_THEME_KEY = "aeon-theme";
/**
 * The key the ui-kit's own `useTheme` reads. Kit charts call that hook, and on mount it applies the mode it finds there
 * (light when the key is absent) to the page, overriding the choice made in Settings. Keeping the key equal to ours makes
 * that apply a no-op.
 */
const KIT_THEME_KEY = "ui-kit-demo-theme";
export const DEFAULT_SETTINGS: AppSettings = { theme: "system", controlTone: "emerald", textTone: "emerald", surfaceTone: "emerald" };

function validTone(value: unknown): value is TrueColor { return typeof value === "string" && (TRUE_COLORS as readonly string[]).includes(value); }
function validTheme(value: unknown): value is AeonTheme { return value === "light" || value === "dark" || value === "system"; }

function loadSettings(): AppSettings {
  try {
    const raw = window.localStorage.getItem(KEY);
    const saved = raw ? JSON.parse(raw) as Partial<AppSettings> : {};
    const legacy = window.localStorage.getItem(LEGACY_THEME_KEY);
    return { theme: validTheme(saved.theme) ? saved.theme : validTheme(legacy) ? legacy : DEFAULT_SETTINGS.theme,
      controlTone: validTone(saved.controlTone) ? saved.controlTone : DEFAULT_SETTINGS.controlTone,
      textTone: validTone(saved.textTone) ? saved.textTone : DEFAULT_SETTINGS.textTone,
      surfaceTone: validTone(saved.surfaceTone) ? saved.surfaceTone : DEFAULT_SETTINGS.surfaceTone };
  } catch { return DEFAULT_SETTINGS; }
}

let current = loadSettings();
const listeners = new Set<() => void>();
const emit = () => listeners.forEach((listener) => listener());
const root = () => document.documentElement;

function applySettings(settings: AppSettings): void {
  const dark = settings.theme === "dark" || (settings.theme === "system" &&
    typeof window.matchMedia === "function" && window.matchMedia("(prefers-color-scheme: dark)").matches);
  root().classList.toggle("dark", dark);
  try { window.localStorage.setItem(KIT_THEME_KEY, settings.theme); } catch { /* the kit falls back to its own default */ }
  root().dataset.controlTone = settings.controlTone;
  root().dataset.textTone = settings.textTone;
  root().dataset.surfaceTone = settings.surfaceTone;
  root().style.setProperty("--aeon-accent-text", `var(--color-${settings.textTone}-${dark ? "300" : "700"})`);
  root().style.setProperty("--aeon-body-text", `var(--color-${settings.textTone}-${dark ? "100" : "950"})`);
  root().style.setProperty("--aeon-accent-surface", `var(--color-${settings.surfaceTone}-${dark ? "950" : "100"})`);
  root().style.setProperty("--aeon-accent-wash", `var(--color-${settings.surfaceTone}-${dark ? "900" : "200"})`);
  root().style.setProperty("--aeon-control-fill", `var(--color-${settings.controlTone}-${dark ? "600" : "700"})`);
  root().style.setProperty("--aeon-background-glow", `var(--color-${settings.controlTone}-${dark ? "900" : "100"})`);
}

export const appSettings = {
  getSnapshot: () => current,
  subscribe: (listener: () => void) => { listeners.add(listener); return () => { listeners.delete(listener); }; },
  set: (patch: Partial<AppSettings>) => {
    current = { ...current, ...patch };
    try { window.localStorage.setItem(KEY, JSON.stringify(current)); } catch { /* current window keeps its choice */ }
    applySettings(current);
    emit();
  },
  reset: () => appSettings.set(DEFAULT_SETTINGS),
};

export function useAppSettings(): AppSettings { return useSyncExternalStore(appSettings.subscribe, appSettings.getSnapshot, appSettings.getSnapshot); }

/** One mount per product window; observes OS theme and other app windows. */
export function startAppSettings(): () => void {
  applySettings(current);
  const media = typeof window.matchMedia === "function" ? window.matchMedia("(prefers-color-scheme: dark)") : null;
  const onSystem = () => { if (current.theme === "system") applySettings(current); };
  const onStorage = (event: StorageEvent) => {
    if (event.key !== KEY && event.key !== LEGACY_THEME_KEY) return;
    current = loadSettings(); applySettings(current); emit();
  };
  media?.addEventListener?.("change", onSystem);
  window.addEventListener("storage", onStorage);
  return () => { media?.removeEventListener?.("change", onSystem); window.removeEventListener("storage", onStorage); };
}
