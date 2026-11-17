import { useCallback, useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { Alert, Button, ContentActions, SearchBar, Select, Spinner } from '../../components/kit';
import { PageTitle } from '../../components/PageTitle';
import { OverlaySidePanel } from '../../components/OverlaySidePanel';
import { rpc, describeError } from '../../lib/bridge';
import type { LiveController, LiveState } from '../../lib/live';
import type { RoutesListResult, RouteView } from '../../lib/protocol';
import { modelName } from './modelConfig';
import { filterRoutes, providersOf } from './modelsModel';
import { ModelsTable } from './ModelsTable';
import { ModelTestPanel } from './ModelTestPanel';

// `/models` — the models the daemon can call, in the same frame as the other management pages: a header
// with the one verb (Add model), a search and a provider filter, and a table. Test opens a side panel
// that sends a real request or checks the declared capabilities; Edit is its own page; Delete asks first.
export function ModelsScreen({ live, controller, canWrite }: { live: LiveState; controller: LiveController; canWrite: boolean }) {
  const [catalog, setCatalog] = useState<RoutesListResult | null>(null);
  const [error, setError] = useState('');
  const [q, setQ] = useState('');
  const [provider, setProvider] = useState('');
  const [testing, setTesting] = useState<RouteView | null>(null);
  const [deleting, setDeleting] = useState<RouteView | null>(null);
  const [busy, setBusy] = useState(false);
  const read = useCallback(async () => {
    try { setCatalog(await rpc<RoutesListResult>('routes.list')); setError(''); }
    catch (cause) { setError(describeError(cause)); }
  }, []);
  useEffect(() => controller.registerReader(read), [controller, read]);
  useEffect(() => { if (live.state === 'current') void read(); }, [live.state, live.state === 'current' ? live.generation : -1, read]);
  const remove = async (route: RouteView) => {
    if (!canWrite || busy) return;
    setBusy(true);
    try { await rpc('routes.delete', { id: route.id }); setDeleting(null); await read(); }
    catch (cause) { setError(describeError(cause)); }
    finally { setBusy(false); }
  };
  const routes = catalog?.routes ?? [];
  const rows = useMemo(() => filterRoutes(routes, q, provider), [routes, q, provider]);
  const providers = useMemo(() => providersOf(routes), [routes]);

  return <section className="flex min-h-0 flex-1 flex-col gap-4" data-testid="product-models">
    <ContentActions
      className="shrink-0" gap="md" align="end"
      actions={canWrite ? <Link to="/models/new" className="aeon-accent-wash aeon-accent-outline rounded-lg border px-4 py-2 text-sm font-medium">Add model</Link> : undefined}
    >
      <PageTitle title="Models" purpose="The models Aeon can call: what each one is, what it can do, what it costs. Test one before you rely on it." help={{ title: 'What is a model here?', content: 'A model is one **route**: a provider connection, the exact model name the provider accepts, and what you have declared it can do (images, tools, thinking effort…) and what it costs per million tokens.\n\n**Test** sends one short request, or checks the declared capabilities. It only runs when you press the button and never edits the model.' }} />
    </ContentActions>
    {!canWrite && <Alert className="shrink-0" intent="warning" title="Model changes unavailable" description="Connect to a compatible daemon to add, edit or remove models." />}
    {error && <Alert className="shrink-0" intent="danger" title="Could not update models" description={error} />}
    {catalog && !catalog.persisted && <Alert className="shrink-0" intent="warning" title="Changes are not persisted" description="This daemon has no attached store. Model configuration will be lost on restart." />}
    {!catalog && !error && live.state === 'current' && <Spinner />}
    {catalog && (
      <>
        <div className="flex shrink-0 flex-wrap items-center gap-3" data-testid="models-action-bar">
          <label className="block w-full max-w-md"><span className="sr-only">Search models</span>
            <SearchBar placeholder="Search by name, model or description" debounceMs={200} initialValue={q} onSearch={setQ} onClear={() => setQ('')} /></label>
          {providers.length > 1 && <div className="w-56 shrink-0"><Select variant="ghost" size="md" aria-label="Provider" value={provider} onChange={(e) => setProvider(e.target.value)}>
            <option value="">Provider: all</option>{providers.map((p) => <option key={p} value={p}>{p}</option>)}</Select></div>}
          <span className="ml-auto text-xs opacity-60">{rows.length} of {routes.length}</span>
        </div>
        <div className="min-h-[16rem] min-w-0 flex-1">
          <ModelsTable rows={rows} canWrite={canWrite} filtered={routes.length > 0} onTest={setTesting} onDelete={setDeleting} />
        </div>
      </>
    )}
    {testing && <ModelTestPanel key={testing.id} route={testing} canRun={live.state === 'current'} onClose={() => setTesting(null)} />}
    {deleting && (
      <OverlaySidePanel title={`Remove ${modelName(deleting)}`} titleClassName="text-xl font-semibold" variant="simple" width={480} onClose={() => !busy && setDeleting(null)}>
        <div className="rounded-lg border border-red-500/30 p-3" role="group" aria-label={`Confirm deletion of ${modelName(deleting)}`}>
          <p className="text-sm">Remove {modelName(deleting)} and its saved credential? Loops referencing this route will need another model.</p>
          <div className="mt-3 flex gap-2"><Button size="sm" color="red" disabled={!canWrite || busy} onClick={() => void remove(deleting)}>Remove model</Button><Button size="sm" variant="ghost" disabled={busy} onClick={() => setDeleting(null)}>Cancel</Button></div>
        </div>
      </OverlaySidePanel>
    )}
  </section>;
}
