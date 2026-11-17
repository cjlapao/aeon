import { useEffect, useRef, useState } from "react";

/** One sub-question of a multi-question form, as the daemon encodes it in the question's options. */
export interface FormQuestion { text: string; options: string[] }

const PREFIX = "form:";
/** What a skipped question reads as, in the answer the agent gets and in the answered card. */
export const SKIPPED = "(skipped)";

/** The sub-questions a form question carries, or undefined when this is an ordinary question. */
export function parseForm(options: readonly string[] | undefined): FormQuestion[] | undefined {
  if (!options || options.length < 2 || !options.every((o) => o.startsWith(PREFIX))) return undefined;
  try {
    return options.map((o) => {
      const v = JSON.parse(o.slice(PREFIX.length)) as { text?: unknown; options?: unknown };
      return { text: String(v.text ?? ""), options: Array.isArray(v.options) ? v.options.map(String) : [] };
    });
  } catch { return undefined; }
}

/** The text the agent gets back: one numbered line per question, in the order asked. `null` is a skipped question. */
export function formAnswer(questions: readonly FormQuestion[], answers: readonly (string | null | undefined)[]): string {
  return questions.map((q, i) => `${i + 1}. ${q.text}\n   → ${answers[i] === null ? SKIPPED : (answers[i] ?? "").trim()}`).join("\n");
}

export interface FormAnswerRow { n: number; question: string; answer: string; skipped: boolean }

/** A sent form answer, read back into one row per question (undefined when the text is not a form answer). */
export function parseFormAnswer(answer: string | undefined): FormAnswerRow[] | undefined {
  if (!answer) return undefined;
  const rows: FormAnswerRow[] = [];
  const re = /^(\d+)\. ([\s\S]*?)\n {3}→ ([\s\S]*?)(?=\n\d+\. |$)/gm;
  let m: RegExpExecArray | null;
  while ((m = re.exec(answer)) !== null) rows.push({ n: Number(m[1]), question: m[2]!.trim(), answer: m[3]!.trim(), skipped: m[3]!.trim() === SKIPPED });
  return rows.length >= 2 ? rows : undefined;
}

/** The part of a form's body before its numbered list: what the wizard shows above the first question. */
export function formIntro(body: string): string {
  const i = body.search(/(^|\n\n?)1\. /);
  return (i <= 0 ? "" : body.slice(0, i)).trim();
}

const RECOMMENDED = /\s*[\[(]\s*recommended\s*[\])]\s*|\s+—\s+recommended\s*$/i;
/** An option as shown: a Recommended mark taken out of its text and drawn as a tag. */
function readOption(option: string): { label: string; recommended: boolean } {
  const recommended = RECOMMENDED.test(option);
  return { label: option.replace(RECOMMENDED, " ").trim(), recommended };
}

const BTN = "inline-flex items-center rounded-lg px-4 py-1.5 text-sm font-medium transition disabled:cursor-not-allowed disabled:opacity-50";

/**
 * The questions of one call, asked one at a time: pick an option or type your own to move to the next, skip one, go
 * back and change an earlier answer, and on the last page review everything and send it all in one go. Sending is
 * allowed when every question is answered or skipped.
 */
