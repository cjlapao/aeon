import { useCallback, useEffect, useRef, useState } from "react";
import { Button, Select } from "../../components/kit";
import { openMic } from "./audio";
import { orionSettings, useOrionSettings } from "./orionSettings";

interface Mic { id: string; label: string }

/** The inputs this device has. Names stay blank until the page has been allowed the microphone once, so ask once to read them. */
export async function listMics(): Promise<Mic[]> {
  const read = async () => (await navigator.mediaDevices.enumerateDevices()).filter((d) => d.kind === "audioinput");
  let found = await read();
  if (found.length > 0 && found.every((d) => d.label === "")) {
    try { (await navigator.mediaDevices.getUserMedia({ audio: true })).getTracks().forEach((t) => t.stop()); found = await read(); } catch { /* refused: keep the unnamed list */ }
  }
  return found.map((d, i) => ({ id: d.deviceId, label: d.label || `Microphone ${i + 1}` }));
}

/** Loudness (0–1) of what the chosen microphone hears, drawn as a bar while it is on, so a dead or wrong input is seen. */
function Meter({ deviceId }: { deviceId: string }) {
  const [on, setOn] = useState(false);
  const [level, setLevel] = useState(0);
  const [error, setError] = useState("");
  const stop = useRef<() => void>(() => undefined);

  const off = useCallback(() => { stop.current(); stop.current = () => undefined; setOn(false); setLevel(0); }, []);
  useEffect(() => off, [off]);
  useEffect(() => { if (on) { off(); void start(); } /* a different microphone restarts the meter */ // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [deviceId]);

  async function start() {
    setError("");
    try {
      const stream = await openMic(deviceId);
      const ctx = new AudioContext();
      const analyser = ctx.createAnalyser();
      analyser.fftSize = 1024;
      ctx.createMediaStreamSource(stream).connect(analyser);
      const frame = new Float32Array(analyser.fftSize);
      let raf = 0;
      const tick = () => {
        analyser.getFloatTimeDomainData(frame);
        let sum = 0;
        for (let i = 0; i < frame.length; i += 1) sum += frame[i]! * frame[i]!;
        setLevel(Math.min(1, Math.sqrt(sum / frame.length) * 8));
        raf = requestAnimationFrame(tick);
      };
      tick();
      stop.current = () => { cancelAnimationFrame(raf); stream.getTracks().forEach((t) => t.stop()); void ctx.close(); };
      setOn(true);
    } catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)); }
  }

  return <div className="flex items-center gap-3" data-testid="mic-meter">
    <Button size="sm" variant="ghost" onClick={() => (on ? off() : void start())} data-testid="mic-meter-toggle">{on ? "Stop listening" : "Check this microphone"}</Button>
    <div className="h-2 w-40 overflow-hidden rounded-full bg-slate-200 dark:bg-white/10!" role="meter" aria-label="Microphone level" aria-valuemin={0} aria-valuemax={1} aria-valuenow={level}>
      <div className="h-full rounded-full bg-emerald-500" style={{ width: `${Math.round(level * 100)}%` }} data-testid="mic-level" />
    </div>
    {error && <span className="text-xs text-red-700 dark:text-red-300" role="alert">{error}</span>}
  </div>;
}

/** Which microphone Orion and the speech-to-text check listen with, and a live level to tell a working one from a silent one. */
export function MicPicker() {
  const s = useOrionSettings();
  const [mics, setMics] = useState<Mic[] | undefined>();
  const [error, setError] = useState("");
  const refresh = useCallback(() => { listMics().then((m) => { setMics(m); setError(""); }, (cause) => setError(cause instanceof Error ? cause.message : String(cause))); }, []);
  useEffect(() => {
    refresh();
    navigator.mediaDevices?.addEventListener?.("devicechange", refresh);
    return () => navigator.mediaDevices?.removeEventListener?.("devicechange", refresh);
  }, [refresh]);
  const missing = s.micId !== "" && mics !== undefined && !mics.some((m) => m.id === s.micId);
  return <div className="space-y-2" data-testid="mic-picker">
    <label className="block text-sm"><span className="font-medium">Microphone</span>
      <Select variant="ghost" size="md" aria-label="Microphone" value={s.micId} onChange={(e) => orionSettings.set((cur) => ({ ...cur, micId: e.target.value }))}>
        <option value="">System default</option>
        {missing && <option value={s.micId}>The saved microphone (not connected)</option>}
        {(mics ?? []).map((m) => <option key={m.id} value={m.id}>{m.label}</option>)}
      </Select>
      <span className="block text-xs text-slate-500">{error ? `The microphones could not be listed: ${error}` : mics?.length === 0 ? "No microphone was found on this device." : "If speech is not heard, pick the one your voice moves the bar for."}</span></label>
    <Meter deviceId={s.micId} />
  </div>;
}
