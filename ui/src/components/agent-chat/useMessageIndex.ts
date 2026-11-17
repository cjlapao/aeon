import { useEffect, useLayoutEffect, useRef, useState, type RefObject } from "react";
import type { LoopMessageIndexEntry, LoopMessageIndexProps } from "./MessageIndex";

export function useMessageIndex(index: LoopMessageIndexProps | undefined, scroller: RefObject<HTMLDivElement | null>, onStart: () => void, onLatest: () => void) {
  const nodes = useRef(new Map<string, HTMLDivElement>());
  const request = useRef<AbortController | null>(null);
  const anchor = useRef<{ itemId: string; entryId: string | null; latest: boolean } | null>(null);
  const historical = useRef(false);
  const remoteWindow = useRef(false);
  const [activeId, setActiveId] = useState<string | null>(null);
  const [activeItemId, setActiveItemId] = useState<string | null>(null);
  const active = useRef<string | null>(null);
  const [pendingId, setPendingId] = useState<string | null>(null);
  const [restoring, setRestoring] = useState(false);
  const [error, setError] = useState("");
  const [away, setAway] = useState(false);
  const [revision, setRevision] = useState(0);
  const latestProps = useRef({ index, onStart, onLatest });
  latestProps.current = { index, onStart, onLatest };
  const markActive = (id: string | null) => {
    if (active.current === id) return;
    active.current = id;
    setActiveId(id);
    latestProps.current.index?.onActiveChange?.(id);
  };
  useLayoutEffect(() => {
    request.current?.abort(); request.current = null; anchor.current = null;
    historical.current = false; remoteWindow.current = false;
    setPendingId(null); setRestoring(false); setError(""); setAway(false); setActiveItemId(null); markActive(null);
    return () => { request.current?.abort(); request.current = null; anchor.current = null; };
  }, [index?.scopeKey]);

  const start = () => {
    request.current?.abort();
    const controller = new AbortController();
    request.current = controller; anchor.current = null;
    historical.current = true; setAway(true); setError("");
    setRestoring(false); setRevision((value) => value + 1);
    latestProps.current.onStart();
    return controller;
  };
  const fail = (controller: AbortController, cause: unknown) => {
    if (controller.signal.aborted || request.current !== controller) return;
    controller.abort();
    request.current = null; anchor.current = null; setPendingId(null); setRestoring(false);
    setError(cause instanceof Error ? cause.message : String(cause));
  };
  const jump = async (entry: LoopMessageIndexEntry) => {
    const controller = start();
    setPendingId(entry.id);
    const itemId = entry.targetItemId ?? entry.id;
    if (nodes.current.has(itemId)) {
      anchor.current = { itemId, entryId: entry.id, latest: false };
      setRevision((value) => value + 1);
      return;
    }
    const config = latestProps.current.index;
    if (!config?.onRequestJump) { fail(controller, new Error("This interaction is outside the loaded history.")); return; }
    // The host may replace the window even if this request later fails.
    remoteWindow.current = true;
    try {
      const count = (value: number | undefined) => Number.isFinite(value) ? Math.min(100, Math.max(0, Math.floor(value!))) : 20;
      const result = await config.onRequestJump(entry, { before: count(config.before), after: count(config.after), signal: controller.signal });
      if (controller.signal.aborted || request.current !== controller) return;
      if (!result.targetItemId) throw new Error("History loader returned no target item.");
      anchor.current = { itemId: result.targetItemId, entryId: entry.id, latest: false };
      setRevision((value) => value + 1);
    } catch (cause) { fail(controller, cause); }
  };
  const goLatest = async () => {
    const needsWindow = remoteWindow.current || Boolean(latestProps.current.index?.onRequestLatest);
    const controller = start(); setPendingId(null); setRestoring(true);
    if (!needsWindow) {
      const element = scroller.current;
      if (element) element.scrollTop = element.scrollHeight;
      historical.current = false; setAway(false); setPendingId(null); setRestoring(false); request.current = null;
      setActiveItemId(null);
      markActive(null);
      latestProps.current.onLatest();
      return;
    }
    const loader = latestProps.current.index?.onRequestLatest;
    if (!loader) { fail(controller, new Error("Restoring the live tail requires onRequestLatest.")); return; }
    try {
      const result = await loader({ signal: controller.signal });
      if (controller.signal.aborted || request.current !== controller) return;
      if (!result.targetItemId) throw new Error("Latest-history loader returned no target item.");
      anchor.current = { itemId: result.targetItemId, entryId: null, latest: true };
      setRevision((value) => value + 1);
    } catch (cause) { fail(controller, cause); }
  };

  // Ref callbacks have run by this point, including after a host commits a new window.
  useLayoutEffect(() => {
    const target = anchor.current;
    const element = scroller.current;
    if (!target || !element) return;
    const node = nodes.current.get(target.itemId);
    if (!node) return;
    element.scrollTop = target.latest ? element.scrollHeight : Math.max(0, element.scrollTop + node.getBoundingClientRect().top - element.getBoundingClientRect().top - 12);
    anchor.current = null; request.current = null; setPendingId(null); setRestoring(false);
    if (target.latest) {
      historical.current = false; remoteWindow.current = false; setAway(false);
      setActiveItemId(null);
      markActive(null);
      latestProps.current.onLatest();
    } else { setActiveItemId(target.itemId); markActive(target.entryId); }
  });
  // A successful loader must eventually commit its promised target; never spin forever.
  useEffect(() => {
    const controller = request.current;
    if ((!pendingId && !restoring) || !controller) return;
    const timer = setTimeout(() => fail(controller, new Error("The requested history target did not become available. Try again.")), 10000);
    return () => clearTimeout(timer);
  }, [pendingId, restoring, revision]);

  const syncVisible = () => {
    if (request.current || !scroller.current) return;
    const top = scroller.current.getBoundingClientRect().top + 72;
    let nearest: string | null = null;
    let distance = Infinity;
    for (const entry of latestProps.current.index?.entries ?? []) {
      const node = nodes.current.get(entry.targetItemId ?? entry.id);
      if (!node) continue;
      const next = Math.abs(node.getBoundingClientRect().top - top);
      if (next < distance) { distance = next; nearest = entry.id; }
    }
    markActive(nearest);
  };
  return { nodes, activeId, activeItemId, pendingId, restoring, error, away, historical, navigating: request, jump, goLatest, syncVisible };
}
