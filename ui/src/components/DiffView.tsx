import { Highlight } from "prism-react-renderer";
import { useMemo } from "react";
import { CODE_THEME } from "./CodeBlock";
import { languageOf, parseUnified, toSplit, type DiffRow } from "./diffModel";

const ADD = "bg-emerald-500/10";
const DEL = "bg-red-500/10";
const NUM = "w-10 shrink-0 select-none pr-2 text-right text-slate-400";

function Code({ text, language }: { text: string; language: string }) {
  return <Highlight theme={CODE_THEME} code={text || " "} language={language}>
    {({ tokens, getTokenProps }) => <span className="whitespace-pre">{tokens[0]?.map((token, i) => <span key={i} {...getTokenProps({ token })} />)}</span>}
  </Highlight>;
}

function Unified({ rows, language }: { rows: readonly DiffRow[]; language: string }) {
  return <div className="min-w-max font-mono text-xs leading-5">
    {rows.map((row, i) => row.kind === "hunk"
      ? <div key={i} className="bg-blue-500/10 px-3 py-0.5 text-blue-700 dark:text-blue-300!">{row.text}</div>
      : <div key={i} className={`flex ${row.kind === "add" ? ADD : row.kind === "del" ? DEL : ""}`}>
        <span className={NUM}>{row.kind === "add" ? "" : row.oldNo}</span>
        <span className={NUM}>{row.kind === "del" ? "" : row.newNo}</span>
        <span className="w-4 shrink-0 select-none text-slate-400">{row.kind === "add" ? "+" : row.kind === "del" ? "−" : ""}</span>
        <Code text={row.text} language={language} />
      </div>)}
  </div>;
}

function Split({ rows, language }: { rows: readonly DiffRow[]; language: string }) {
  const split = useMemo(() => toSplit(rows), [rows]);
  return <div className="min-w-max font-mono text-xs leading-5">
    {split.map((row, i) => row.kind === "hunk"
      ? <div key={i} className="bg-blue-500/10 px-3 py-0.5 text-blue-700 dark:text-blue-300!">{row.text}</div>
      : <div key={i} className="grid grid-cols-2">
        <div className={`flex border-r border-slate-200 dark:border-white/10! ${row.left?.kind === "del" ? DEL : ""}`}>
          <span className={NUM}>{row.left?.oldNo}</span><Code text={row.left?.text ?? ""} language={language} />
        </div>
        <div className={`flex ${row.right?.kind === "add" ? ADD : ""}`}>
          <span className={`${NUM} pl-2`}>{row.right?.newNo}</span><Code text={row.right?.text ?? ""} language={language} />
        </div>
      </div>)}
  </div>;
}

/**
 * A file's unified diff, drawn with line numbers, added and removed lines tinted, and the app's own code colours
 * (the same palette as every code block). `split` puts old and new side by side; the default is one column, which is
 * what fits the inspector's width.
 */
export function DiffView({ diff, path, split = false }: { diff: string; path: string; split?: boolean }) {
  const rows = useMemo(() => parseUnified(diff), [diff]);
  const language = languageOf(path);
  if (rows.length === 0) return <p className="px-3 py-2 text-xs text-slate-500" data-testid="diff-empty">No line changes.</p>;
  return <div data-testid="diff-view" data-mode={split ? "split" : "unified"} className="overflow-x-auto rounded-lg border border-slate-200 bg-white py-1 dark:border-white/10! dark:bg-black/30!">
    {split ? <Split rows={rows} language={language} /> : <Unified rows={rows} language={language} />}
  </div>;
}
