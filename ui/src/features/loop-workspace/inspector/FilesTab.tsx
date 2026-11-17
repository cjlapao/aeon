import { useCallback, useEffect, useMemo, useState } from "react";
import { DiffLines, type FileChange } from "../../../components/agent-chat";
import { CustomIcon, Input, Tree, type TreeItem } from "../../../components/kit";
import { invokeShell, toRpcError } from "../../../lib/bridge";
import type { GitStatusView } from "../center/useGitStatus";
import { useFilePreview } from "./useFilePreview";

type Kind = "read" | "created" | "modified";
interface Entry { name: string; path: string; kind: "directory" | "file" }
interface Listing { entries: Entry[]; truncated: boolean }

/** A project tree has no use for these; the shell lists them because it lists everything. */
const HIDDEN = new Set([".git", "node_modules"]);
const DOT: Record<Kind, string> = { created: "bg-emerald-500", modified: "bg-amber-500", read: "bg-slate-400" };

function Glyph({ directory, marker }: { directory: boolean; marker?: Kind }) {
  return <span className="relative inline-flex">
    <CustomIcon icon={directory ? "Folder" : "Script"} size="xs" />
    {marker && <span data-marker={marker} title={marker} className={`absolute -right-0.5 -top-0.5 h-1.5 w-1.5 rounded-full ${DOT[marker]}`} />}
  </span>;
}

const gitKind = (index: string, worktree: string): Kind => (index === "?" || index === "A" || worktree === "A" ? "created" : "modified");

export interface FilesTabProps {
  componentRoot: string;
  /** Files the loop touched, from its own tool calls — the only marker source in a workspace that is not a repository. */
  scope: ReadonlyMap<string, Kind>;
  changes: readonly FileChange[];
  git: GitStatusView | null;
  onOpenFile(path: string): void;
}

function FileCard({ componentRoot, path, kind, change, onOpenFile }: { componentRoot: string; path: string; kind?: Kind; change?: FileChange; onOpenFile(path: string): void }) {
  const { loading, diff, preview, error } = useFilePreview(componentRoot, path);
  const head = preview?.content?.split("\n").slice(0, 8);
  const lines = preview?.content ? preview.content.split("\n").length : undefined;
  return <div data-testid="file-card" className="space-y-2 border-t border-slate-200 bg-white/70 p-3 dark:border-white/10! dark:bg-white/5!">
    <div className="flex items-center gap-2">
      <CustomIcon icon="Script" size="sm" className="text-blue-600" />
      <span className="min-w-0 flex-1 truncate font-medium text-slate-900 dark:text-slate-100!" title={path}>{path.split("/").pop()}</span>
      {kind && <span className="rounded bg-slate-100 px-1.5 font-mono text-xs text-slate-600 dark:bg-white/10! dark:text-slate-300!">{kind}</span>}
      {(change?.linesAdded ?? 0) > 0 && <span className="rounded bg-emerald-50 px-1.5 font-mono text-xs text-emerald-700 dark:bg-emerald-400/10! dark:text-emerald-300!">+{change!.linesAdded}</span>}
      {(change?.linesRemoved ?? 0) > 0 && <span className="rounded bg-red-50 px-1.5 font-mono text-xs text-red-700 dark:bg-red-400/10! dark:text-red-300!">−{change!.linesRemoved}</span>}
    </div>
    {lines !== undefined && <p className="font-mono text-xs text-slate-500">{lines} lines</p>}
    {loading ? <p className="text-xs text-slate-500">Opening…</p>
      : diff ? <DiffLines text={diff} maxLines={8} />
      : head ? <div className="overflow-hidden rounded-lg border border-slate-200 bg-white p-2 font-mono text-xs leading-5 text-slate-700 dark:border-white/10! dark:bg-black/30! dark:text-slate-300!">
        {head.map((line, i) => <div key={i} className="truncate">{line || " "}</div>)}</div>
      : error ? <p role="alert" className="text-xs text-red-700 dark:text-red-300!">{error}</p>
      : <p className="text-xs text-slate-500">{preview?.reason ?? "No preview."}</p>}
    <div className="flex gap-2">
      <button type="button" onClick={() => onOpenFile(path)} className="rounded-md bg-blue-700 px-3 py-1 text-xs font-medium text-white hover:bg-blue-800">Open</button>
      <button type="button" onClick={() => void navigator.clipboard?.writeText(path).catch(() => undefined)}
        className="rounded-md border border-slate-300 px-3 py-1 text-xs text-slate-700 hover:bg-slate-100 dark:border-white/20! dark:text-slate-200! dark:hover:bg-white/10!">Copy path</button>
    </div>
  </div>;
}

