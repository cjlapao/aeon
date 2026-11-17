import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { Button, EmptyState, Panel, Pill } from '../../../components/kit';
import { Combobox, useI18n, CustomIcon, registerIcons } from '@cjlapao/ui-kit';
import { onEvent, rpc, rpcTyped, toRpcError } from '../../../lib/bridge';
import { useAppSettings } from '../../../lib/appSettings';
import { pinnedComponents, usePinnedComponents } from '../../../lib/pinnedComponents';
import type { LiveController, LiveState } from '../../../lib/live';
import type { RpcMethods } from '../../../proto/contract';
import { ComponentIcon } from '../../components/ComponentIcon';
import { Logo } from '../../../components/Logo';
import { AeonIcon } from '../../../components/icons/AeonIcon';
import { HomeProjects, HomeStats, NeedsYouFeed, needsYouRows, rowsFromInbox, type InboxItem } from './HomeOverview';
import { HomeLaunchComposer, type HomeLaunchComposerHandle, type LaunchBlueprintReport } from './HomeLaunchComposer';


type Loop = RpcMethods['loop.list']['result']['loops'][number];
type Component = RpcMethods['component.list']['result']['components'][number];
type Route = RpcMethods['routes.list']['result']['routes'][number];
type PendingQuestion = RpcMethods['question.pending']['result']['question'];

const ACTIVE_STATUSES = new Set(['running', 'waiting', 'suspended']);
const RECENT_LIMIT = 5;

function componentLabel(component: Component): string {
  return (
    component.name ||
    component.root
      .split(/[\\/]+/)
      .filter(Boolean)
      .pop() ||
    component.root
  );
}

function statusTone(status: string): 'red' | 'amber' | 'emerald' | 'neutral' {
  if (status === 'blocked' || status === 'failed') return 'red';
  if (status === 'waiting' || status === 'suspended') return 'amber';
  if (ACTIVE_STATUSES.has(status)) return 'emerald';
  return 'neutral';
}

function LoopRow({ loop }: { loop: Loop }) {
  const { t } = useI18n();
  return (
    <Panel variant="simple" data-testid="home-loop-row" className="flex flex-wrap items-center justify-between gap-3">
      <div className="min-w-0">
        <p className="truncate text-base font-medium">{loop.title || loop.slug}</p>
        <p className="truncate text-sm text-slate-500 dark:text-slate-400">
          {loop.componentRoot} · {loop.phase || t('home.notStarted')}
        </p>
      </div>
      <div className="flex items-center gap-2">
        <Pill tone={statusTone(loop.status)} size="sm">
          {loop.status}
        </Pill>
        <Link className="aeon-accent-text text-sm underline" to={`/loops/${encodeURIComponent(loop.id)}`}>
          {t('home.open')}
        </Link>
      </div>
    </Panel>
  );
}

const unregister = registerIcons({ AeonLogo: AeonIcon }, { override: true });
if (import.meta.hot) import.meta.hot.dispose(unregister);

