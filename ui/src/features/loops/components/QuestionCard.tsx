import { useState } from "react";
import { Button, Panel } from "../../../components/kit";

/** The same decision surface for a model step question and a blueprint gate. */
export function QuestionCard({ prompt, options, busy, onAnswer }: {
  prompt: string; options: readonly string[]; busy: boolean; onAnswer: (answer: string) => void;
}) {
  const [freeText, setFreeText] = useState("");
  return <Panel tone="amber" data-testid="question-card">
    <p className="text-xs uppercase tracking-[.2em] text-amber-800">Your decision</p>
    <p className="mt-2 text-base font-medium">{prompt}</p>
    <div className="mt-4 flex flex-wrap gap-2">
      {options.map((option) => <Button key={option} size="sm" variant="outline" disabled={busy}
        onClick={() => onAnswer(option)}>{option}</Button>)}
      {options.length === 0 && <form onSubmit={(event) => { event.preventDefault(); if (freeText.trim()) onAnswer(freeText.trim()); }}>
        <label className="block text-sm">Answer <input className="ml-2 rounded-lg border border-slate-300 bg-white p-2 text-slate-900" value={freeText}
          onChange={(event) => setFreeText(event.target.value)} disabled={busy} /></label>
        <Button type="submit" size="sm" disabled={busy || !freeText.trim()}>Send answer</Button>
      </form>}
    </div>
  </Panel>;
}
