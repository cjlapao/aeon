import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react';
import {  matchPath, useLocation, useNavigate } from 'react-router-dom';
import { ScrollArea, SideMenuIconButton, SideMenuLayout, type SideMenuItem } from '../components/kit';
import { rpcTyped, workspaceActive } from '../lib/bridge';
import { useAppSettings } from '../lib/appSettings';
import { useWaitsFor } from '../lib/pageWait';
import type { LiveController, LiveState } from '../lib/live';
import type { RpcMethods } from '../proto/contract';
import { NotificationBell } from './NotificationBell';
import { UserMenu } from './UserMenu';
import { DaemonStatusButton } from './DaemonStatusButton';
import { APP_ROUTES, BENCH_PAGES, menuPath } from './routes';
import { CONFIG_GROUPS, menuConfigPages, firstReadyConfigPage, getConfigPage } from '../features/config/lib/configRegistry';
import { Logo } from '../components/Logo';
import { rememberOrigin } from '../lib/origin';
import { buildTree, countByRoot, rollup, workPath, type TreeNode, type WorkComponent } from '../features/work/workModel';

type Loop = RpcMethods['loop.list']['result']['loops'][number];
type Project = RpcMethods['component.list']['result']['components'][number];
type SideMenuItemLinkT = Extract<SideMenuItem, { type?: 'link' }>;
type SectionId = 'home' | 'work' | 'blueprints' | 'system' | 'settings' | 'models' | 'config' | 'bench';

/**
 * Loop/step detail pages ride under the Loops section so the rail stays honest about where you are.
 *
 * `components` was missing here until now — `/components` has its own rail entry (routes.ts's
 * `menu.group`), but with no section mapping it fell through to the `'home'` fallback below: the
 * rail highlighted Home while you were looking at a component, and the secondary panel showed
 * Home's (empty) content instead of just staying out of the way.
 */
const SECTION_OF_ROUTE: Record<string, SectionId> = {
  home: 'home',
  work: 'work',
  // A loop is opened from, and belongs to, a component: the rail and tree stay on Work.
  loop: 'work',
  loopClassic: 'work',
  step: 'work',
  blueprints: 'blueprints',
  system: 'system',
  settings: 'settings',
  config: 'config',
  models: 'models', modelNew: 'models', modelEdit: 'models',
  bench: 'bench',
};
const SECTION_LABELS: Record<SectionId, string> = { home: 'Home', work: 'Work', blueprints: 'Flows', system: 'System', settings: 'Settings', config: 'Configuration', models: 'Models', bench: 'Test bench' };

/** The rail keeps the navigation the sidebar had: the workspace items plus the app tools. */
const RAIL_ITEMS: SideMenuItem[] = (() => {
  const links = (group: 'workspace' | 'app'): SideMenuItem[] =>
    APP_ROUTES.filter((route) => route.menu?.group === group).map((route) => ({
      // The bench's optional page param is a matcher detail, never a URL to link to.
      slug: route.id,
      type: 'link' as const,
      label: route.menu!.label,
      path: menuPath(route.path),
      icon: route.menu!.icon,
    }));
  return [{ slug: 'workspace', type: 'group', label: 'Workspace' }, ...links('workspace'), { slug: 'app', type: 'group', label: 'App', hasDivider: true }, ...links('app')];
})();

function loopIdFrom(pathname: string): string {
  const raw = /^\/loops\/([^/]+)/.exec(pathname)?.[1];
  if (!raw) return '';
  try {
    return decodeURIComponent(raw);
  } catch {
    return raw;
  }
}

/**
 * Whether the page at this pathname claims the viewport (ConfigPageDef.fill):
 * the layout then hands the height down instead of scrolling the page, and the
 * page owns its own scroll. The decision reads the same registry the menu
 * lists, so one def says both things. Unknown, planned and ordinary pages get
 * the scrolled branch, unchanged.
 */
