import { useEffect, useMemo, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { Alert } from "../../components/kit";
import { rpc } from "../../lib/bridge";
import type { LiveController, LiveState } from "../../lib/live";
import type { RoutesListResult } from "../../lib/protocol";
import type { ContextLoopRow } from "../../lib/contextPickers";
import { useChatActions } from "./center/useChatActions";
import { useGateDiffs } from "./center/useGateDiffs";
import { useGitStatus } from "./center/useGitStatus";
import { useGitChanges } from "./center/useGitChanges";
import { LoopWorkspaceView } from "./LoopWorkspaceView";
import { useLoopWorkspace } from "./useLoopWorkspace";

/**
 * The loop page, with live data: one data owner (`useLoopWorkspace`) plus the
 * git state, gate diffs and the chat's RPC wiring, handed to the view that
 * draws them. Everything about how it looks is in `LoopWorkspaceView`.
 */
export function LoopWorkspaceScreen({ controller, live }: { controller: LiveController; live: LiveState; canWrite: boolean }) {
  const { id = "" } = useParams();
  const navigate = useNavigate();
  const ws = useLoopWorkspace(id, live, controller);
  const { loop, session, items } = ws;
  const componentRoot = loop?.root ?? "";

  const [routes, setRoutes] = useState<{ value: string; label: string; reasoningEfforts?: string[] | null; thinking?: boolean }[]>([]);
  useEffect(() => {
    if (!ws.connected) return;
    void rpc<RoutesListResult>("routes.list").then((r) => setRoutes((r.routes ?? []).map((x) => ({ value: x.id, label: x.displayName || `${x.provider} · ${x.model}`, reasoningEfforts: x.reasoningEfforts, thinking: Boolean(x.capabilities?.includes('thinking') || x.wireCapabilities?.includes('thinking')) })))).catch(() => undefined);
  }, [ws.connected]);

  // S77: the `//` picker's world — every loop the daemon lists, so a person
  // mid-turn can hop to a sibling loop by name. One read per connection; a
  // failure leaves the panel speaking its honest empty, never a stale list.
  const [contextLoops, setContextLoops] = useState<readonly ContextLoopRow[]>([]);
  useEffect(() => {
    if (!ws.connected) return;
    void rpc<{ loops?: Array<{ id: string; slug: string; title?: string; status: string; componentRoot: string }> }>("loop.list", { all: true })
      .then((r) => setContextLoops((r.loops ?? []).map((l) => ({ id: l.id, slug: l.slug, title: l.title, status: l.status, componentRoot: l.componentRoot }))))
      .catch(() => undefined);
  }, [ws.connected]);

  const host = useMemo(() => ({ loopId: id, session, rememberSteer: ws.rememberSteer, refresh: ws.refresh }), [id, session, ws.rememberSteer, ws.refresh]);
  // The view turns "view diff" into an inspector tab, so the diff callback here is never used.
  const chat = useChatActions(host, componentRoot, () => undefined);
  const chatItems = useGateDiffs(items, componentRoot);
  const toolEnds = useMemo(() => ws.events.filter((e) => e.kind === "step.tool.end").length, [ws.events]);
  const git = useGitStatus(componentRoot, toolEnds);
  const gitChanges = useGitChanges(componentRoot, toolEnds);

  if (ws.error && !loop) return <div className="p-6"><Alert tone="red" title="Loop unavailable" description={ws.error} /></div>;
  return <LoopWorkspaceView loopId={id} onRefresh={ws.refresh} todos={ws.todos} onTodos={ws.setTodos} loop={loop} live={live} connected={ws.connected} items={chatItems} session={session} steps={ws.steps} git={git} gitChanges={gitChanges}
    chat={chat} routes={routes} componentRoot={componentRoot} onClassic={() => navigate(`/loops/${id}/classic`)}
    contextLoops={contextLoops} onOpenLoop={(lid) => navigate(`/loops/${encodeURIComponent(lid)}`)}
    onLoadOlder={ws.loadOlder} loadingOlder={ws.loadingOlder} hasOlder={ws.hasOlder} />;
}
