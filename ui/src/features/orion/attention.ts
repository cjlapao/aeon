import { findLoop } from "./loopfind";
import type { OrionTool, Rpc, ToolContext } from "./tools";

/**
 * What is waiting on a person in a loop: a question, a permission to run a call, a plan gate. All are one thing to the
 * daemon (a pending question answered through `loop.answer`), and all are read here from the daemon's own records.
 * Nothing Orion says is trusted to describe or to answer one: the question is read from the loop, the answer is checked
 * against what the loop actually offers and against what the PERSON said, and the sentence he asks the person to confirm
 * is written by this code from the verified question, not by the model.
 */
export interface PendingAsk {
  loopId: string; slug: string; title: string; questionId: number; kind: string; body: string; options: string[];
}
interface LoopRow { id: string; slug: string; title?: string; status: string; componentRoot: string }
type Show = { status?: string; questionId?: number; gatePrompt?: string; gateAnswers?: string[]; lastReason?: string };
type Ev = { kind: string; seq: number; payload: Record<string, unknown> };

export type Read = { ask: PendingAsk } | { blocked: string } | { nothing: true };

/** Reads one loop's pending ask fresh from the daemon. */
export async function readPending(rpc: Rpc, loop: LoopRow): Promise<Read> {
  const show = (await rpc("loop.show", { id: loop.id, includeBlueprint: false })) as Show;
  if (show.questionId === undefined) return show.status === "blocked" || loop.status === "blocked" ? { blocked: show.lastReason || "no reason was recorded" } : { nothing: true };
  const evs = ((await rpc("loop.events", { id: loop.id, limit: 300 })) as { events?: Ev[] }).events ?? [];
  const asked = evs.filter((e) => e.kind === "question.asked" && e.payload.id === show.questionId).at(-1);
  const answered = evs.some((e) => e.kind === "question.answered" && e.payload.questionId === show.questionId);
  if (answered) return { nothing: true };
  const p = (asked?.payload ?? {}) as { kind?: string; body?: string; options?: unknown };
  const options = Array.isArray(p.options) ? p.options.filter((o): o is string => typeof o === "string") : show.gateAnswers ?? [];
  return { ask: { loopId: loop.id, slug: loop.slug, title: loop.title || loop.slug, questionId: show.questionId, kind: p.kind ?? "question", body: (p.body ?? show.gatePrompt ?? "").trim(), options } };
}

