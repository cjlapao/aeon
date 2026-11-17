import { useEffect, useRef, useState, type ComponentProps, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useAppSettings } from "../lib/appSettings";
import { SidePanel } from "./kit";

// The app's one right-edge detail panel: a floating-glass, resizable `SidePanel`
// opened as an OVERLAY in a portal, the way the components page opens its form.
//
// # Why overlay, and not `docked`
//
// `docked` sits in normal flow and wants a layout zone (a `PageBodyLayout` cell)
// to live in. On a page without one the panel lands under the content, at the
// bottom. An overlay in a portal needs no zone.
//
// # Why `open` is state and not a constant `true`
//
// The kit animates a panel only when `isOpen` FLIPS: a panel already open on its
// first render appears at full width, and one unmounted the moment it closes
// never plays its slide out. So this shell mounts closed, opens on the next
// frame, and on close flips `isOpen` first and tells the page only after the
// slide has had time to run.
//
// # Vetoing a close
//
// `beforeClose` runs first. Returning `false` means the caller handled the close
// itself (an unsaved-draft question, say) and no animation starts — the panel
// stays open until the caller unmounts it.

export interface OverlaySidePanelProps {
  title: ReactNode;
  /** Classes for the title, for a panel that wants it bigger than the kit's default. */
  titleClassName?: string;
  subtitle?: ReactNode;
  onClose: () => void;
  beforeClose?: () => boolean;
  width?: number;
  minWidth?: number;
  maxWidth?: number;
  resizable?: boolean;
  /** The panel's surface. The app's default is the floating glass; a plain form panel asks for `simple`. */
  variant?: ComponentProps<typeof SidePanel>["variant"];
  testId?: string;
  children: ReactNode;
}

/** A little longer than the kit's slide, so the page unmounts after it ends. */
const SLIDE_OUT_MS = 380;

export function OverlaySidePanel({
  title, titleClassName, subtitle, onClose, beforeClose, width = 620, minWidth = 380, maxWidth = 900, resizable = true, variant = "floating-glass", testId, children,
}: OverlaySidePanelProps) {
  const { controlTone, surfaceTone } = useAppSettings();
  const [open, setOpen] = useState(false);
  const closing = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => {
    const frame = requestAnimationFrame(() => setOpen(true));
    return () => {
      cancelAnimationFrame(frame);
      if (closing.current !== undefined) clearTimeout(closing.current);
    };
  }, []);
  const close = () => {
    if (closing.current !== undefined) return;
    if (beforeClose && !beforeClose()) return;
    setOpen(false);
    closing.current = setTimeout(onClose, SLIDE_OUT_MS);
  };

  const viewport = typeof window === "undefined" ? 1280 : window.innerWidth;
  const w = Math.min(width, viewport - 16);
  const body = testId ? <div data-testid={testId}>{children}</div> : children;
  return createPortal(
    <div className="fixed inset-0 z-50" style={{ pointerEvents: "auto" }}>
      <SidePanel
        isOpen={open}
        overlay
        onClose={close}
        onKeyDown={(event) => {
          if (event.key === "Escape") close();
        }}
        title={titleClassName === undefined ? title : <span className={titleClassName}>{title}</span>}
        subtitle={subtitle}
        side="right"
        inset="sm"
        variant={variant}
        tone={controlTone}
        surfaceTone={surfaceTone}
        width={w}
        resizable={resizable}
        minWidth={Math.min(minWidth, w)}
        maxWidth={Math.max(w, Math.min(maxWidth, viewport - 16))}
      >
        <div className="p-4">
        {body}
        </div>
      </SidePanel>
    </div>,
    document.body,
  );
}
