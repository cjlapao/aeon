import { useState } from "react";
import { Alert, Button, Pill } from "../../components/kit";
import { rpc, describeError } from "../../lib/bridge";
import { base64ToUrl, blobToBase64, recordClip } from "../orion/audio";
import { toWav } from "../orion/wav";
import { MicPicker } from "../orion/MicPicker";
import { orionSettings } from "../orion/orionSettings";

const SENTENCE = "Hello, this is Aeon. If you can hear this sentence, the speech model works.";
const EMBED_TEXT = "Aeon runs agents on your code.";
const RECORD_MS = 5000;
/** Below this loudness a recording counts as silence. */
const SILENT = 0.01;

type Outcome = { ok: boolean; text: string };

/** One check: a title, what it does, a button that is off unless the model declares the capability, and the result. */
function Check({ id, title, what, needs, declared, canRun, label, busy, outcome, onRun }: {
  id: string; title: string; what: string; needs: string; declared: boolean; canRun: boolean; label: string; busy: string | undefined; outcome?: Outcome; onRun: () => void;
}) {
  return <div className="flex flex-col gap-2 rounded-xl border border-current/10 p-3" data-testid={`voice-check-${id}`}>
    <div className="flex items-center justify-between gap-2"><span className="text-sm font-medium">{title}</span>{outcome && <Pill size="sm" tone={outcome.ok ? "green" : "red"}>{outcome.ok ? "works" : "failed"}</Pill>}</div>
    <p className="text-xs opacity-70">{what}</p>
    <div><Button size="sm" onClick={onRun} disabled={!canRun || !declared || busy !== undefined} loading={busy === id} data-testid={`voice-run-${id}`}>{label}</Button></div>
    {!declared && <p className="text-xs opacity-60" data-testid={`voice-needs-${id}`}>Off: this model does not declare {needs}. Tick it in Capabilities first.</p>}
    {outcome && <p className={`break-words text-xs ${outcome.ok ? "" : "text-red-700 dark:text-red-300"}`} data-testid={`voice-result-${id}`}>{outcome.text}</p>}
  </div>;
}

/** What the checks call: a saved route by id, or the unsaved form values as a draft the daemon uses for one call. */
export type VoiceTarget = { route: string } | { draft: Record<string, unknown> };

/**
 * Speech and embedding checks for a model, saved or not yet. Each is off unless the model declares its capability, sends one small
 * real request, and says what came back: the sentence is spoken aloud, the microphone is recorded for a few seconds and
 * the words read back, an embedding is reported as the size of the vector.
 */
