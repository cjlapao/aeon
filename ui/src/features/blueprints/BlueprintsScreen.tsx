import { useEffect, useMemo, useRef, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { Button } from "../../components/kit";
import { useRpcQuery } from "../../lib/useRpcQuery";
import type { LiveController, LiveState } from "../../lib/live";
import type { RpcMethods } from "../../proto/contract";
import { ProposalsDesk } from "../settings/policy/ProposalsDesk";
import { FlowGallery, NewFlowDialog } from "./builder/FlowLibrary";
import { FlowWorkspace } from "./builder/FlowWorkspace";
import { BLUEPRINT_PROPOSAL_KINDS, listRows } from "./blueprintsModel";

// BlueprintsScreen.tsx — the Flows page.
//
// Two places, never both at once: the library (every flow as a card that says
// what it is for) and one open flow (its builder, source, history and users).
// The side menu lists flows too, as navigation; this page does not repeat that
// list as a second one — with no flow open it is a gallery you can search and
// read, with one open it is the workspace.
//
// What the page owns: which flow is open (`?name=`, shared with the side menu),
// the new-flow dialog, and the proposals pile narrowed to this slice's kind. It
// answers nothing about a flow itself: the workspace asks the daemon and shows
// what comes back, so there is no verdict here a save could be guessed from.

export interface BlueprintsScreenProps {
  controller: LiveController;
  live: LiveState;
  /** @default `live.state === "current"` — writes are refused while not current. */
  canWrite?: boolean;
}

export function BlueprintsScreen({ controller, live, canWrite = live.state === "current" }: BlueprintsScreenProps) {
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const open = params.get("name") ?? "";
  const [view, setView] = useState<"flows" | "proposals">("flows");
  const [creating, setCreating] = useState(false);
  // The workspace holds the draft; the page only needs to know one is unsaved, so it can ask before leaving it.
  const dirtyRef = useRef(false);

  const [filters, setFilters] = useState({ origin: "", q: "" });
  const list = useRpcQuery("blueprint.list", filters as RpcMethods["blueprint.list"]["params"], controller, live);
  const rows = useMemo(() => listRows(list.data), [list.data]);

  const go = (name: string) => {
    if (dirtyRef.current && name !== open && !window.confirm("Leave this flow? Your unsaved changes will be lost.")) return;
    dirtyRef.current = false;
    navigate(name === "" ? "/blueprints" : `/blueprints?name=${encodeURIComponent(name)}`, { replace: true });
  };

  // Leaving the page with a draft open asks first; the browser's own prompt is the only door out of a window close.
  useEffect(() => {
    const warn = (event: BeforeUnloadEvent) => { if (dirtyRef.current) event.preventDefault(); };
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, []);

  return (
    <section className="flex h-full min-h-0 w-full min-w-0 max-w-full flex-col gap-4 overflow-x-hidden" data-testid="product-blueprints">
      {(view === "proposals" || open === "") && <div className="flex shrink-0 flex-wrap items-end justify-between gap-3">
        <div>
          <p className="text-xs uppercase tracking-widest aeon-accent-text">Flow library</p>
          <h1 className="mt-1 text-3xl font-semibold">Flows</h1>
          <p className="mt-1 text-sm text-slate-400">The automation behind every loop: lanes that run in order, who does each one, and where a rejection sends the work.</p>
        </div>
        <Button variant="outline" size="sm" onClick={() => setView(view === "flows" ? "proposals" : "flows")} data-testid="proposals-toggle">
          {view === "flows" ? "Proposals" : "Back to flows"}
        </Button>
      </div>}

      {/* Only the library scrolls as a page; an open flow hands its height to the builder. */}

      <div className="relative min-h-0 flex-1"><div className={`absolute inset-0 ${open !== "" && view === "flows" ? "" : "overflow-y-auto"}`}>
      {view === "proposals"
        ? <ProposalsDesk controller={controller} live={live} canWrite={canWrite} kinds={BLUEPRINT_PROPOSAL_KINDS} />
        : open === ""
          ? <FlowGallery rows={rows} loading={list.loading} error={list.error} onRetry={list.refetch} onOpen={go} onNew={() => setCreating(true)} canWrite={canWrite}
              q={filters.q} origin={filters.origin} onFilter={(f) => setFilters((current) => ({ ...current, ...f }))} />
          : <FlowWorkspace key={open} name={open} controller={controller} live={live} canWrite={canWrite}
              onBack={() => go("")} onChanged={list.refetch} onDuplicated={go} onDeleted={() => { dirtyRef.current = false; go(""); }}
              onDirtyChange={(dirty) => { dirtyRef.current = dirty; }} />}
      </div></div>

      {creating && <NewFlowDialog existing={rows.map((r) => r.name)} onClose={() => setCreating(false)}
        onCreated={(name) => { setCreating(false); list.refetch(); go(name); }} />}
    </section>
  );
}
