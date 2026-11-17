import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react';
import { AgentChat, agentMood, useNow, type ChatItem, type ChatSession, type ComposerOption, type ComposerDraft } from '../../components/agent-chat';
import { GradientBackdrop, PageBodyLayout, WorkflowTrackerRail } from '../../components/kit';
import { HeaderPortal } from '../../app/HeaderSlot';
import type { LiveState } from '../../lib/live';
import { REASONING_EFFORTS } from '../../lib/protocol';
import { useSkillPicker } from '../../lib/skills';
import { useContextPickers, mergeAddActionHandlers, mergeSpecialKeyHandlers, applyTokenInsert, type ContextLoopRow } from '../../lib/contextPickers';
import type { LoopDetail } from '../loops/pages/LoopScreen';
import { HeaderBreadcrumb, HeaderNav } from './HeaderContent';
import { LoopAccess } from './LoopAccess';
import { PageBar } from './center/PageBar';
import { TerminalDock } from './center/TerminalDock';
import type { ChatWiring } from './center/useChatActions';
import type { GitStatusView } from './center/useGitStatus';
import { laneAggregate } from './stepSummaries';
import { useLaneCalls } from './useLaneCalls';
import type { LoopSteps } from './useLoopSteps';
import { DocumentsTab } from './inspector/DocumentsTab';
import { ChangesTab } from './inspector/ChangesTab';
import type { GitChangesView } from './center/useGitChanges';
import { TasksTab } from './inspector/TasksTab';
import { FilesTab } from './inspector/FilesTab';
import { FileTab } from './inspector/FileTab';
import { Inspector } from './inspector/Inspector';
import { StepTab } from './inspector/StepTab';
import { usePersisted } from './persist';
import { useAgentAvatars } from './useAgentAvatars';
import { AttendednessNotice } from './AttendednessNotice';
import { useTodoPanel } from './todo/useTodoPanel';
import { LoopBlockedBanner } from './LoopBlockedBanner';
import type { LoopTodos } from './todo/todoModel';
import { RunBlueprintPanel } from './RunBlueprintPanel';
import { AgentsPanel } from './rail/AgentsPanel';
import { fileChanges, filesInScope, footprint, stepTelemetry, workflowFromLoop } from './workspaceModel';

const EFFORTS = REASONING_EFFORTS.map((e) => ({ value: e, label: e }));
const ENDED = ['done', 'blocked', 'failed', 'aborted'];

/** How long an agent wears the `done` face after its loop finishes. */
const DONE_MOOD_MS = 6000;

/** Below this width the rail and the inspector start closed, so the chat keeps its room. */
function isNarrow(): boolean {
  try {
    return window.matchMedia('(max-width: 1099px)').matches;
  } catch {
    return false;
  }
}

export interface LoopWorkspaceViewProps {
  loopId: string;
  /** Re-read the loop (after a resume). */
  onRefresh?: () => void | Promise<void>;
  /** S75: the loop's todo list, pinned above the composer. */
  todos?: LoopTodos | null;
  onTodos?: (next: LoopTodos) => void;
  loop: LoopDetail | null;
  /** Drives the header's engine pill; the page passes the real liveness, the bench a fixed one. */
  live: LiveState;
  connected: boolean;
  items: ChatItem[];
  session: ChatSession;
  /** The daemon's per-step summaries and loop totals; the bench passes none. */
  steps?: LoopSteps;
  git: GitStatusView | null;
  /** What changed in the working tree, with line counts; the bench passes none. */
  gitChanges?: GitChangesView | null;
  chat: ChatWiring;
  routes: ComposerOption[];
  componentRoot: string;
  /** S77: the loops the `//` picker offers (the page's own `loop.list` read;
   * the bench passes none and the panel honestly says nothing to open). */
  contextLoops?: readonly ContextLoopRow[];
  /** Picking a `//` row navigates there — the page owns the router. */
  onOpenLoop?: (loopId: string) => void;
  /** The menu's "Classic view". Omit and the menu is not shown. */
  onClassic?: () => void;
  /** Replace the Files / open-file tabs (the bench has no workspace to list). */
  renderFiles?: (openFile: (path: string) => void) => ReactNode;
  renderFile?: (path: string) => ReactNode;
  /** S42's FIFO history window — passed straight through to the chat stream. */
  onLoadOlder?: () => void;
  loadingOlder?: boolean;
  hasOlder?: boolean;
}

