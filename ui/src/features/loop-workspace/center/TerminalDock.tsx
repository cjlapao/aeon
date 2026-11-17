import { useCallback, useEffect, useRef, useState, type KeyboardEvent, type PointerEvent } from "react";
import { AppIcon } from "../../../components/icons";
import { TerminalTabs, type TerminalSession } from "../../../components/kit";
import { onTerminal, rpc, toRpcError, type TerminalNotification } from "../../../lib/bridge";

/** Terminal size limits. The upper bound follows the window, so the chat never vanishes.
 * The dock is its own height owner — `PageBodyLayout` (0.2.14) just sizes the
 * bottom zone to whatever it is given — so nothing else may clamp it. */
export const MIN_HEIGHT = 160;
const KEY_STEP = 24;
export const maxHeight = () => Math.max(MIN_HEIGHT, Math.round(window.innerHeight * 0.7));
const clamp = (h: number) => Math.min(maxHeight(), Math.max(MIN_HEIGHT, h));

type TerminalHandle = Parameters<NonNullable<React.ComponentProps<typeof TerminalTabs>["onReady"]>>[1];
interface Opened { terminalId: string; shell: string; cwd: string }
interface Listed extends Opened { exited?: boolean }
interface DockSession extends TerminalSession { terminalId: string }

/** The colours the terminal draws with: opaque, on the dock's own background, so text is never light-on-light. */
export const TERMINAL_COLORS = { background: "#0b1220", foreground: "#e2e8f0", cursor: "#38bdf8" };

const base = (path: string) => path.replace(/\/+$/, "").split("/").pop() || path;
const decode = (b64: string) => Uint8Array.from(atob(b64), (c) => c.charCodeAt(0));
const encode = (text: string) => new TextEncoder().encode(text);
/** First tab names its folder; later ones are numbered, like a desktop terminal's. */
const labelFor = (index: number, o: Opened) => (index === 0 ? `${base(o.shell)} · ${base(o.cwd)}` : `${base(o.shell)} ${index + 1}`);

export interface TerminalDockProps {
  componentRoot: string;
  open: boolean;
  height: number;
  onHeightChange(height: number): void;
  /** The ✕ on the panel — hides it. Terminals keep running in the daemon (LW9) and reattach on reopen. */
  onClose(): void;
}

/**
 * The bottom terminal: the kit's TerminalTabs wired to the daemon's
 * `terminal.*` methods. Output arrives as notifications on `aeon://terminal`
 * (never the event log, LW7); keystrokes and size go back as `terminal.input`
 * and `terminal.resize`. Opening the dock first looks for terminals this
 * project already has, so a window reload does not orphan a running shell.
 */
