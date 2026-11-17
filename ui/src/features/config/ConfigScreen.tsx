import { Link, Navigate, useParams } from "react-router-dom";
import { Alert, Panel } from "../../components/kit";
import { TabBoundary } from "../../components/TabBoundary";
import { PageWaitOwnerContext } from "../../lib/pageWait";
import { firstReadyConfigPage, getConfigPage, type ConfigPageContext, type RegisteredConfigPage } from "./lib/configRegistry";
import { ConfigPageFrame } from "./ConfigPageFrame";

/**
 * S72 CF5: the in-shell not-found for an unknown or planned page id. It
 * names the truth (no page / planned) and links to the first ready page —
 * never a blank, never a redirect that hides the typo.
 */
function ConfigPageMissing({ page, def }: { page: string; def?: RegisteredConfigPage }) {
  const first = firstReadyConfigPage();
  return (
    <Panel tone="amber" data-testid="config-page-missing" className="m-4 md:m-7">
      <h1 className="text-xl font-semibold">Page not found</h1>
      <p className="mt-2 text-sm">
        {def?.status === "planned"
          ? `${def.label} is planned — its page is not wired yet. ${def.summary ?? ""}`
          : `“${page}” is not a configuration page.`}
      </p>
      {first && (
        <Link className="aeon-accent-text mt-3 inline-block underline" to={`/config/${first.id}`}>
          Go to {first.label} →
        </Link>
      )}
    </Panel>
  );
}

/**
 * The ready page, one level under the boundary: `def.render(...)` runs while
 * THIS component renders, so a throwing page is caught by the `TabBoundary`
 * above rather than unwinding the route — calling it in the parent's own JSX
 * lets the throw escape the boundary entirely (measured in
 * test/ConfigScreen.test.tsx).
 *
 * It also names the page as the owner of whatever it waits for
 * (`PageWaitOwnerContext`, lib/pageWait). Context rather than a value set from
 * an effect, because a child's effects run BEFORE its parent's — by the time
 * this component could have announced itself, the page's first queries would
 * already have started unattributed, and the menu row would sit there looking
 * idle through the exact seconds it exists to report. The id is the menu row's
 * own slug (`config:<page id>`, PageLayout), so a page reports into the row
 * the user clicked without either side importing the other.
 */
function ConfigPageBody({ def, ctx }: { def: RegisteredConfigPage; ctx: ConfigPageContext }) {
  return (
    <PageWaitOwnerContext.Provider value={`config:${def.id}`}>
      <ConfigPageFrame def={def}>{def.render?.(ctx)}</ConfigPageFrame>
    </PageWaitOwnerContext.Provider>
  );
}

/**
 * S72 CF5–CF7: the body of `/config/:page?`.
 *
 * No page redirects to the first ready page; an unknown or planned id gets
 * the in-shell not-found; a ready page renders in the frame inside its own
 * `TabBoundary`, keyed by id so navigating between pages clears one page's
 * crash rather than pinning the shell to it. A page that needs a connection
 * and has none shows the shell's one not-connected state (TB12), never an
 * empty or zero page.
 */
export function ConfigScreen({ controller, live, canWrite }: ConfigPageContext) {
  const { page } = useParams();

  if (!page) {
    const first = firstReadyConfigPage();
    return first ? <Navigate replace to={`/config/${first.id}`} /> : <ConfigPageMissing page="" />;
  }

  const def = getConfigPage(page);
  if (!def || def.status === "planned") return <ConfigPageMissing page={page} def={def} />;
  if (def.movedTo) return <Navigate replace to={def.movedTo} />;
  if (def.needsConnection && live.state !== "current") {
    return (
      <Alert
        tone="amber"
        title="Not connected"
        description="There is nothing to configure or call. The Status page names the path it tried and offers Reconnect."
        data-testid="tab-needs-connection"
      />
    );
  }

  return (
    <TabBoundary key={def.id} name={def.label}>
      <ConfigPageBody def={def} ctx={{ controller, live, canWrite }} />
    </TabBoundary>
  );
}
