// useComponentRoots — the projects folder's own ROOT-COMPONENT read.
//
// S53 T5. `component.list` was spelled in `ProjectsScreen.tsx` alone; the
// Projects → History screen needs the same list to offer its scope picker,
// and a second copy of the literal would be a second owner of one door. So
// the read lives here, one file, one literal, and both surfaces call it:
// the screen keeps its own presentation, the hook keeps the wire shape.
//
// It is NOT inside `history/` on purpose: the History surface's guard
// (`history/historyRpc.guard.test.ts`) pins its folder to exactly the five
// `history.*` doors, and a component door inside that folder would either
// widen the allow-list beyond what the History slice owns or push the folder
// to spell a door twice. roadmap's guard carries the same note for S49's
// blueprint picker — a borrowed door stays owned by its own folder.
//
// A "project" is a root component (S44): `parentId` empty means the row is a
// top-level root. `ProjectsScreen.tsx` filters the same way, locally.

import { useMemo } from "react";
import { useRpcQuery } from "../../lib/useRpcQuery";
import type { LiveController, LiveState } from "../../lib/live";
import type { RpcMethods } from "../../proto/contract";

type Component = RpcMethods["component.list"]["result"]["components"][number];

/** The whole component list, as `component.list` answered it. */
export function useComponentList(controller: LiveController, live: LiveState) {
  const query = useRpcQuery("component.list", {}, controller, live);
  const components: Component[] = useMemo(() => {
    const list = (query.data as { components?: unknown } | undefined)?.components;
    return Array.isArray(list) ? (list as Component[]) : [];
  }, [query.data]);
  return { ...query, components };
}

/** The roots of that list — the scopes a person can point a view at. */
export function useComponentRoots(controller: LiveController, live: LiveState) {
  const query = useComponentList(controller, live);
  const roots = useMemo(() => query.components.filter((component) => !component.parentId), [query.components]);
  return { ...query, roots };
}
