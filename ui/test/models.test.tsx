import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { ChatComposer } from '../src/components/agent-chat/ChatComposer';
import { ProductApp } from '../src/app/router';
import { installFakeDaemon, resetFakeDaemon } from './helpers';
import type { RouteView, SetRouteParams } from '../src/lib/protocol';
import { modelDraft, modelParams, readModelIcon, routeIdForSlug } from '../src/features/models/modelConfig';

const png = 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/nWQAAAAASUVORK5CYII=';
function catalog(initial: RouteView[] = [], protocolVersion = 1) {
  let routes = initial;
  return installFakeDaemon({ negotiated: { daemonVersion: 'test', protocolVersion, endpoint: '/tmp/aeon.sock' }, responses: {
    'routes.list': () => ({ routes, providers: ['anthropic', 'openai'], persisted: true }),
    'routes.set': (raw: unknown) => {
      const { apiKey, ...params } = raw as SetRouteParams;
      const old = routes.find((r) => r.id === params.id);
      const route: RouteView = { ...old, ...params, source: 'stored', hasKey: params.clearKey ? false : Boolean(apiKey || old?.hasKey), maxOutputTokens: params.maxOutputTokens ?? 4096, priced: false };
      routes = [...routes.filter((r) => r.id !== route.id), route];
      return { route };
    },
    'routes.delete': (raw: unknown) => { routes = routes.filter((r) => r.id !== (raw as { id: string }).id); return { deleted: true }; },
    'loop.list': { loops: [], all: true }, 'component.list': { projects: [] },
    'blueprint.list': { names: [] }, subscribe: { subscriptionId: 's1', from: 0 },
  } });
}
const existing: RouteView = { id: 'research', model: 'provider-slug', provider: 'openai', displayName: 'Research', displayDescription: 'Careful work', icon: png, capabilities: ['text', 'thinking-effort'], reasoningEfforts: ['low', 'max'], reasoningEffort: 'max', maxOutputTokens: 4096, hasKey: true, keyHint: '…abcd', source: 'stored', priced: false };
beforeEach(() => { vi.stubGlobal('ResizeObserver', class { observe() {} disconnect() {} }); window.location.hash = '#/work'; });
afterEach(() => { cleanup(); resetFakeDaemon(); window.location.hash = '#/'; vi.unstubAllGlobals(); });

function change(label: RegExp, value: string) { fireEvent.change(screen.getByRole('textbox', { name: label }), { target: { value } }); }