export function HomeScreen({ controller, live, canWrite }: { controller: LiveController; live: LiveState; canWrite: boolean }) {
  const navigate = useNavigate();
  const { t } = useI18n();
  const { controlTone } = useAppSettings();
  const pinnedRoots = usePinnedComponents();
  // The launch composer is its own component (built on the agent-chat
  // MessageComposer); this screen hands it its lists and the navigation,
  // and reaches it only through the handle for a pinned "Quick launch".
  const composerHandle = useRef<HomeLaunchComposerHandle>(null);
  // "Start a loop" from a component or the Ctrl K box arrives as /?component=<root>: pick it in
  // the composer once, then drop the parameter so a reload does not re-pick it. Nothing is pinned.
  const [params, setParams] = useSearchParams();
  const wanted = params.get('component') ?? '';

  const [loops, setLoops] = useState<Loop[]>([]);
  const [components, setComponents] = useState<Component[]>([]);
  const [blueprints, setBlueprints] = useState<string[]>([]);
  // The flows' own titles and descriptions: the composer shows the title and keeps the slug as the secondary line.
  const [blueprintInfo, setBlueprintInfo] = useState<Record<string, { title: string; description: string }>>({});
  const [blueprintReports, setBlueprintReports] = useState<Record<string, LaunchBlueprintReport>>({});
  // The FULL route rows ride down to the composer: the capability gate
  // reads `capabilities` off them, and absent does not mean yes (P31).
  const [routes, setRoutes] = useState<Route[]>([]);
  const [error, setError] = useState('');
  const [pinPick, setPinPick] = useState('');
  // The daemon's own attention feed (inbox.list). null = could not be read: Home then derives questions and blocked loops itself.
  const [inbox, setInbox] = useState<InboxItem[] | null>(null);
  const [gates, setGates] = useState<{ loop: Loop; question: PendingQuestion }[]>([]);

  const read = useCallback(async () => {
    try {
      const [stored, savedComponents, named, configured] = await Promise.all([
        rpcTyped('loop.list', { all: true }),
        rpcTyped('component.list', {}),
        rpcTyped('blueprint.list', { origin: '' }),
        rpcTyped('routes.list', {}),
      ]);
      setLoops(stored.loops.slice().sort((a, b) => Date.parse(b.updatedAt) - Date.parse(a.updatedAt)));
      setComponents(savedComponents.components);
      setBlueprints(named.names);
      setBlueprintInfo(Object.fromEntries((named.blueprints ?? []).map((b) => [b.name, { title: b.title, description: b.description }])));
      setRoutes(configured.routes);
      setError('');
      try {
        const feed = await rpc<{ items: InboxItem[] }>('inbox.list', { rootId: '' });
        setInbox(feed.items);
      } catch {
        setInbox(null);
      }
    } catch (cause) {
      setError(toRpcError(cause).message);
    }
  }, []);

  useEffect(() => controller.registerReader(read), [controller, read]);
  useEffect(() => {
    if (live.state === 'current') void read();
  }, [live.state, read]);

  // Live updates: a loop event keeps the dashboard current without a refetch on every tick.
  useEffect(() => {
    if (live.state !== 'current') return;
    let closed = false;
    let stop: (() => void) | undefined;
    let subscriptionId = '';
    void (async () => {
      try {
        stop = await onEvent((raw) => {
          const event = raw as { kind: string; payload?: { row?: Loop } };
          if (!event.kind.startsWith('loop.')) return;
          const row = event.payload?.row;
          if (!row) {
            void read();
            return;
          }
          setLoops((current) => {
            const next = current.filter((item) => item.id !== row.id);
            next.push(row);
            return next.sort((a, b) => Date.parse(b.updatedAt) - Date.parse(a.updatedAt));
          });
        });
        const sub = await rpcTyped('subscribe', { filter: { kinds: ['loop.*'] } });
        subscriptionId = sub.subscriptionId;
        if (closed) stop?.();
      } catch (cause) {
        if (!closed) setError(toRpcError(cause).message);
      }
    })();
    return () => {
      closed = true;
      stop?.();
      if (subscriptionId) void rpc('unsubscribe', { subscriptionId });
    };
  }, [live.state === 'current' ? live.generation : -1, read]);

  // Every blueprint offered in the composer carries its own diagnostics, the way the Loops page does —
  // this is now the first place most people start a loop, so it keeps the same safety net.
  useEffect(() => {
    if (live.state !== 'current') return;
    let disposed = false;
    void Promise.all(
      blueprints.map(async (name) => {
        try {
          const report = await rpc<{ diagnostics: Array<{ state: string; kind: string; path: string; slice?: string }> }>('blueprint.validate', { name });
          const unavailable = report.diagnostics.find((item) => item.kind === 'agent-registry' || item.kind === 'resolver-registry');
          return [
            name,
            {
              invalid: report.diagnostics.filter((item) => item.state === 'invalid').length,
              unverifiable: report.diagnostics.filter((item) => item.state === 'unverifiable').length,
              unavailable: unavailable ? `${unavailable.path} needs a handler arriving at ${unavailable.slice || 'a later slice'}` : undefined,
            },
          ] as const;
        } catch (cause) {
          return [name, { invalid: 0, unverifiable: 0, error: toRpcError(cause).message }] as const;
        }
      }),
    ).then((entries) => {
      if (!disposed) setBlueprintReports(Object.fromEntries(entries));
    });
    return () => {
      disposed = true;
    };
  }, [blueprints.join('|'), live.state === 'current' ? live.generation : -1]);

  useEffect(() => {
    if (!wanted || !components.some((item) => item.root === wanted)) return;
    composerHandle.current?.focusWithComponent(wanted);
    setParams((current) => { const next = new URLSearchParams(current); next.delete('component'); return next; }, { replace: true });
  }, [wanted, components, setParams]);

  const activeLoops = useMemo(() => loops.filter((loop) => ACTIVE_STATUSES.has(loop.status)), [loops]);
  const recentLoops = useMemo(() => loops.filter((loop) => !ACTIVE_STATUSES.has(loop.status)).slice(0, RECENT_LIMIT), [loops]);
  const runningCount = useMemo(() => loops.filter((loop) => loop.status === 'running').length, [loops]);
  const pinnedComponentItems = useMemo(() => components.filter((item) => pinnedRoots.includes(item.root)), [components, pinnedRoots]);
  const unpinnedComponents = useMemo(() => components.filter((item) => !pinnedRoots.includes(item.root)), [components, pinnedRoots]);

  // Gates: every loop actually waiting on a human, across every project and component — not just
  // the one most recently touched. `loop.awaiting` is the daemon's own derived flag (never a guess
  // this screen makes), and each one still gets its own `question.pending` call for the real prompt,
  // because the flag alone doesn't carry it.
  const gateRows = useMemo(() => gates.map((item) => ({ loop: item.loop, prompt: item.question.prompt })), [gates]);
  const needsRows = useMemo(() => (inbox ? rowsFromInbox(inbox, loops, gateRows, components) : needsYouRows(loops, gateRows)), [inbox, loops, gateRows, components]);
  const now = useMemo(() => Date.now(), [loops]);
  const awaitingLoops = useMemo(() => loops.filter((loop) => loop.awaiting), [loops]);
  const awaitingKey = awaitingLoops.map((loop) => `${loop.id}:${loop.updatedAt}`).join(',');
  useEffect(() => {
    if (awaitingLoops.length === 0 || live.state !== 'current') {
      setGates([]);
      return;
    }
    let cancelled = false;
    void (async () => {
      const settled = await Promise.allSettled(
        awaitingLoops.map(async (loop) => {
          const result = await rpcTyped('question.pending', { componentRoot: loop.componentRoot, slug: loop.slug });
          return result.pending ? { loop, question: result.question } : null;
        }),
      );
      if (cancelled) return;
      setGates(settled.flatMap((entry) => (entry.status === 'fulfilled' && entry.value ? [entry.value] : [])));
    })();
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps -- awaitingKey stands in for awaitingLoops: re-run only when the awaiting SET actually changes, not on every unrelated loop update.
  }, [awaitingKey, live.state]);


  return (
    <section className="w-full min-w-0 max-w-full space-y-6 p-3" data-testid="product-home">
      {/* `min-w-0 w-full max-w-full`: a page in a scrolling region sizes to its widest unbreakable child unless told it may shrink, which is what made it wider than a small window. */}
      <div className="flex min-w-0 flex-col items-center gap-3 text-center">
        <Pill tone={controlTone} size="sm">
          {live.state === 'current' ? t('home.engine.version', { version: live.daemonVersion }) : t('home.engine.unknown')} · {t('home.engine.loopsRunning', { count: runningCount })}
        </Pill>
        <section className="flex min-w-0 flex-wrap items-center justify-center gap-3">
          <Logo isAnimated className="h-16 w-16 object-contain m-0!" />
          <h1 className="text-3xl font-semibold tracking-tight">{t('home.heading')}</h1>
        </section>
        <p className="max-w-150 min-w-0 text-sm text-slate-500 dark:text-slate-400">{t('home.subtitle')}</p>
      </div>

      {error && (
        <Panel tone="red">
          <p role="alert">{error}</p>
        </Panel>
      )}

      <section className="flex w-full min-w-0 flex-col items-center justify-center space-y-4">
        {/* The launch composer (MessageComposer + context pickers + attachment gate) is its own component. */}
        <HomeLaunchComposer
          connected={live.state === 'current'}
          canWrite={canWrite}
          loops={loops}
          components={components}
          blueprints={blueprints}
          blueprintInfo={blueprintInfo}
          blueprintReports={blueprintReports}
          routes={routes}
          onLaunched={(id) => navigate(`/loops/${encodeURIComponent(id)}`)}
          onOpenLoop={(id) => navigate(`/loops/${encodeURIComponent(id)}`)}
          handleRef={composerHandle}
        />
      </section>

      <HomeStats loops={loops} rows={needsRows} components={components} now={now} />

      <NeedsYouFeed rows={needsRows} components={components} />

      <section className="space-y-3" aria-label="Active loops">
        <div className="flex items-center justify-between">
          <h2 className="text-xl font-semibold">
            Active loops <span className="text-sm font-normal text-slate-500">{activeLoops.length} running</span>
          </h2>
          <Link to="/work/all/loops" className="aeon-accent-text text-sm underline">
            View all →
          </Link>
        </div>
        {activeLoops.length === 0 ? (
          <EmptyState icon={<CustomIcon icon="AeonLogo" />} variant="simple" title="Nothing running" subtitle="Launch a loop above to see it here." fullWidth size="xs" />
        ) : (
          <div className="space-y-3">
            {activeLoops.map((loop) => (
              <LoopRow key={loop.id} loop={loop} />
            ))}
          </div>
        )}
      </section>

      <HomeProjects loops={loops} components={components} />

      <div className="grid min-w-0 grid-cols-[minmax(0,1fr)] gap-5 lg:grid-cols-[minmax(0,1fr)_minmax(280px,360px)]">
        <section className="min-w-0 space-y-3" aria-label="Recently completed loops">
          <h2 className="text-xl font-semibold">Recent activity</h2>
          {recentLoops.length === 0 ? (
            <EmptyState title="No completed loops yet" fullWidth size="xs" />
          ) : (
            <div className="space-y-3">
              {recentLoops.map((loop) => (
                <LoopRow key={loop.id} loop={loop} />
              ))}
            </div>
          )}
        </section>

        <section className="min-w-0 space-y-3" aria-label="Pinned components" data-testid="home-pinned-components">
          <h2 className="text-xl font-semibold">Pinned components</h2>
          {pinnedComponentItems.length === 0 ? (
            <EmptyState title="No pinned components" subtitle="Pin a component below for one-click access." fullWidth />
          ) : (
            <div className="space-y-2">
              {pinnedComponentItems.map((item) => (
                <Panel key={item.id} variant="simple" className="flex items-center justify-between gap-3">
                  <div className="flex min-w-0 items-center gap-2">
                    <ComponentIcon icon={item.icon} name={item.name} slug={item.slug} />
                    <div className="min-w-0">
                      <p className="truncate text-sm font-medium">{componentLabel(item)}</p>
                      <p className="truncate text-xs text-slate-500 dark:text-slate-400">{item.root}</p>
                    </div>
                  </div>
                  <div className="flex shrink-0 items-center gap-2">
                    <Button size="sm" onClick={() => composerHandle.current?.focusWithComponent(item.root)}>
                      Quick launch
                    </Button>
                    <Button size="sm" variant="soft" aria-label={`Unpin ${componentLabel(item)}`} onClick={() => pinnedComponents.unpin(item.root)}>
                      Unpin
                    </Button>
                  </div>
                </Panel>
              ))}
            </div>
          )}
          {unpinnedComponents.length > 0 && (
            <div className="flex items-end gap-2">
              <label className="block flex-1 text-sm">
                Pin a component
                <Combobox
                  value={pinPick}
                  onChange={setPinPick}
                  size="sm"
                  placeholder="Choose a component"
                  variant="elevated"
                  options={unpinnedComponents.map((item) => ({ value: item.root, label: componentLabel(item) }))}
                />
              </label>
              <Button
                size="sm"
                variant="outline"
                disabled={!pinPick}
                onClick={() => {
                  pinnedComponents.pin(pinPick);
                  setPinPick('');
                }}
              >
                Pin
              </Button>
            </div>
          )}
        </section>
      </div>
    </section>
  );
}