function fillFor(pathname: string): boolean {
  // The flow builder owns its own scroll (the list and the inspector scroll apart), so it takes the height.
  if (matchPath({ path: '/blueprints', end: true }, pathname)) return true;
  // Models is a table page: the list owns its scroll (the add and edit pages scroll as pages).
  if (matchPath({ path: '/models', end: true }, pathname)) return true;
  // Work's Loops tab is a table that owns its scroll; its other tabs scroll as pages.
  if (matchPath({ path: '/work/:component/loops', end: true }, pathname)) return true;
  const page = matchPath({ path: '/config/:page', end: true }, pathname)?.params.page;
  return page === undefined ? false : getConfigPage(page)?.fill === true;
}

function projectName(project: Project | undefined, root: string): string {
  if (project?.name) return project.name;
  const segments = root.split(/[\\/]+/).filter(Boolean);
  return segments[segments.length - 1] || root;
}

/**
 * The active workspace's name, read once at boot. It cannot change without
 * the hard reload a workspace switch performs (S41), so it needs no live
 * subscription — unlike `LiveState`, this is never shown as stale or
 * reconnecting, because there is nothing for it to go stale from. Feeds
 * `DaemonStatusButton`'s own label rather than a separate header line, so
 * the app never says "which daemon" in two places that could disagree.
 */
function useActiveWorkspaceName(): string | undefined {
  const [name, setName] = useState<string | undefined>(undefined);
  useEffect(() => {
    let active = true;
    void workspaceActive().then((workspace) => { if (active) setName(workspace.name); }, () => { /* no Tauri bridge (web dev path) — stays unnamed */ });
    return () => { active = false; };
  }, []);
  return name;
}

