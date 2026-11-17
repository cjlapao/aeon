import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { Modal, useI18n } from "@cjlapao/ui-kit";
import { Pill } from "../../components/kit";
import { rpcTyped } from "../../lib/bridge";
import type { LiveState } from "../../lib/live";
import { menuConfigPages } from "../config/lib/configRegistry";
import type { WorkComponent, WorkLoop, WorkTab } from "../work/workModel";
import { buildItems, GROUP_ORDER, type PaletteItem, type PalettePage } from "./paletteModel";

/**
 * Ctrl K (Cmd K on a Mac): one box that takes you to a component, a loop, a page. It only
 * navigates — every result is an address the app already has, and nothing here writes.
 * It remembers the last place it took you, so reopening it offers that first.
 */

const LAST_KEY = "aeon.palette.last";

function remembered(): string {
  try { return window.localStorage.getItem(LAST_KEY) ?? ""; } catch { return ""; }
}
function remember(to: string): void {
  try { window.localStorage.setItem(LAST_KEY, to); } catch { /* a blocked store only costs the shortcut */ }
}

export function CommandPalette({ live }: { live: LiveState }) {
  const { t } = useI18n();
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [at, setAt] = useState(0);
  const [components, setComponents] = useState<WorkComponent[]>([]);
  const [loops, setLoops] = useState<WorkLoop[]>([]);
  const input = useRef<HTMLInputElement>(null);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        setOpen((value) => !value);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  // Read the lists each time it opens, so a component added a minute ago is there.
  useEffect(() => {
    if (!open) return;
    setQuery("");
    setAt(0);
    if (live.state !== "current") return;
    let stale = false;
    void Promise.allSettled([rpcTyped("component.list", {}), rpcTyped("loop.list", { all: true })]).then(([c, l]) => {
      if (stale) return;
      if (c.status === "fulfilled") setComponents(c.value.components as WorkComponent[]);
      if (l.status === "fulfilled") setLoops(l.value.loops as WorkLoop[]);
    });
    return () => { stale = true; };
  }, [open, live.state]);

  const pages = useMemo<PalettePage[]>(() => {
    const fixed: PalettePage[] = [
      { key: "home", label: t("palette.page.home"), to: "/" },
      { key: "work", label: t("palette.page.work"), to: "/work" },
      { key: "flows", label: t("palette.page.flows"), to: "/blueprints" },
      { key: "models", label: t("palette.page.models"), to: "/models" },
      { key: "settings", label: t("palette.page.settings"), to: "/settings" },
      { key: "start", label: t("palette.page.start"), to: "/" },
    ];
    const config = menuConfigPages().filter((page) => page.status === "ready").map((page) => ({ key: `config-${page.id}`, label: `${t("palette.page.config")} › ${page.label}`, to: `/config/${page.id}` }));
    return [...fixed, ...config];
  }, [t]);

  const tabLabel = useCallback((tab: WorkTab) => t(`work.tabs.${tab}`), [t]);
  const items = useMemo(() => {
    const found = buildItems(query, components, loops, pages, tabLabel, (name) => t("palette.startIn", { name }));
    const last = query.trim() === "" ? remembered() : "";
    // With no query, the last place you went comes first.
    return last ? [...found.filter((item) => item.to === last), ...found.filter((item) => item.to !== last)] : found;
  }, [query, components, loops, pages, tabLabel]);

  useEffect(() => setAt(0), [query]);

  const go = (item: PaletteItem | undefined) => {
    if (!item) return;
    remember(item.to);
    setOpen(false);
    navigate(item.to);
  };

  const onKeyDown = (event: React.KeyboardEvent) => {
    if (event.key === "ArrowDown") { event.preventDefault(); setAt((value) => Math.min(value + 1, Math.max(items.length - 1, 0))); }
    else if (event.key === "ArrowUp") { event.preventDefault(); setAt((value) => Math.max(value - 1, 0)); }
    else if (event.key === "Enter") { event.preventDefault(); go(items[at]); }
  };

  if (!open) return null;
  return (
    <Modal isOpen onClose={() => setOpen(false)} title={t("palette.title")} size="md" initialFocusRef={input as React.RefObject<HTMLElement>} data-testid="command-palette">
      <div className="space-y-2" onKeyDown={onKeyDown}>
        <input
          ref={input}
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          placeholder={t("palette.placeholder")}
          aria-label={t("palette.placeholder")}
          data-testid="palette-input"
          className="w-full rounded-lg border border-slate-300 bg-transparent px-3 py-2 text-sm outline-none focus:border-sky-500 dark:border-slate-600"
        />
        {items.length === 0 ? (
          <p className="px-1 py-3 text-sm text-slate-500" data-testid="palette-empty">{t("palette.empty")}</p>
        ) : (
          <div role="listbox" aria-label={t("palette.title")} className="max-h-80 overflow-auto">
            {GROUP_ORDER.map((group) => {
              const rows = items.filter((item) => item.group === group);
              if (rows.length === 0) return null;
              return (
                <div key={group}>
                  <p className="px-2 pb-1 pt-2 text-xs font-semibold uppercase tracking-widest text-slate-500">{t(`palette.group.${group}`)}</p>
                  {rows.map((item) => {
                    const index = items.indexOf(item);
                    return (
                      <div
                        key={item.key}
                        role="option"
                        aria-selected={index === at}
                        data-testid={`palette-item-${item.key}`}
                        onMouseEnter={() => setAt(index)}
                        onClick={() => go(item)}
                        className={`flex cursor-pointer items-center gap-2 rounded-lg px-3 py-2 text-sm ${index === at ? "bg-sky-500/15 text-sky-700 dark:text-sky-300" : ""}`}
                      >
                        <span className="min-w-0 truncate">{item.label}</span>
                        {item.chip && <span className="shrink-0 rounded bg-slate-500/10 px-2 text-xs">{item.chip}</span>}
                        <span className="ml-auto flex shrink-0 items-center gap-2">
                          {item.status && <Pill tone="neutral" size="sm">{item.status}</Pill>}
                          {index === at && <span className="text-xs text-slate-500">↵</span>}
                        </span>
                      </div>
                    );
                  })}
                </div>
              );
            })}
          </div>
        )}
      </div>
    </Modal>
  );
}
