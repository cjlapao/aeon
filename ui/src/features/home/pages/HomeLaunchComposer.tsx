/**
 * HomeLaunchComposer — the Home screen's launch composer (S77 host
 * integration), built on the agent-chat `MessageComposer`: the SAME input
 * surface the chat hosts use, given launch meaning.
 *
 * What the composer surface brings and this file wires:
 *  - **attachments** — files staged here ride the launch: bytes are put
 *    ONCE at `attachment.put` under a minted slug + the chosen component,
 *    and `loop.start` carries the receipts beside the goal. The daemon
 *    receipts them onto the kickoff row (S77), and every removal before
 *    send is the composer's built-in chip × — nothing staged is ever
 *    un-removable. With NOTHING staged the start sends EXACTLY the pre-
 *    attachment params (absent, never `[]`).
 *  - **the capability gate** — a file whose type the chosen route does
 *    not declare (absent ≠ yes, P31) DISABLES the launch with the daemon's
 *    own sentence per file, before a byte is put.
 *  - **context pickers** — `/` skills (S67/S68), `@` the SELECTED
 *    component's files (gated until one is chosen), `//` the loops there
 *    (picking navigates), `$` the agents. The picker LOGIC is the lib's
 *    (`useContextPickers`, `useSkillPicker`); this file only merges them
 *    into the composer's one delegate and owns the draft they insert into.
 */
import { useCallback, useEffect, useImperativeHandle, useMemo, useRef, useState, type Ref } from 'react';
import { Link } from 'react-router-dom';
import { Button, Picker, Select, type PickerItem, type PickerTag } from '@cjlapao/ui-kit';
import { rpcTyped } from '../../../lib/bridge';
import { useI18n } from '@cjlapao/ui-kit';
import { useSkillPicker } from '../../../lib/skills';
import { useContextPickers, mergeAddActionHandlers, mergeSpecialKeyHandlers, applyTokenInsert, type ContextSpecialRequest } from '../../../lib/contextPickers';
import { attachmentLaunchGate, putAllAttachments, type RouteCapabilityView } from '../../../lib/attachments';
import { REASONING_EFFORTS } from '../../../lib/protocol';
import { MessageComposer, type ComposerDraft } from '../../../components/agent-chat/MessageComposer';
import { ComponentIcon } from '../../components/ComponentIcon';

export interface LaunchBlueprintReport {
  invalid: number;
  unverifiable: number;
  unavailable?: string;
  error?: string;
}

/** What the component picker reads off a `component.list` row. */
export interface LaunchComponentRow {
  root: string;
  name?: string;
  icon?: string;
  slug?: string;
  shortDescription?: string;
  tags?: readonly string[];
}

/** What the provider picker and the capability gate read off a `routes.list` row. */
export interface LaunchRouteRow extends RouteCapabilityView {
  /** The wire flags the route declares; `thinking` here means the model can think. */
  wireCapabilities?: readonly string[] | null;
  /** The efforts the route accepts, and the one it carries. Either says the effort dial exists. */
  reasoningEfforts?: readonly string[] | null;
  reasoningEffort?: string;
  id: string;
  displayName?: string;
  provider?: string;
  model?: string;
}

export interface HomeLaunchComposerHandle {
  /** The pinned component's "Quick launch": select it, scroll here, focus the goal. */
  focusWithComponent: (root: string) => void;
}

const QUICK_STARTERS: Array<{ labelKey: string; goal: string }> = [
  { labelKey: 'home.quickStarter.automatedQa', goal: 'Write and run an automated QA pass across the changed files, and report any regressions.' },
  { labelKey: 'home.quickStarter.prReview', goal: 'Review the open pull request for correctness, security and style issues, and propose fixes.' },
  { labelKey: 'home.quickStarter.fixFailingTests', goal: 'Find the failing tests in this workspace, diagnose the root cause, and fix them.' },
];

/** A launch that attaches files must name its OWN slug: the puts bind to
 * it, and `loop.start` checks the receipts against it (a minted slug owns
 * no earlier puts — the daemon's own rule at the door). Minted from the
 * goal's words plus a short time tag so two launches never share a scope. */
export function mintLaunchSlug(goal: string): string {
  const words = goal.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '').slice(0, 40).replace(/-+$/g, '');
  return `${words || 'launch'}-${Date.now().toString(36)}`;
}

function componentLabel(component: LaunchComponentRow): string {
  return component.name || component.root.split(/[\\/]+/).filter(Boolean).pop() || component.root;
}

