import { useState } from 'react';
import { Led, Pill, Popover, SideMenuIconButton } from '../components/kit';
import { checkProtocolCompat } from '../lib/protocolCompat';
import type { LiveState } from '../lib/live';

type StatusTone = 'emerald' | 'amber' | 'red';

const STATE_LABEL: Record<LiveState['state'], string> = {
  current: 'Connected',
  connecting: 'Connecting',
  stale: 'Reconnecting',
  disconnected: 'Disconnected',
};

/**
 * Green/connected, red/disconnected, amber/"needs a look" — the third bucket
 * covers both transitional states (connecting, reconnecting) and a settled
 * connection whose protocol this app can't write against. All three read
 * the same at a glance: not the clean "all good" of green.
 */
function toneFor(live: LiveState, mismatched: boolean): StatusTone {
  if (live.state === 'disconnected') return 'red';
  if (live.state === 'current' && !mismatched) return 'emerald';
  return 'amber';
}

/**
 * The connection status, as its own rail button rather than a panel that only
 * exists while a page happens to have a secondary menu (S41). A click opens
 * the same detail a `daemonFooter` row used to carry — version, protocol, and
 * the mismatch explanation when there is one — now available from every page,
 * not just the ones with something else to navigate.
 */
export function DaemonStatusButton({ live, workspaceName }: { live: LiveState; workspaceName?: string }) {
  const [open, setOpen] = useState(false);
  const compat = checkProtocolCompat(live);
  const tone = toneFor(live, compat.mismatched);
  const negotiated = live.state === 'current' || live.state === 'stale' ? live : undefined;
  const retained = live.state === 'stale';

  return (
    <Popover
      visible={open}
      arrow={false}
      onOpenChange={setOpen}
      placement="right"
      trigger={
        <SideMenuIconButton label={`Daemon: ${STATE_LABEL[live.state]}`} data-testid="daemon-status-trigger">
          <Led tone={tone} animation="none" variant="flat" />
        </SideMenuIconButton>
      }
    >
      <div className="w-56 space-y-2" data-testid="daemon-status-panel">
        <div className="flex items-center justify-between gap-3">
          <span className="font-semibold" data-testid="daemon-status-workspace">
            {workspaceName ?? 'Local daemon'}
          </span>
          <Pill tone={tone} size="sm" data-testid="daemon-status-state">
            {STATE_LABEL[live.state]}
          </Pill>
        </div>
        <dl className="space-y-1 text-xs text-slate-500 dark:text-slate-400">
          <div className="flex items-center justify-between gap-3">
            <dt>Version</dt>
            <dd data-testid="daemon-status-version">
              {negotiated?.daemonVersion ?? 'Unknown'}
              {retained && negotiated?.daemonVersion && ' (last confirmed)'}
            </dd>
          </div>
          <div className="flex items-center justify-between gap-3">
            <dt>Protocol</dt>
            <dd data-testid="daemon-status-protocol">
              {negotiated?.protocolVersion ?? 'Unknown'}
              {retained && negotiated?.protocolVersion !== undefined && ' (last confirmed)'}
            </dd>
          </div>
        </dl>
        {compat.mismatched && (
          <p className="text-xs text-amber-700 dark:text-amber-400" data-testid="daemon-status-mismatch">
            {compat.explanation}
          </p>
        )}
      </div>
    </Popover>
  );
}
