import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { DaemonStatusButton } from '../src/app/DaemonStatusButton';
import type { LiveState } from '../src/lib/live';

beforeEach(() => {
  vi.stubGlobal('ResizeObserver', class { observe() {} disconnect() {} });
  vi.stubGlobal('matchMedia', () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

function open(live: LiveState, workspaceName?: string) {
  const view = render(<DaemonStatusButton live={live} workspaceName={workspaceName} />);
  fireEvent.click(screen.getByTestId('daemon-status-trigger'));
  return view;
}

describe('the daemon status button — one rail control, on every page (S41)', () => {
  it.each([
    { state: 'connecting', endpoint: '/tmp/aeon.sock' } as LiveState,
    { state: 'disconnected', endpoint: '/tmp/aeon.sock', reason: 'not running' } as LiveState,
  ])('shows unknown negotiation details while $state', async (live) => {
    open(live);
    const panel = await screen.findByTestId('daemon-status-panel');
    expect(screen.getByTestId('daemon-status-state').textContent).toBe(live.state === 'connecting' ? 'Connecting' : 'Disconnected');
    expect(panel.querySelector('[data-testid="daemon-status-version"]')?.textContent).toBe('Unknown');
    expect(panel.querySelector('[data-testid="daemon-status-protocol"]')?.textContent).toBe('Unknown');
  });

  it('replaces confirmed values with visibly retained values on connection loss, then clears them when disconnected', async () => {
    const { rerender } = open({ state: 'current', endpoint: '/tmp/aeon.sock', daemonVersion: '9.8.7', protocolVersion: 2, generation: 1 });
    expect(screen.getByTestId('daemon-status-state').textContent).toBe('Connected');
    expect(screen.getByTestId('daemon-status-version').textContent).toBe('9.8.7');
    expect(screen.getByTestId('daemon-status-protocol').textContent).toBe('2');

    rerender(<DaemonStatusButton live={{ state: 'stale', endpoint: '/tmp/aeon.sock', daemonVersion: '9.8.7', protocolVersion: 2, since: 0, cause: 'socket lost', retry: 0 }} />);
    expect(screen.getByTestId('daemon-status-state').textContent).toBe('Reconnecting');
    expect(screen.getByTestId('daemon-status-version').textContent).toBe('9.8.7 (last confirmed)');
    expect(screen.getByTestId('daemon-status-protocol').textContent).toBe('2 (last confirmed)');

    rerender(<DaemonStatusButton live={{ state: 'disconnected', endpoint: '/tmp/aeon.sock', reason: 'retries exhausted' }} />);
    expect(screen.getByTestId('daemon-status-version').textContent).toBe('Unknown');
    expect(screen.getByTestId('daemon-status-protocol').textContent).toBe('Unknown');
  });

  it('does not invent retained values when the first connection has never succeeded', async () => {
    open({ state: 'stale', endpoint: '/tmp/aeon.sock', since: 0, cause: 'socket lost', retry: 0 });
    expect(screen.getByTestId('daemon-status-state').textContent).toBe('Reconnecting');
    expect(screen.getByTestId('daemon-status-version').textContent).toBe('Unknown');
    expect(screen.getByTestId('daemon-status-protocol').textContent).toBe('Unknown');
    expect(screen.queryByText('(last confirmed)', { exact: false })).toBeNull();
  });

  it('shows the workspace name and falls back to "Local daemon"', async () => {
    const { rerender } = open({ state: 'current', endpoint: 'e', daemonVersion: '1', protocolVersion: 2, generation: 1 });
    expect(screen.getByTestId('daemon-status-workspace').textContent).toBe('Local daemon');
    rerender(<DaemonStatusButton live={{ state: 'current', endpoint: 'e', daemonVersion: '1', protocolVersion: 2, generation: 1 }} workspaceName="Dev box" />);
    expect(screen.getByTestId('daemon-status-workspace').textContent).toBe('Dev box');
  });

  it('flags a protocol newer than this app understands, with an explanation naming both versions', async () => {
    open({ state: 'current', endpoint: 'e', daemonVersion: '9.9', protocolVersion: 99, generation: 1 });
    expect(screen.getByTestId('daemon-status-protocol').textContent).toBe('99');
    const mismatch = screen.getByTestId('daemon-status-mismatch');
    expect(mismatch.textContent).toContain('negotiated 99');
  });

  it('does not flag a protocol this app understands', async () => {
    open({ state: 'current', endpoint: 'e', daemonVersion: '1', protocolVersion: 1, generation: 1 });
    expect(screen.queryByTestId('daemon-status-mismatch')).toBeNull();
  });
});
