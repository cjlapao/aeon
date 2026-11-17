import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MicPicker, listMics } from "../src/features/orion/MicPicker";
import { openMic } from "../src/features/orion/audio";
import { DEFAULT_ORION, orionSettings, sanitizeOrion } from "../src/features/orion/orionSettings";

const devices = [{ kind: "audioinput", deviceId: "built-in", label: "Built-in Audio" }, { kind: "audioinput", deviceId: "usb", label: "USB Headset" }, { kind: "videoinput", deviceId: "cam", label: "Camera" }];

describe("choosing the microphone", () => {
  let getUserMedia: ReturnType<typeof vi.fn>;
  beforeEach(() => {
    orionSettings.set(DEFAULT_ORION);
    getUserMedia = vi.fn(async () => ({ getTracks: () => [{ stop() {} }] }));
    vi.stubGlobal("navigator", { mediaDevices: { enumerateDevices: async () => devices, getUserMedia, addEventListener() {}, removeEventListener() {} } });
  });
  afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

  it("lists only the audio inputs by name and saves the one picked", async () => {
    render(<MicPicker />);
    const select = (await screen.findByLabelText("Microphone")) as HTMLSelectElement;
    await vi.waitFor(() => expect([...select.options].map((o) => o.text)).toEqual(["System default", "Built-in Audio", "USB Headset"]));
    fireEvent.change(select, { target: { value: "usb" } });
    expect(orionSettings.getSnapshot().micId).toBe("usb");
  });

  it("asks once to read the names when the browser hides them, and says when a saved microphone is gone", async () => {
    const hidden = devices.map((d) => ({ ...d, label: "" }));
    let calls = 0;
    vi.stubGlobal("navigator", { mediaDevices: { enumerateDevices: async () => (calls++ === 0 ? hidden : devices), getUserMedia } });
    expect((await listMics()).map((m) => m.label)).toEqual(["Built-in Audio", "USB Headset"]);
    expect(getUserMedia).toHaveBeenCalledTimes(1);
    orionSettings.set({ ...DEFAULT_ORION, micId: "unplugged" });
    render(<MicPicker />);
    expect(await screen.findByText("The saved microphone (not connected)")).toBeTruthy();
  });

  it("opens the chosen microphone exactly, and falls back to the default when it is gone", async () => {
    await openMic("usb");
    expect(getUserMedia.mock.calls[0]![0].audio.deviceId).toEqual({ exact: "usb" });
    getUserMedia.mockReset();
    getUserMedia.mockRejectedValueOnce(new DOMException("gone", "OverconstrainedError")).mockResolvedValueOnce("default-stream");
    expect(await openMic("usb")).toBe("default-stream");
    expect(getUserMedia.mock.calls[1]![0].audio.deviceId).toBeUndefined();
    getUserMedia.mockReset();
    getUserMedia.mockRejectedValueOnce(new DOMException("denied", "NotAllowedError"));
    await expect(openMic("usb")).rejects.toThrow("denied"); // a refusal is not hidden behind a fallback
  });

  it("keeps a saved microphone and repairs a damaged one", () => {
    expect(sanitizeOrion({ micId: "usb" }).micId).toBe("usb");
    expect(sanitizeOrion({ micId: 7 }).micId).toBe("");
  });
});
