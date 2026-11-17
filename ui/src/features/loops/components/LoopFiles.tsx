import { useCallback, useEffect, useState } from "react";
import { Button, Panel } from "../../../components/kit";
import { rpc } from "../../../lib/bridge";
import { fileReaderError } from "../../../lib/fileReader";
import type { LiveState } from "../../../lib/live";
import { MarkdownView } from "./MarkdownView";

interface FileEntry { name: string; path: string; kind: "directory" | "file" }
interface DbDocument { file: string; bytes: number; writtenBy: string }

export function LoopFiles({ id, slug, root, live, produced }: { id: string; slug: string; root: string; live: LiveState; produced: string[] }) {
  const [directory, setDirectory] = useState(".");
  const [files, setFiles] = useState<FileEntry[]>([]);
  const [documents, setDocuments] = useState<DbDocument[]>([]);
  const [selected, setSelected] = useState<{ path: string; source: "workspace" | "store" } | null>(null);
  const [content, setContent] = useState("");
  const [loading, setLoading] = useState(false);
  const [directoryError, setDirectoryError] = useState("");
  const [fileError, setFileError] = useState("");
  const readDirectory = useCallback(async () => {
    if (live.state !== "current") return;
    try {
      const result = await rpc<{ files: FileEntry[] }>("loop.files", { id, path: directory });
      if (!Array.isArray(result.files)) throw new Error("Daemon did not return a file list");
      setFiles(result.files); setDirectoryError("");
    } catch (cause) { setFiles([]); setDirectoryError(fileReaderError(cause, "loop.files")); }
  }, [id, directory, live.state]);
  useEffect(() => { void readDirectory(); }, [readDirectory, live.state === "current" ? live.generation : -1]);
  useEffect(() => {
    if (live.state !== "current") return;
    let active = true;
    void rpc<{ documents: DbDocument[] }>("loop.read", { slug, componentRoot: root }).then((result) => {
      if (active) setDocuments(result.documents || []);
    }).catch(() => { if (active) setDocuments([]); });
    return () => { active = false; };
  }, [slug, root, live.state, live.state === "current" ? live.generation : -1]);
  useEffect(() => {
    if (!selected || live.state !== "current") return;
    let active = true;
    setLoading(true); setContent(""); setFileError("");
    const method = selected.source === "workspace" ? "loop.file.read" : "loop.read";
    const params = selected.source === "workspace" ? { id, path: selected.path } : { slug, componentRoot: root, name: selected.path };
    void rpc<{ content: string }>(method, params).then((result) => {
      if (typeof result.content !== "string") throw new Error("Daemon did not return file text");
      if (active) setContent(result.content);
    })
      .catch((cause) => { if (active) setFileError(fileReaderError(cause, method)); })
      .finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, [selected, id, slug, root, live.state, live.state === "current" ? live.generation : -1]);

  const parent = directory === "." ? "." : directory.split("/").slice(0, -1).join("/") || ".";
  return <Panel tone="slate" data-testid="loop-files"><div className="space-y-4">
    <div className="flex flex-wrap items-center justify-between gap-2"><div><p className="text-xs uppercase tracking-[.2em] aeon-accent-text">Artifacts</p><h2 className="text-lg font-semibold">Files and documents</h2></div>
      <Button size="sm" variant="outline" onClick={() => { void readDirectory(); setSelected((value) => value && { ...value }); }} disabled={live.state !== "current"}>Refresh</Button></div>
    <div className="grid min-w-0 gap-4 lg:grid-cols-[minmax(220px,300px)_minmax(0,1fr)]">
      <nav aria-label="Loop files" className="max-h-[500px] space-y-1 overflow-auto rounded-lg border border-white/10 p-2">
        {produced.length > 0 && <><p className="px-2 text-xs text-slate-500">Blueprint outputs</p>
          {produced.map((path) => <button key={path} className="block w-full truncate rounded px-2 py-1 text-left text-sm hover:bg-white/10" title={path} onClick={() => setSelected({ path, source: "workspace" })}>◇ {path}</button>)}
          <p className="mt-3 border-t border-white/10 pt-3" /></>}
        <p className="break-all px-2 text-xs text-slate-500">Workspace / {directory === "." ? "" : directory}</p>
        {directoryError && <p role="alert" className="px-2 py-2 text-sm text-red-700 dark:text-red-300">{directoryError}</p>}
        {directory !== "." && <button className="block w-full rounded px-2 py-1 text-left text-sm hover:bg-white/10" onClick={() => setDirectory(parent)}>← Parent folder</button>}
        {files.map((file) => <button key={file.path} className={`block w-full truncate rounded px-2 py-1 text-left text-sm hover:bg-white/10 ${selected?.path === file.path ? "aeon-accent-wash" : ""}`}
          title={file.path} onClick={() => file.kind === "directory" ? setDirectory(file.path) : setSelected({ path: file.path, source: "workspace" })}>
          {file.kind === "directory" ? "▸" : "◇"} {file.name}</button>)}
        {files.length === 0 && !directoryError && <p className="px-2 text-xs text-slate-500">No visible files here.</p>}
        {documents.length > 0 && <><p className="mt-4 border-t border-white/10 px-2 pt-3 text-xs text-slate-500">Stored loop documents</p>
          {documents.map((doc) => <button key={doc.file} className="block w-full truncate rounded px-2 py-1 text-left text-sm hover:bg-white/10" onClick={() => setSelected({ path: doc.file, source: "store" })}>{doc.file}</button>)}</>}
      </nav>
      <div className="min-h-48 min-w-0 overflow-auto rounded-lg border border-white/10 p-4">
        {selected && <p className="mb-4 break-all border-b border-white/10 pb-2 text-sm font-medium">{selected.path} <span className="text-xs text-slate-500">· {selected.source === "store" ? "database" : "workspace"}</span></p>}
        {fileError ? <p role="alert" className="text-red-700 dark:text-red-300">{fileError}</p> : loading ? <p>Reading file…</p> : !selected ? <p className="text-sm text-slate-500">Choose a workspace file or stored document to read it here.</p> :
          selected.path.toLowerCase().endsWith(".md") ? <MarkdownView content={content} /> : <pre className="whitespace-pre-wrap break-words text-sm">{content}</pre>}
      </div>
    </div>
  </div></Panel>;
}
