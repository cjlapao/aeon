import { render } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useLocation } from "react-router-dom";
import { I18nProvider } from "@cjlapao/ui-kit";
import { ConfigScreen } from "../src/features/config/ConfigScreen";
import { LiveController, type LiveState } from "../src/lib/live";
import { APP_LOCALES } from "../src/lib/i18n";

// S72 T2 (CF10): one way to mount a Configuration page at its route, used by
// the per-page smoke test and by the section tests whose old assertion was
// "mounted inside SettingsScreen" — the mount moved, and this is where it
// lives now. The registration side effect comes from pages/registerAll, the
// same file app/routes.ts imports in the product.
import "../src/features/config/pages/registerAll";

/**
 * Mounts `<ConfigScreen>` at `/config/<page>` with the given connection.
 * `canWrite` defaults to the protocol rule the shell applies (`live.state
 * === "current"`), matching router.tsx.
 */
/** Where a page that moved into Work lands: the test reads the address it was sent to. */
function MovedTo() { return <div data-testid="moved-to">{useLocation().pathname}</div>; }

export function mountConfigPage(
  page: string,
  live: LiveState,
  opts: { controller?: LiveController; canWrite?: boolean } = {},
) {
  const controller = opts.controller ?? new LiveController();
  return render(
    <I18nProvider locales={APP_LOCALES}>
      <MemoryRouter initialEntries={[`/config/${page}`]}>
        <Routes>
          <Route path="/config/:page?" element={
            <ConfigScreen controller={controller} live={live} canWrite={opts.canWrite ?? live.state === "current"} />
          } />
          <Route path="/work/*" element={<MovedTo />} />
        </Routes>
      </MemoryRouter>
    </I18nProvider>,
  );
}

/** A current-connection LiveState for mounts that must behave as connected. */
export const configLiveCurrent: LiveState = {
  state: "current", endpoint: "/run/user/1000/aeon/rpc.sock", daemonVersion: "1.0", protocolVersion: 1, generation: 1,
};
