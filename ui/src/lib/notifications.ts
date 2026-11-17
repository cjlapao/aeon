import type { ToastApi } from "@cjlapao/ui-kit";
import { createPersistedStore, usePersistedStore } from "./createPersistedStore";

export type NotifyTone = "info" | "success" | "warning" | "danger";

export interface AppNotification {
  id: string;
  title: string;
  body?: string;
  tone: NotifyTone;
  /** Where it came from: a daemon event, or a purely local action (no daemon involved). */
  source: "daemon" | "local";
  /** ISO timestamp. */
  at: string;
  read: boolean;
  /**
   * S58 SV4/SV7: what the notification is ABOUT, in the daemon's own
   * subject key. Optional and additive — entries written before S58 stay
   * valid (`isAppNotification` never looked at it and still does not).
   */
  attentionKey?: string;
  /** S58 SV9: a place to go, never an answer to carry (see `AeonNotifyHint.deepLink`). */
  deepLink?: string;
}

export interface NotifyInput {
  title: string;
  body?: string;
  /** @default "info" */
  tone?: NotifyTone;
  /** @default "local" — this call wasn't made by the daemon listener. */
  source?: "daemon" | "local";
  /** Dedups repeated pushes of the SAME thing (see `AppNotification.attentionKey`). */
  attentionKey?: string;
  /** Carries an "Open" action on the toast (see `registerNavigateHandler`). */
  deepLink?: string;
}

const STORAGE_KEY = "aeon-notifications-v1";
// A bound, not a design choice someone needs to configure: this is a local
// history for a person to glance back at, not a log. (§4c-adjacent: nothing
// here is a credential, but "never in the daemon" extends to not letting a
// browser profile grow an unbounded one either.)
const MAX_HISTORY = 200;

/**
 * SV-D10: repeated notifications about one subject collapse into one.
 * The window is the DAEMON's — `internal/notify/dispatcher.go`'s
 * `dedupWindow = 10 * time.Minute` — mirrored here deliberately rather
 * than tuned locally: the dispatcher already suppresses repeats of one
 * key inside ten minutes on the channel path, and the app suppresses the
 * live-event path with the same number, so the same subject that
 * announces itself twice (`loop.waiting` + `question.asked` for one
 * question) toasts exactly once anywhere. Changing one without the other
 * is a drift bug; both constants name each other.
 */
const DEDUP_WINDOW_MS = 10 * 60 * 1000;

function isAppNotification(value: unknown): value is AppNotification {
  if (typeof value !== "object" || value === null) return false;
  const v = value as Record<string, unknown>;
  return typeof v.id === "string" && typeof v.title === "string" && typeof v.tone === "string" &&
    typeof v.source === "string" && typeof v.at === "string" && typeof v.read === "boolean";
}

const store = createPersistedStore<AppNotification[]>(STORAGE_KEY, [], (parsed) =>
  Array.isArray(parsed) ? parsed.filter(isAppNotification) : [],
);

/**
 * The live `ToastApi`, registered by whichever component mounted
 * `<ToastProvider>` (`ProductApp`). `push` below is callable from anywhere —
 * a save handler, the global daemon-event listener — and must not itself be
 * a hook, which is why this is a plain registered reference rather than
 * `useToast()` called here.
 */
let toastApi: ToastApi | null = null;
export function registerToastApi(api: ToastApi | null): void {
  toastApi = api;
}

/**
 * The OS-notification hook, registered by the component that owns the
 * "only when the window is unfocused" rule and the Tauri plugin call
 * itself (`lib/osNotify.ts`) — kept out of this module so a plain browser
 * build never needs to know the plugin exists.
 */
let osNotifier: ((input: NotifyInput) => void) | null = null;
export function registerOsNotifier(fn: ((input: NotifyInput) => void) | null): void {
  osNotifier = fn;
}

