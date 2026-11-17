import { useCallback, useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { Button, EmptyState, InfiniteScrollPanel, Panel, Pill, SearchBar, StatCard, WorkflowTrackerRail, WorkflowTrackerStepDetails } from '../../components/kit';
import { rpc, toRpcError } from '../../lib/bridge';
import { useAppSettings } from '../../lib/appSettings';
import type { LiveState } from '../../lib/live';
import type { RpcMethods } from '../../proto/contract';
import { workflowData } from '../loops/components/Workflow';
import { useLoopAnswer } from '../loops/components/useLoopAnswer';
import type { LoopDetail } from '../loops/pages/LoopScreen';
import { ComponentIcon } from './ComponentIcon';

type Loop = RpcMethods['loop.list']['result']['loops'][number];
// `component.create` / `component.update` results omit `parentId` (only `component.list`
// carries it); the dashboard never reads `parentId`, so the shared type widens it to
// optional so the cache the ComponentsScreen feeds in is assignable.
type Component = Omit<RpcMethods['component.list']['result']['components'][number], 'parentId'> & { parentId?: string };
export interface ComponentGroup {
  path: string;
  saved?: Component;
  items: Loop[];
}
type Filter = 'all' | 'saved' | 'recorded' | 'attention';
type Sort = 'recent' | 'name' | 'loops';
const PAGE_SIZE = 9;
const isActive = (status: string) => ['running', 'waiting', 'suspended'].includes(status);
const needsAttention = (status: string) => ['waiting', 'blocked', 'failed'].includes(status);
const titleOf = (component: ComponentGroup) => component.saved?.name || component.path.split('/').filter(Boolean).at(-1) || component.path;
const dateOf = (component: ComponentGroup) => Date.parse(component.items[0]?.updatedAt || component.saved?.updatedAt || '') || 0;

function ComponentCard({ component }: { component: ComponentGroup }) {
  const latest = component.items[0];
  const active = component.items.filter((loop) => isActive(loop.status));
  const attention = component.items.filter((loop) => needsAttention(loop.status));
  const title = titleOf(component);
    const { surfaceTone } = useAppSettings();
  return (
    <Panel  tone={surfaceTone} data-testid="component-card" variant="simple" hoverable>
      <div className="flex min-w-0 items-start gap-3">
        <ComponentIcon icon={component.saved?.icon || ''} name={component.saved?.name || ''} slug={component.saved?.slug || title} />
        <div className="min-w-0 flex-1">
          <p className="text-[11px] font-semibold uppercase tracking-[.15em] aeon-accent-text">{component.saved ? 'Saved component' : 'Recorded workspace'}</p>
          <Link className="mt-1 block truncate text-lg font-semibold hover:underline" to={`/components?root=${encodeURIComponent(component.path)}`}>
            {title}
          </Link>
        </div>
        {attention.length > 0 && (
          <Pill tone="red" size="sm">
            {attention.length} attention
          </Pill>
        )}
      </div>
      <p className="mt-4 truncate rounded-lg border border-slate-300/30 bg-slate-950/5 px-3 py-2 font-mono text-xs dark:border-white/10 dark:bg-black/20" title={component.path}>
        {component.path}
      </p>
      <div className="mt-4 flex flex-wrap gap-2 text-xs">
        <Pill tone="blue" size="sm">
          {component.items.length} loop{component.items.length === 1 ? '' : 's'}
        </Pill>
        {active.length > 0 && (
          <Pill tone="emerald" size="sm">
            {active.length} active
          </Pill>
        )}
        {component.items.some((loop) => loop.status === 'blocked') && (
          <Pill tone="red" size="sm">
            {component.items.filter((loop) => loop.status === 'blocked').length} blocked
          </Pill>
        )}
      </div>
      <div className="mt-auto flex min-w-0 items-center justify-between gap-3 border-t border-slate-300/30 pt-4 text-xs dark:border-white/10">
        <p className="min-w-0 truncate text-slate-600 dark:text-slate-400">{latest ? `${latest.title || latest.slug} · ${latest.status}` : 'Ready for its first loop'}</p>
        <Link className="shrink-0 aeon-accent-text underline" to={active[0] ? `/loops/${encodeURIComponent(active[0].id)}` : `/components?root=${encodeURIComponent(component.path)}`}>
          {active[0] ? 'Inspect loop' : 'Open component'}
        </Link>
      </div>
    </Panel>
  );
}

function ActiveLoopCard({ loop, live, canWrite, onRefresh }: { loop: Loop; live: LiveState; canWrite: boolean; onRefresh: () => Promise<void> }) {
  const [detail, setDetail] = useState<LoopDetail | null>(null);
  const [error, setError] = useState('');
  const [selectedLane, setSelectedLane] = useState('');
  const { controlTone, surfaceTone } = useAppSettings();
  const read = useCallback(async () => {
    try {
      const result = await rpc<LoopDetail>('loop.show', { id: loop.id, includeBlueprint: false });
      setDetail(result);
      setError('');
    } catch (cause) {
      setError(toRpcError(cause).message);
    }
  }, [loop.id]);
  useEffect(() => {
    if (live.state === 'current') void read();
  }, [read, live.state, live.state === 'current' ? live.generation : -1, loop.updatedAt]);
  const answer = useLoopAnswer(detail, null, canWrite, live.state !== 'current', async () => {
    await Promise.all([read(), onRefresh()]);
  });
  const lane = detail?.lanes.some((item) => item.id === selectedLane) ? selectedLane : detail?.position.lane || '';
  const data = detail ? workflowData(detail, detail.lastReason, { activeStepId: lane, gateBusy: answer.busy }) : null;
  const canAnswer = !!detail && detail.status === 'waiting' && !!detail.gateAnswers?.length && canWrite && live.state === 'current';
  const action = canAnswer
    ? (stepId: string, choice: string) => {
        if (stepId === detail.position.lane && detail.gateAnswers?.includes(choice)) void answer.answer(choice);
      }
    : undefined;
  const styling = { variant: 'liquid-glass' as const, cardTone: surfaceTone, accentColor: controlTone };
  return (
    <Panel tone={surfaceTone} data-testid="active-loop-card" className="min-w-0 space-y-4" variant="simple" onClick={() => { } }>
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <p className="text-xs uppercase tracking-[.18em] aeon-accent-text">Current loop activity · {loop.slug}</p>
          <h3 className="mt-1 text-xl font-semibold">{detail?.blueprint || loop.title || loop.slug}</h3>
          <p className="mt-1 text-sm text-slate-600 dark:text-slate-400">{loop.componentRoot}</p>
        </div>
        <div className="flex items-center gap-2">
          <Pill tone={loop.status === 'waiting' ? 'amber' : 'blue'}>{loop.status === 'waiting' ? 'Approval requested' : loop.status}</Pill>
          <Link to={`/loops/${encodeURIComponent(loop.id)}`} className="aeon-accent-text text-sm underline">
            Open full workflow →
          </Link>
        </div>
      </div>
      {error && (
        <p role="alert" className="text-sm text-red-700 dark:text-red-300">
          {error}
        </p>
      )}
      {answer.error && (
        <p role="alert" className="text-sm text-red-700 dark:text-red-300">
          {answer.error}
        </p>
      )}
      {detail && data ? (
        <div className="grid min-w-0 gap-4 lg:grid-cols-[minmax(240px,330px)_minmax(0,1fr)]" data-testid="active-loop-workflow">
          <WorkflowTrackerRail data={data} {...styling} onStepSelect={setSelectedLane} onStepAction={action} showLegend={false} />
          <div className="min-w-0">
            <WorkflowTrackerStepDetails data={data} {...styling} onStepAction={action} />
            {detail.goal && (
              <p className="mt-3 line-clamp-3 text-sm text-slate-600 dark:text-slate-400" title={detail.goal}>
                {detail.goal}
              </p>
            )}
          </div>
        </div>
      ) : (
        !error && <p className="text-sm text-slate-500">Loading the current workflow…</p>
      )}
    </Panel>
  );
}

export function ComponentsDashboard({ components, loops, live, canWrite, onRefresh }: { components: ComponentGroup[]; loops: Loop[]; live: LiveState; canWrite: boolean; onRefresh: () => Promise<void> }) {
  const { controlTone, surfaceTone } = useAppSettings();
  const [query, setQuery] = useState('');
  const [filter, setFilter] = useState<Filter>('all');
  const [sort] = useState<Sort>('recent');
  const [visibleCount, setVisibleCount] = useState(PAGE_SIZE);
  const active = loops.filter((loop) => isActive(loop.status)).sort((a, b) => Date.parse(b.updatedAt) - Date.parse(a.updatedAt));
  const waiting = loops.filter((loop) => loop.status === 'waiting').length;
  const blocked = loops.filter((loop) => loop.status === 'blocked').length;
  const filtered = useMemo(() => {
    const needle = query.trim().toLowerCase();
    return components
      .filter((component) => {
        if (filter === 'saved' && !component.saved) return false;
        if (filter === 'recorded' && component.saved) return false;
        if (filter === 'attention' && !component.items.some((loop) => needsAttention(loop.status))) return false;
        return !needle || [titleOf(component), component.path, component.saved?.slug || '', ...component.items.map((loop) => loop.title || loop.slug)].some((part) => part.toLowerCase().includes(needle));
      })
      .sort((a, b) => (sort === 'name' ? titleOf(a).localeCompare(titleOf(b)) : sort === 'loops' ? b.items.length - a.items.length : dateOf(b) - dateOf(a)));
  }, [components, query, filter, sort]);
  useEffect(() => setVisibleCount(PAGE_SIZE), [query, filter, sort]);
  const more = useCallback(async () => {
    setVisibleCount((count) => count + PAGE_SIZE);
  }, []);
  const filters: { id: Filter; label: string; count: number }[] = [
    { id: 'all', label: 'All', count: components.length },
    { id: 'saved', label: 'Saved', count: components.filter((component) => !!component.saved).length },
    { id: 'recorded', label: 'Recorded', count: components.filter((component) => !component.saved).length },
    { id: 'attention', label: 'Attention', count: components.filter((component) => component.items.some((loop) => needsAttention(loop.status))).length },
  ];
  const savedCount = components.filter((component) => !!component.saved).length;
  const recordedCount = components.length - savedCount;

  return (
    <div className="space-y-5 " data-testid="components-dashboard">
      <div className="grid gap-3" style={{ gridTemplateColumns: 'repeat(auto-fit, minmax(min(100%, 170px), 1fr))' }} data-testid="component-stats">
        <StatCard
          variant="simple"
          size="sm"
          padding="sm"
          tone={controlTone}
          icon="Folder"
          label="Workspaces"
          value={components.length}
          subtitle={`${savedCount} saved · ${recordedCount} recorded`}
          className="min-w-0"
          data-testid="component-metric"
        />
        <StatCard
          variant="simple"
          size="sm"
          padding="sm"
          tone={controlTone}
          icon="Rocket"
          label="Active loops"
          value={active.length}
          subtitle="Running or awaiting a decision"
          className="min-w-0"
          data-testid="component-metric"
        />
        <StatCard
          variant="simple"
          size="sm"
          padding="sm"
          tone={controlTone}
          icon="Pause"
          label="Approval requests"
          value={waiting}
          subtitle="Waiting for an answer"
          className="min-w-0"
          data-testid="component-metric"
        />
        <StatCard
          variant="simple"
          size="sm"
          padding="sm"
          tone="red"
          icon="Warning"
          label="Blocked loops"
          value={blocked}
          subtitle="Open a loop to inspect the cause"
          className="min-w-0"
          data-testid="component-metric"
        />
        <StatCard
          variant="simple"
          size="sm"
          padding="sm"
          tone={controlTone}
          icon="HealthCheck"
          label="Daemon"
          value={live.state === 'current' ? 'Online' : live.state}
          subtitle={live.state === 'current' ? 'Component data is current' : 'Showing the last known snapshot'}
          className="min-w-0"
          data-testid="component-metric"
        />
      </div>
      <div className="flex flex-wrap items-center gap-3">
        <SearchBar
          placeholder="Search components, paths, or loops…"
          onSearch={setQuery}
          onClear={() => setQuery('')}
          debounceMs={0}
          variant="flat"
          color={controlTone}
          className="min-w-57.5 flex-1"
        />
        <div role="group" aria-label="Component filters" className="flex flex-wrap gap-1.5">
          {filters.map((item) => (
            <Button key={item.id} size="sm" variant="soft" aria-pressed={filter === item.id} onClick={() => setFilter(item.id)}>
              {item.label} {item.count}
            </Button>
          ))}
        </div>
      </div>
      <InfiniteScrollPanel<ComponentGroup>
        items={filtered.slice(0, visibleCount)}
        hasMore={visibleCount < filtered.length}
        onLoadMore={more}
        renderItem={(component) => <ComponentCard component={component} />}
        getItemKey={(component) => component.path}
        layout="grid"
        minColumnWidth={270}
        maxColumns={3}
        gap="md"
        tone={surfaceTone}
        emptyComponent={
          components.length === 0 ? (
            <EmptyState title="No components yet" subtitle="Create a component to save its workspace before its first loop." fullWidth />
          ) : (
            <p className="p-10 text-center text-sm text-slate-500">No components match this search or filter.</p>
          )
        }
      />
      {active.length > 0 && (
        <section className="space-y-3" aria-label="Current loop activity">
          <div>
            <p className="text-xs uppercase tracking-[.18em] aeon-accent-text">Latest activity</p>
            <h2 className="mt-1 text-xl font-semibold">
              Current loop activity <span className="text-sm font-normal text-slate-500">{active.length}</span>
            </h2>
          </div>
          {active.map((loop) => (
            <ActiveLoopCard key={loop.id} loop={loop} live={live} canWrite={canWrite} onRefresh={onRefresh} />
          ))}
        </section>
      )}
    </div>
  );
}
