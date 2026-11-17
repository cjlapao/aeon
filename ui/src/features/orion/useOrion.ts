import { useEffect, useMemo, useRef, useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";
import { rpc } from "../../lib/bridge";
import { navMap } from "./navmap";
import { notifications } from "../../lib/notifications";
import { base64ToUrl, blobToBase64, createAudioEngine, type AudioEngine } from "./audio";
import { OrionController, type OrionSnapshot } from "./controller";
import { toWav } from "./wav";
import { orionSettings, useOrionSettings } from "./orionSettings";
import { DEFAULT_VAD } from "./vad";

export interface OrionView {
  snapshot: OrionSnapshot;
  /** The microphone stream or the playback analyser, whichever is the one making sound now. */
  audioSource: MediaStream | AnalyserNode | null;
  /** The browser is holding the audio back until the page is clicked; Orion cannot hear until then. */
  blocked: boolean;
  toggle(): void;
}

const OFF: OrionSnapshot = { phase: "off", missing: [] };

/** Runs Orion while he is enabled in settings; stops him, and closes the microphone, the moment he is not. */
export function useOrion(connected: boolean): OrionView {
  const settings = useOrionSettings();
  const navigate = useNavigate();
  // Read through a ref, so the controller built once always sees the page on screen now.
  const location = useLocation();
  const here = useRef(location.pathname + location.search);
  here.current = location.pathname + location.search;
  const [snapshot, setSnapshot] = useState<OrionSnapshot>(OFF);
  const [blocked, setBlocked] = useState(false);
  const engineRef = useRef<AudioEngine | null>(null);
  const controllerRef = useRef<OrionController | null>(null);

  // A different microphone needs the stream reopened, so it rebuilds Orion like turning him off and on.
  const key = `${settings.enabled && connected}:${settings.micId}`;
  useEffect(() => {
    if (!settings.enabled || !connected) { setSnapshot(OFF); return; }
    const engine = createAudioEngine(DEFAULT_VAD, () => orionSettings.getSnapshot().micId, setBlocked);
    engineRef.current = engine;
    const controller = new OrionController({
      rpc: (method, params) => rpc(method, params),
      audio: engine,
      codec: { prepare: toWav, toBase64: blobToBase64, toUrl: base64ToUrl, release: (u) => URL.revokeObjectURL(u) },
      settings: () => orionSettings.getSnapshot(),
      ui: { navigate: (to) => navigate(to), where: () => here.current, map: navMap },
    });
    // The map is held in memory from the moment he is on, so the first "go to…" does not wait for the daemon.
    void navMap.refresh((m, p) => rpc(m, p), true).catch(() => undefined);
    controllerRef.current = controller;
    const off = controller.subscribe(setSnapshot);
    void controller.start();
    // Notifications the app raises from now on (not the history already there) are handed to him; he decides by setting.
    let seen = new Set(notifications.getSnapshot().map((n) => n.id));
    const unsubscribe = notifications.subscribe(() => {
      const fresh = notifications.getSnapshot().filter((n) => !seen.has(n.id)).reverse();
      seen = new Set(notifications.getSnapshot().map((n) => n.id));
      for (const n of fresh) void controller.react(n);
    });
    return () => { unsubscribe(); off(); controller.stop(); engine.dispose(); engineRef.current = null; controllerRef.current = null; setSnapshot(OFF); };
    // The controller reads the latest settings through `settings()`; only turning him on or off rebuilds him.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key]);

  // A change of models while he runs needs no rebuild, but a change of wake mode needs the microphone reopened.
  const mode = settings.wakeMode;
  const firstMode = useRef(true);
  useEffect(() => {
    if (firstMode.current) { firstMode.current = false; return; } // the build above already started him in the current mode
    void controllerRef.current?.applyWakeMode();
  }, [mode]);

  return useMemo<OrionView>(() => ({
    snapshot, blocked,
    // The orb moves with what the microphone hears whenever it is open (always, while he waits for his name), and with his own voice while he speaks.
    audioSource: snapshot.phase === "speaking" ? engineRef.current?.output ?? null : snapshot.phase === "awake" || (snapshot.phase === "idle" && mode === "word") ? engineRef.current?.input ?? null : null,
    toggle: () => void controllerRef.current?.toggle(),
  }), [snapshot, blocked, mode]);
}
