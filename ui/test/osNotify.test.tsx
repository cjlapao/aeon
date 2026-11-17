import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const isPermissionGranted = vi.fn();
const requestPermission = vi.fn();
const sendNotification = vi.fn();

vi.mock("@tauri-apps/plugin-notification", () => ({
  isPermissionGranted: () => isPermissionGranted(),
  requestPermission: () => requestPermission(),
  sendNotification: (options: unknown) => sendNotification(options),
}));

// Imported after the mock so the dynamic import inside osNotify.ts resolves
// to the mocked module, not the real plugin (which has nothing to call to
// in this test environment).
const { notifyOS, resetOsNotifyPermissionCache } = await import("../src/lib/osNotify");

describe("notifyOS — the OS-mirror rule (S41: only when the window is unfocused)", () => {
  beforeEach(() => {
    resetOsNotifyPermissionCache();
    isPermissionGranted.mockReset();
    requestPermission.mockReset();
    sendNotification.mockReset();
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("never calls the plugin at all when the window is focused", async () => {
    vi.spyOn(document, "hasFocus").mockReturnValue(true);
    await notifyOS({ title: "Loop finished", tone: "success" });
    expect(isPermissionGranted).not.toHaveBeenCalled();
    expect(sendNotification).not.toHaveBeenCalled();
  });

  it("sends the OS notification when unfocused and permission is already granted", async () => {
    vi.spyOn(document, "hasFocus").mockReturnValue(false);
    isPermissionGranted.mockResolvedValue(true);
    await notifyOS({ title: "Loop finished", body: "l1 is done", tone: "success" });
    expect(requestPermission).not.toHaveBeenCalled();
    expect(sendNotification).toHaveBeenCalledWith({ title: "Loop finished", body: "l1 is done" });
  });

  it("requests permission once when not yet granted, and sends only if the user grants it", async () => {
    vi.spyOn(document, "hasFocus").mockReturnValue(false);
    isPermissionGranted.mockResolvedValue(false);
    requestPermission.mockResolvedValue("denied");
    await notifyOS({ title: "Loop blocked", tone: "warning" });
    expect(requestPermission).toHaveBeenCalledTimes(1);
    expect(sendNotification).not.toHaveBeenCalled();
  });

  it("caches the permission result across calls — the prompt is asked at most once per session", async () => {
    vi.spyOn(document, "hasFocus").mockReturnValue(false);
    isPermissionGranted.mockResolvedValue(false);
    requestPermission.mockResolvedValue("granted");

    await notifyOS({ title: "First", tone: "info" });
    await notifyOS({ title: "Second", tone: "info" });

    expect(isPermissionGranted).toHaveBeenCalledTimes(1);
    expect(requestPermission).toHaveBeenCalledTimes(1);
    expect(sendNotification).toHaveBeenCalledTimes(2);
  });

  it("stays silent, never throwing, when the plugin call itself fails (the plain-browser dev path)", async () => {
    vi.spyOn(document, "hasFocus").mockReturnValue(false);
    isPermissionGranted.mockRejectedValue(new Error("no Tauri bridge"));
    await expect(notifyOS({ title: "Loop finished", tone: "success" })).resolves.toBeUndefined();
    expect(sendNotification).not.toHaveBeenCalled();
  });
});