export function QuestionForm({ questions, disabled, onSubmit }: { questions: readonly FormQuestion[]; disabled?: boolean; onSubmit: (answer: string) => void }) {
  const total = questions.length;
  const [step, setStep] = useState(0); // 0..total-1 are the questions; `total` is the review page
  const [answers, setAnswers] = useState<(string | null | undefined)[]>(() => questions.map(() => undefined));
  const [draft, setDraft] = useState("");
  const typed = useRef<HTMLInputElement>(null);
  const review = step >= total;
  const q = review ? undefined : questions[step];
  const answered = (i: number) => answers[i] !== undefined;
  const complete = questions.every((_, i) => answered(i));

  // Opening a question shows what was already answered, so going back to change it starts from it.
  useEffect(() => {
    if (review || !q) return;
    const current = answers[step];
    setDraft(typeof current === "string" && !q.options.includes(current) ? current : "");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [step]);

  const set = (i: number, value: string | null) => setAnswers((a) => a.map((x, j) => (j === i ? value : x)));
  const go = (to: number) => setStep(Math.max(0, Math.min(total, to)));
  const choose = (value: string) => { set(step, value); go(step + 1); };
  const next = () => {
    if (draft.trim()) { set(step, draft.trim()); go(step + 1); return; }
    if (answered(step)) go(step + 1);
  };
  const skip = () => { set(step, null); go(step + 1); };

  const pager = <div className="flex items-center gap-2 text-sm text-slate-500" data-testid="question-pager">
    <button type="button" aria-label="Previous question" disabled={step === 0 || disabled} onClick={() => go(step - 1)} className="px-1 disabled:opacity-30">‹</button>
    <span className="font-mono">{review ? "review" : `${step + 1}/${total}`}</span>
    <button type="button" aria-label="Next question" disabled={review || !answered(step) || disabled} onClick={() => go(step + 1)} className="px-1 disabled:opacity-30">›</button>
  </div>;

  return <div data-testid="agent-chat-question-form" className="mt-2 rounded-xl border border-slate-200 bg-white p-4 dark:border-white/10! dark:bg-white/5!">
    {review ? <>
      <p className="text-base font-medium text-slate-900 dark:text-slate-50!">Review your answers</p>
      <ul className="mt-3 divide-y divide-slate-200 dark:divide-white/10!" data-testid="question-review">
        {questions.map((item, i) => <li key={i}>
          <button type="button" onClick={() => go(i)} disabled={disabled} title="Change this answer"
            className="flex w-full items-start gap-3 py-2 text-left hover:bg-slate-50 dark:hover:bg-white/5!">
            <span className="mt-0.5 inline-flex h-6 w-6 shrink-0 items-center justify-center rounded bg-slate-200 text-xs text-slate-700 dark:bg-white/10! dark:text-slate-200!">{i + 1}</span>
            <span className="min-w-0 flex-1">
              <span className="block text-sm text-slate-600 dark:text-slate-300!">{item.text}</span>
              <span className={`block text-sm font-medium ${answers[i] === null ? "italic text-slate-400" : answers[i] === undefined ? "text-amber-600" : "text-slate-900 dark:text-slate-50!"}`}>
                {answers[i] === null ? "Skipped" : answers[i] === undefined ? "Not answered yet" : answers[i]}
              </span>
            </span>
          </button>
        </li>)}
      </ul>
    </> : <>
      <p className="text-xs text-slate-500">Question {step + 1} of {total}</p>
      <p className="mt-1 text-base font-medium text-slate-900 dark:text-slate-50!" data-testid="question-title">{q!.text}</p>
      <div className="mt-3 space-y-1.5" role="group" aria-label="Options">
        {q!.options.map((option, i) => {
          const { label, recommended } = readOption(option);
          const on = answers[step] === option;
          return <button key={option} type="button" disabled={disabled} aria-pressed={on} onClick={() => choose(option)}
            className={`flex w-full items-center gap-3 rounded-lg border px-3 py-2 text-left text-sm transition ${on
              ? "border-blue-500 bg-blue-50 dark:border-blue-400/60! dark:bg-blue-400/10!" : "border-transparent hover:bg-slate-50 dark:hover:bg-white/5!"}`}>
            <span className="inline-flex h-6 w-6 shrink-0 items-center justify-center rounded bg-slate-200 text-xs text-slate-700 dark:bg-white/10! dark:text-slate-200!">{i + 1}</span>
            <span className="min-w-0 flex-1 text-slate-900 dark:text-slate-100!">{label}</span>
            {recommended && <span className="shrink-0 rounded bg-blue-100 px-1.5 py-0.5 text-[11px] font-medium text-blue-700 dark:bg-blue-400/15! dark:text-blue-300!">Recommended</span>}
          </button>;
        })}
        <div className="flex items-center gap-3 rounded-lg px-3 py-2">
          <span className="inline-flex h-6 w-6 shrink-0 items-center justify-center rounded bg-slate-200 text-xs text-slate-700 dark:bg-white/10! dark:text-slate-200!">✎</span>
          <input ref={typed} value={draft} onChange={(e) => setDraft(e.target.value)} disabled={disabled} aria-label={`answer ${step + 1}`}
            onKeyDown={(e) => { if (e.key === "Enter" && !e.nativeEvent.isComposing) { e.preventDefault(); next(); } }}
            placeholder="Type your answer" className="min-w-0 flex-1 bg-transparent text-sm outline-none placeholder:text-slate-400" />
        </div>
      </div>
    </>}
    <div className="mt-4 flex items-center justify-between gap-3">
      {pager}
      <div className="flex gap-2">
        {review
          ? <button type="button" data-testid="question-send" disabled={disabled || !complete} onClick={() => onSubmit(formAnswer(questions, answers))}
            className={`${BTN} bg-blue-600 text-white hover:bg-blue-700`}>Send answers</button>
          : <>
            <button type="button" data-testid="question-skip" disabled={disabled} onClick={skip}
              className={`${BTN} border border-slate-300 bg-white text-slate-700 hover:bg-slate-50 dark:border-white/20! dark:bg-transparent! dark:text-slate-200!`}>Skip</button>
            <button type="button" data-testid="question-next" disabled={disabled || (!draft.trim() && !answered(step))} onClick={next}
              className={`${BTN} bg-slate-900 text-white hover:bg-slate-700 dark:bg-white! dark:text-slate-900!`}>{step === total - 1 ? "Review" : "Next"}</button>
          </>}
      </div>
    </div>
  </div>;
}