export function ModelVoiceTests({ target, capabilities, canRun }: { target: VoiceTarget; capabilities: readonly string[]; canRun: boolean }) {
  const caps = capabilities;
  const [busy, setBusy] = useState<string | undefined>();
  // The voice the speech model is asked for. Servers name voices differently (alloy, af_heart, aai-kyla) and some answer an
  // unknown one with a 500, so it is set here for the check, starting from the one Orion uses.
  const [voice, setVoice] = useState(orionSettings.getSnapshot().voice);
  const [out, setOut] = useState<Record<string, Outcome>>({});
  // The last recording, kept so it can be played back: the way to tell a silent capture from a model that did not understand.
  const [clip, setClip] = useState<{ url: string; bytes: number; type: string; ms: number; peak: number } | undefined>();
  const set = (id: string, o: Outcome) => setOut((cur) => ({ ...cur, [id]: o }));
  const run = async (id: string, work: () => Promise<string>) => {
    setBusy(id);
    try { set(id, { ok: true, text: await work() }); } catch (cause) { set(id, { ok: false, text: describeError(cause) }); } finally { setBusy(undefined); }
  };

  const speak = () => run("tts", async () => {
    const started = performance.now();
    const res = await rpc<{ audio: string; mime: string }>("voice.speak", { ...target, text: SENTENCE, voice: voice.trim() || undefined });
    const ms = Math.round(performance.now() - started);
    const url = base64ToUrl(res.audio, res.mime);
    try { await new Audio(url).play(); } catch { return `Audio came back in ${ms} ms (${res.mime}), but this device would not play it.`; }
    return `Spoke the sentence; the audio came back in ${ms} ms (${res.mime}). Voice used: ${voice.trim() || "the default"}.`;
  });

  const hear = () => run("asr", async () => {
    setOut((cur) => ({ ...cur, asr: { ok: true, text: `Recording for ${RECORD_MS / 1000} seconds… say something.` } }));
    const taken = await recordClip(RECORD_MS, undefined, orionSettings.getSnapshot().micId);
    // What is sent is what is shown and played: the recording as uncompressed WAV, which every speech server accepts.
    const sent = await toWav(taken.blob);
    setClip((old) => { if (old) URL.revokeObjectURL(old.url); return { url: URL.createObjectURL(sent), bytes: sent.size, type: sent.type, ms: taken.ms, peak: taken.peak }; });
    const started = performance.now();
    const res = await rpc<{ text: string }>("voice.transcribe", { ...target, audio: await blobToBase64(sent), mime: sent.type });
    if (res.text) return `Heard: “${res.text}” (${Math.round(performance.now() - started)} ms)`;
    return taken.peak < SILENT ? "The model answered, but heard no words, and the recording itself is silent: the microphone captured nothing. Pick another microphone above." : "The model answered, but heard no words, although the recording has sound. Play it back below: if you hear yourself, the problem is the model or its server, not the microphone. The audio was sent as WAV.";
  });

  const embed = () => run("embedding", async () => {
    const started = performance.now();
    const res = await rpc<{ dimensions: number; inputTokens?: number }>("voice.embed", { ...target, text: EMBED_TEXT });
    return `One sentence became ${res.dimensions} numbers in ${Math.round(performance.now() - started)} ms${res.inputTokens ? ` (${res.inputTokens} tokens)` : ""}.`;
  });

  return <div className="flex flex-col gap-3" data-testid="model-test-voice">
    <p className="text-sm opacity-70">Each check sends one small real request to the provider. A check is off until the model declares its capability.</p>
    {!canRun && <Alert intent="warning" title="Reconnecting — tests are off until the daemon is current." />}
    <label className="flex flex-col gap-1 text-xs"><span className="font-medium">Voice for the speech check</span>
      <input className="rounded-lg border border-slate-300 bg-transparent px-3 py-1.5 text-sm dark:border-white/20!" aria-label="Voice for the speech check" value={voice} onChange={(e) => setVoice(e.target.value)} placeholder="the voice name your server expects, e.g. af_heart" data-testid="voice-name" />
      <span className="opacity-60">Save the same name in Settings → Orion to have him use it.</span></label>
    <Check id="tts" title="Text to speech" what={`Says: “${SENTENCE}”`} needs="Text to speech" declared={caps.includes("tts")} canRun={canRun} label="Say it" busy={busy} outcome={out.tts} onRun={() => void speak()} />
    {caps.includes("asr") && <MicPicker />}
    <Check id="asr" title="Speech to text" what={`Records ${RECORD_MS / 1000} seconds from the microphone chosen above and shows the words it heard.`} needs="Speech to text" declared={caps.includes("asr")} canRun={canRun} label="Record and transcribe" busy={busy} outcome={out.asr} onRun={() => void hear()} />
    {clip && caps.includes("asr") && <div className="flex flex-col gap-2 rounded-xl border border-current/10 p-3" data-testid="voice-clip">
      <span className="text-sm font-medium">Last recording</span>
      <audio controls src={clip.url} className="w-full" data-testid="voice-clip-audio" />
      <p className="text-xs opacity-70" data-testid="voice-clip-facts">{(clip.bytes / 1024).toFixed(1)} KB · {clip.type || "unknown type"} · {(clip.ms / 1000).toFixed(1)} s · loudest moment {Math.round(Math.min(1, clip.peak * 4) * 100)}%</p>
      {clip.peak < SILENT && <p className="text-xs text-amber-700 dark:text-amber-300" data-testid="voice-clip-silent">This recording is silent. The microphone is muted, wrong, or blocked.</p>}
    </div>}
    <Check id="embedding" title="Embedding" what={`Embeds “${EMBED_TEXT}” and shows the size of the vector.`} needs="Embedding" declared={caps.includes("embedding")} canRun={canRun} label="Embed a sentence" busy={busy} outcome={out.embedding} onRun={() => void embed()} />
  </div>;
}
