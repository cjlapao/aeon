// ProjectsScreen — S44: the screen shows ROOT components. A "project" is now a
// root component (parent_id NULL); the fold folded `project` + `component`
// into one self-referencing `component`. `component.list` carries the whole
// tree; the roots are the top level, and a root with children shows its
// children. The folding is `visibleLayers`, which changes what is drawn and
// never what is held.

import { useCallback, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { EmptyState, Panel } from '../../components/kit';
import { rpcTyped, toRpcError } from '../../lib/bridge';
import { useRpcQuery } from '../../lib/useRpcQuery';
import type { LiveController, LiveState } from '../../lib/live';
import type { RpcMethods } from '../../proto/contract';
import { ComponentIcon } from '../components/ComponentIcon';
import { visibleLayers } from './layers';

type Component = RpcMethods['component.list']['result']['components'][number];
type ShowResult = RpcMethods['component.show']['result'];

export function ProjectsScreen({ controller, live }: { controller: LiveController; live: LiveState }) {
  const { data, error: rawError } = useRpcQuery('component.list', {}, controller, live);
  const components: Component[] = data?.components ?? [];
  const error = rawError && `Component records are unavailable: ${rawError}. Rebuild and restart the daemon to use component.create, component.update, component.list, and component.show.`;
  const [selectedID, setSelectedID] = useState('');
  const [show, setShow] = useState<ShowResult | null>(null);
  const [showError, setShowError] = useState('');

  const select = useCallback(async (id: string) => {
    setSelectedID(id);
    setShow(null);
    setShowError('');
    try {
      setShow(await rpcTyped('component.show', { component_id: id }));
    } catch (cause) {
      setShowError(toRpcError(cause).message);
    }
  }, []);

  // A "project" is a root component: parentId empty means the row is a top-level
  // root. The screen lists those; a root's children are the rows filed under it.
  const roots = useMemo(() => components.filter((component) => !component.parentId), [components]);
  const layers = visibleLayers(roots);
  const selected = roots.find((component) => component.id === selectedID);
  const children = components.filter((component) => component.parentId === selectedID);

  return (
    <div className="space-y-6">
      <header className="flex items-end justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold">Projects</h1>
          <p className="mt-1 text-sm text-slate-600 dark:text-slate-400">A project is a root component (S44). The screen lists the roots; a root with children shows the components filed under it.</p>
        </div>
      </header>

      {error && <Panel tone="red" title="The component tier is not answering">{error}</Panel>}

      {!error && !layers.showProjects && (
        <div className="space-y-3">
          <EmptyState title={roots.length === 0 ? 'No roots yet' : 'Nothing has been organised yet'} subtitle={layers.reason} />
        </div>
      )}

      {layers.showProjects && (
        <div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_minmax(0,1.2fr)]">
          <Panel title="Roots" subtitle={`${roots.length} carried`}>
            <ul className="divide-y divide-slate-300/20 dark:divide-white/10">
              {roots.map((component) => (
                <li key={component.id}>
                  <button type="button" onClick={() => void select(component.id)} className={`flex w-full items-center gap-3 py-3 text-left ${component.id === selectedID ? 'aeon-accent-text' : ''}`}>
                    <ComponentIcon icon={component.icon || ''} name={component.name} slug={component.slug || component.id} />
                    <span className="min-w-0 flex-1">
                      <span className="block truncate font-medium">{component.name}</span>
                      <span className="block truncate text-xs text-slate-500">{component.shortDescription || component.root || component.id}</span>
                    </span>
                  </button>
                </li>
              ))}
            </ul>
          </Panel>

          <div className="space-y-4">
            {!selected && <Panel tone="neutral">Pick a root to see the components filed under it.</Panel>}
            {selected && showError && <Panel tone="red" title="component.show refused">{showError}</Panel>}
            {show && (
              <Panel title={show.name} subtitle={show.root || show.slug || undefined}>
                {children.length === 0 ? (
                  <p className="text-sm text-slate-500">No components under this root yet — components name themselves into it at creation (<Link className="aeon-accent-text underline" to="/components">Components</Link>).</p>
                ) : (
                  <ul className="divide-y divide-slate-300/20 dark:divide-white/10">
                    {children.map((member) => (
                      <li key={member.id}>
                        <Link to={`/components/${member.id}`} className="flex items-center gap-3 py-3">
                          <ComponentIcon icon={member.icon || ''} name={member.name} slug={member.slug || member.id} />
                          <span className="min-w-0 flex-1">
                            <span className="block truncate font-medium">{member.name} <span className="text-xs text-slate-500">· {member.slug}</span></span>
                            <span className="block truncate text-xs text-slate-500">{member.root}</span>
                          </span>
                        </Link>
                      </li>
                    ))}
                  </ul>
                )}
              </Panel>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
