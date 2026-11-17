import { AiOrb } from "@cjlapao/ui-kit";
import { Link } from "react-router-dom";
import { useOrion } from "./useOrion";
import { useOrionSettings } from "./orionSettings";

const LABEL = { off: "Orion is off", idle: "Orion is listening for his name", awake: "Orion is listening to you", thinking: "Orion is thinking", speaking: "Orion is speaking", error: "Orion hit a problem" } as const;

/**
 * Orion, floating at the bottom right. Click the orb to wake him (or to cut him off while he talks, or to put him back to
 * sleep). What he heard and what he said show beside it; what is missing, or what went wrong, shows too and never goes
 * silent. Nothing renders unless he is enabled in Settings.
 *
 * Woken by his name, the orb is hidden while he waits and appears only once the name is heard, so its presence means he
 * is listening to you now. Woken by a click, it stays on screen because it is the button.
 */
export function OrionOrb({ connected }: { connected: boolean }) {
  const settings = useOrionSettings();
  const { snapshot, audioSource, toggle, blocked } = useOrion(connected);
  if (!settings.enabled) return null;
  const { phase } = snapshot;
  const orbState = phase === "speaking" ? "speaking" : phase === "awake" ? "listening" : "idle";
  // Asleep and waiting for his name: no orb. A message that needs reading (blocked audio) still shows, as a bubble alone.
  const hidden = settings.wakeMode === "word" && phase === "idle";
  const bubble = snapshot.missing.length > 0
    ? <span>Orion needs {snapshot.missing.join(", ")}. <Link className="underline" to="/settings">Choose them in Settings</Link></span>
    : blocked && phase !== "off" && settings.wakeMode === "word" ? <span data-testid="orion-blocked">Click anywhere once so the app lets Orion listen.</span>
    : snapshot.error ? <span role="alert" className="text-red-700 dark:text-red-300">{snapshot.error}</span>
    : phase === "thinking" ? <span className="italic opacity-70">thinking…</span>
    : snapshot.said && (phase === "speaking" || phase === "awake") ? <span>{snapshot.said}</span>
    : snapshot.heard && phase === "awake" ? <span className="opacity-70">“{snapshot.heard}”</span>
    : null;
  if (hidden && !bubble) return null;
  return (
    <div data-testid="orion" data-phase={phase} className="pointer-events-none fixed bottom-5 right-5 z-50 flex flex-col items-end gap-2">
      {bubble && <div data-testid="orion-bubble" className="pointer-events-auto max-w-xs rounded-2xl border border-slate-200 bg-white/90 px-3 py-2 text-sm shadow-lg backdrop-blur dark:border-white/10! dark:bg-slate-900/90!">{bubble}</div>}
      {!hidden && <button type="button" onClick={toggle} aria-label={LABEL[phase]} title={LABEL[phase]} data-testid="orion-orb"
        className={`pointer-events-auto rounded-full transition-opacity ${snapshot.missing.length > 0 ? "opacity-50" : "opacity-100"}`}>
        <AiOrb state={orbState} preset={settings.preset} audioSource={audioSource} size={76} quality="balanced" label={LABEL[phase]} />
      </button>}
    </div>
  );
}