/** True for a model that declares speech or embedding work and does not declare text. */
export const isNonTextModel = (capabilities: readonly string[] | null | undefined): boolean =>
  Boolean(capabilities?.some((c) => c === 'asr' || c === 'tts' || c === 'embedding') && !capabilities.includes('text'));

/** A reasoning effort as a person reads it ("xhigh" is Extra High); an effort this app has no name for shows as the daemon spells it. */
export const EFFORT_KEYS = ['none', 'minimal', 'low', 'medium', 'high', 'xhigh', 'max'] as const;
export const effortLabel = (t: (key: string) => string, value: string): string => ((EFFORT_KEYS as readonly string[]).includes(value) ? t(`home.composer.effortName.${value}`) : value);

/** The small caption above each advanced field: the same everywhere, so the fields share one rhythm. */
const FIELD_LABEL = 'mb-1 block text-xs font-medium opacity-80';

export function HomeLaunchComposer({ connected, canWrite, loops, components, blueprints, blueprintInfo = {}, blueprintReports, routes: allRoutes, onLaunched, onOpenLoop, handleRef }: {
  connected: boolean;
  canWrite: boolean;
  loops: readonly { id: string; slug: string; title?: string; status: string; componentRoot: string }[];
  components: readonly LaunchComponentRow[];
  blueprints: readonly string[];
  /** Each flow's title and description by slug; a flow without one shows its slug alone. */
  blueprintInfo?: Readonly<Record<string, { title: string; description: string }>>;
  blueprintReports: Record<string, LaunchBlueprintReport>;
  routes: readonly LaunchRouteRow[];
  /** The started loop: the host navigates to it. */
  onLaunched: (loopId: string) => void;
  onOpenLoop: (loopId: string) => void;
  handleRef?: Ref<HomeLaunchComposerHandle>;
}) {
  const { t } = useI18n();
  // A loop is run by a text model. A model that declares itself a speech or embedding model (and not a text one) is in the
  // list for other jobs and cannot be chosen here. A model that declares nothing (saved before capabilities existed), or
  // only things like tool-calling or vision, is still offered: it did not say it cannot write text.
  const routes = useMemo(() => allRoutes.filter((item) => !isNonTextModel(item.capabilities)), [allRoutes]);
  const panelRef = useRef<HTMLDivElement>(null);
  const goalRef = useRef<HTMLTextAreaElement>(null);
  const [draft, setDraft] = useState<ComposerDraft>({ text: '', attachments: [] });
  const [componentRoot, setComponentRoot] = useState('');
  const [blueprint, setBlueprint] = useState('');
  const [route, setRoute] = useState('');
  const [mode, setMode] = useState('standard');
  // The model options, chosen per launch: thinking on/off and the effort. Empty keeps the route's own.
  const [thinking, setThinking] = useState('');
  const [effort, setEffort] = useState('');
  const [advancedOpen, setAdvancedOpen] = useState(false);

  // Lists self-default (the first flow, the first provider) exactly as the
  // old composer did — but never overwrite a choice that still holds.
  useEffect(() => {
    setBlueprint((current) => (blueprints.includes(current) ? current : blueprints[0] || ''));
  }, [blueprints]);
  useEffect(() => {
    setRoute((current) => (routes.some((item) => item.id === current) ? current : routes[0]?.id || ''));
  }, [routes]);

  const skills = useSkillPicker(connected);
  const context = useContextPickers({
    connected,
    componentRoot,
    loops,
    onOpenLoop,
    onInsert: useCallback((request: ContextSpecialRequest, replacement: string) => {
      setDraft((current) => ({ ...current, text: applyTokenInsert(current.text, request, replacement) }));
      goalRef.current?.focus();
    }, []),
    copy: {
      filesTitle: t('home.composer.panel.filesTitle'),
      filesGate: t('home.composer.panel.filesGate'),
      filesEmpty: t('home.composer.panel.filesEmpty'),
      filesSearching: t('home.composer.panel.filesSearching'),
      loopsTitle: t('home.composer.panel.loopsTitle'),
      loopsEmpty: t('home.composer.panel.loopsEmpty'),
      agentsTitle: t('home.composer.panel.agentsTitle'),
      agentsEmpty: t('home.composer.panel.agentsEmpty'),
      commandsTitle: t('home.composer.panel.commandsTitle'),
      commandsEmpty: t('home.composer.panel.commandsEmpty'),
    },
  });

  useImperativeHandle(handleRef, () => ({
    focusWithComponent: (root: string) => {
      setComponentRoot(root);
      panelRef.current?.scrollIntoView?.({ behavior: 'smooth', block: 'center' });
      goalRef.current?.focus();
    },
  }), []);

  const componentPickerItems = useMemo<PickerItem[]>(
    () => components.map((item) => ({
      id: item.root,
      icon: <ComponentIcon icon={item.icon ?? ''} size="xs" name={item.name ?? ''} slug={item.slug ?? ''} border={false} />,
      title: componentLabel(item),
      subtitle: item.root,
      description: item.shortDescription || undefined,
      tags: (item.tags ?? []).slice(0, 3).map((tag) => ({ label: tag, tone: 'blue' })) as PickerTag[],
    })),
    [components],
  );
  const blueprintPickerItems = useMemo<PickerItem[]>(
    () => blueprints.map((name) => {
      const report = blueprintReports[name];
      const tags: PickerTag[] = [];
      if (report?.error) tags.push({ label: t('home.composer.flowSuffix.validationUnavailable'), tone: 'red' });
      else if (report?.invalid) tags.push({ label: t('home.composer.flowSuffix.invalid'), tone: 'red' });
      else if (report?.unavailable) tags.push({ label: t('home.composer.flowSuffix.needsLaterSlice'), tone: 'amber' });
      else if (report?.unverifiable) tags.push({ label: t('home.composer.flowSuffix.cannotFullyVerify'), tone: 'amber' });
      const info = blueprintInfo[name];
      const title = info?.title?.trim();
      // The title is what a person reads and searches; the slug is the secondary line, as a project shows its folder.
      return { id: name, title: title || name, subtitle: title && title !== name ? name : undefined, description: info?.description?.trim() || undefined, tags };
    }),
    [blueprints, blueprintInfo, blueprintReports, t],
  );
  const providerPickerItems = useMemo<PickerItem[]>(
    () => routes.map((item) => ({
      id: item.id,
      title: item.displayName || item.id,
      subtitle: [item.provider, item.model].filter(Boolean).join(' · ') || undefined,
    })),
    [routes],
  );

  const routeRow = useMemo(() => routes.find((item) => item.id === route), [routes, route]);
  // Offered only where the chosen route says the model has them: a thinking switch for a route that declares
  // `thinking`, an effort dial for one that lists efforts or carries one. Changing route drops a choice the new route
  // does not have, so a launch never sends an option its model cannot take.
  const thinkingEnabled = Boolean(routeRow?.capabilities?.includes('thinking') || routeRow?.wireCapabilities?.includes('thinking'));
  const effortChoices = useMemo(() => (routeRow?.reasoningEfforts?.length ? [...routeRow.reasoningEfforts] : routeRow?.reasoningEffort ? [...REASONING_EFFORTS] : []), [routeRow]);
  useEffect(() => { if (!thinkingEnabled) setThinking(''); }, [thinkingEnabled]);
  useEffect(() => { if (effort && !effortChoices.includes(effort)) setEffort(''); }, [effort, effortChoices]);
  // The gate speaks BEFORE a byte is put: per staged file, the daemon's
  // own words about what the chosen route does not declare. Empty = every
  // file can ride.
  const gateSentences = useMemo(
    () => attachmentLaunchGate(draft.attachments.map((attachment) => attachment.file), routeRow),
    [draft.attachments, routeRow],
  );

  const report = blueprintReports[blueprint];
  const launchDisabled =
    !canWrite ||
    !blueprint || !report || !!report.invalid || !!report.unavailable || !!report.error ||
    !draft.text.trim() ||
    !componentRoot.trim() ||
    !route.trim() ||
    gateSentences.length > 0;

  async function launch(submitted: ComposerDraft) {
    const goal = submitted.text.trim();
    const files = submitted.attachments.map((attachment) => attachment.file);
    // Bytes once, refs forever (S69 rule, S77 launch): put under the slug
    // this launch is about to claim, then start carrying the receipts.
    // Nothing staged → the field stays ABSENT and the start is the exact
    // pre-attachment request, byte for byte.
    const slug = files.length > 0 ? mintLaunchSlug(goal) : undefined;
    const attachments = slug ? await putAllAttachments({ slug, componentRoot: componentRoot.trim() }, files) : undefined;
    const chosen = skills.chosenNames();
    const started = await rpcTyped('loop.start', {
      blueprint, goal, workspaceRoot: componentRoot.trim(), route, mode,
      ...(thinkingEnabled && thinking ? { thinking } : {}),
      ...(effortChoices.length > 0 && effort ? { reasoningEffort: effort } : {}),
      // A loop started from the app is started by a person at the keyboard: the
      // declaration (D2) the daemon records, so asks reach a question they answer.
      attended: true,
      ...(chosen.length > 0 ? { skills: chosen } : {}),
      ...(slug ? { slug } : {}),
      ...(attachments ? { attachments } : {}),
    });
    if (chosen.length > 0) skills.clear();
    onLaunched(started.id);
  }

  const openPanel = skills.panel ?? context.panel;
  const quickStart = (goal: string) => {
    setDraft((current) => ({ ...current, text: goal }));
    goalRef.current?.focus();
  };

  return (
    <section ref={panelRef} className="w-full min-w-0 max-w-[80%] space-y-4 max-md:max-w-full">
      <MessageComposer
        data-testid="home-launch-composer"
        variant="standalone"
        value={draft}
        onChange={setDraft}
        onSubmit={launch}
        submitDisabled={launchDisabled}
        submitLabel={t('home.composer.launch')}
        submittingLabel={t('home.composer.launching')}
        submitIconOnly
        disabled={!canWrite}
        placeholder={t('home.composer.placeholder')}
        inputLabel={t('home.composer.placeholder')}
        inputRef={goalRef}
        rows={3}
        chips={skills.chips}
        specialKeys={['/', '//', '@', '$', 'commands']}
        onSpecialKeyChange={mergeSpecialKeyHandlers([skills.onSpecialKeyChange, context.onSpecialKeyChange])}
        onAddAction={mergeAddActionHandlers([skills.onAddAction, context.onAddAction])}
        picker={openPanel ?? undefined}
        onPickerDismiss={() => { skills.close(); context.close(); }}
        selectors={
          <>
            <label className="block min-w-50 flex-1 text-sm" data-testid="composer-component-field">
              <Picker
                items={componentPickerItems}
                selectedId={componentRoot}
                onSelect={(item) => setComponentRoot(item.id)}
                disabled={!canWrite || components.length === 0}
                size="md"
                variant="elevated"
                placeholder={t('home.composer.componentPlaceholder')}
                searchPlaceholder={t('home.composer.componentSearchPlaceholder')}
              />
            </label>
            <label className="block min-w-50 flex-1 text-sm" data-testid="composer-flow-field">
              <span className="sr-only">{t('home.composer.flow')}</span>
              <Picker
                items={blueprintPickerItems}
                selectedId={blueprint}
                onSelect={(item) => setBlueprint(item.id)}
                disabled={!canWrite || blueprints.length === 0}
                size="md"
                variant="elevated"
                placeholder={t('home.composer.flowPlaceholder')}
                searchPlaceholder={t('home.composer.flowSearchPlaceholder')}
              />
            </label>
          </>
        }
        footer={
          <div className="space-y-2">
            {advancedOpen && (
              // One grid, one label style, one control size, so the fields line up on their tops whatever a model offers:
              // thinking and effort appear only where the chosen model has them, and the rest do not move.
              <div className="grid grid-cols-1 items-start gap-x-3 gap-y-3 sm:grid-cols-2 lg:grid-cols-4" data-testid="composer-advanced">
                <label className="col-span-full block min-w-0 text-sm lg:col-span-2" data-testid="composer-provider-field">
                  <span className={FIELD_LABEL}>{t('home.composer.provider')}</span>
                  <Picker
                    items={providerPickerItems}
                    selectedId={route}
                    onSelect={(item) => setRoute(item.id)}
                    disabled={!canWrite || routes.length === 0}
                    size="md"
                    variant="elevated"
                    placeholder={t('home.composer.providerPlaceholder')}
                    searchPlaceholder={t('home.composer.providerSearchPlaceholder')}
                  />
                </label>
                <label className="col-span-full block min-w-0 text-sm lg:col-span-2" data-testid="composer-mode-field">
                  <span className={FIELD_LABEL}>{t('home.composer.mode')}</span>
                  <Select value={mode} onChange={(e) => setMode(e.target.value)} disabled={!canWrite} size="md" data-testid="composer-mode">
                    <option value="standard">{t('home.composer.modeStandard')}</option>
                    <option value="simple">{t('home.composer.modeSimple')}</option>
                    <option value="poc">{t('home.composer.modePoc')}</option>
                  </Select>
                  <span className="mt-1 block text-xs opacity-70" data-testid="composer-mode-hint">{t(`home.composer.mode${mode === 'poc' ? 'Poc' : mode === 'simple' ? 'Simple' : 'Standard'}Hint`)}</span>
                </label>
                {thinkingEnabled && (
                  <label className="block min-w-0 text-sm sm:col-span-1 lg:col-span-2" data-testid="composer-thinking-field">
                    <span className={FIELD_LABEL}>{t('home.composer.thinking')}</span>
                    <Select value={thinking} onChange={(e) => setThinking(e.target.value)} disabled={!canWrite} size="md" data-testid="composer-thinking">
                      <option value="">{t('home.composer.thinkingDefault')}</option>
                      <option value="on">{t('home.composer.thinkingOn')}</option>
                      <option value="off">{t('home.composer.thinkingOff')}</option>
                    </Select>
                  </label>
                )}
                {effortChoices.length > 0 && (
                  <label className="block min-w-0 text-sm sm:col-span-1 lg:col-span-2" data-testid="composer-effort-field">
                    <span className={FIELD_LABEL}>{t('home.composer.effort')}</span>
                    <Select value={effort} onChange={(e) => setEffort(e.target.value)} disabled={!canWrite} size="md" data-testid="composer-effort">
                      <option value="">{t('home.composer.effortDefault')}</option>
                      {effortChoices.map((value) => <option key={value} value={value}>{effortLabel(t, value)}</option>)}
                    </Select>
                  </label>
                )}
              </div>
            )}
            {skills.selected.length > 0 && (
              <p className="text-[10px] text-slate-400" data-testid="composer-skills-note">{t('home.composer.skillsNote')}</p>
            )}
            {gateSentences.length > 0 && (
              <div role="alert" className="space-y-1 rounded-lg border border-red-200 bg-red-50 p-2 dark:border-red-500/30! dark:bg-red-500/10!">
                <p className="text-sm font-medium text-red-800 dark:text-red-300" data-testid="composer-attachment-gate">{t('home.composer.attachmentsBlocked')}</p>
                {gateSentences.map((sentence) => (
                  <p key={sentence} className="text-xs text-red-700 dark:text-red-300">{sentence}</p>
                ))}
              </div>
            )}
            {!canWrite && <p className="text-sm text-amber-800 dark:text-amber-300">{t('home.composer.connectWarning')}</p>}
            {components.length === 0 && (
              <p className="text-sm text-amber-800 dark:text-amber-300">
                {t('home.composer.noComponents')}{' '}
                <Link to="/projects" className="underline">{t('home.composer.goToProjects')}</Link>
              </p>
            )}
            {blueprints.length === 0 && <p className="text-sm text-amber-800 dark:text-amber-300">{t('home.composer.noFlows')}</p>}
            {routes.length === 0 && (
              <p className="text-sm text-amber-800 dark:text-amber-300">
                {t('home.composer.noProviders')}{' '}
                <Link to="/models" className="underline">{t('home.composer.goToModels')}</Link>
              </p>
            )}
            {report?.error ? (
              <p className="text-sm text-red-700 dark:text-red-300">
                {t('home.composer.validationUnavailable', { error: report.error })}{' '}
                <Link to="/blueprints" className="underline">{t('home.composer.viewDiagnostics')}</Link>.
              </p>
            ) : report?.invalid ? (
              <p className="text-sm text-red-700 dark:text-red-300">
                {t('home.composer.invalidDiagnostics')}{' '}
                <Link to="/blueprints" className="underline">{t('home.composer.viewDiagnostics')}</Link>{' '}
                {t('home.composer.invalidDiagnosticsBefore')}
              </p>
            ) : report?.unavailable ? (
              <p className="text-sm text-amber-800 dark:text-amber-300">
                {t('home.composer.cannotRunYet', { reason: report.unavailable })}{' '}
                <Link to="/blueprints" className="underline">{t('home.composer.viewDiagnostics')}</Link>.
              </p>
            ) : report?.unverifiable ? (
              <p className="text-sm text-amber-800 dark:text-amber-300">
                {t('home.composer.unverifiable')}{' '}
                <Link to="/blueprints" className="underline">{t('home.composer.viewDiagnostics')}</Link>.
              </p>
            ) : null}
          </div>
        }
        actions={
          <Button size="sm" variant="ghost" type="button" onClick={() => setAdvancedOpen((open) => !open)} aria-expanded={advancedOpen}>
            {advancedOpen ? t('home.composer.hideAdvanced') : t('home.composer.showAdvanced')}
          </Button>
        }
      />
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-xs uppercase tracking-[.18em] aeon-accent-text">{t('home.quickStarters')}</span>
        {QUICK_STARTERS.map((starter) => (
          <Button key={starter.labelKey} size="sm" variant="soft" onClick={() => quickStart(starter.goal)}>
            {t(starter.labelKey)}
          </Button>
        ))}
      </div>
    </section>
  );
}