export function TerminalDock({ componentRoot, open, height, onHeightChange, onClose }: TerminalDockProps) {
  const [sessions, setSessions] = useState<DockSession[]>([]);
  const [active, setActive] = useState("");
  const [error, setError] = useState("");
  const handles = useRef(new Map<string, TerminalHandle>());
  const pending = useRef(new Map<string, Uint8Array[]>());
  const started = useRef(false);
  const opened = useRef(0);

  const fail = useCallback((cause: unknown) => setError(toRpcError(cause).message), []);

  const openOne = useCallback(async () => {
    try {
      const result = await rpc<Opened>("terminal.open", { componentRoot, cols: 80, rows: 24 });
      const session: DockSession = { id: result.terminalId, terminalId: result.terminalId, label: labelFor(opened.current, result), closable: true };
      opened.current += 1;
      setSessions((all) => [...all, session]);
      setActive(session.id);
      setError("");
    } catch (cause) { fail(cause); }
  }, [componentRoot, fail]);

  // Another project: the shells on screen belong to the old one. Drop them from the view (they keep
  // running in the daemon, LW9) and let the next open list this project's own. Declared before the
  // effect that opens one, so on the commit where the root changes the reset runs first.
  const root = useRef(componentRoot);
  useEffect(() => {
    if (root.current === componentRoot) return;
    root.current = componentRoot;
    handles.current.clear(); pending.current.clear();
    started.current = false; opened.current = 0;
    setSessions([]); setActive(""); setError("");
  }, [componentRoot]);

  useEffect(() => {
    if (!open || started.current) return;
    started.current = true;
    void (async () => {
      try {
        const existing = (await rpc<Listed[]>("terminal.list", { componentRoot })).filter((t) => !t.exited);
        if (existing.length === 0) { await openOne(); return; }
        const restored: DockSession[] = [];
        for (const t of existing) {
          const attached = await rpc<{ scrollback?: string }>("terminal.attach", { terminalId: t.terminalId });
          restored.push({ id: t.terminalId, terminalId: t.terminalId, label: labelFor(opened.current, t), closable: true,
            initialData: attached.scrollback ? decode(attached.scrollback) : undefined });
          opened.current += 1;
        }
        setSessions(restored);
        setActive(restored[0]!.id);
      } catch (cause) { fail(cause); }
    })();
  }, [open, componentRoot, openOne, fail]);

  // Output and exits. A chunk that beats its terminal's onReady is held and flushed there.
  useEffect(() => {
    if (!open) return;
    let disposed = false;
    let stop: (() => void) | undefined;
    void onTerminal((n: TerminalNotification) => {
      const handle = handles.current.get(n.terminalId);
      if ("data" in n) {
        const bytes = decode(n.data);
        if (handle) handle.write(bytes); else pending.current.set(n.terminalId, [...(pending.current.get(n.terminalId) ?? []), bytes]);
        return;
      }
      const how = n.signal ? `signal ${n.signal}` : `code ${n.exitCode ?? "unknown"}`;
      const line = `[process exited with ${how}]`;
      if (handle) handle.writeln(`\r\n${line}`); else pending.current.set(n.terminalId, [...(pending.current.get(n.terminalId) ?? []), encode(`\r\n${line}\r\n`)]);
      setSessions((all) => all.map((s) => (s.id === n.terminalId ? { ...s, readOnly: true, label: `${s.label} (exited)` } : s)));
    }).then((unlisten) => { if (disposed) unlisten(); else stop = unlisten; }, fail);
    return () => { disposed = true; stop?.(); };
  }, [open, fail]);

  const close = (id: string) => {
    void rpc("terminal.close", { terminalId: id }).catch(fail);
    handles.current.delete(id);
    pending.current.delete(id);
    setSessions((all) => {
      const rest = all.filter((s) => s.id !== id);
      setActive((current) => (current === id ? rest.at(-1)?.id ?? "" : current));
      return rest;
    });
  };

  // --- the drag handle ---
  const drag = useRef<{ y: number; h: number } | null>(null);
  const onPointerDown = (e: PointerEvent<HTMLDivElement>) => { e.currentTarget.setPointerCapture?.(e.pointerId); drag.current = { y: e.clientY, h: height }; };
  const onPointerMove = (e: PointerEvent<HTMLDivElement>) => { if (drag.current) onHeightChange(clamp(drag.current.h - (e.clientY - drag.current.y))); };
  const onPointerUp = () => { drag.current = null; };
  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (e.key === "ArrowUp") { e.preventDefault(); onHeightChange(clamp(height + KEY_STEP)); }
    if (e.key === "ArrowDown") { e.preventDefault(); onHeightChange(clamp(height - KEY_STEP)); }
  };

  if (!open) return null;
  return <div data-testid="terminal-dock" className="relative flex shrink-0 flex-col border-t border-slate-800 bg-[#0b1220]" style={{ height }}>
    <div role="separator" aria-orientation="horizontal" aria-label="Resize terminal" aria-valuemin={MIN_HEIGHT} aria-valuenow={height} aria-valuemax={maxHeight()}
      tabIndex={0} onPointerDown={onPointerDown} onPointerMove={onPointerMove} onPointerUp={onPointerUp} onKeyDown={onKeyDown}
      className="absolute inset-x-0 -top-1 z-10 flex h-2 cursor-row-resize items-center justify-center">
      <span className="h-1 w-10 rounded-full bg-slate-500/60" />
    </div>
    <button type="button" aria-label="Close terminal panel" onClick={onClose}
      className="absolute right-2 top-1.5 z-10 rounded p-1 text-slate-400 hover:bg-white/10 hover:text-white"><AppIcon name="cross" /></button>
    {error && <p role="alert" className="border-b border-red-900/40 bg-red-950/40 px-3 py-1.5 font-mono text-xs text-red-300">{error}</p>}
    {/* The terminal paints itself on this dock's own dark colour and sizes against an absolutely placed box, so it is
        visible whatever the app theme is and whatever a webview does with a percentage height inside a flex item. */}
    <div className="relative min-h-0 flex-1">
      <div className="absolute inset-0">
      <TerminalTabs scheme="dark" terminalTheme={TERMINAL_COLORS} sessions={sessions} value={active} onValueChange={setActive} height="100%"
        onAdd={() => void openOne()} onClose={close}
        onData={(id, data) => void rpc("terminal.input", { terminalId: id, data }).catch(fail)}
        onResize={(id, size) => void rpc("terminal.resize", { terminalId: id, cols: size.cols, rows: size.rows }).catch(fail)}
        onReady={(id, handle) => {
          handles.current.set(id, handle);
          for (const chunk of pending.current.get(id) ?? []) handle.write(chunk);
          pending.current.delete(id);
        }}
        onError={(_id, cause) => fail(cause)}
        emptyState={<button type="button" onClick={() => void openOne()} className="m-4 rounded-lg border border-slate-600 px-3 py-1.5 text-sm text-slate-200 hover:bg-white/10">Open a terminal</button>} />
      </div>
    </div>
  </div>;
}
