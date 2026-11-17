import { useEffect, useRef, useState } from "react";
import { Alert, Button, MultiToggle, Panel, Pill } from "../../../components/kit";
import { notifications, type NotifyTone } from "../../../lib/notifications";

const TONES: { value: NotifyTone; label: string }[] = [
  { value: "info", label: "Info" },
  { value: "success", label: "Success" },
  { value: "warning", label: "Warning" },
  { value: "danger", label: "Danger" },
];

const DELAY_SECONDS = 5;

/**
 * Exercises `lib/notifications.ts` directly — the same door the daemon's
 * global event listener pushes through (`NotificationListener.tsx`) — so
 * this needs no daemon call and renders with or without a connection.
 *
 * Two separate buttons, because the two notifications the service raises
 * answer to different rules. A toast (`ToastViewport`, always mounted)
 * fires the instant `notifications.push` runs, focused or not — there is
 * nothing to time. An OS notification (`lib/osNotify.ts`) fires ONLY when
 * `document.hasFocus()` is false at the moment it's sent, which this very
 * window always is right after a click — so testing it needs a countdown
 * long enough to alt-tab away before it fires, not an immediate call.
 */
export function NotificationsPage() {
  const [tone, setTone] = useState<NotifyTone>("info");
  const [countdown, setCountdown] = useState<number | null>(null);
  const [focused, setFocused] = useState(() => document.hasFocus());
  const timerRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);

  useEffect(() => {
    const onFocus = () => setFocused(true);
    const onBlur = () => setFocused(false);
    window.addEventListener("focus", onFocus);
    window.addEventListener("blur", onBlur);
    return () => {
      window.removeEventListener("focus", onFocus);
      window.removeEventListener("blur", onBlur);
    };
  }, []);

  useEffect(() => () => clearTimeout(timerRef.current), []);

  function sendToast() {
    notifications.push({ title: "Test toast", body: `A ${tone} toast, sent on demand from the test bench.`, tone });
  }

  function sendSystemNotification() {
    clearTimeout(timerRef.current);
    setCountdown(DELAY_SECONDS);
    const tick = (remaining: number) => {
      if (remaining <= 0) {
        setCountdown(null);
        notifications.push({ title: "Test system notification", body: `A ${tone} notification, delayed so you could switch away.`, tone });
        return;
      }
      setCountdown(remaining);
      timerRef.current = setTimeout(() => tick(remaining - 1), 1000);
    };
    tick(DELAY_SECONDS);
  }

  function cancelSystemNotification() {
    clearTimeout(timerRef.current);
    setCountdown(null);
  }

  return (
    <div className="space-y-4" data-testid="bench-notifications">
      <Panel tone="slate">
        <h2 className="text-lg font-semibold">Notifications</h2>
        <p className="mt-1 text-sm">Fires through the real notification service — the same door the daemon's own events push through — so what you see here is what a person sees.</p>
        <label className="mt-3 block text-sm">
          Tone
          <div className="mt-1">
            <MultiToggle aria-label="Tone" options={TONES} value={tone} onChange={(value) => setTone(value as NotifyTone)} size="sm" indicator="tonal" />
          </div>
        </label>
      </Panel>

      <Panel tone="slate">
        <h3 className="text-base font-semibold">Toast</h3>
        <p className="mt-1 text-sm">Shows immediately, in `ToastViewport` — focus doesn't matter.</p>
        <Button className="mt-3" onClick={sendToast} data-testid="bench-notifications-toast">
          Send test toast
        </Button>
      </Panel>

      <Panel tone="slate">
        <h3 className="text-base font-semibold">System notification</h3>
        <p className="mt-1 text-sm">
          Only sent to the OS while this window is unfocused. Click below, then switch to another app — the {DELAY_SECONDS}-second countdown is the time to do that.
        </p>
        <div className="mt-2 flex items-center gap-2 text-sm">
          <span>Window is currently:</span>
          <Pill tone={focused ? "amber" : "emerald"} size="sm" data-testid="bench-notifications-focus">
            {focused ? "focused" : "unfocused"}
          </Pill>
        </div>
        {focused && countdown === null && (
          <Alert className="mt-2" tone="amber" description="The window is focused right now — the notification will be skipped unless you switch away before the countdown ends." />
        )}
        <div className="mt-3 flex items-center gap-3">
          {countdown === null ? (
            <Button onClick={sendSystemNotification} data-testid="bench-notifications-system">
              Send system notification ({DELAY_SECONDS}s delay)
            </Button>
          ) : (
            <>
              <Pill tone="violet" size="sm" data-testid="bench-notifications-countdown">
                Sending in {countdown}s…
              </Pill>
              <Button size="sm" variant="outline" onClick={cancelSystemNotification}>
                Cancel
              </Button>
            </>
          )}
        </div>
      </Panel>
    </div>
  );
}