/**
 * The navigate handler the toast's "Open" action calls (S58 T8), registered
 * by `NavigateListener` — the same registry shape as the OS notifier, and
 * for the same reason: `push` is callable from anywhere, and the router only
 * exists inside the mounted app. No handler (plain browser build, tests that
 * mount nothing) and the toast still toasts; only the action is inert.
 */
let navigateHandler: ((link: string) => void) | null = null;
export function registerNavigateHandler(fn: ((link: string) => void) | null): void {
  navigateHandler = fn;
}

/**
 * The unread-count sink, registered by the always-mounted listener
 * (`NotificationListener`) — the same registry shape as the OS notifier, and
 * for the same reason: the mutation methods below are callable from anywhere,
 * and the shell door exists only inside the mounted app. The sink mirrors the
 * bell's number onto the tray icon (the shell's `tray_unread` command). No
 * sink — plain browser build, tests that mount nothing — and unread changes
 * simply go unmirrored; every notification behaves exactly as it always did.
 */
let unreadSink: ((unread: number) => void) | null = null;
export function registerUnreadSink(fn: ((unread: number) => void) | null): void {
  unreadSink = fn;
}

/** The bell's number, as of right now, announced to the sink if there is one. */
function announceUnread(): void {
  unreadSink?.(store.getSnapshot().filter((item) => !item.read).length);
}

function makeId(): string {
  return `${Date.now()}-${Math.random().toString(36).slice(2, 9)}`;
}

/**
 * The one door both the daemon's global listener and any component's own
 * local action (a save, an add, a remove) push a notification through —
 * same shape either way, so a screen does not need to know or care which
 * kind it is raising.
 */
export const notifications = {
  getSnapshot: store.getSnapshot,
  subscribe: store.subscribe,

  push(input: NotifyInput): AppNotification {
    const tone = input.tone ?? "info";
    const now = Date.now();
    if (input.attentionKey !== undefined) {
      // SV-D10, app side: the same subject, seen twice inside the window,
      // is ONE notification — the first one stays on screen and in history,
      // and the second is dropped before it can toast, enter history, or
      // ping the OS. Dropped quietly, because "quiet" is what dedup MEANS;
      // the subject is not gone, only its second announcement is.
      const key = input.attentionKey;
      const seen = store.getSnapshot().find(
        (item) => item.attentionKey === key && now - Date.parse(item.at) <= DEDUP_WINDOW_MS,
      );
      if (seen) return seen;
    }
    const entry: AppNotification = {
      id: makeId(),
      title: input.title,
      body: input.body,
      tone,
      source: input.source ?? "local",
      at: new Date().toISOString(),
      read: false,
    };
    if (input.attentionKey !== undefined) entry.attentionKey = input.attentionKey;
    if (input.deepLink !== undefined) entry.deepLink = input.deepLink;
    store.set((current) => [entry, ...current].slice(0, MAX_HISTORY));
    toastApi?.[tone](entry.title, entry.body, {
      meta: entry.id,
      // SV9: the toast action navigates, and nothing else. The label is a
      // literal because the catalogs carry no notifications namespace beyond
      // the two strings T8 adds (`notifications.link.*`) — an app-wide
      // English action reads no worse beside the English daemon sentences
      // the same toast already shows.
      actions: entry.deepLink
        ? [{ label: "Open", onClick: () => navigateHandler?.(entry.deepLink ?? "") }]
        : undefined,
    });
    osNotifier?.(input);
    announceUnread();
    return entry;
  },

  markRead(id: string): void {
    store.set((current) => current.map((item) => (item.id === id ? { ...item, read: true } : item)));
    announceUnread();
  },
  markAllRead(): void {
    store.set((current) => current.map((item) => (item.read ? item : { ...item, read: true })));
    announceUnread();
  },
  clear(): void {
    store.set([]);
    announceUnread();
  },
};

export function useNotifications(): AppNotification[] {
  return usePersistedStore(store);
}

export function useUnreadNotificationCount(): number {
  return useNotifications().filter((item) => !item.read).length;
}
