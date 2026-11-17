import { createContext, useCallback, useContext, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";

/**
 * The app header's slot — how a page puts controls into the fixed header.
 *
 * The header is chrome the shell owns; an ordinary page adds nothing to it.
 * The loop workspace needs its breadcrumb and nav there, so it renders
 * <HeaderPortal> and the content lands in the header's <HeaderSlot>. Two
 * sides ("start" beside the wordmark, "end" beside the connection status)
 * because the header has two regions. Unmounting the page removes its
 * content — the portal IS the content, nothing is copied into state.
 */
export type HeaderSide = "start" | "end";

interface Targets { start: HTMLElement | null; end: HTMLElement | null; setTarget(side: HeaderSide, el: HTMLElement | null): void }

const HeaderSlotContext = createContext<Targets | null>(null);

export function HeaderSlotProvider({ children }: { children: ReactNode }) {
  const [start, setStart] = useState<HTMLElement | null>(null);
  const [end, setEnd] = useState<HTMLElement | null>(null);
  // Targets live in state (not refs) so a portal re-renders when its target mounts.
  const setTarget = useCallback((side: HeaderSide, el: HTMLElement | null) => (side === "start" ? setStart(el) : setEnd(el)), []);
  return <HeaderSlotContext.Provider value={{ start, end, setTarget }}>{children}</HeaderSlotContext.Provider>;
}

/** Where portalled content lands. Rendered by the shell, once per side. */
export function HeaderSlot({ side }: { side: HeaderSide }) {
  const setTarget = useContext(HeaderSlotContext)?.setTarget;
  // A stable callback ref: an inline one is a new function every render, so React would detach and
  // re-attach it (null, then the element) on each render and churn the targets' state for nothing.
  const ref = useCallback((el: HTMLDivElement | null) => setTarget?.(side, el), [setTarget, side]);
  return <div data-header-slot={side} className="flex min-w-0 items-center gap-2" ref={ref} />;
}

/** Puts children in the header. Renders nothing until the slot exists, and outside a provider. */
export function HeaderPortal({ side, children }: { side: HeaderSide; children: ReactNode }) {
  const targets = useContext(HeaderSlotContext);
  const target = targets?.[side] ?? null;
  return target ? createPortal(children, target) : null;
}
