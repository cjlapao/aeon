import { useCallback, useEffect, useMemo, useState } from 'react';
import { createPortal } from 'react-dom';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { Button, EmptyState, Panel, SidePanel } from '../../components/kit';
import { rpc, rpcTyped, toRpcError } from '../../lib/bridge';
import { useAppSettings } from '../../lib/appSettings';
import type { LiveController, LiveState } from '../../lib/live';
import type { RpcMethods } from '../../proto/contract';
import { ComponentFileBrowser } from './ComponentFileBrowser';
import { ComponentIcon } from './ComponentIcon';
import { ComponentsDashboard } from './ComponentsDashboard';
import { DeletedLoops, LoopCard, useDeleteLoop, type ComponentLoop, type Tombstone } from './LoopCard';

type Loop = RpcMethods['loop.list']['result']['loops'][number];
// The component list view carries `parentId`; `component.create` / `component.update`
// results do not, so the cache widens it to optional (this screen groups by `root`
// and never reads `parentId`).
type Component = Omit<RpcMethods['component.list']['result']['components'][number], 'parentId'> & { parentId?: string };
type Draft = { name: string; slug: string; root: string; remote: string; owner: string; tags: string; remoteKey: string; repoKey: string; icon: string };
const emptyDraft: Draft = { name: '', slug: '', root: '', remote: '', owner: '', tags: '', remoteKey: '', repoKey: '', icon: '' };
const slugOf = (value: string) =>
  value
    .toLowerCase()
    .trim()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-|-$/g, '');
const draftOf = (component: Component): Draft => ({
  name: component.name,
  slug: component.slug,
  root: component.root,
  remote: component.remote ?? '',
  owner: component.owner ?? '',
  tags: (component.tags ?? []).join(', '),
  remoteKey: component.remoteKey ?? '',
  repoKey: component.repoKey ?? '',
  icon: component.icon ?? '',
});

function Detail({ label, value }: { label: string; value?: string }) {
  return (
    <div>
      <dt className="text-xs uppercase tracking-wide text-slate-500 dark:text-slate-400">{label}</dt>
      <dd className="mt-1 break-all text-sm">{value || 'Not set'}</dd>
    </div>
  );
}
const fieldClass = 'mt-1 w-full rounded-lg border border-slate-300 bg-white p-2 text-slate-900 dark:border-white/20 dark:bg-slate-900 dark:text-white';
const fieldColumns = { gridTemplateColumns: 'repeat(auto-fit, minmax(min(100%, 190px), 1fr))' };
function Field({
  label,
  value,
  onChange,
  required,
  placeholder,
  pattern,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  required?: boolean;
  placeholder?: string;
  pattern?: string;
}) {
  return (
    <label className="block text-sm">
      {label} {!required && <span className="text-slate-500">optional</span>}
      <input className={fieldClass} value={value} onChange={(event) => onChange(event.target.value)} required={required} placeholder={placeholder} pattern={pattern} />
    </label>
  );
}

