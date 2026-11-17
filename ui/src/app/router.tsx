import { lazy, Suspense } from "react";
import { HashRouter, Link, useOutletContext, useParams, useRoutes } from "react-router-dom";
import { I18nProvider, ToastProvider } from "@cjlapao/ui-kit";
import { APP_LOCALES } from "../lib/i18n";
import { Alert, Panel } from "../components/kit";
import { TabBoundary } from "../components/TabBoundary";
import { BlueprintsScreen } from "../features/blueprints/BlueprintsScreen";
import { LoopWorkspaceScreen } from "../features/loop-workspace/LoopWorkspaceScreen";
import { LoopScreen } from "../features/loops/pages/LoopScreen";
import { StepScreen } from "../features/loops/pages/StepScreen";
import { HomeScreen } from "../features/home/pages/HomeScreen";
import { WorkScreen } from "../features/work/WorkScreen";
import { LegacyRedirect } from "../features/work/LegacyRedirect";
import { SettingsScreen } from "../features/settings/SettingsScreen";
import { SystemScreen } from "../features/system/SystemScreen";
import { ProductShell, type AppRouteContext } from "./App";
import { routeAccess } from "./routeAccess";
import { APP_ROUTES, isBenchPage, type AppRoute } from "./routes";

const ModelsScreen = lazy(() => import("../features/models/ModelsScreen").then((module) => ({ default: module.ModelsScreen })));
const ModelEditor = lazy(() => import("../features/models/ModelEditor").then((module) => ({ default: module.ModelEditor })));
const TestBench = lazy(() => import("../features/test-bench/TestBench").then((module) => ({ default: module.TestBench })));
// S26 T6 — the forward Gantt, lazy like the surfaces a person visits
// deliberately rather than lands on.
// S72 — Configuration, lazy so the shell's pages never enlarge the initial
// bundle (CF2); the individual pages lazy-load again behind it.
const ConfigScreen = lazy(() => import("../features/config/ConfigScreen").then((module) => ({ default: module.ConfigScreen })));

function NotFound() {
  return <Panel tone="amber"><h1 className="text-xl font-semibold">Page not found</h1>
    <p className="mt-2 text-sm">This address is not part of Aeon.</p>
    <Link className="aeon-accent-text mt-3 inline-block underline" to="/work">Back to Work →</Link></Panel>;
}

/** Route access runs before a page is mounted; later auth rules have one entry point. */
function RoutePage({ route }: { route: AppRoute }) {
  const context = useOutletContext<AppRouteContext>();
  const { controller, live, canWrite, benchConnection } = context;
  const { page: benchPage, id } = useParams();
  const decision = routeAccess(route, live);
  if (!decision.allowed) return <Alert tone="amber" title={`${route.title} unavailable`} description={decision.reason} />;
  if (route.id === "bench" && benchPage && !isBenchPage(benchPage)) return <NotFound />;

  let page: React.ReactNode;
  switch (route.id) {
    case "home": page = <HomeScreen controller={controller} live={live} canWrite={canWrite} />; break;
    case "work": page = <WorkScreen controller={controller} live={live} canWrite={canWrite} />; break;
    // The old list pages moved into Work; their addresses redirect to the matching tab.
    case "loops": case "projects": case "components": case "component": case "history": case "roadmap": case "goals":
      page = <LegacyRedirect routeId={route.id} controller={controller} live={live} />; break;
    case "loop": page = <LoopWorkspaceScreen controller={controller} live={live} canWrite={canWrite} />; break;
    case "loopClassic": page = <LoopScreen controller={controller} live={live} canWrite={canWrite} />; break;
    case "step": page = <StepScreen controller={controller} live={live} />; break;
    case "blueprints": page = <BlueprintsScreen controller={controller} live={live} canWrite={canWrite} />; break;
    case "system": page = <SystemScreen controller={controller} live={live} />; break;
    case "models": page = <Suspense fallback={<p>Opening models…</p>}><ModelsScreen live={live} controller={controller} canWrite={canWrite} /></Suspense>; break;
    case "modelNew": page = <Suspense fallback={<p>Opening model editor…</p>}><ModelEditor key="new" live={live} canWrite={canWrite} /></Suspense>; break;
    case "modelEdit": page = <Suspense fallback={<p>Opening model editor…</p>}><ModelEditor key={id} live={live} canWrite={canWrite} editing /></Suspense>; break;
    case "settings": page = <SettingsScreen controller={controller} live={live} />; break;
    case "config": page = <Suspense fallback={<p>Opening configuration…</p>}>
      <ConfigScreen controller={controller} live={live} canWrite={canWrite} />
    </Suspense>; break;
    case "bench": page = <Suspense fallback={<p>Opening test bench…</p>}>
      <TestBench managed={{ connection: benchConnection, reconnect: () => void controller.reconnect() }}
        activePage={benchPage && isBenchPage(benchPage) ? benchPage : "status"} />
    </Suspense>; break;
  }
  return <TabBoundary name={route.title}>{page}</TabBoundary>;
}

function AppRoutes() {
  return useRoutes([{ path: "/", element: <ProductShell />, children: [
    ...APP_ROUTES.map((route) => ({ path: route.path, element: <RoutePage route={route} /> })),
    { path: "*", element: <NotFound /> },
  ] }]);
}

export function ProductApp() {
  return (
    <I18nProvider locales={APP_LOCALES}>
      <ToastProvider>
        <HashRouter><AppRoutes /></HashRouter>
      </ToastProvider>
    </I18nProvider>
  );
}
