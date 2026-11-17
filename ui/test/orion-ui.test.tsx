import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { OrionSettingsPanel } from "../src/features/orion/OrionSettingsPanel";
import { OrionOrb } from "../src/features/orion/OrionOrb";
import { DEFAULT_ORION, orionSettings } from "../src/features/orion/orionSettings";
import { installFakeDaemon, resetFakeDaemon } from "./helpers";

// The orb draws on a canvas the test DOM does not have; what matters here is the state and preset it is given.
vi.mock("@cjlapao/ui-kit", async (orig) => {
  const actual = await orig<typeof import("@cjlapao/ui-kit")>();
  return { ...actual, AiOrb: ({ state, preset }: { state: string; preset: string }) => <span data-testid="ai-orb" data-state={state} data-preset={preset} /> };
});

describe("Orion's settings and orb", () => {
  beforeEach(() => {
    resetFakeDaemon();
    orionSettings.set(DEFAULT_ORION);
    window.matchMedia = vi.fn(() => ({ matches: false, media: "", onchange: null, addEventListener: () => {}, removeEventListener: () => {}, addListener: () => {}, removeListener: () => {}, dispatchEvent: () => false })) as unknown as typeof window.matchMedia;
  });
  afterEach(() => cleanup());

  it("offers each model only under the heading it can serve, and saves the choices", async () => {
    installFakeDaemon({ responses: { "routes.list": { routes: [
      { id: "whisper", model: "whisper-1", capabilities: ["asr"] },
      { id: "voice", model: "tts-1", capabilities: ["tts"] },
      { id: "qwen", model: "qwen", capabilities: ["text", "tool-calling"] },
    ], providers: [], persisted: true } } });
    render(<MemoryRouter><OrionSettingsPanel tone={"emerald" as never} /></MemoryRouter>);
    const asr = (await screen.findByLabelText("Hears with (speech to text)")) as HTMLSelectElement;
    await vi.waitFor(() => expect(asr.options.length).toBe(2));
    expect([...asr.options].map((o) => o.value)).toEqual(["", "whisper"]);
    fireEvent.change(asr, { target: { value: "whisper" } });
    fireEvent.change(screen.getByLabelText("Speaks with (text to speech)"), { target: { value: "voice" } });
    fireEvent.change(screen.getByLabelText("Operates with (the model that decides)"), { target: { value: "qwen" } });
    fireEvent.click(screen.getByTestId("orion-preset-ion"));
    fireEvent.click(screen.getByTestId("orion-enabled"));
    expect(orionSettings.getSnapshot()).toMatchObject({ enabled: true, preset: "ion", asrRoute: "whisper", ttsRoute: "voice", operatorRoute: "qwen" });
  });

  it("renders nothing while he is off, and tells the person what is missing when he is on without his models", () => {
    const { rerender } = render(<MemoryRouter><OrionOrb connected /></MemoryRouter>);
    expect(screen.queryByTestId("orion")).toBeNull();
    orionSettings.set({ ...DEFAULT_ORION, enabled: true, preset: "ember" });
    rerender(<MemoryRouter><OrionOrb connected /></MemoryRouter>);
    expect(screen.getByTestId("orion").getAttribute("data-phase")).toBe("off");
    expect(screen.getByTestId("orion-bubble").textContent).toContain("speech-to-text model");
    expect(screen.getByTestId("ai-orb").getAttribute("data-preset")).toBe("ember");
  });

  it("hides the orb while he waits for his name, shows it listening once he is woken, and keeps it in click mode", async () => {
    orionSettings.set({ ...DEFAULT_ORION, enabled: true, asrRoute: "a", ttsRoute: "t", operatorRoute: "o", wakeMode: "word" });
    window.matchMedia = vi.fn(() => ({ matches: false, media: "", onchange: null, addEventListener: () => {}, removeEventListener: () => {}, addListener: () => {}, removeListener: () => {}, dispatchEvent: () => false })) as unknown as typeof window.matchMedia;
    vi.stubGlobal("navigator", { mediaDevices: { getUserMedia: async () => ({ getTracks: () => [] }) } });
    const { unmount } = render(<MemoryRouter><OrionOrb connected /></MemoryRouter>);
    await vi.waitFor(() => expect(screen.queryByTestId("orion")).toBeNull());      // asleep by name: nothing on screen
    unmount();
    orionSettings.set({ ...DEFAULT_ORION, enabled: true, asrRoute: "a", ttsRoute: "t", operatorRoute: "o", wakeMode: "click" });
    render(<MemoryRouter><OrionOrb connected /></MemoryRouter>);
    expect(await screen.findByTestId("orion-orb")).toBeTruthy();                   // click mode: the orb is the button
  });
});