export function ComponentsScreen({ controller, live, canWrite = live.state === 'current', componentId = '' }: { controller: LiveController; live: LiveState; canWrite?: boolean; componentId?: string }) {
  const navigate = useNavigate();
  const [params] = useSearchParams();
  // S38 T4 — /components/:id is an ADDRESS, not a filter: the id resolves to
  // its row's root once the rows land, and the row is then selected.
  const [deepLinkRoot, setDeepLinkRoot] = useState('');
  const root = params.get('root') || deepLinkRoot;
  const [loops, setLoops] = useState<Loop[]>([]);
  const [stored, setStored] = useState<Component[]>([]);
  // Archived loops stay out of the list unless asked for; deleted ones leave a record, read per component.
  const [showArchived, setShowArchived] = useState(false);
  const del = useDeleteLoop(() => void read());
  const [tombstones, setTombstones] = useState<Tombstone[]>([]);
  const [error, setError] = useState('');
  const [componentError, setComponentError] = useState('');
  const [formMode, setFormMode] = useState<'create' | 'edit' | null>(null);
  const [formOpen, setFormOpen] = useState(false);
  const [viewportWidth, setViewportWidth] = useState(() => window.innerWidth);
  const { controlTone, surfaceTone } = useAppSettings();
  const [editingID, setEditingID] = useState('');
  const [draft, setDraft] = useState<Draft>(emptyDraft);
  const [slugEdited, setSlugEdited] = useState(false);
  const [busy, setBusy] = useState(false);
  const [iconLoading, setIconLoading] = useState(false);
  const [formError, setFormError] = useState('');
  useEffect(() => {
    const onResize = () => setViewportWidth(window.innerWidth);
    window.addEventListener('resize', onResize);
    return () => window.removeEventListener('resize', onResize);
  }, []);
  const read = useCallback(async () => {
    const [componentsResult, loopsResult] = await Promise.allSettled([rpcTyped('component.list', {}), rpcTyped('loop.list', (showArchived ? { all: true, archived: 'include' } : { all: true }) as never)]);
    if (componentsResult.status === 'fulfilled') {
      setStored(componentsResult.value.components);
      setComponentError('');
    } else setComponentError(`Component records are unavailable: ${toRpcError(componentsResult.reason).message}. Rebuild and restart the daemon to use component.create, component.update, and component.list.`);
    if (loopsResult.status === 'fulfilled') {
      setLoops(loopsResult.value.loops);
      setError('');
    } else setError(toRpcError(loopsResult.reason).message);
  }, [showArchived]);
  useEffect(() => {
    if (!componentId || !stored.length) return;
    const target = stored.find((component) => component.id === componentId);
    if (target) setDeepLinkRoot(target.root);
  }, [componentId, stored]);
  useEffect(() => controller.registerReader(read), [controller, read]);
  useEffect(() => {
    if (live.state === 'current') void read();
  }, [live.state, live.state === 'current' ? live.generation : -1, read]);

  const components = useMemo(() => {
    const grouped = new Map<string, { path: string; saved?: Component; items: Loop[] }>();
    for (const component of stored) grouped.set(component.root, { path: component.root, saved: component, items: [] });
    for (const loop of loops) {
      const entry = grouped.get(loop.componentRoot) || { path: loop.componentRoot, items: [] };
      entry.items.push(loop);
      grouped.set(loop.componentRoot, entry);
    }
    return [...grouped.values()]
      .map((component) => ({ ...component, items: component.items.sort((a, b) => Date.parse(b.updatedAt) - Date.parse(a.updatedAt)) }))
      .sort((a, b) => Date.parse(b.saved?.updatedAt || b.items[0]?.updatedAt || '') - Date.parse(a.saved?.updatedAt || a.items[0]?.updatedAt || ''));
  }, [loops, stored]);
  const selected = components.find((component) => component.path === root);
  useEffect(() => {
    if (!selected) { setTombstones([]); return; }
    let on = true;
    void rpc<{ tombstones?: Tombstone[] }>('loop.tombstones', { componentRoot: selected.path })
      .then((r) => { if (on) setTombstones(Array.isArray(r?.tombstones) ? r.tombstones : []); }, () => { if (on) setTombstones([]); });
    return () => { on = false; };
  }, [selected?.path, loops]);

  function openCreate(path = '') {
    setDraft({ ...emptyDraft, root: path });
    setEditingID('');
    setSlugEdited(false);
    setFormError('');
    setFormMode('create');
    setFormOpen(true);
  }
  function openEdit(component: Component) {
    setDraft(draftOf(component));
    setEditingID(component.id);
    setSlugEdited(true);
    setFormError('');
    setFormMode('edit');
    setFormOpen(true);
  }
  function closeForm() {
    if (!busy) setFormOpen(false);
  }

  async function save() {
    if (!canWrite || busy || iconLoading || componentError || !draft.name.trim() || !draft.slug.trim() || !draft.root.trim()) return;
    setBusy(true);
    setFormError('');
    try {
      const fields = {
        name: draft.name.trim(),
        slug: draft.slug.trim(),
        remote: draft.remote.trim(),
        owner: draft.owner.trim(),
        remoteKey: draft.remoteKey.trim(),
        repoKey: draft.repoKey.trim(),
        icon: draft.icon.trim(),
        tags: [
          ...new Set(
            draft.tags
              .split(',')
              .map((tag) => tag.trim())
              .filter(Boolean),
          ),
        ],
      };
      if (formMode === 'edit') {
        const updated = await rpcTyped('component.update', { id: editingID, ...fields });
        setStored((current) => current.map((component) => (component.id === updated.id ? updated : component)));
      } else {
        const created = await rpcTyped('component.create', { root: draft.root.trim(), ...fields });
        setStored((current) => [created, ...current]);
        navigate(`/components?root=${encodeURIComponent(created.root)}`);
      }
      setFormOpen(false);
    } catch (cause) {
      setFormError(toRpcError(cause).message);
    } finally {
      setBusy(false);
    }
  }

  async function chooseIcon(file: File | undefined) {
    if (!file) return;
    if (!['image/png', 'image/jpeg', 'image/webp', 'image/gif'].includes(file.type) || file.size > 256 * 1024) {
      setFormError('Choose a PNG, JPEG, WebP, or GIF image no larger than 256 KiB.');
      return;
    }
    setIconLoading(true);
    try {
      const icon = await new Promise<string>((resolve, reject) => {
        const reader = new FileReader();
        reader.onload = () => resolve(String(reader.result));
        reader.onerror = () => reject(reader.error);
        reader.readAsDataURL(file);
      });
      setDraft((current) => ({ ...current, icon }));
      setFormError('');
    } catch {
      setFormError('Could not read the selected image.');
    } finally {
      setIconLoading(false);
    }
  }

  return (
    <section className="space-y-5" data-testid="product-components">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <p className="text-xs uppercase tracking-[.2em] aeon-accent-text">Workspace</p>
          <h1 className="mt-1 text-3xl font-semibold">Components</h1>
          <p className="mt-2 text-sm text-slate-600 dark:text-slate-400">Save a workspace first, then run one or more loops in it.</p>
        </div>
        <Button onClick={() => (formOpen && formMode === 'create' ? closeForm() : openCreate())} disabled={!canWrite || !!componentError}>
          {formOpen && formMode === 'create' ? 'Close form' : 'New component'}
        </Button>
      </div>
      {error && (
        <Panel tone="red">
          <p role="alert">{error}</p>
        </Panel>
      )}
      {componentError && (
        <Panel tone="amber">
          <p role="alert">{componentError}</p>
        </Panel>
      )}
      {createPortal(
        <div className="fixed inset-0 z-50" style={{ pointerEvents: formOpen ? 'auto' : 'none' }}>
          <SidePanel
            isOpen={formOpen}
            onClose={closeForm}
            onKeyDown={(event) => {
              if (event.key === 'Escape') closeForm();
            }}
            title="Component details"
            subtitle={formMode === 'edit' ? 'Edit component' : 'New component'}
            side="right"
            variant="floating-glass"
            tone={controlTone}
            surfaceTone={surfaceTone}
            overlay
            resizable
            width={Math.min(520, viewportWidth - 16)}
            minWidth={Math.min(320, viewportWidth - 16)}
            maxWidth={Math.min(900, viewportWidth - 16)}
            footer={
              <div className="space-y-2 border-t border-slate-300/30 pt-3 dark:border-white/10">
                {formError && <p role="alert" className="text-sm text-red-700 dark:text-red-300">{formError}</p>}
                <div className="flex flex-wrap gap-2">
                  <Button type="submit" form="component-form" disabled={!canWrite || busy || iconLoading || !draft.name.trim() || !draft.slug.trim() || !draft.root.trim()}>
                    {busy ? 'Saving…' : iconLoading ? 'Loading image…' : formMode === 'edit' ? 'Save changes' : 'Create component'}
                  </Button>
                  <Button type="button" onClick={closeForm} disabled={busy}>
                    Cancel
                  </Button>
                </div>
              </div>
            }
          >
            <form
              id="component-form"
              className="space-y-4 p-4"
              onSubmit={(event) => {
                event.preventDefault();
                void save();
              }}
            >
              <p className="text-sm text-slate-600 dark:text-slate-400">
                {formMode === 'edit'
                  ? 'The workspace root stays fixed so existing loop history keeps its identity.'
                  : 'The workspace must already exist and be visible to the daemon. Creating a component does not start a loop.'}
              </p>
              <div className="aeon-accent-outline aeon-accent-wash flex flex-wrap items-center gap-4 rounded-xl border p-4">
                <ComponentIcon icon={draft.icon} name={draft.name} slug={draft.slug} size="lg" />
                <div className="min-w-0 flex-1 space-y-2">
                  <label className="block text-sm">
                    Icon emoji <span className="text-slate-500">optional</span>
                    <input
                      className={fieldClass}
                      value={draft.icon.startsWith('data:') ? '' : draft.icon}
                      placeholder="🧪"
                      maxLength={32}
                      onChange={(event) => setDraft((current) => ({ ...current, icon: event.target.value }))}
                    />
                  </label>
                  <p className="text-xs text-slate-600 dark:text-slate-400">Use an emoji, upload a small image, or leave it empty for initials.</p>
                </div>
                <div className="flex flex-col gap-2">
                  <label className="aeon-accent-outline cursor-pointer rounded-lg border px-3 py-2 text-sm">
                    Upload image
                    <input
                      className="sr-only"
                      type="file"
                      accept="image/png,image/jpeg,image/webp,image/gif"
                      onChange={(event) => {
                        void chooseIcon(event.target.files?.[0]);
                      }}
                    />
                  </label>
                  {draft.icon && (
                    <Button type="button" onClick={() => setDraft((current) => ({ ...current, icon: '' }))}>
                      Use initials
                    </Button>
                  )}
                </div>
              </div>
              <div className="grid gap-4" style={fieldColumns}>
                <Field label="Name" required value={draft.name} onChange={(name) => setDraft((current) => ({ ...current, name, slug: slugEdited ? current.slug : slugOf(name) }))} />
                <Field
                  label="Slug"
                  required
                  pattern="[a-z0-9]+(-[a-z0-9]+)*"
                  value={draft.slug}
                  onChange={(slug) => {
                    setSlugEdited(true);
                    setDraft((current) => ({ ...current, slug }));
                  }}
                />
              </div>
              {formMode === 'edit' ? (
                <div>
                  <p className="text-sm">Workspace root</p>
                  <p className="mt-1 break-all rounded-lg border border-slate-300 p-2 text-sm dark:border-white/20">{draft.root}</p>
                </div>
              ) : (
                <Field label="Workspace root" required placeholder="/home/me/component" value={draft.root} onChange={(root) => setDraft((current) => ({ ...current, root }))} />
              )}
              <div className="grid gap-4" style={fieldColumns}>
                <Field label="Remote URL" value={draft.remote} onChange={(remote) => setDraft((current) => ({ ...current, remote }))} />
                <Field label="Owner" value={draft.owner} onChange={(owner) => setDraft((current) => ({ ...current, owner }))} />
              </div>
              <Field label="Tags (comma separated)" placeholder="go, qa" value={draft.tags} onChange={(tags) => setDraft((current) => ({ ...current, tags }))} />
              <details className="aeon-accent-outline rounded-xl border p-3">
                <summary className="cursor-pointer text-sm font-medium">Advanced identity fields</summary>
                <p className="mt-2 text-xs text-slate-600 dark:text-slate-400">Aeon stores these keys but does not infer them from the remote URL yet.</p>
                <div className="mt-3 grid gap-4" style={fieldColumns}>
                  <Field label="Remote key" value={draft.remoteKey} onChange={(remoteKey) => setDraft((current) => ({ ...current, remoteKey }))} />
                  <Field label="Repository key" value={draft.repoKey} onChange={(repoKey) => setDraft((current) => ({ ...current, repoKey }))} />
                </div>
              </details>
            </form>
          </SidePanel>
        </div>,
        document.body,
      )}
      {root && (
        <Link to="/components" className="text-sm aeon-accent-text underline">
          ← All components
        </Link>
      )}
      {root && !selected && <Panel tone="red">No component or recorded loops use this workspace.</Panel>}
      {selected ? (
        <>
          <Panel tone={surfaceTone} variant="liquid-glass">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <div className="flex min-w-0 items-center gap-4">
                <ComponentIcon
                  size="lg"
                  icon={selected.saved?.icon || ''}
                  name={selected.saved?.name || ''}
                  slug={selected.saved?.slug || selected.path.split('/').filter(Boolean).at(-1) || selected.path}
                />
                <div>
                  <p className="text-xs uppercase tracking-wide text-slate-500 dark:text-slate-400">{selected.saved ? 'Saved component' : 'Workspace from recorded loops'}</p>
                  <h2 className="mt-1 text-xl font-semibold">{selected.saved?.name || selected.path.split('/').filter(Boolean).at(-1) || selected.path}</h2>
                  <p className="mt-1 break-all text-sm">{selected.path}</p>
                </div>
              </div>
              <div className="flex flex-wrap gap-2">
                {selected.saved ? (
                  <Button onClick={() => openEdit(selected.saved!)} disabled={!canWrite || !!componentError}>
                    Edit component
                  </Button>
                ) : (
                  <Button onClick={() => openCreate(selected.path)} disabled={!canWrite || !!componentError}>
                    Save as component
                  </Button>
                )}
                <Link to={`/?componentRoot=${encodeURIComponent(selected.path)}`} className="aeon-accent-button rounded-lg px-4 py-2 text-sm">
                  Start a loop here
                </Link>
              </div>
            </div>
            {selected.saved ? (
              <dl className="mt-5 grid gap-4 border-t border-slate-300/40 pt-4 sm:grid-cols-2 xl:grid-cols-4 dark:border-white/10">
                <Detail label="Component ID" value={selected.saved.id} />
                <Detail label="Slug" value={selected.saved.slug} />
                <Detail label="Owner" value={selected.saved.owner} />
                <Detail label="Remote URL" value={selected.saved.remote} />
                <Detail label="Remote key" value={selected.saved.remoteKey} />
                <Detail label="Repository key" value={selected.saved.repoKey} />
                <Detail label="Tags" value={selected.saved.tags.join(', ')} />
                <Detail label="Created" value={new Date(selected.saved.createdAt).toLocaleString()} />
                <Detail label="Updated" value={new Date(selected.saved.updatedAt).toLocaleString()} />
              </dl>
            ) : (
              <p className="mt-4 text-sm text-slate-600 dark:text-slate-400">This workspace has loops but no saved component metadata yet. Create a named component with this root to record it.</p>
            )}
          </Panel>
          <ComponentFileBrowser key={selected.path} root={selected.path} />
          <div className="flex items-center gap-3">
            <h2 className="text-lg font-semibold">Loops in this component</h2>
            <label className="ml-auto flex items-center gap-2 text-sm text-slate-600 dark:text-slate-400">
              <input type="checkbox" checked={showArchived} onChange={(e) => setShowArchived(e.target.checked)} data-testid="show-archived" />
              Show archived
            </label>
          </div>
          {selected.items.length === 0 ? (
            <EmptyState title="No loops yet" subtitle="Start a loop in this component's workspace when you are ready." fullWidth />
          ) : (
            <div className="grid gap-3 md:grid-cols-2">
              {selected.items.map((loop) => (
                <LoopCard key={loop.id} loop={loop as ComponentLoop} canWrite={canWrite} onChanged={() => void read()} onDelete={del.open} />
              ))}
            </div>
          )}
          <DeletedLoops tombstones={tombstones} />
          {del.dialog}
        </>
      ) : (
        !root && <ComponentsDashboard components={components} loops={loops} live={live} canWrite={canWrite} onRefresh={read} />
      )}
    </section>
  );
}
