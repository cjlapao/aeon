import { lazy, Suspense } from "react";
import type { LiveController, LiveState } from "../../lib/live";

const HistoryScreen = lazy(() => import("../projects/history/HistoryScreen").then((module) => ({ default: module.HistoryScreen })));

/** History tab body — the existing history screen, loaded only when its tab is opened. */
export default function HistoryLazy({ controller, live, canWrite, initialRoot = "" }: { controller: LiveController; live: LiveState; canWrite: boolean; initialRoot?: string }) {
  return <Suspense fallback={<p>…</p>}><HistoryScreen controller={controller} live={live} canWrite={canWrite} initialRoot={initialRoot} /></Suspense>;
}
