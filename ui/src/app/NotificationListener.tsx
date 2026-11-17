import { useEffect } from "react";
import { useToast } from "@cjlapao/ui-kit";
import { NOTIFY_PATTERN } from "../lib/eventHub";
import { useLiveSubscription } from "../lib/useLiveSubscription";
import { notifications, registerOsNotifier, registerToastApi, registerUnreadSink, type NotifyInput } from "../lib/notifications";
import { invokeShell } from "../lib/bridge";
import { notifyOS } from "../lib/osNotify";
import type { LiveState } from "../lib/live";
import type { AeonEvent } from "../lib/protocol";

/**
 * The one always-mounted listener behind the whole notification system
 * (S41): it watches every event the daemon delivers, not just the kinds a
 * particular screen cares about, and acts only when `event.notify` is
 * present — the same event a screen's own narrower `useLiveSubscription`
 * call already used to update its own state, carrying one additional,
 * optional, human-facing hint.
 *
 * Mounted once, inside `ProductShell` where `live` already exists — not per
 * screen, so a toast fires regardless of which page happens to be open.
 */
export function NotificationListener({ live }: { live: LiveState }) {
  const { toast } = useToast();

  useEffect(() => {
    registerToastApi(toast);
    return () => registerToastApi(null);
  }, [toast]);

  useEffect(() => {
    const notify = (input: NotifyInput) => void notifyOS(input);
    registerOsNotifier(notify);
    return () => registerOsNotifier(null);
  }, []);

  useEffect(() => {
    // The tray-icon mirror: every unread-count change (and once on mount, so
    // a reload re-syncs a shell that never saw this store's pushes) reports
    // the bell's number to the shell, which paints or clears the dot from it
    // alone — read them all and the indicator goes away, always. A shell
    // feature, not a daemon door — and swallowed like the OS notifier
    // swallows a missing plugin: a plain browser build has nothing to call,
    // which is not a failure worth surfacing.
    const pushToShell = (unread: number) => {
      void invokeShell("tray_unread", { unread }).catch(() => {});
    };
    registerUnreadSink(pushToShell);
    pushToShell(notifications.getSnapshot().filter((item) => !item.read).length);
    return () => registerUnreadSink(null);
  }, []);

  useLiveSubscription(live, [NOTIFY_PATTERN], (event: AeonEvent) => {
    if (!event.notify) return;
    // S58 T8: the two optional fields pass through untouched. A hint
    // without them behaves exactly as the T1 golden recorded — the dedup
    // never fires without a key, the action never appears without a link.
    notifications.push({
      title: event.notify.title,
      body: event.notify.body,
      tone: event.notify.tone,
      source: "daemon",
      attentionKey: event.notify.attentionKey,
      deepLink: event.notify.deepLink,
    });
  });

  return null;
}
