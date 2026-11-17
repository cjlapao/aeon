import { useCallback, useEffect, useState } from "react";
import { rpc, toRpcError } from "../../../lib/bridge";
import { MarkdownView } from "../../loops/components/MarkdownView";

interface Doc { file: string; rev: number; bytes: number; writtenBy?: string; writtenAt: string }
interface DocsResult { slug: string; componentRoot: string; documents: Doc[] }

const size = (n: number) => (n >= 1024 ? `${(n / 1024).toFixed(1)} KB` : `${n} B`);

function DocBody({ slug, componentRoot, name }: { slug: string; componentRoot: string; name: string }) {
  const [text, setText] = useState<string | null>(null);
  const [error, setError] = useState("");
  useEffect(() => {
    let on = true;
    setText(null); setError("");
    rpc<{ content?: string }>("loop.read", { slug, componentRoot, name })
      .then((r) => { if (on) setText(typeof r?.content === "string" ? r.content : ""); }, (cause) => { if (on) setError(toRpcError(cause).message); });
    return () => { on = false; };
  }, [slug, componentRoot, name]);
  if (error) return <p role="alert" className="px-3 pb-3 text-xs text-red-700 dark:text-red-300!">{error}</p>;
  if (text === null) return <p className="px-3 pb-3 text-xs text-slate-500">Opening…</p>;
  const markdown = /\.md$/i.test(name);
  return <div data-testid="document-body" className="max-h-[28rem] overflow-auto px-3 pb-3">
    {markdown ? <MarkdownView content={text} /> : <pre className="whitespace-pre-wrap font-mono text-xs text-slate-700 dark:text-slate-300!">{text}</pre>}
  </div>;
}

/**
 * The files the loop's agents have filed in the database, which the workspace never shows: plan, discovery, answers,
 * task specs. Click one to read it.
 */
export function DocumentsTab({ loopId, refreshKey }: { loopId: string; refreshKey?: unknown }) {
  const [docs, setDocs] = useState<DocsResult | null>(null);
  const [open, setOpen] = useState<string | null>(null);
  const [error, setError] = useState("");
  const load = useCallback(async () => {
    try { setDocs(await rpc<DocsResult>("loop.documents", { id: loopId })); setError(""); }
    catch (cause) { setError(toRpcError(cause).message); }
  }, [loopId]);
  useEffect(() => { void load(); }, [load, refreshKey]);
  if (error) return <p role="alert" className="p-3 text-sm text-red-700 dark:text-red-300!">{error}</p>;
  if (!docs) return <p className="p-3 text-sm text-slate-500">Reading the loop's files…</p>;
  if (docs.documents.length === 0) return <p data-testid="documents-empty" className="p-3 text-sm text-slate-500">The agents have not filed any files in this loop yet.</p>;
  return <div data-testid="documents-tab" className="divide-y divide-slate-200 dark:divide-white/10!">
    <p className="px-3 py-2 text-xs uppercase tracking-wider text-slate-500">{docs.documents.length} files in this loop</p>
    {docs.documents.map((d) => <div key={d.file}>
      <button type="button" aria-expanded={open === d.file} onClick={() => setOpen(open === d.file ? null : d.file)}
        className="flex w-full min-w-0 items-center gap-2 px-3 py-2 text-left hover:bg-slate-50 dark:hover:bg-white/5!">
        <span className="min-w-0 flex-1 truncate font-mono text-sm text-slate-900 dark:text-slate-100!" title={d.file}>{d.file}</span>
        <span className="shrink-0 font-mono text-[11px] text-slate-500">rev {d.rev} · {size(d.bytes)}</span>
      </button>
      <p className="px-3 pb-1 text-[11px] text-slate-400">{d.writtenBy || "unknown"} · {new Date(d.writtenAt).toLocaleString()}</p>
      {open === d.file && <DocBody slug={docs.slug} componentRoot={docs.componentRoot} name={d.file} />}
    </div>)}
  </div>;
}