/** Owns the fixed app chrome — joined rail + contextual panel — and the single scrolling page viewport. */
export function PageLayout({ children, fullBleed = false, controller, live }: { children: ReactNode; fullBleed?: boolean; controller: LiveController; live: LiveState }) {
  const location = useLocation();
  const navigate = useNavigate();
  const { controlTone } = useAppSettings();
  const workspaceName = useActiveWorkspaceName();
  const [loops, setLoops] = useState<Loop[]>([]);
  const [projects, setProjects] = useState<Project[]>([]);
  const [blueprints, setBlueprints] = useState<string[]>([]);

  // The panel lists workspaces and their loops, so it reads the same rows the Projects screen reads —
  // and, for Flows, the same names `blueprint.list` gives the Flows screen itself.
  const read = useCallback(async () => {
    const [projectsResult, loopsResult, blueprintsResult] = await Promise.allSettled([
      rpcTyped('component.list', {}),
      rpcTyped('loop.list', { all: true }),
      rpcTyped('blueprint.list', { origin: '' }),
    ]);
    if (projectsResult.status === 'fulfilled') setProjects(projectsResult.value.components ?? []);
    if (loopsResult.status === 'fulfilled') setLoops(loopsResult.value.loops ?? []);
    if (blueprintsResult.status === 'fulfilled') setBlueprints(blueprintsResult.value.names);
  }, []);
  useEffect(() => controller.registerReader(read), [controller, read]);
  useEffect(() => {
    if (live.state === 'current') void read();
  }, [live.state, live.state === 'current' ? live.generation : -1, read]);

  const section: SectionId = useMemo(() => {
    const route = APP_ROUTES.find((candidate) => matchPath({ path: candidate.path, end: true }, location.pathname));
    return (route && SECTION_OF_ROUTE[route.id]) || 'home';
  }, [location.pathname]);

  // A loop (or one of its steps) wants the width for the conversation: the component tree folds away,
  // the way Home has none, and comes back when the person leaves. Everything else remembers where the
  // person was, so the loop page can send them back to it.
  const onLoopPage = useMemo(() => {
    const route = APP_ROUTES.find((candidate) => matchPath({ path: candidate.path, end: true }, location.pathname));
    return route?.id === 'loop' || route?.id === 'loopClassic' || route?.id === 'step';
  }, [location.pathname]);
  const [secondaryOpen, setSecondaryOpen] = useState(true);
  useEffect(() => setSecondaryOpen(!onLoopPage), [onLoopPage]);
  useEffect(() => {
    if (!onLoopPage) rememberOrigin(`${location.pathname}${location.search}`);
  }, [onLoopPage, location.pathname, location.search]);

  const selectedBlueprint = new URLSearchParams(location.search).get('name') ?? '';
  const activeLoopId = loopIdFrom(location.pathname);
  const tree = useMemo(() => buildTree(projects as unknown as WorkComponent[]), [projects]);
  // The component the page on screen belongs to: the address names it, or — on a loop page — the
  // loop's own component does. The ancestors of it open in the tree so the node is always visible.
  const activeWorkId = useMemo(() => {
    const fromPath = matchPath('/work/:component?/:tab?', location.pathname)?.params.component;
    if (fromPath && fromPath !== 'all') return decodeURIComponent(fromPath);
    const loop = activeLoopId ? loops.find((item) => item.id === activeLoopId) : undefined;
    return loop ? projects.find((project) => project.root === loop.componentRoot)?.id ?? '' : '';
  }, [location.pathname, activeLoopId, loops, projects]);
  const openIds = useMemo(() => {
    const open = new Set<string>();
    const byId = new Map(projects.map((project) => [project.id, project as unknown as WorkComponent]));
    for (let cursor = byId.get(activeWorkId); cursor; cursor = cursor.parentId ? byId.get(cursor.parentId) : undefined) open.add(cursor.id);
    return open;
  }, [projects, activeWorkId]);

  // S72: the configuration row the page on screen belongs to, and what that
  // page is still waiting for. The registry tells the menu which pages exist;
  // only the page itself knows it is still loading, and it reports that
  // through the wait ticket its `useRpcQuery` calls take (lib/pageWait). The
  // owner id IS the row's slug — `config:<page id>` — so the shell declares
  // it once (`ConfigScreen`) and the row recognises itself in it.
  const configOwner =
    section === 'config'
      ? `config:${matchPath('/config/:page?', location.pathname)?.params.page || firstReadyConfigPage()?.id || ''}`
      : null;
  const configWaits = useWaitsFor(configOwner ?? '');

  // Projects: each project is a workspace row; its loops hang beneath it as the workspace items.
  const panelItems = useMemo<SideMenuItem[]>(() => {
    if (section === 'models') return [
      { slug: 'models-list', type: 'link', label: 'All models', path: '/models' },
      { slug: 'models-new', type: 'link', label: 'Add model', path: '/models/new' },
    ];
    if (section === 'config') {
      // S72 CF4: the pages come from the registry (features/config), grouped
      // in CONFIG_GROUPS order — the same shape the bench branch proves: the
      // kit renders a group header only when a link names it via groupName,
      // so every link below carries its own group's slug.
      // Q1 (T1, measured — recorded in the task Notes): the kit's
      // SideMenuItemLink has NO `disabled` field, and a failing guard HIDES a
      // row rather than disabling it; every link renders as a router anchor.
      // A planned row therefore carries its real `/config/<id>` path — where
      // the screen itself says "planned, not wired" (CF5) — plus
      // `meta: 'planned'`, and its click is swallowed by `onItemSelect` at
      // the bottom of this file before the router acts: never navigable from
      // the menu, always labelled, and a deep link tells the truth.
      const items: SideMenuItem[] = [];
      for (const group of CONFIG_GROUPS) {
        const pages = menuConfigPages().filter((page) => page.group === group.id);
        if (pages.length === 0) continue;
        items.push({ slug: `config-group:${group.id}`, type: 'group', label: group.label });
        for (const page of pages) {
          const slug = `config:${page.id}`;
          items.push({
            slug, type: 'link' as const, label: page.label,
            path: `/config/${page.id}`,
            meta: page.status === 'ready' ? undefined : 'planned',
            description: page.summary,
            groupName: `config-group:${group.id}`,
            // The kit's own work-in-progress state: its loader takes the
            // icon's place (rail too) and the row carries `aria-busy`. It
            // shows while THIS page is the one on screen and still waiting —
            // a page you left keeps its own wait and its own row, and the
            // row you are on is the one that should say "still working".
            loading: configOwner === slug && configWaits > 0,
          });
        }
      }
      return items;
    }
    if (section === 'bench') {
      // Trust group (S35 T8, TD11): derived from BENCH_PAGES' own `group`
      // field — mirroring RAIL_ITEMS' literal group-item shape — so a
      // future reorder of BENCH_PAGES can't silently desync this file from
      // it. Every other bench tab stays the flat link list it always was;
      // only pages carrying `group: 'trust'` get a header above them, once,
      // right before the first one.
      // The kit's own SideMenu only renders a `type: 'group'` item when at
      // least one link names it via `groupName` (groupsWithVisibleLinks,
      // index.js) — positional adjacency alone renders nothing. Every
      // Trust link below carries `groupName: 'bench-group:trust'` for
      // exactly this reason.
      const items: SideMenuItem[] = [];
      let groupOpened = false;
      for (const page of BENCH_PAGES) {
        const inTrust = 'group' in page && page.group === 'trust';
        if (inTrust && !groupOpened) {
          items.push({ slug: 'bench-group:trust', type: 'group', label: 'Trust' });
          groupOpened = true;
        }
        items.push({
          slug: `bench:${page.id}`, type: 'link' as const, label: page.label, path: page.path,
          ...(inTrust ? { groupName: 'bench-group:trust' } : {}),
        });
      }
      return items;
    }
    // Flows: the same list the Flows screen itself shows, so you can jump straight to one.
    if (section === 'blueprints') return blueprints.map((name) => ({
      slug: `blueprint:${name}`, type: 'link' as const, label: name, path: `/blueprints?name=${encodeURIComponent(name)}`, icon: 'Blueprint',
    }));
    if (section !== 'work') return [];
    // Work: the component tree, with what is running and what needs a person counted up through
    // collapsed parents, so a closed node still shows what is inside it.
    const counts = countByRoot(loops);
    const toItem = (node: TreeNode): SideMenuItemLinkT => {
      const sum = rollup(node, counts);
      return {
        slug: `work:${node.component.id}`,
        type: 'link',
        label: projectName(node.component as unknown as Project, node.component.root),
        path: workPath(node.component.id),
        icon: node.component.icon || 'Folder',
        defaultOpen: openIds.has(node.component.id),
        badge: sum.needsYou > 0 ? <span className="rounded-full bg-amber-500/20 px-1.5 text-[11px] text-amber-300" aria-label={`${sum.needsYou} need you`}>{sum.needsYou}</span> : undefined,
        meta: sum.running > 0 ? `${sum.running} running` : undefined,
        children: node.children.length > 0 ? node.children.map(toItem) : undefined,
      };
    };
    return [
      { slug: 'work:all', type: 'link' as const, label: 'All projects', path: '/work', icon: 'Group' },
      ...tree.map(toItem),
    ];
  }, [section, tree, openIds, loops, blueprints, configOwner, configWaits]);

  const activePanelSlug = useMemo(() => {
    if (section === 'models') return location.pathname === '/models/new' ? 'models-new' : 'models-list';
    if (section === 'config') {
      // S72 CF4: an explicit slug even for unknown or planned pages, so the
      // kit never falls back to prefix matching and highlights the wrong row.
      const match = matchPath('/config/:page?', location.pathname);
      return `config:${match?.params.page || firstReadyConfigPage()?.id || ''}`;
    }
    if (section === 'bench') {
      const match = matchPath('/bench/:page?', location.pathname);
      const page = match?.params.page ?? 'status';
      // Pass an explicit slug even for unknown pages: undefined lets the kit
      // fall back to prefix matching and would incorrectly select Status.
      return `bench:${page}`;
    }
    if (section === 'blueprints') return selectedBlueprint ? `blueprint:${selectedBlueprint}` : undefined;
    if (section === 'work') return `work:${activeWorkId || 'all'}`;
    return undefined;
  }, [section, location.pathname, selectedBlueprint, activeWorkId]);

  return (
    <div className="h-full">
      <SideMenuLayout
        multiMenuMode="joined"
        secondaryOpen={secondaryOpen}
        onSecondaryOpenChange={setSecondaryOpen}
        sideMenuProps={{
          title: 'Aeon navigation',
          color: controlTone,
          logoIcon: <Logo alt="" className="h-10 w-10 object-contain" />,
          items: RAIL_ITEMS,
          // Pinned custom controls (S9: "export the button type") rather than cramming both
          // into one `footerItem`'s single media slot — each inherits this menu's tone and
          // collapsed state on its own, through the kit's own SideMenuIconButton. The daemon
          // status rides here too: always on screen, not just on pages with a secondary menu.
          // The Configuration entry moved out of the App rail group into this footer
          // section — the same stack that carries the daemon-status LED. It navigates to
          // /config like the rail row did (the Escape dispatch is the mobile-drawer close
          // the rail rows fire), and says so with `aria-current` since a button, unlike
          // the kit's router link, carries no selection state of its own.
          footer: () => (
            <div className="flex flex-col items-center gap-2">
              <NotificationBell />
              <SideMenuIconButton
                label="Configuration"
                icon="Settings"
                data-testid="configuration-entry"
                active={section === 'config'}
                aria-current={section === 'config' ? 'page' : undefined}
                onClick={(event) => {
                  event.currentTarget.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }));
                  navigate('/config');
                }}
              />
              <DaemonStatusButton live={live} workspaceName={workspaceName} />

              <UserMenu />
            </div>
          ),
          activeItemSlug: section,
          onItemSelect: (_item, event) => {
            // The joined shell owns the mobile drawer, and Escape is its own close path;
            // rail rows never dismiss it themselves, so fire the layout's Escape on click.
            event.currentTarget.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }));
          },
        }}
        // No secondary props at all hides the panel (the kit's own rule) — a page with nothing
        // to list (Home, Loops, Components, System, Settings all own their full-width content)
        // gets the space back instead of an empty box next to it.
        secondarySideMenuProps={panelItems.length === 0 ? undefined : {
          title: SECTION_LABELS[section],
          logoText: <span className="text-lg font-semibold">{SECTION_LABELS[section]}</span>,
          color: controlTone,
          items: panelItems,
          activeItemSlug: activePanelSlug,
          // S72 acceptance 5: a planned Configuration row is never clickable.
          // The kit has no disabled link (Q1), so the click is cancelled
          // here — the kit calls this before the router link acts, and
          // react-router skips a defaultPrevented click.
          onItemSelect: (item: SideMenuItem, event: { preventDefault(): void }) => {
            if (item.type !== 'group' && item.type !== 'divider' && item.meta === 'planned') event.preventDefault();
          },
          search: (section === 'work' || section === 'blueprints' || section === 'config') && panelItems.length > 0,
          searchPlaceholder: section === 'blueprints' ? 'Search flows' : section === 'config' ? 'Search configuration' : 'Find a component',
        }}

        bodyClassName="p-0"
      >
        {fullBleed ? (
          <div key={`${location.pathname}${location.search}`} className="h-full min-h-0 w-full" role="region" aria-label="Page content">
            {children}
          </div>
        ) : fillFor(location.pathname) ? (
          // The fill branch: same padding and width cap as the scrolled one,
          // but the height goes to the page — the page scrolls nothing, its
          // table does. `overflow-auto` stays as the safety valve for states
          // that carry no fill chain (a crash boundary, a warning on a very
          // short window); a filling page is exactly viewport tall and never
          // reaches it.
          <div key={`${location.pathname}${location.search}`} className="h-full min-h-0 w-full overflow-y-auto p-4" role="region" aria-label="Page content">
            <div className="mx-auto flex h-full min-h-0 w-full max-w-375 flex-col">{children}</div>
          </div>
        ) : (

            <ScrollArea key={`${location.pathname}${location.search}`} className="h-full min-h-0 w-full p-4" role="region" aria-label="Page content">
              <div className="mx-auto w-full max-w-375">{children}</div>
            </ScrollArea>

        )}
      </SideMenuLayout>
    </div>
  );
}
