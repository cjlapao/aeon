import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { installFakeDaemon, resetFakeDaemon } from "../../../test/helpers";
import { ModelVoiceTests } from "./ModelVoiceTests";

beforeEach(() => {
  URL.createObjectURL = vi.fn(() => "blob:x");
  vi.stubGlobal("Audio", class { play() { return Promise.resolve(); } });
});
afterEach(() => { cleanup(); resetFakeDaemon(); vi.unstubAllGlobals(); });

describe("the speech and embedding checks", () => {
  it("keeps each check off, and says why, unless the model declares its capability", () => {
    installFakeDaemon({});
    render(<ModelVoiceTests target={{ route: "kokoro" }} capabilities={["tts"]} canRun />);
    expect((screen.getByTestId("voice-run-tts") as HTMLButtonElement).disabled).toBe(false);
    expect((screen.getByTestId("voice-run-asr") as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByTestId("voice-run-embedding") as HTMLButtonElement).disabled).toBe(true);
    expect(screen.getByTestId("voice-needs-asr").textContent).toContain("Speech to text");
    expect(screen.queryByTestId("voice-needs-tts")).toBeNull();
  });

  it("tests the unsaved form values as a draft, with no route id", async () => {
    const daemon = installFakeDaemon({ responses: { "voice.speak": { audio: "QVVESU8=", mime: "audio/wav" } } });
    render(<ModelVoiceTests target={{ draft: { provider: "openai", model: "hexgrad/Kokoro-82M", audioApi: "synthesize", capabilities: ["tts"] } }} capabilities={["tts"]} canRun />);
    fireEvent.change(screen.getByTestId("voice-name"), { target: { value: "aai-kyla" } });
    fireEvent.click(screen.getByTestId("voice-run-tts"));
    await screen.findByTestId("voice-result-tts");
    expect((daemon.paramsOf("voice.speak") as { voice?: string }).voice).toBe("aai-kyla");
    const sent = daemon.paramsOf("voice.speak") as { route?: string; draft?: { audioApi: string } };
    expect(sent.route).toBeUndefined();
    expect(sent.draft?.audioApi).toBe("synthesize");
  });

  it("speaks a prepared sentence through the model and says it worked", async () => {
    const daemon = installFakeDaemon({ responses: { "voice.speak": { audio: "QVVESU8=", mime: "audio/wav" } } });
    render(<ModelVoiceTests target={{ route: "kokoro" }} capabilities={["tts"]} canRun />);
    fireEvent.click(screen.getByTestId("voice-run-tts"));
    expect((await screen.findByTestId("voice-result-tts")).textContent).toContain("audio/wav");
    expect(daemon.paramsOf("voice.speak")).toMatchObject({ route: "kokoro" });
    expect(String((daemon.paramsOf("voice.speak") as { text: string }).text)).toContain("Aeon");
  });

  it("reports the embedding's size, and shows a failure in the daemon's words", async () => {
    installFakeDaemon({ responses: { "voice.embed": { dimensions: 1024, inputTokens: 7 } } });
    render(<ModelVoiceTests target={{ route: "kokoro" }} capabilities={["embedding"]} canRun />);
    fireEvent.click(screen.getByTestId("voice-run-embedding"));
    expect((await screen.findByTestId("voice-result-embedding")).textContent).toContain("1024 numbers");
    cleanup(); resetFakeDaemon();
    installFakeDaemon({ responses: { "voice.embed": () => { throw Object.assign(new Error("embeddings endpoint refused"), { code: -32000 }); } } });
    render(<ModelVoiceTests target={{ route: "kokoro" }} capabilities={["embedding"]} canRun />);
    fireEvent.click(screen.getByTestId("voice-run-embedding"));
    expect((await screen.findByTestId("voice-result-embedding")).textContent).toContain("refused");
    expect(screen.getByText("failed")).toBeTruthy();
  });

  it("offers the microphone choice only when the model can hear", () => {
    vi.stubGlobal("navigator", { mediaDevices: { enumerateDevices: async () => [], getUserMedia: async () => ({ getTracks: () => [] }), addEventListener() {}, removeEventListener() {} } });
    installFakeDaemon({});
    const { unmount } = render(<ModelVoiceTests target={{ route: "kokoro" }} capabilities={["tts"]} canRun />);
    expect(screen.queryByTestId("mic-picker")).toBeNull();
    unmount();
    render(<ModelVoiceTests target={{ route: "kokoro" }} capabilities={["asr"]} canRun />);
    expect(screen.getByTestId("mic-picker")).toBeTruthy();
  });

  it("records the microphone and shows the words the model heard", async () => {
    const stop = vi.fn();
    const track = { stop };
    vi.stubGlobal("navigator", { mediaDevices: { getUserMedia: async () => ({ getTracks: () => [track] }) } });
    class Rec { static isTypeSupported() { return true; } state = "inactive"; mimeType = "audio/webm"; ondataavailable: ((e: { data: Blob }) => void) | null = null; onstop: (() => void) | null = null;
      start() { this.state = "recording"; } stop() { this.state = "inactive"; this.ondataavailable?.({ data: new Blob(["x"]) }); this.onstop?.(); } }
    vi.stubGlobal("MediaRecorder", Rec);
    const daemon = installFakeDaemon({ responses: { "voice.transcribe": { text: "start a new loop" } } });
    vi.useFakeTimers({ shouldAdvanceTime: true });
    render(<ModelVoiceTests target={{ route: "kokoro" }} capabilities={["asr"]} canRun />);
    fireEvent.click(screen.getByTestId("voice-run-asr"));
    await vi.advanceTimersByTimeAsync(5200);
    vi.useRealTimers();
    expect((await screen.findByTestId("voice-result-asr")).textContent).toContain("start a new loop");
    expect(daemon.paramsOf("voice.transcribe")).toMatchObject({ route: "kokoro" });
    expect(stop).toHaveBeenCalled(); // the microphone is released
    // The recording is kept to play back, and a recording with no sound is called silent.
    expect(screen.getByTestId("voice-clip-audio")).toBeTruthy();
    expect(screen.getByTestId("voice-clip-facts").textContent).toContain("audio/webm");
    expect(screen.getByTestId("voice-clip-silent")).toBeTruthy();
  });
});
