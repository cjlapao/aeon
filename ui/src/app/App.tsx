import { CommandPalette } from "../features/palette/CommandPalette";
import { useEffect, useLayoutEffect, useMemo, useState, useSyncExternalStore } from 'react';
import { Outlet, useLocation } from 'react-router-dom';
import { ToastViewport } from '@cjlapao/ui-kit';
import { Button } from '../components/kit';
import { LiveController, type LiveState } from '../lib/live';
import { connected, connectingTo, failed, type Connection } from '../lib/connection';
import { startAppSettings } from '../lib/appSettings';
import { NotificationListener } from './NotificationListener';
import { NavigateListener } from './NavigateListener';
import { PageLayout } from './PageLayout';
import { SplashScreen } from './SplashScreen';
import { OrionOrb } from '../features/orion/OrionOrb';
import { layoutFor } from './routes';
import { checkProtocolCompat } from '../lib/protocolCompat';
import './App.css';

const SPLASH_SHOW_DELAY_MS = 150;
// A connection that never settles (no Tauri bridge at all — the documented "web Chrome" dev path)
// must not trap the app behind the splash forever; past this, the shell takes over and shows its
// own inline "connecting…" the way it always has.
const SPLASH_GIVE_UP_MS = 8000;

/**
 * True only across the very first connection attempt, and only once it has taken long enough to
 * be worth announcing — a daemon that answers inside the delay never shows it at all, so a fast
 * local boot never flashes. Once this clears it never returns: a later drop has its own inline
 * "Connection lost" handling.
 */
function useBootSplash(live: LiveState): boolean {
  const [resolvedOnce, setResolvedOnce] = useState(false);
  const [worthShowing, setWorthShowing] = useState(false);
  useEffect(() => {
    if (live.state !== 'connecting') setResolvedOnce(true);
  }, [live.state]);
  useEffect(() => {
    const showTimer = setTimeout(() => setWorthShowing(true), SPLASH_SHOW_DELAY_MS);
    const giveUpTimer = setTimeout(() => setResolvedOnce(true), SPLASH_GIVE_UP_MS);
    return () => { clearTimeout(showTimer); clearTimeout(giveUpTimer); };
  }, []);
  return false
  return !resolvedOnce && worthShowing;
}

export interface AppRouteContext {
  controller: LiveController;
  live: LiveState;
  canWrite: boolean;
  benchConnection: Connection;
}

function useLive(controller: LiveController): LiveState {
  return useSyncExternalStore(controller.subscribe, controller.getState, controller.getState);
}

export function ProductShell() {
  const location = useLocation();
  const controller = useMemo(() => new LiveController(), []);
  const live = useLive(controller);
  const showSplash = useBootSplash(live);

  useLayoutEffect(startAppSettings, []);

  useEffect(() => {
    void controller.start();
    return () => controller.stop();
  }, [controller]);

  const { canWrite } = checkProtocolCompat(live);
  const benchConnection =
    live.state === 'current' ? connected(live.daemonVersion, live.protocolVersion, live.endpoint) : live.state === 'disconnected' ? failed(live.reason, live.endpoint) : connectingTo(live.endpoint);

  if (showSplash) {
    return <SplashScreen status={live.state === 'disconnected' ? 'Could not reach the daemon' : 'Starting the daemon…'} />;
  }

  return (
    <>
      <NotificationListener live={live} />
      {/* S58 T8: the tray/deep-link half of the way back in — mounted once beside the
          notification listener, for the same reason (it hears the shell no
          screen's narrower subscription would). */}
      <NavigateListener />
      <CommandPalette live={live} />
      <ToastViewport />
      <OrionOrb connected={live.state === 'current'} />
      <PageLayout controller={controller} live={live} fullBleed={layoutFor(location.pathname) === 'workspace'}>
        {live.state === 'disconnected' && (
          <div className="flex items-center justify-between gap-3 rounded-xl border border-amber-500/30 p-3">
            <span>Connection lost. Saved information may be out of date.</span>
            <Button size="sm" variant="outline" onClick={() => void controller.reconnect()}>
              Reconnect
            </Button>
          </div>
        )}
        <Outlet context={{ controller, live, canWrite, benchConnection } satisfies AppRouteContext} />
      </PageLayout>
    </>
  );
}
