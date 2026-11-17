import { useState } from "react";
import { Badge, Button, EmptyState, Popover, SideMenuIconButton } from "../components/kit";
import { notifications, useNotifications, useUnreadNotificationCount, type AppNotification } from "../lib/notifications";

const TONE_DOT: Record<AppNotification["tone"], string> = {
  info: "bg-sky-500",
  success: "bg-emerald-500",
  warning: "bg-amber-500",
  danger: "bg-red-500",
};

function NotificationRow({ item }: { item: AppNotification }) {
  return (
    <button
      type="button"
      onClick={() => notifications.markRead(item.id)}
      className={`flex w-full items-start gap-2 rounded-lg p-2 text-left text-sm hover:bg-black/5 dark:hover:bg-white/5 ${item.read ? "opacity-60" : ""}`}
    >
      <span className={`mt-1.5 h-2 w-2 shrink-0 rounded-full ${TONE_DOT[item.tone]}`} aria-hidden="true" />
      <span className="min-w-0 flex-1">
        <span className="block font-medium">{item.title}</span>
        {item.body && <span className="block text-xs text-slate-500 dark:text-slate-400">{item.body}</span>}
      </span>
    </button>
  );
}

/**
 * The history side of the notification system (S41) — the ephemeral toast
 * is a separate, transient surface (`NotificationListener`/`ToastViewport`);
 * this is what's left to look back at, backed by the same localStorage
 * store (`lib/notifications.ts`), never the daemon.
 */
export function NotificationBell() {
  const items = useNotifications();
  const unread = useUnreadNotificationCount();
  const [open, setOpen] = useState(false);

  return (
    <Popover
      visible={open}
      arrow={ false}
      onOpenChange={setOpen}
      placement="right"
      trigger={
        <SideMenuIconButton
          label="Notifications"
          icon="Notification"
          data-testid="notification-bell-trigger"
          badge={unread > 0 ? <Badge count={unread} tone="red" label={`${unread} unread notifications`} /> : undefined}
        />
      }
    >
      <div className="space-y-2" data-testid="notification-bell-panel">
        <div className="flex items-center justify-between gap-2">
          <span className="text-sm font-semibold">Notifications</span>
          {items.length > 0 && (
            <div className="flex gap-1">
              <Button size="sm" variant="ghost" disabled={unread === 0} onClick={() => notifications.markAllRead()}>
                Mark all read
              </Button>
              <Button size="sm" variant="ghost" onClick={() => notifications.clear()}>
                Clear
              </Button>
            </div>
          )}
        </div>
        {items.length === 0 ? (
          <EmptyState variant="plain" padding="md" title="No notifications yet" fullWidth size="xs" icon="Notification" iconBackground={false} dashed={ false} iconSize="xs" />
        ) : (
          <div className="max-h-96 space-y-1 overflow-auto">
            {items.map((item) => (
              <NotificationRow key={item.id} item={item} />
            ))}
          </div>
        )}
      </div>
    </Popover>
  );
}
