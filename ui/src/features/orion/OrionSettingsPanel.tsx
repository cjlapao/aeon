import { useEffect, useState } from "react";
import { AiOrb } from "@cjlapao/ui-kit";
import { Panel, Select, type TrueColor } from "../../components/kit";
import { rpc } from "../../lib/bridge";
import { MicPicker } from "./MicPicker";
import { orionSettings, ORB_PRESETS, useOrionSettings, type OrbPreset } from "./orionSettings";

interface RouteRow { id: string; displayName?: string; provider?: string; model?: string; capabilities?: string[] | null }
const nameOf = (r: RouteRow) => r.displayName || `${r.provider ?? ""} · ${r.model ?? r.id}`;

const PRESET_LABEL: Record<OrbPreset, string> = { solar: "Solar", ion: "Ion", ember: "Ember" };

function RouteSelect({ label, hint, value, rows, onChange }: { label: string; hint: string; value: string; rows: RouteRow[]; onChange: (id: string) => void }) {
  return <label className="block text-sm">
    <span className="font-medium">{label}</span>
    <Select variant="ghost" size="md" aria-label={label} value={value} onChange={(e) => onChange(e.target.value)}>
      <option value="">{rows.length === 0 ? "No model has this capability yet" : "Choose a model…"}</option>
      {rows.map((r) => <option key={r.id} value={r.id}>{nameOf(r)}</option>)}
    </Select>
    <span className="block text-xs text-slate-500">{hint}</span>
  </label>;
}

/**
 * Orion's settings: turn him on, pick how the orb looks, how he is woken, and the three models that make him work: the
 * one that hears, the one that speaks, and the one that decides and operates. The models are routes from Models, so a
 * model appears under a heading only when it says it can do that.
 */
export function OrionSettingsPanel({ tone }: { tone: TrueColor }) {
  const s = useOrionSettings();
  const [routes, setRoutes] = useState<RouteRow[]>([]);
  useEffect(() => {
    let on = true;
    void rpc<{ routes?: RouteRow[] }>("routes.list", {}).then((r) => { if (on) setRoutes(r.routes ?? []); }, () => undefined);
    return () => { on = false; };
  }, []);
  const has = (cap: string) => routes.filter((r) => (r.capabilities ?? []).includes(cap));
  const chat = routes.filter((r) => (r.capabilities ?? []).includes("tool-calling") || (r.capabilities ?? []).includes("text"));
  const set = (patch: Partial<typeof s>) => orionSettings.set((cur) => ({ ...cur, ...patch }));
  return <Panel title="Orion" subtitle="A voice assistant that can look at and operate Aeon" tone={tone} variant="liquid-glass">
    <div className="space-y-4" data-testid="orion-settings">
      <label className="flex items-center gap-3 text-sm">
        <input type="checkbox" checked={s.enabled} onChange={(e) => set({ enabled: e.target.checked })} data-testid="orion-enabled" />
        <span><strong>Enable Orion.</strong> He appears at the bottom right and uses this device's microphone while enabled.</span>
      </label>
      <div role="group" aria-label="Orb look" className="flex flex-wrap items-center gap-4">
        {ORB_PRESETS.map((p) => <button key={p} type="button" aria-pressed={s.preset === p} onClick={() => set({ preset: p })} data-testid={`orion-preset-${p}`}
          className={`flex flex-col items-center gap-1 rounded-xl border p-2 text-xs ${s.preset === p ? "aeon-accent-outline aeon-accent-wash" : "border-slate-200 dark:border-white/10!"}`}>
          <AiOrb preset={p} state="idle" size={56} quality="low" label={`${PRESET_LABEL[p]} orb`} />{PRESET_LABEL[p]}
        </button>)}
      </div>
      <div className="grid gap-4 md:grid-cols-2">
        <RouteSelect label="Hears with (speech to text)" hint="A model with the Speech to text capability." value={s.asrRoute} rows={has("asr")} onChange={(asrRoute) => set({ asrRoute })} />
        <RouteSelect label="Speaks with (text to speech)" hint="A model with the Text to speech capability." value={s.ttsRoute} rows={has("tts")} onChange={(ttsRoute) => set({ ttsRoute })} />
        <RouteSelect label="Operates with (the model that decides)" hint="A chat model that can call tools; it reads your words and does the work." value={s.operatorRoute} rows={chat} onChange={(operatorRoute) => set({ operatorRoute })} />
        <label className="block text-sm"><span className="font-medium">Voice</span>
          <input className="mt-1 w-full rounded-lg border border-slate-300 bg-transparent px-3 py-1.5 text-sm dark:border-white/20!" aria-label="Voice" placeholder="af_heart" value={s.voice} onChange={(e) => set({ voice: e.target.value })} />
          <span className="block text-xs text-slate-500">The voice name your speech model expects: alloy for OpenAI, af_heart or af_bella for Kokoro. A wrong name makes the model refuse.</span></label>
      </div>
      <MicPicker />
      <div className="grid gap-4 md:grid-cols-2">
        <label className="block text-sm"><span className="font-medium">Wake</span>
          <Select variant="ghost" size="md" aria-label="Wake" value={s.wakeMode} onChange={(e) => set({ wakeMode: e.target.value as "word" | "click" })}>
            <option value="word">By saying his name</option>
            <option value="click">Only by clicking the orb</option>
          </Select>
          <span className="block text-xs text-slate-500">{s.wakeMode === "word" ? "The microphone stays open and each phrase you say is checked by the speech-to-text model, which costs a little per phrase." : "The microphone is closed until you click the orb."}</span></label>
        {s.wakeMode === "word" && <label className="block text-sm"><span className="font-medium">Wake word</span>
          <input className="mt-1 w-full rounded-lg border border-slate-300 bg-transparent px-3 py-1.5 text-sm dark:border-white/20!" aria-label="Wake word" value={s.wakeWord} onChange={(e) => set({ wakeWord: e.target.value })} />
          <span className="block text-xs text-slate-500">A name that is not a common word recognises better.</span></label>}
      </div>
      <label className="block text-sm"><span className="font-medium">Reacting to notifications</span>
        <Select variant="ghost" size="md" aria-label="Reacting to notifications" value={s.reactTo} onChange={(e) => set({ reactTo: e.target.value as "off" | "attention" | "all" })} data-testid="orion-react">
          <option value="off">Never</option>
          <option value="attention">Only what needs me (warnings and failures)</option>
          <option value="all">Everything the app notifies</option>
        </Select>
        <span className="block text-xs text-slate-500">When a loop asks for something or fails, Orion appears and tells you, and you can answer by voice. What the notification says is passed to him as text, not as an order.</span></label>
      <p className="text-xs text-slate-500">Orion asks you to say yes before he starts a loop, steers an agent or stops a loop. He cannot delete, approve, answer a question or change policy.</p>
    </div>
  </Panel>;
}