describe('model management', () => {
  it('opens Models from the user menu and creates a named model with its own effort subset', async () => {
    const daemon = catalog();
    render(<ProductApp />);
    await screen.findByTestId('work-screen');
    expect(screen.queryByRole('link', { name: 'Models' })).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'User menu' }));
    fireEvent.click(screen.getByRole('menuitem', { name: 'Models' }));
    expect(await screen.findByText('Build your model collection')).toBeTruthy();
    expect(screen.queryByRole('menu')).toBeNull();
    fireEvent.click(screen.getAllByRole('link', { name: 'Add model' })[0]!);
    await screen.findByRole('textbox', { name: 'Display name' });
    change(/^Display name/, 'Everyday reasoning');
    change(/^Slug/, 'provider-model');
    change(/^Display description/, 'A model for focused work.');
    for (const label of ['Text', 'Image', 'Tool calling', 'File', 'PDF', 'Thinking', 'Thinking effort']) {
      const box = screen.getByRole('checkbox', { name: label });
      if (!(box as HTMLInputElement).checked) fireEvent.click(box);
    }
    const efforts = screen.getByRole('group', { name: 'Available reasoning efforts' });
    expect(within(efforts).getAllByRole('checkbox')).toHaveLength(7);
    fireEvent.click(within(efforts).getByRole('checkbox', { name: /^low / }));
    fireEvent.click(within(efforts).getByRole('checkbox', { name: /^max / }));
    const defaultEffort = screen.getByRole('combobox', { name: 'Default reasoning effort' });
    expect(within(defaultEffort).getAllByRole('option').map((option) => option.textContent)).toEqual(['Provider default', 'low', 'max']);
    fireEvent.change(defaultEffort, { target: { value: 'max' } });
    fireEvent.change(screen.getByLabelText(/^Model icon/), { target: { files: [new File([Uint8Array.from(atob(png.split(',')[1]!), (c) => c.charCodeAt(0))], 'model.png', { type: 'image/png' })] } });
    await waitFor(() => expect(screen.getByRole('button', { name: 'Remove icon' })).toBeTruthy());
    fireEvent.click(screen.getByRole('button', { name: 'Add model' }));
    await screen.findByText('Everyday reasoning');
    expect(daemon.paramsOf('routes.set')).toMatchObject({ id: 'provider-model', model: 'provider-model', displayName: 'Everyday reasoning', displayDescription: 'A model for focused work.', icon: png, reasoningEfforts: ['low', 'max'], reasoningEffort: 'max' });
    expect((daemon.paramsOf('routes.set') as SetRouteParams).capabilities).toHaveLength(7);
    expect(daemon.countOf('connect')).toBe(1);
    fireEvent.click(screen.getByTestId('daemon-status-trigger'));
    expect(await screen.findByTestId('daemon-status-state')).toHaveProperty('textContent', 'Connected');
  }, 15000);

  it('edits display metadata without reading or replacing the saved key and confirms deletion', async () => {
    const daemon = catalog([existing]); window.location.hash = '#/models/research/edit';
    render(<ProductApp />);
    await screen.findByRole('textbox', { name: 'Display name' });
    expect((screen.getByRole('textbox', { name: /^Route ID/ }) as HTMLInputElement).readOnly).toBe(true);
    expect((screen.getByLabelText(/^API key/) as HTMLInputElement).value).toBe('');
    change(/^Display name/, 'Renamed research');
    fireEvent.click(screen.getByRole('button', { name: 'Save changes' }));
    await screen.findByText('Renamed research');
    expect((daemon.paramsOf('routes.set') as SetRouteParams).apiKey).toBeUndefined();
    fireEvent.click(screen.getByRole('button', { name: 'Delete Renamed research' }));
    expect(daemon.methods()).not.toContain('routes.delete');
    fireEvent.click(await screen.findByRole('button', { name: 'Remove model' }));
    await screen.findByText('Build your model collection');
    expect(daemon.paramsOf('routes.delete')).toEqual({ id: 'research' });
  }, 15000);

  it('preserves unspecified legacy declarations and does not replace a route on create', async () => {
    const legacy = { ...existing, displayName: undefined, capabilities: undefined, reasoningEfforts: undefined };
    const params = modelParams(modelDraft(legacy));
    expect(routeIdForSlug('qwen/Qwen3.5-27B')).toBe('qwen-Qwen3.5-27B');
    expect(params.capabilities).toBeUndefined(); expect(params.reasoningEfforts).toBeUndefined();
    const daemon = catalog([existing]); window.location.hash = '#/models/new';
    render(<ProductApp />); await screen.findByRole('textbox', { name: 'Display name' });
    change(/^Display name/, 'Collision'); change(/^Slug/, 'research');
    fireEvent.click(screen.getByRole('button', { name: 'Add model' }));
    await screen.findByText(/That route ID already exists/);
    expect(daemon.methods()).not.toContain('routes.set');
  });

  it('keeps a blank price a decision, not an erasure (S18 O8)', () => {
    const base = { ...modelDraft(undefined, 'openai'), id: 'free', model: 'llama3.3', displayName: 'Free llama' };
    // Blank prices go as ABSENT, never `{}` — an empty PricesView is still
    // non-nil, and the daemon would keep it in place of the saved prices.
    expect(modelParams(base).prices).toBeUndefined();
    expect(modelParams(base).noPrice).toBeUndefined();
    // The declaration is what makes "no price" a decision.
    const declared = modelParams({ ...base, noPrice: true });
    expect(declared.noPrice).toBe(true); expect(declared.prices).toBeUndefined();
    // Price and unmetered are two decisions in one breath — refused, never
    // precedence-resolved, like apiKey/clearKey before them.
    expect(() => modelParams({ ...base, noPrice: true, inputPrice: '3' })).toThrow('opposite things');
    const priced = modelParams({ ...base, inputPrice: '3' });
    expect(priced.prices).toEqual({ inputUsdPerMTok: 3 }); expect(priced.noPrice).toBeUndefined();
    // And the view's declaration reads back into the form.
    expect(modelDraft({ ...existing, noPrice: true }).noPrice).toBe(true);
    expect(modelDraft(existing).noPrice).toBe(false);
  });

  it('shows unavailable writes when the session is lost and on a newer protocol', async () => {
    const daemon = catalog([existing], 3); window.location.hash = '#/models';
    render(<ProductApp />); await screen.findByText('Research');
    expect(screen.queryByRole('link', { name: 'Edit Research' })).toBeNull();
    expect((screen.getByRole('button', { name: 'Delete Research' }) as HTMLButtonElement).disabled).toBe(true);
    act(() => daemon.close('offline'));
    await screen.findByText('Model changes unavailable');
    expect(daemon.methods()).not.toContain('routes.set');
  });

  it('shows a missing model and rejects oversized and non-image icons', async () => {
    catalog(); window.location.hash = '#/models/missing/edit'; render(<ProductApp />);
    await screen.findByText('Model not found');
    await expect(readModelIcon(new File(['<svg/>'], 'icon.svg', { type: 'image/svg+xml' }))).rejects.toThrow('PNG, JPEG or WebP');
    await expect(readModelIcon(new File([new Uint8Array(256 * 1024 + 1)], 'icon.png', { type: 'image/png' }))).rejects.toThrow('256 KiB');
  });
});

it('offers only the selected model\'s supported efforts in the agent composer', () => {
  const onSteer = vi.fn();
  render(<ChatComposer targets={[{ value: 'agent', label: 'Agent' }]} currentRoute="research" currentEffort="low"
    routes={[{ value: 'research', label: 'Research', reasoningEfforts: ['low', 'max'] }, { value: 'plain', label: 'Plain', reasoningEfforts: [] }]}
    efforts={[{ value: 'high', label: 'high' }]} onSteer={onSteer} />);
  const effort = screen.getByRole('combobox', { name: 'reasoning effort' });
  expect(within(effort).queryByRole('option', { name: 'high' })).toBeNull();
  fireEvent.change(effort, { target: { value: 'max' } });
  fireEvent.change(screen.getByRole('combobox', { name: 'provider' }), { target: { value: 'plain' } });
  expect(screen.queryByRole('combobox', { name: 'reasoning effort' })).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: 'Steer' }));
  expect(onSteer).toHaveBeenCalledWith({ target: 'agent', text: '', route: 'plain' });
});
