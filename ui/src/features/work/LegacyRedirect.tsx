import { Navigate, useLocation, useParams, useSearchParams } from "react-router-dom";
import type { LiveController, LiveState } from "../../lib/live";
import { useRpcQuery } from "../../lib/useRpcQuery";
import { legacyTarget, workPath, type WorkComponent } from "./workModel";

/**
 * The old list pages (Loops, Projects, Components, History, Roadmap, Goals) now live inside Work.
 * A bookmark or a link in a daemon message to one of them lands on the matching tab, scoped to the
 * component the old `?root=` or `/components/:id` named. The redirect waits for the component list
 * only when it has a `root` to resolve; otherwise it goes at once.
 */
export function LegacyRedirect({ routeId, controller, live }: { routeId: string; controller: LiveController; live: LiveState }) {
  const [search] = useSearchParams();
  const { id = "" } = useParams();
  const location = useLocation();
  const root = search.get("root") || search.get("componentRoot") || "";
  const needsLookup = root !== "";
  const list = useRpcQuery("component.list", {}, controller, live);
  if (needsLookup && list.data === undefined && !list.error) return <p>…</p>;
  const rows = ((list.data as { components?: unknown } | undefined)?.components ?? []) as WorkComponent[];
  const resolved = id || rows.find((component) => component.root === root)?.id || "";
  const target = (routeId === "component" ? workPath(resolved, "overview") : legacyTarget(routeId, resolved)) ?? "/work";
  return <Navigate replace to={target} state={{ from: location.pathname }} />;
}