/**
 * The project's files, as the kit Tree: folders load as they are opened, files
 * the loop touched carry a marker, and the selected file's card sits below. The
 * kit's tree items have no trailing slot, so per-file detail (lines added, the
 * diff) lives in the card rather than on the row.
 */
export function FilesTab({ componentRoot, scope, changes, git, onOpenFile }: FilesTabProps) {
  const [directories, setDirectories] = useState<Record<string, Listing>>({});
  const [expanded, setExpanded] = useState<string[]>([]);
  const [selected, setSelected] = useState("");
  const [filter, setFilter] = useState("");
  const [error, setError] = useState("");

  const load = useCallback(async (path: string) => {
    try {
      const listing = await invokeShell<Listing>("component_list", { root: componentRoot, path });
      if (!Array.isArray(listing.entries)) throw new Error("The shell did not return a file list.");
      setDirectories((all) => ({ ...all, [path]: listing }));
      setError("");
    } catch (cause) { setError(`${path === "." ? "Workspace" : path}: ${toRpcError(cause).message}`); }
  }, [componentRoot]);

  // No root yet (the loop is still being read) means nothing to list — not an error to show.
  useEffect(() => { setDirectories({}); setExpanded([]); setSelected(""); if (componentRoot) void load("."); }, [componentRoot, load]);

  const gitMarks = useMemo(() => new Map((git?.state === "ok" ? git.changed ?? [] : []).map((c) => [c.path, gitKind(c.index, c.worktree)] as const)), [git]);
  const markerOf = useCallback((path: string): Kind | undefined => scope.get(path) ?? gitMarks.get(path), [scope, gitMarks]);

  const items = useMemo((): TreeItem[] => {
    const build = (path: string): TreeItem[] => (directories[path]?.entries ?? []).filter((e) => !HIDDEN.has(e.name)).map((e) => ({
      id: e.path, label: e.name, icon: <Glyph directory={e.kind === "directory"} marker={e.kind === "file" ? markerOf(e.path) : undefined} />,
      children: e.kind === "directory" ? build(e.path) : undefined,
    }));
    return build(".");
  }, [directories, markerOf]);

  const isDirectory = useCallback((path: string) => Object.values(directories).some((l) => l.entries.some((e) => e.path === path && e.kind === "directory")), [directories]);
  const open = useCallback((path: string) => {
    setExpanded((current) => (current.includes(path) ? current : [...current, path]));
    if (!directories[path]) void load(path);
  }, [directories, load]);

  const created = [...scope.values()].filter((k) => k === "created").length;
  const added = changes.reduce((sum, c) => sum + (c.linesAdded ?? 0), 0);
  const change = changes.find((c) => c.path === selected);

  return <div className="flex h-full min-h-0 w-full min-w-0 max-w-full flex-col overflow-hidden" data-testid="files-tab">
    <div className="space-y-2 p-3">
      <Input value={filter} onChange={(e) => setFilter(e.target.value)} placeholder="Filter files…" aria-label="Filter files" />
      <div className="flex items-center justify-between text-xs text-slate-600 dark:text-slate-300!">
        <span className="inline-flex items-center gap-1.5"><span className="h-1.5 w-1.5 rounded-full bg-emerald-500" aria-hidden />
          {`${scope.size} ${scope.size === 1 ? "file" : "files"} in scope${created ? ` (${created} created)` : ""}`}</span>
        {added > 0 && <span data-testid="scope-added" className="rounded bg-emerald-50 px-1.5 font-mono text-emerald-700 dark:bg-emerald-400/10! dark:text-emerald-300!">+{added}</span>}
      </div>
      {error && <p role="alert" className="text-xs text-red-700 dark:text-red-300!">{error}</p>}
    </div>
    <div className="min-h-0 flex-1 overflow-auto px-2">
      {directories["."] && <Tree items={items} ariaLabel="Workspace files" selectionMode="single" filter={filter}
        selectedIds={selected ? [selected] : []}
        onSelectionChange={(ids) => { const path = ids[0]; if (!path) return; if (isDirectory(path)) open(path); else setSelected(path); }}
        expandedIds={expanded} onExpandedChange={(ids) => { setExpanded(ids); ids.filter((id) => !directories[id]).forEach((id) => void load(id)); }}
        emptyMessage="No files in this workspace." />}
    </div>
    {selected && <FileCard componentRoot={componentRoot} path={selected} kind={markerOf(selected)} change={change} onOpenFile={onOpenFile} />}
  </div>;
}
