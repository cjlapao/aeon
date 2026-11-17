import { createElement, lazy, Suspense, useContext, useEffect, type ComponentType } from "react";
import { Spinner } from "../../../components/kit";
import { beginWait, PageWaitOwnerContext } from "../../../lib/pageWait";
import type { ConfigPageContext } from "./configRegistry";

/**
 * S72 CF2: the one way a page file turns a section component into a
 * registration `render` — lazy (Configuration never enlarges the initial
 * bundle), suspended behind the kit's loader, and fed the shared context props.
 *
 * `pick` narrows what the section receives: every page gets
 * `{ controller, live, canWrite }` as SettingsScreen passed them, and a
 * section that never took `canWrite` (ToolsSection today) keeps exactly its
 * old props by passing a picker. A `.ts` file on purpose — createElement, so
 * page files stay plain TypeScript per the CF3 naming.
 */
// eslint-disable-next-line @typescript-eslint/no-explicit-any
type AnySection = ComponentType<any>;

/**
 * What a page looks like while its chunk is still arriving, and the moment
 * the page's wait count starts.
 *
 * The chunk is the smaller wait — seconds of data are what people feel — but
 * it is a real one, and a page that is not on screen yet has to say it is on
 * the way just the same. The kit's `Spinner` is the same loader the menu row
 * wears, which is the point: one visual word for "this is working", from the
 * row you clicked to the panel it opened.
 */
function ConfigOpening() {
  const owner = useContext(PageWaitOwnerContext);
  // The effect's return value IS the release: the fallback unmounts the
  // instant the chunk lands, and that is exactly when the wait ends.
  useEffect(() => beginWait(owner, "chunk"), [owner]);
  return createElement(Spinner, { size: "xs", label: "Opening…" });
}

export function lazyConfigRender(
  loader: () => Promise<{ default: AnySection }>,
  pick: (ctx: ConfigPageContext) => Record<string, unknown> = (ctx) => ({ ...ctx }),
) {
  const Lazy = lazy(loader);
  return (ctx: ConfigPageContext) =>
    createElement(Suspense, { fallback: createElement(ConfigOpening) },
      createElement(Lazy, pick(ctx)));
}
