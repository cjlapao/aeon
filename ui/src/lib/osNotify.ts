import type { NotifyInput } from "./notifications";

let permissionChecked = false;
let permissionGranted = false;

async function ensurePermission(): Promise<boolean> {
  if (permissionChecked) return permissionGranted;
  try {
    const { isPermissionGranted, requestPermission } = await import("@tauri-apps/plugin-notification");
    permissionGranted = await isPermissionGranted();
    if (!permissionGranted) {
      const result = await requestPermission();
      permissionGranted = result === "granted";
    }
  } catch {
    // No Tauri bridge at all (the documented "web Chrome" dev path) — the
    // dynamic import's calls throw rather than resolve. Either way there is
    // no OS to notify, and this is the same "stays silent" rule the rest of
    // the bridge follows for that path.
    permissionGranted = false;
  } finally {
    permissionChecked = true;
  }
  return permissionGranted;
}

/**
 * Mirrors a notification to the OS — but only when nobody is actually
 * looking at the app. That's the one rule this module owns (S41): a
 * focused window already shows the in-app toast, so a second OS
 * notification on top of it would just be noise. An unfocused one has
 * nothing else telling the person anything happened.
 */
export async function notifyOS(input: NotifyInput): Promise<void> {
  if (document.hasFocus()) return;
  if (!(await ensurePermission())) return;
  try {
    const { sendNotification } = await import("@tauri-apps/plugin-notification");
    // SV9/T8: the link rides along as `extra`, the field the plugin defines
    // for exactly this — VERIFIED against @tauri-apps/plugin-notification
    // 2.8.0's own types (`extra?: Record<string, unknown>`; its click
    // callback is the mobile action path — desktop delivers no click event
    // for a plain notification). So the OS notification stays what it is:
    // informational, with the address inside it. The way BACK is the in-app
    // toast's Open action, which exists even when no OS click ever lands.
    sendNotification(
      input.deepLink
        ? { title: input.title, body: input.body, extra: { deepLink: input.deepLink } }
        : { title: input.title, body: input.body },
    );
  } catch {
    // Same reasoning as ensurePermission: a plain browser build has nothing
    // to call, and that is not a failure worth surfacing.
  }
}

/** Test seam: the permission cache survives across tests otherwise. */
export function resetOsNotifyPermissionCache(): void {
  permissionChecked = false;
  permissionGranted = false;
}
