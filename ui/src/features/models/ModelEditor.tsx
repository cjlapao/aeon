import { useEffect, useState, type ReactNode } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { Alert, Button, ContentActions, Input, Panel, PasswordInput, Select, Spinner, Textarea } from '../../components/kit';
import { PageTitle } from '../../components/PageTitle';
import { rpc, describeError } from '../../lib/bridge';
import type { LiveState } from '../../lib/live';
import { AUDIO_APIS, OPENAI_ENDPOINTS, REASONING_EFFORTS, type RouteView, type RoutesListResult } from '../../lib/protocol';
import { availableEfforts, CAPABILITIES, EFFORT_LABELS, modelDraft, modelParams, readModelIcon, routeIdForSlug, voiceDraft, type ModelDraft } from './modelConfig';
import { ModelIcon } from './ModelIcon';
import { ModelTestPanel } from './ModelTestPanel';
import { ModelVoiceTests } from './ModelVoiceTests';
import { OverlaySidePanel } from '../../components/OverlaySidePanel';

function Group({ title, description, children }: { title: string; description: string; children: ReactNode }) {
  return <Panel variant="simple" clickable={false}><div className="grid gap-6 lg:grid-cols-[200px_1fr]">
    <div><h2 className="text-base font-semibold">{title}</h2><p className="mt-2 text-xs leading-relaxed opacity-70">{description}</p></div>
    <div className="min-w-0 space-y-4">{children}</div>
  </div></Panel>;
}
function Field({ label, hint, children }: { label: string; hint?: string; children: ReactNode }) {
  return <label className="block min-w-0 space-y-1.5 text-sm"><span className="font-medium">{label}</span>{children}{hint && <span className="block text-xs font-normal opacity-60">{hint}</span>}</label>;
}

const SPEECH_API_LABEL: Record<string, string> = { synthesize: 'Synthesize (/synthesize)', 'synthesize-stream': 'Synthesize, streamed (/synthesize/stream)', chat: 'Omni chat (hears and speaks through chat completions)' };
const SPEECH_API_HINT: Record<string, string> = {
  openai: 'Speech: POST /audio/speech. Hearing: POST /audio/transcriptions. Used by models with Speech to text or Text to speech ticked.',
  synthesize: 'Speech only: POST /synthesize with {text, voice_id, container, stream}; answers with raw audio.',
  'synthesize-stream': 'Speech only: POST /synthesize/stream with the same body and stream true; the streamed audio is gathered before it plays.',
  chat: 'One omni model for both: chat completions carry the recording in and the spoken audio out. Tick both Speech to text and Text to speech.',
};