const norm = (s: string) => s.toLowerCase().normalize("NFD").replace(/[̀-ͯ]/g, "").replace(/[^a-z0-9' ]+/g, " ").replace(/\s+/g, " ").trim();
const words = (s: string) => norm(s).split(" ").filter(Boolean);
const POSITIVE = /\b(yes|yeah|yep|approve|approved|allow|allowed|go ahead|do it|okay|ok|sure|accept|proceed|continue|confirm|please do)\b/;
const NEGATIVE = /\b(no|nope|deny|denied|reject|rejected|don't|do not|never|refuse|block|cancel|decline)\b/;
const positiveOption = (o: string) => /^(approve|accept|allow|continue|yes|proceed)/i.test(o);
const negativeOption = (o: string) => /^(deny|reject|no\b|refuse|decline|cancel)/i.test(o);
const STOP = new Set(["the", "a", "an", "to", "of", "and", "is", "it", "that", "this", "for", "in", "on", "please", "should", "be", "with", "as"]);

export type Verified = { ok: true; ask: PendingAsk; answer: string; sentence: string } | { ok: false; problem: string };

/** What a spoken confirmation says, built from the verified question. */
function sentenceFor(a: PendingAsk, answer: string): string {
  const body = a.body.replace(/\s+/g, " ").slice(0, 260);
  return a.kind === "permission" ? `In ${a.title}: ${body} Answer "${answer}"?` : `In ${a.title}, about "${body.slice(0, 160)}": answer "${answer.slice(0, 200)}"?`;
}

/**
 * The checks, as pure code. `said` is what the person said (never the model's words, never a notification): the answer must
 * be one the loop offers, must agree with what the person said, and a standing rule ("always allow") is never given by voice.
 */
export function verifyAnswer(ask: PendingAsk, input: { questionId?: number; choice?: string; text?: string }, said: readonly string[]): Verified {
  if (input.questionId !== undefined && input.questionId !== ask.questionId) return { ok: false, problem: `that question (${input.questionId}) is not the one waiting in ${ask.title}; the one waiting is ${ask.questionId}. Read it again` };
  const spoken = said.map(norm).filter(Boolean).join(" . ");
  if (spoken === "") return { ok: false, problem: "the person has not said an answer yet; ask them" };
  if (ask.options.length > 0) {
    const want = norm(input.choice ?? "");
    if (want === "") return { ok: false, problem: `this needs one of its options: ${ask.options.join(", ")}` };
    const exact = ask.options.filter((o) => norm(o) === want);
    const partial = exact.length ? exact : ask.options.filter((o) => norm(o).startsWith(want));
    if (partial.length !== 1) return { ok: false, problem: `"${input.choice}" is not exactly one of the options: ${ask.options.join(", ")}` };
    const option = partial[0]!;
    if (/^always\b/i.test(option)) return { ok: false, problem: `"${option}" sets a standing rule; that is done in the app by the person, not by voice. Offer the one-time options instead` };
    const yes = POSITIVE.test(spoken); const no = NEGATIVE.test(spoken);
    if (positiveOption(option)) { if (!yes || no) return { ok: false, problem: `the person did not clearly say yes to "${option}"; ask them` }; }
    else if (negativeOption(option)) { if (!no || yes) return { ok: false, problem: `the person did not clearly say no to "${option}"; ask them` }; }
    else if (!words(option).filter((w) => !STOP.has(w)).every((w) => spoken.includes(w))) return { ok: false, problem: `the person did not say "${option}"; ask them` };
    if (/\bfor this loop\b/i.test(option) && !/\bloop\b/.test(spoken)) return { ok: false, problem: `"${option}" lasts for the whole loop; the person must say that they mean the loop` };
    return { ok: true, ask, answer: option, sentence: sentenceFor(ask, option) };
  }
  const text = (input.text ?? "").trim();
  if (text === "" || text.length > 2000) return { ok: false, problem: "a free answer is needed, up to 2000 characters" };
  // The words of the answer must be the person's: an answer is not something Orion composes.
  const mine = words(text).filter((w) => !STOP.has(w));
  const hits = mine.filter((w) => spoken.includes(w)).length;
  if (mine.length === 0 || hits / mine.length < 0.7) return { ok: false, problem: "that answer contains words the person did not say; use only what they said" };
  return { ok: true, ask, answer: text, sentence: sentenceFor(ask, text) };
}

async function loopsRows(rpc: Rpc): Promise<LoopRow[]> { return ((await rpc("loop.list", { all: true })) as { loops?: LoopRow[] }).loops ?? []; }
const s = (v: unknown) => (typeof v === "string" ? v.trim() : "");

async function verifyFor(input: Record<string, unknown>, rpc: Rpc, ctx?: ToolContext): Promise<Verified> {
  const { loop, problem } = findLoop((await loopsRows(rpc)).filter((l) => l.status === "waiting" || l.status === "blocked" || l.status === "running"), s(input.loop));
  if (!loop) return { ok: false, problem: problem ?? "no such loop" };
  const read = await readPending(rpc, loop);
  if (!("ask" in read)) return { ok: false, problem: "nothing in that loop is waiting for an answer" };
  return verifyAnswer(read.ask, { questionId: typeof input.questionId === "number" ? input.questionId : undefined, choice: s(input.choice), text: s(input.text) }, ctx?.said ?? []);
}

export const ATTENTION_TOOLS: OrionTool[] = [
  {
    name: "pending_asks", kind: "read",
    description: "What is waiting on the person right now: loops asking a question, asking permission to run something, waiting at a plan gate, or blocked. Read this before describing or answering anything; the text comes from the loop itself.",
    parameters: { type: "object", properties: { loop: { type: "string", description: "One loop (id, slug or words of its title). Empty means every loop that is waiting or blocked." } } },
    run: async (input, rpc) => {
      const all = await loopsRows(rpc);
      const want = s(input.loop);
      const rows = want ? (() => { const f = findLoop(all, want); return f.loop ? [f.loop] : []; })() : all.filter((l) => l.status === "waiting" || l.status === "blocked");
      if (rows.length === 0) return want ? `no loop matches "${want}"` : "nothing is waiting on you";
      const lines: string[] = [];
      for (const l of rows.slice(0, 6)) {
        const r = await readPending(rpc, l);
        if ("ask" in r) lines.push(`${r.ask.title} (${r.ask.kind}, question ${r.ask.questionId}): ${r.ask.body.replace(/\s+/g, " ").slice(0, 500)}${r.ask.options.length ? ` Options: ${r.ask.options.join(" | ")}` : " (a free-text answer)"}`);
        else if ("blocked" in r) lines.push(`${l.title || l.slug} is blocked: ${r.blocked.slice(0, 300)}. Only the person can unblock it in the app; offer to open it`);
      }
      return lines.length ? lines.join("\n") : "nothing is waiting on you";
    },
  },
  {
    name: "answer_ask", kind: "control",
    description: "Answer what a loop is waiting for: pick one of its offered options (choice), or give the person's own words (text) for a free question. Use only what the person actually said. The answer is checked in code against the loop's own question and the person's words, and the person is asked to confirm out loud.",
    parameters: { type: "object", properties: {
      loop: { type: "string", description: "The loop (id, slug or words of its title)." },
      questionId: { type: "number", description: "The question number from pending_asks." },
      choice: { type: "string", description: "Exactly one of the offered options." },
      text: { type: "string", description: "For a question with no options: the person's answer, in their words." },
    }, required: ["loop"] },
    verify: async (input, rpc, _ui, ctx) => {
      const v = await verifyFor(input, rpc, ctx);
      return v.ok ? { ok: true, sentence: v.sentence, input: { ...input, questionId: v.ask.questionId, choice: v.ask.options.length ? v.answer : undefined, text: v.ask.options.length ? undefined : v.answer } } : v;
    },
    run: async (input, rpc, _ui, ctx) => {
      // Checked again now, against the question as it is NOW: it may have been answered or replaced while the person was confirming.
      const v = await verifyFor(input, rpc, ctx);
      if (!v.ok) return `not sent: ${v.problem}`;
      if (v.ask.questionId !== input.questionId) return "not sent: the question changed while confirming; read it again";
      const res = (await rpc("loop.answer", { id: v.ask.loopId, answer: v.answer })) as { status?: string; questionId?: number };
      return `answered question ${res.questionId ?? v.ask.questionId} in ${v.ask.title}: "${v.answer.slice(0, 120)}"${res.status ? ` (${res.status})` : ""}`;
    },
  },
];