/**
 * The loop page as a picture of the data it is given, arranged by the kit's
 * `PageBodyLayout` (0.2.14: zones only, sized by their content): the workflow
 * rail and agents in the left zone, the chat in the centre with the terminal
 * dock beneath it, the inspector — a docked `SidePanel` — in the right zone,
 * and the page bar as the layout's header (the app header above it still
 * carries the breadcrumb and nav). Each zone's component collapses and
 * resizes itself in place and the centre follows; this owns only view state
 * — which lane is selected, which agent the chat is zoomed to, which files
 * are open, what is collapsed — so the page (live data) and the bench
 * (scripted data) draw exactly the same thing.
 */
export function LoopWorkspaceView({ loopId, onRefresh, todos, onTodos, loop, live, connected, items, session, steps, git, gitChanges, chat, routes, componentRoot, contextLoops, onOpenLoop, onClassic, renderFiles, renderFile, onLoadOlder, loadingOlder, hasOlder }: LoopWorkspaceViewProps) {
  const [selectedLane, setSelectedLane] = useState<string | null>(null);
  const [agent, setAgent] = useState('');
  const [tab, setTab] = useState('step');
  const [openFiles, setOpenFiles] = useState<string[]>([]);
  const [terminalOpen, setTerminalOpen] = useState(false);
  const [railCollapsed, setRailCollapsed] = usePersisted('aeon.loop.rail', isNarrow());
  // Open by default so the current step's own activity is on screen the
  // moment a loop loads, not only after a manual toggle — narrow windows
  // still start closed, the same rule `isNarrow` already gives the rail.
  const [inspectorOpen, setInspectorOpen] = usePersisted('aeon.loop.inspector', !isNarrow());
  const [terminalHeight, setTerminalHeight] = usePersisted('aeon.terminal.height', 240);
  const [inspectorWidth] = usePersisted('aeon.loop.inspector.width', 380);
  // S77: the composer draft is host-owned (context pickers INSERT into it —
  // the composer computes the splice, this file owns the words).
  const [draft, setDraft] = useState<ComposerDraft>({ text: '', attachments: [] });
  useEffect(() => {
    setSelectedLane(null);
    setAgent('');
    setTab('step');
    setOpenFiles([]);
    setRunPanelOpen(false);
    setDraft({ text: '', attachments: [] });
  }, [loopId]);

  const running = session.runs.filter((r) => r.state === 'running');
  const ended = session.loopStatus === 'finished' || session.loopStatus === 'blocked' || (session.loopStatus === 'unknown' && loop !== null && ENDED.includes(loop.status));
  // S39 D3/D8: `idle` is REST, not end. The loop stands with nothing in
  // flight — the composer STAYS (in message mode) where a finished loop's
  // goes away. `ended` deliberately never learns the word.
  const idle = session.loopStatus === 'idle' || (session.loopStatus === 'unknown' && loop?.status === 'idle');
  // D3, exactly: a turn starting is the moment the composer becomes a steer
  // again — the person who is being answered is the one being steered.
  const messageMode = idle && running.length === 0;
  const [runPanelOpen, setRunPanelOpen] = useState(false);
  const now = useNow(connected && !ended && !idle);
  // S67: the catalog reaches the composer here — `/` and the plus menu open
  // the skills picker, chosen skills ride the send as chips woven into the
  // text. All the meaning lives in useSkillPicker; this host only spreads.
  const skills = useSkillPicker(connected);
  // S77: the context-aware panels, scoped to THIS loop's component. `@` names
  // its files (changed + code search — the same lib the home composer uses),
  // `//` the loops the page was given (picking navigates), `$` the agents.
  // The workspace loop's own files browse (`loop.files`) may widen `@` later;
  // what exists today is the component-scoped pair, spoken not invented.
  const context = useContextPickers({
    connected,
    componentRoot,
    loops: contextLoops ?? [],
    onOpenLoop: (id) => onOpenLoop?.(id),
    onInsert: useCallback((request, replacement) => {
      setDraft((current) => ({ ...current, text: applyTokenInsert(current.text, request, replacement) }));
    }, []),
  });

  const openFile = useCallback(
    (path: string) => {
      setOpenFiles((files) => (files.includes(path) ? files : [...files, path]));
      setTab(`file:${path}`);
      setInspectorOpen(true);
    },
    [setInspectorOpen],
  );
  const closeFile = (path: string) => {
    setOpenFiles((files) => files.filter((f) => f !== path));
    setTab((current) => (current === `file:${path}` ? 'files' : current));
  };
  // "view diff" in the chat opens the file in the inspector, where there is room for it.
  const [accessSignal, setAccessSignal] = useState(0);
  const [askSignal, setAskSignal] = useState(0);
  const [accessState, setAccessState] = useState<{ mode: 'ask' | 'tools' | 'full'; label: string } | undefined>();
  const actions = useMemo(() => ({ ...chat.actions, onViewDiff: (file: { path: string }) => openFile(file.path), onAllowAll: () => setAccessSignal((n) => n + 1), onShowTasks: () => { setTab('tasks'); setInspectorOpen(true); }, allowedAll: accessState?.mode === 'full' }), [chat.actions, openFile, accessState?.mode]);

  const lane = selectedLane ?? loop?.position.lane ?? '';
  // The selected step's calls come from the journal narrowed to that step when the page has none loaded for it, so an
  // old step lists its calls without the whole loop's history being read.
  const laneStepIds = useMemo(() => (loop ? [...session.byStep.values()].filter((r) => r.lane === lane).map((r) => r.stepId) : []), [loop, session.byStep, lane]);
  const stepsWithCalls = useMemo(() => new Set(items.flatMap((i) => (i.kind === 'tool' && i.stepId !== undefined ? [i.stepId] : []))), [items]);
  const laneCalls = useLaneCalls(loopId, laneStepIds, stepsWithCalls, loop !== null);
  const modelItems = useMemo(() => (laneCalls.length === 0 ? items : [...laneCalls, ...items]), [items, laneCalls]);
  const summaries = steps?.summaries;
  const workflow = useMemo(() => (loop ? { ...workflowFromLoop(loop, session, modelItems, loop.lastReason, now, summaries), activeStepId: lane } : null), [loop, session, modelItems, now, lane, summaries]);
  const telemetry = useMemo(() => (loop ? stepTelemetry(loop, session, modelItems, lane, now) : null), [loop, session, modelItems, now, lane]);
  const laneSummary = useMemo(() => (summaries ? laneAggregate(summaries, lane) : undefined), [summaries, lane]);
  const scope = useMemo(() => filesInScope(items), [items]);
  const changes = useMemo(() => fileChanges(items), [items]);
  const cost = useMemo(() => (loop ? footprint(loop) : { perStep: [], totalUsd: undefined, partial: false }), [loop]);
  // Every agent that speaks in the chat wears its avatar, not just the ones
  // with a run row: a chat agent's first turn lands before its run is listed.
  const avatars = useAgentAvatars(useMemo(() => {
    const slugs = new Set(session.runs.map((r) => r.agent));
    for (const item of items) if ('agent' in item && typeof item.agent === 'string') slugs.add(item.agent);
    slugs.delete('you');
    return [...slugs];
  }, [session.runs, items]));
  const todoPanel = useTodoPanel(loopId, todos, connected, onTodos);
  // The changes the agents made: the inspector's Changes tab shows them in full; a panel above the to-dos shows the
  // first few, and is only on screen when the person turns it on (off by default).
  const [showChanges, setShowChanges] = useState(false);
  const [changesFocus, setChangesFocus] = useState<string | undefined>();
  const changedFiles = gitChanges?.state === 'ok' ? (gitChanges.files ?? []) : [];
  const openChanges = useCallback((path?: string) => {
    setChangesFocus(path);
    setTab('changes');
    setInspectorOpen(true);
  }, [setInspectorOpen]);
  const changesPanel = useMemo(() => (showChanges && changedFiles.length > 0
    ? { files: changedFiles, onOpenFile: (path: string) => openChanges(path), onOpenAll: () => openChanges() } : undefined), [showChanges, changedFiles, openChanges]);
  // S76: an agent's avatar wears the mood its situation gives it (waiting on a
  // person is stuck, working while it works, idle at rest). A chat opened after
  // the fact has no events yet, so the loop's own status stands in for the
  // stream's.
  const rawStatus = session.loopStatus !== 'unknown' ? session.loopStatus
    : loop?.status === 'finished' || loop?.status === 'blocked' || loop?.status === 'waiting' || loop?.status === 'running' ? loop.status : 'unknown' as const;
  // `done` is a moment, not a place to live: a few seconds after the loop
  // finishes the agent rests again (S76).
  const [doneFaded, setDoneFaded] = useState(false);
  useEffect(() => {
    if (rawStatus !== 'finished') { setDoneFaded(false); return; }
    const timer = setTimeout(() => setDoneFaded(true), DONE_MOOD_MS);
    return () => clearTimeout(timer);
  }, [rawStatus]);
  const moodSession = useMemo(() => ({
    runs: session.runs,
    loopStatus: rawStatus === 'finished' && doneFaded ? 'idle' as const : rawStatus,
  }), [session.runs, rawStatus, doneFaded]);
  const agents = useMemo(() => {
    const out: Record<string, { id: string; role?: string; avatar?: { type: string; color: string; mood?: string } }> = {};
    for (const [slug, avatar] of Object.entries(avatars)) out[slug] = { id: slug, avatar: { ...avatar, mood: agentMood(slug, items, moodSession) } };
    for (const r of session.runs) out[r.agent] = { ...out[r.agent], id: r.agent, role: r.step };
    return out;
  }, [session.runs, avatars, items, moodSession]);

  const target = agent || running[0]?.agent;
  const run = target ? session.runs.find((r) => r.agent === target) : undefined;
  const index = loop ? loop.lanes.findIndex((l) => l.id === lane) : -1;
  const stepForTab = workflow?.steps.find((s) => s.id === lane);
  const status = [loop?.status ?? 'opening', loop?.position.lane].filter(Boolean).join(' · ');

  // Since kit 0.2.14 PageBodyLayout is zones only — each zone is exactly as
  // big as what is put in it, and the zone's component collapses, animates
  // and resizes itself in place. The rail owns its fold (340px open, the
  // kit's rail width collapsed) and the agents panel rides folded inside it.
  const rail = (
    <WorkflowTrackerRail
      corner="rounded-sm"
      side="left"
      data={workflow ?? undefined}
      loading={!workflow}
      collapsible
      fillHeight
      disableFutureSteps
      collapsed={railCollapsed}
      variant="simple"
      onCollapsedChange={setRailCollapsed}
      style={{ width: 340 }}
      onStepSelect={(stepId) => {
        setSelectedLane(stepId);
        setTab('step');
      }}
      renderControls={({ collapsed }) => (
        <AgentsPanel collapsed={collapsed} runs={session.runs} items={items} selected={agent} onSelect={setAgent} footprint={cost} live={!ended && connected} agents={agents} />
      )}
    />
  );

  // The inspector is the kit's docked SidePanel: it keeps its own width
  // (dragged on its edge, 280–560), its collapse animation and its rail
  // strip; the zone, and the chat, simply follow. `inspectorWidth` is the
  // starting width — the panel offers no width callback to persist a drag.
  const inspector = (
    <Inspector
      open={inspectorOpen}
      onOpenChange={setInspectorOpen}
      width={inspectorWidth}
      tab={tab}
      onTabChange={setTab}
      openFiles={openFiles.map((path) => ({ path, added: changes.find((c) => c.path === path)?.linesAdded }))}
      onCloseFile={closeFile}
      step={
        telemetry && stepForTab && loop ? (
          <StepTab telemetry={telemetry} step={stepForTab} laneSummary={laneSummary} loopTotals={steps?.totals} summaries={summaries} position={{ index: index + 1, total: loop.lanes.length }} onOpenFile={openFile} />
        ) : (
          <p className="p-4 text-sm text-slate-500">No step selected.</p>
        )
      }
      changes={gitChanges !== undefined ? <ChangesTab componentRoot={componentRoot} changes={gitChanges} focusPath={changesFocus} /> : undefined}
      changesCount={changedFiles.length}
      documents={loop ? <DocumentsTab loopId={loopId} refreshKey={loop.updatedAt} /> : undefined}
      tasks={loop ? <TasksTab slug={loop.slug} componentRoot={loop.root || componentRoot} refreshKey={loop.updatedAt} /> : undefined}
      files={renderFiles ? renderFiles(openFile) : <FilesTab componentRoot={componentRoot} scope={scope} changes={changes} git={git} onOpenFile={openFile} />}
      file={(path) => (renderFile ? renderFile(path) : <FileTab componentRoot={componentRoot} path={path} />)}
    />
  );

  // The dock drags its own top edge and clamps to its own MIN_HEIGHT and
  // maxHeight(); while it is closed the zone is simply not there.
  const terminal =
    terminalOpen && componentRoot !== '' ? (
      <TerminalDock componentRoot={componentRoot} open height={terminalHeight} onHeightChange={setTerminalHeight} onClose={() => setTerminalOpen(false)} />
    ) : null;

  return (
    <div className="flex h-full min-h-0 w-full flex-col" data-testid="loop-workspace">
      <HeaderPortal side="start">
        <HeaderBreadcrumb blueprint={loop?.blueprint} root={loop?.root} />
      </HeaderPortal>
      <HeaderPortal side="end">
        <HeaderNav loopId={loopId} live={live} />
      </HeaderPortal>
      {/* The page header slot is not mounted in this shell, so the way back lives here: a slim bar, always visible. */}
      <div className="flex h-10 shrink-0 items-center border-b border-slate-200 px-3 dark:border-white/10" data-testid="loop-topbar">
        <HeaderBreadcrumb blueprint={loop?.blueprint} root={loop?.root} />
        <span className="flex-1" />
        <LoopAccess loopId={loopId} live={live} canWrite={connected} openSignal={accessSignal} askSignal={askSignal} onAccess={(mode, label) => setAccessState((cur) => (cur?.mode === mode && cur.label === label ? cur : { mode, label }))} />
      </div>
      <GradientBackdrop theme="graphite" className="min-h-0 flex-1">
        <PageBodyLayout
          corner="none"
          variant="elevated"
          padding="none"
          header={
            <PageBar
              title="Loop view"
              loopTitle={loop?.title}
              loopGoal={loop?.goal}
              agent={agent || undefined}
              onBackToLoop={() => setAgent('')}
              status={status}
              git={git}
              onOpenChanges={gitChanges !== undefined ? () => openChanges() : undefined}
              terminalOpen={terminalOpen}
              onToggleTerminal={() => setTerminalOpen((v) => !v)}
              inspectorOpen={inspectorOpen}
              onToggleInspector={() => setInspectorOpen(!inspectorOpen)}
              menu={onClassic ? [{ label: 'Classic view', onSelect: onClassic }] : undefined}
            />
          }
          left={rail}
          right={inspector}
          bottom={terminal}
        >
          {/* The kit row is `min-width: fit-content`, so any unwrapped line in the chat
              (a long question, the blocked reason) sets the page's minimum width and
              the shell scrolls sideways. Inline-size containment makes the chat
              contribute no intrinsic width: it takes the room it is given. */}
          <div className="h-full w-full min-w-0" style={{ contain: 'inline-size' }}>
          <AgentChat
            className="h-full rounded-none"
            items={items}
            agents={agents}
            actions={actions}
            agentFilter={agent || undefined}
            readOnly={ended}
            liveClock
            working={!ended && connected && running.length > 0}
            onLoadOlder={onLoadOlder}
            loadingOlder={loadingOlder}
            hasOlder={hasOlder}
            empty={<p className="py-10 text-center text-sm text-slate-500">No activity for this loop in the live window yet.</p>}
            taskList={todoPanel}
            changes={changesPanel}
            header={idle || loop?.attended !== undefined || loop?.status === 'blocked' ? (
              <div>
                {loop?.status === 'blocked' && <LoopBlockedBanner loop={loop} canWrite={connected} onResumed={() => void onRefresh?.()} />}
                {/* CR7/D8: the loop's declared attendedness, stated once where
                    the person decides — for resting and running loops alike.
                    It hangs in the header, the one host-owned strip above the
                    stream and composer (same slot constraint T11 recorded for
                    the panel). */}
                <AttendednessNotice attended={loop?.attended} />
                {idle && (<>
                <div className="flex items-center justify-end">
                  <button type="button" onClick={() => setRunPanelOpen((v) => !v)} data-testid="run-blueprint-open"
                    className="inline-flex items-center gap-1.5 rounded-lg border border-slate-300 bg-white px-3 py-1 text-sm font-medium text-slate-700 hover:bg-slate-50 dark:border-white/20! dark:bg-transparent! dark:text-slate-200!">
                    Run blueprint…
                  </button>
                </div>
                {/* The dialogue BEFORE the start (CR5): what can run, with the daemon's verdict.
                    Closing it calls nothing — `loop.run.started` is what moves the view. */}
                {runPanelOpen && <RunBlueprintPanel load={chat.loadRunOptions} onRun={chat.onRunBlueprint} onClose={() => setRunPanelOpen(false)} />}
                </>)}
              </div>
            ) : undefined}
            composer={{
              mode: messageMode ? 'message' : 'steer',
              access: accessState ? { mode: accessState.mode, label: accessState.mode === 'full' ? 'Approve all' : accessState.mode === 'tools' ? accessState.label : 'Ask', disabled: !connected, onAsk: () => setAskSignal((n) => n + 1), onFull: () => setAccessSignal((n) => n + 1) } : undefined,
              // S68: the chosen skills ride the send as a `skills` FIELD —
              // the operator's words stay pure and the daemon's door turns
              // the names into instruction lines in the turn's system half.
              // The chips above the textarea state exactly what will ride;
              // they clear only when the send resolves. The attaching itself
              // lives in the hook (one rule for every host).
              onMessage: chat.onMessage ? skills.messageWith(chat.onMessage) : undefined,
              // Every agent that took part is listed (the running ones first), so the picker is whole after a reload;
              // a steer still needs one running, which `steerable` says.
              targets: [...session.runs].sort((a, b) => Number(b.state === 'running') - Number(a.state === 'running')).map((r) => ({ value: r.agent, label: r.state === 'running' ? r.agent : `${r.agent} · ${r.state}` })),
              steerable: running.length > 0,
              changesToggle: gitChanges !== undefined ? { count: changedFiles.length, on: showChanges, onToggle: () => setShowChanges((v) => !v) } : undefined,
              routes,
              efforts: EFFORTS,
              // What the agent is on; a reopened page has no run event for it, so the loop's own settings stand in.
              currentRoute: run?.route ?? loop?.route,
              currentEffort: run?.reasoningEffort ?? loop?.reasoningEffort,
              loopOptions: {
                thinking: loop?.thinking ?? '',
                effort: loop?.reasoningEffort ?? '',
                disabled: !connected,
                onChange: (o: { thinking?: string; reasoningEffort?: string }) => chat.onLoopOptions({ thinking: loop?.thinking ?? '', reasoningEffort: loop?.reasoningEffort ?? '', ...o }),
              },
              onSteer: skills.steerWith(chat.onSteer),
              onStop: chat.onStop,
              // S77: the draft is host-owned and BOTH picker sets answer the
              // composer's one delegate — each opens for its own keys and
              // closes for the rest (the merge is the lib's, no host logic).
              value: draft,
              onChange: setDraft,
              onSpecialKeyChange: mergeSpecialKeyHandlers([skills.onSpecialKeyChange, context.onSpecialKeyChange]),
              onAddAction: mergeAddActionHandlers([skills.onAddAction, context.onAddAction]),
              chips: skills.chips,
              disabled: !connected || (!messageMode && running.length === 0),
              // The model controls stay on when no agent runs: they set the loop's choice for its next step.
              disabledReason: !connected ? 'Disconnected from the daemon — reconnecting…' : messageMode ? undefined : 'No agent is running — nothing to steer.',
            }}
            picker={(skills.panel ?? context.panel) ?? undefined}
            onPickerDismiss={() => { skills.close(); context.close(); }}
          />
          </div>
        </PageBodyLayout>
      </GradientBackdrop>
    </div>
  );
}