export function ModelEditor({ live, canWrite, editing = false }: { live: LiveState; canWrite: boolean; editing?: boolean }) {
  const { id } = useParams();
  const navigate = useNavigate();
  const [catalog, setCatalog] = useState<RoutesListResult | null>(null);
  const [original, setOriginal] = useState<RouteView | undefined>();
  const [draft, setDraft] = useState<ModelDraft | null>(null);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [testing, setTesting] = useState(false);
  const [draftTesting, setDraftTesting] = useState(false);
  useEffect(() => {
    if (live.state !== 'current') return;
    let disposed = false;
    void rpc<RoutesListResult>('routes.list').then((result) => {
      if (disposed) return;
      setCatalog(result);
      const route = editing ? result.routes.find((r) => r.id === id) : undefined;
      setOriginal(route);
      if (!editing || route) setDraft((existing) => existing ?? modelDraft(route, result.providers[0]));
    }).catch((cause) => { if (!disposed) setError(describeError(cause)); });
    return () => { disposed = true; };
  }, [editing, id, live.state, live.state === 'current' ? live.generation : -1]);
  const update = <K extends keyof ModelDraft>(field: K, value: ModelDraft[K]) => setDraft((current) => current && ({ ...current, [field]: value }));
  const toggleCapability = (cap: string) => setDraft((current) => {
    if (!current) return current;
    let next = current.capabilities ?? [];
    next = next.includes(cap) ? next.filter((value) => value !== cap) : [...next, cap];
    if (cap === 'pdf' && next.includes('pdf') && !next.includes('file')) next = [...next, 'file'];
    if (cap === 'file' && !next.includes('file')) next = next.filter((value) => value !== 'pdf');
    const hasEffort = next.includes('thinking-effort');
    return { ...current, capabilities: next, reasoningEfforts: hasEffort ? current.reasoningEfforts ?? [] : [], reasoningEffort: hasEffort ? current.reasoningEffort : '' };
  });
  const toggleEffort = (effort: string) => setDraft((current) => {
    if (!current) return current;
    const selected = current.reasoningEfforts ?? [];
    const next = selected.includes(effort) ? selected.filter((value) => value !== effort) : [...selected, effort];
    return { ...current, reasoningEfforts: next, reasoningEffort: next.includes(current.reasoningEffort) ? current.reasoningEffort : '' };
  });
  const save = async () => {
    if (!draft || !canWrite || busy) return;
    setError('');
    try {
      const params = modelParams(draft);
      if (!editing && catalog?.routes.some((route) => route.id === params.id)) throw new Error('That route ID already exists. Choose another ID to add a model.');
      setBusy(true);
      const result = await rpc<{ route: RouteView }>('routes.set', params);
      if (result.route.displayName !== params.displayName || (result.route.displayDescription ?? '') !== params.displayDescription || (result.route.icon ?? '') !== params.icon ||
          (draft.capabilities !== null && JSON.stringify([...(result.route.capabilities ?? [])].sort()) !== JSON.stringify([...draft.capabilities].sort())) ||
          (draft.reasoningEfforts !== null && JSON.stringify([...(result.route.reasoningEfforts ?? [])].sort()) !== JSON.stringify([...draft.reasoningEfforts].sort()))) {
        throw new Error('The daemon did not confirm the model metadata. Rebuild and restart the daemon before saving this configuration.');
      }
      navigate('/models');
    } catch (cause) { setError(describeError(cause)); }
    finally { setBusy(false); }
  };
  return <section className="mx-auto max-w-5xl space-y-4" data-testid="model-editor">
    <ContentActions
      className="shrink-0" gap="md" align="end"
      actions={<>{draft && <Button size="sm" variant="ghost" onClick={() => setDraftTesting(true)} data-testid="editor-test-draft">Test these settings</Button>}{editing && original && <Button size="sm" variant="ghost" onClick={() => setTesting(true)} data-testid="editor-test">Test this model</Button>}</>}
    >
      <div>
        <Link to="/models" className="aeon-accent-text text-xs underline">← All models</Link>
        <div className="mt-2"><PageTitle title={editing ? 'Edit model' : 'Add a model'} purpose="A connection, a clear identity, and the capabilities you can rely on. Save it, then use Test to check it works." /></div>
      </div>
    </ContentActions>
    {!canWrite && <Alert tone="amber" title="Model changes unavailable" description="Connect to a compatible daemon before saving changes." />}
    {error && <Alert tone="red" title="Could not save model" description={error} />}
    {catalog && !catalog.persisted && <Alert tone="amber" title="Changes are not persisted" description="This daemon has no attached store. Model configuration will be lost on restart." />}
    {editing && catalog && !original && <Alert tone="amber" title="Model not found" description="This route may have been removed. Return to All models to choose another." />}
    {!draft && !error && live.state === 'current' && !(editing && catalog && !original) && <Spinner />}
    {draft && <form onSubmit={(event) => { event.preventDefault(); void save(); }}>
      <fieldset disabled={!canWrite || busy} className="space-y-5 p-4">
        <Group title="Identity" description="Give the model a recognizable name. The slug is sent to your provider; the route ID identifies this configuration.">
          <div className="flex flex-wrap items-center gap-5"><ModelIcon icon={draft.icon} large />
            <div className="space-y-2"><Field label="Model icon" hint="PNG, JPEG or WebP · up to 256 KiB"><input type="file" accept="image/png,image/jpeg,image/webp" className="block max-w-full text-xs" onChange={(event) => {
              const file = event.target.files?.[0]; event.target.value = '';
              if (file) { setBusy(true); void readModelIcon(file).then((icon) => update('icon', icon)).catch((cause) => setError(describeError(cause))).finally(() => setBusy(false)); }
            }} /></Field>{draft.icon && <Button type="button" size="sm" variant="ghost" onClick={() => update('icon', '')}>Remove icon</Button>}</div></div>
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="Display name"><Input required maxLength={200} value={draft.displayName} onChange={(e) => update('displayName', e.target.value)} placeholder="Your everyday reasoning model" /></Field>
            <Field label="Slug" hint="The exact model identifier accepted by your provider."><Input required value={draft.model} onChange={(e) => { const model = e.target.value; setDraft((d) => d && ({ ...d, model, id: !editing && (!d.id || d.id === routeIdForSlug(d.model)) ? routeIdForSlug(model) : d.id })); }} placeholder="provider-model-id" /></Field>
          </div>
          <Field label="Display description"><Textarea rows={3} maxLength={4000} value={draft.displayDescription} onChange={(e) => update('displayDescription', e.target.value)} placeholder="What makes this model useful for your work?" /></Field>
          <Field label="Route ID" hint={editing ? 'Fixed to preserve references from loops and steps.' : 'Unique letters, digits, hyphens, underscores, single dots, or a slash such as hexgrad/Kokoro-82M. This is separate from the provider slug.'}><Input required readOnly={editing} value={draft.id} onChange={(e) => update('id', e.target.value)} /></Field>
        </Group>
        <Group title="Connection" description="Choose a provider adapter and its endpoint. Keys are sent once and never read back into this form.">
          <div className="grid gap-4 sm:grid-cols-2"><Field label="Provider"><Select value={draft.provider} onChange={(e) => update('provider', e.target.value)}>{catalog?.providers.map((provider) => <option key={provider} value={provider}>{provider}</option>)}</Select></Field>
            {draft.provider === 'openai' && <Field label="OpenAI API"><Select value={draft.openaiEndpoint} onChange={(e) => update('openaiEndpoint', e.target.value)}><option value="">Default (Chat completions)</option>{OPENAI_ENDPOINTS.map((value) => <option key={value} value={value}>{value}</option>)}</Select></Field>}</div>
          {draft.provider === 'openai' && <Field label="Speech API" hint={SPEECH_API_HINT[draft.audioApi || 'openai']}><Select value={draft.audioApi} onChange={(e) => update('audioApi', e.target.value)} data-testid="speech-api"><option value="">Default (OpenAI audio: /audio/speech, /audio/transcriptions)</option>{AUDIO_APIS.filter((v) => v !== 'openai').map((value) => <option key={value} value={value}>{SPEECH_API_LABEL[value]}</option>)}</Select></Field>}
          <Field label="Base URL" hint="Optional override, including any API path required by the provider."><Input value={draft.baseUrl} onChange={(e) => update('baseUrl', e.target.value)} placeholder="Provider default" /></Field>
          <Field label="API key" hint={original?.hasKey ? 'A key is saved. Leave blank to keep it.' : 'Optional for providers that do not require a key.'}><PasswordInput autoComplete="new-password" value={draft.apiKey} disabled={draft.clearKey} onChange={(e) => update('apiKey', e.target.value)} /></Field>
          {original?.hasKey && <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={draft.clearKey} onChange={(e) => { update('clearKey', e.target.checked); update('apiKey', ''); }} />Remove saved API key</label>}
        </Group>
        <Group title="Capabilities" description="Declare what this model supports. Image means image input, and PDF also requires file support.">
          {draft.capabilities === null && <p className="text-xs opacity-65">Capabilities are unspecified for this existing model. Selecting a capability creates an explicit declaration.</p>}
          <div className="grid gap-2 sm:grid-cols-2">{CAPABILITIES.map((cap) => <label key={cap.id} className={`flex cursor-pointer items-start gap-3 rounded-xl border p-3 ${draft.capabilities?.includes(cap.id) ? 'aeon-accent-outline aeon-accent-wash' : 'border-current/10'}`}>
            <input type="checkbox" aria-label={cap.label} className="mt-1" checked={draft.capabilities?.includes(cap.id) ?? false} onChange={() => toggleCapability(cap.id)} />
            <span><span className="block text-sm font-medium">{cap.label}</span><span className="mt-1 block text-xs opacity-60">{cap.description}</span></span>
          </label>)}</div>
        </Group>
        <Group title="Reasoning" description="Choose only the effort levels this model accepts. Thinking output and adjustable effort are separate capabilities.">
          {draft.reasoningEfforts === null && <p className="text-xs opacity-65">Available efforts are unspecified. Enable Thinking effort above to declare a supported subset.</p>}
          <div role="group" aria-label="Available reasoning efforts" className="grid gap-2 sm:grid-cols-2">{REASONING_EFFORTS.map((effort) => <label key={effort} className="flex items-start gap-2 rounded-lg border border-current/10 p-3 text-sm">
            <input type="checkbox" aria-label={`${effort} — ${EFFORT_LABELS[effort]}`} disabled={!draft.capabilities?.includes('thinking-effort')} checked={draft.reasoningEfforts?.includes(effort) ?? false} onChange={() => toggleEffort(effort)} />
            <span><span className="block font-mono text-xs">{effort}</span><span className="mt-1 block text-xs opacity-60">{EFFORT_LABELS[effort]}</span></span>
          </label>)}</div>
          <Field label="Default reasoning effort"><Select value={draft.reasoningEffort} onChange={(e) => update('reasoningEffort', e.target.value)}><option value="">Provider default</option>{availableEfforts({ reasoningEfforts: draft.reasoningEfforts }).map((effort) => <option key={effort} value={effort}>{effort}</option>)}</Select></Field>
        </Group>
        <Group title="Limits & pricing" description="Token budgets and prices per million tokens. A new route must either carry a price or be declared unmetered below — a silently blank price is a cost nobody ever re-checks.">
          <div className="grid gap-4 sm:grid-cols-2"><Field label="Max output tokens"><Input type="number" min={1} step={1} required value={draft.maxOutputTokens} onChange={(e) => update('maxOutputTokens', e.target.value)} /></Field><Field label="Context window"><Input type="number" min={1} step={1} value={draft.contextWindow} onChange={(e) => update('contextWindow', e.target.value)} placeholder="Unknown" /></Field>
          {([['inputPrice', 'Input / million tokens'], ['outputPrice', 'Output / million tokens'], ['cacheReadPrice', 'Cache read / million tokens'], ['cacheWritePrice', 'Cache write / million tokens']] as const).map(([key, label]) => <Field key={key} label={label}><Input type="number" min={0} step="any" value={draft[key]} onChange={(e) => update(key, e.target.value)} placeholder="USD · unknown" /></Field>)}</div>
          {/* O8's checkbox: the difference between "there is no price" and
              "nobody said". Checked means decided, and the listing says
              "unmetered" rather than leaving a blank to be read as free. */}
          <label className="flex items-center gap-2 text-sm"><input type="checkbox" data-testid="field-no-price" checked={draft.noPrice} onChange={(e) => update('noPrice', e.target.checked)} />No price — declared unmetered on purpose</label>
        </Group>
        <div className="flex justify-end gap-3 pb-6"><Button type="button" variant="ghost" onClick={() => navigate('/models')}>Cancel</Button><Button type="submit" disabled={!canWrite || busy} loading={busy}>{editing ? 'Save changes' : 'Add model'}</Button></div>
      </fieldset>
    </form>}
    {draftTesting && draft && <OverlaySidePanel title="Test these settings" titleClassName="text-xl font-semibold" subtitle="Uses the form as it is now; nothing is saved" variant="simple" width={560} onClose={() => setDraftTesting(false)}>
      <ModelVoiceTests target={{ draft: voiceDraft(draft, editing) }} capabilities={draft.capabilities ?? []} canRun={live.state === 'current'} />
    </OverlaySidePanel>}
    {testing && original && <ModelTestPanel route={original} canRun={live.state === 'current'} onClose={() => setTesting(false)} />}
  </section>;
}
