import { createPersistedStore, usePersistedStore } from "../../lib/createPersistedStore";
import type { ReactTo } from "./notify";

/** The orb looks the kit's AI orb offers. */
export const ORB_PRESETS = ["solar", "ion", "ember"] as const;
export type OrbPreset = (typeof ORB_PRESETS)[number];

/** Orion's settings. They belong to this device (the microphone and the screen are this device's), so they are kept here. */
export interface OrionSettings {
  /** Orion is off until the person turns him on; nothing listens while it is false. */
  enabled: boolean;
  preset: OrbPreset;
  /** How he is woken: by saying the word (he listens for speech and checks each phrase with the speech-to-text model), or only by clicking the orb. */
  wakeMode: "word" | "click";
  /** The word that wakes him. */
  wakeWord: string;
  /** Routes by id: the model that hears (asr), the one that speaks (tts), the one that decides and operates. */
  asrRoute: string;
  ttsRoute: string;
  operatorRoute: string;
  voice: string;
  /** The microphone, by device id; empty is the system default. */
  micId: string;
  /** Whether he speaks up when the app raises a notification (a loop asking for something, a failure): never, only what needs a person, or all. */
  reactTo: ReactTo;
  /** ISO language hint for recognition ("" lets the model decide). */
  language: string;
  /** After an answer, how long he keeps listening for a follow-up without being woken again. */
  followUpSeconds: number;
}

export const DEFAULT_ORION: OrionSettings = {
  enabled: false, preset: "solar", wakeMode: "word", wakeWord: "orion", asrRoute: "", ttsRoute: "", operatorRoute: "", voice: "alloy", micId: "", reactTo: "off", language: "", followUpSeconds: 20,
};

const str = (v: unknown, d: string) => (typeof v === "string" ? v : d);

export function sanitizeOrion(parsed: unknown): OrionSettings {
  const p = (parsed && typeof parsed === "object" ? parsed : {}) as Record<string, unknown>;
  const preset = ORB_PRESETS.find((x) => x === p.preset) ?? DEFAULT_ORION.preset;
  const follow = typeof p.followUpSeconds === "number" && p.followUpSeconds >= 0 && p.followUpSeconds <= 120 ? p.followUpSeconds : DEFAULT_ORION.followUpSeconds;
  return {
    enabled: p.enabled === true, preset, wakeMode: p.wakeMode === "click" ? "click" : "word", wakeWord: str(p.wakeWord, DEFAULT_ORION.wakeWord).trim() || DEFAULT_ORION.wakeWord,
    asrRoute: str(p.asrRoute, ""), ttsRoute: str(p.ttsRoute, ""), operatorRoute: str(p.operatorRoute, ""),
    voice: str(p.voice, DEFAULT_ORION.voice), micId: str(p.micId, ""), reactTo: p.reactTo === "attention" || p.reactTo === "all" ? p.reactTo : "off", language: str(p.language, ""), followUpSeconds: follow,
  };
}

export const orionSettings = createPersistedStore<OrionSettings>("aeon-orion-v1", DEFAULT_ORION, sanitizeOrion);
export const useOrionSettings = () => usePersistedStore(orionSettings);

/** Everything Orion needs to run: the three routes. Names what is missing so the person is told, not left in silence. */
export function missingForOrion(s: OrionSettings): string[] {
  return [s.asrRoute === "" ? "a speech-to-text model" : "", s.ttsRoute === "" ? "a text-to-speech model" : "", s.operatorRoute === "" ? "an operator model" : ""].filter(Boolean);
}
