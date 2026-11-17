import { useCallback, useEffect, useMemo, useState } from "react";
import { Button, Panel, Tree } from "../../components/kit";
import type { TreeItem } from "../../components/kit";
import { useAppSettings } from "../../lib/appSettings";
import { invokeShell } from "../../lib/bridge";
import { MarkdownView } from "../loops/components/MarkdownView";

interface FileEntry { name: string; path: string; kind: "directory" | "file" }
interface Listing { entries: FileEntry[]; truncated: boolean }
interface Preview { kind: "markdown" | "code" | "image" | "binary"; content: string | null; dataUrl: string | null; reason: string | null }

const messageOf = (error: unknown) => error instanceof Error ? error.message : String(error);

/** Exploratory, read-only browser for a component's local workspace. */
export function ComponentFileBrowser({ root }: { root: string }) {
  const [opened, setOpened] = useState(false);
  const [directories, setDirectories] = useState<Record<string, Listing>>({});
  const [selected, setSelected] = useState<string | null>(null);
  const [expandedIds, setExpandedIds] = useState<string[]>([]);
  const [preview, setPreview] = useState<Preview | null>(null);
  const [loadingDirectory, setLoadingDirectory] = useState<string | null>(null);
  const [loadingPreview, setLoadingPreview] = useState(false);
  const [directoryError, setDirectoryError] = useState("");
  const [previewError, setPreviewError] = useState("");
  const { controlTone } = useAppSettings();

  const openDirectory = useCallback(async (path: string) => {
    setLoadingDirectory(path); setDirectoryError("");
    try {
      const listing = await invokeShell<Listing>("component_list", { root, path });
      if (!Array.isArray(listing.entries)) throw new Error("The shell did not return a file list.");
      setDirectories((previous) => ({ ...previous, [path]: listing }));
    } catch (error) { setDirectoryError(`${path === "." ? "Component" : path}: ${messageOf(error)}`); }
    finally { setLoadingDirectory(null); }
  }, [root]);

  useEffect(() => {
    setOpened(false); setDirectories({}); setSelected(null); setExpandedIds([]); setPreview(null);
    setDirectoryError(""); setPreviewError("");
  }, [root]);

  useEffect(() => {
    if (!selected) return;
    let active = true;
    setLoadingPreview(true); setPreview(null); setPreviewError("");
    void invokeShell<Preview>("component_read", { root, path: selected }).then((result) => {
      if (active) setPreview(result);
    }).catch((error) => { if (active) setPreviewError(messageOf(error)); })
      .finally(() => { if (active) setLoadingPreview(false); });
    return () => { active = false; };
  }, [root, selected]);

  const items = useMemo((): TreeItem[] => {
    const build = (path: string): TreeItem[] => (directories[path]?.entries || []).map((entry) => ({
      id: entry.path,
      label: entry.name,
      icon: entry.kind === "directory" ? "Folder" : "Script",
      children: entry.kind === "directory" ? build(entry.path) : undefined,
    }));
    return build(".");
  }, [directories]);

  const selectItem = useCallback((ids: string[]) => {
    const path = ids[0];
    if (!path) return;
    const entry = Object.values(directories).flatMap((listing) => listing.entries).find((item) => item.path === path);
    if (entry?.kind === "directory") {
      setSelected(null);
      setExpandedIds((current) => current.includes(path) ? current : [...current, path]);
      void openDirectory(path);
    } else if (entry?.kind === "file") {
      setSelected(path);
    }
  }, [directories, openDirectory]);

  return <Panel tone="slate" data-testid="component-file-browser"><div className="space-y-4">
    <div className="flex flex-wrap items-center justify-between gap-3"><div>
      <p className="text-xs uppercase tracking-[.2em] aeon-accent-text">Component preview</p>
      <h2 className="mt-1 text-lg font-semibold">Component files</h2>
      <p className="mt-1 text-sm text-slate-600 dark:text-slate-400">Browse local files in this component. Previews are read-only.</p>
    </div><Button size="sm" onClick={() => { setOpened(true); void openDirectory("."); }} disabled={loadingDirectory !== null}>
      {opened ? "Refresh root" : "Browse component"}</Button></div>
    {opened && <div className="grid min-w-0 gap-4 lg:grid-cols-[minmax(240px,360px)_minmax(0,1fr)]">
      <nav aria-label="Component files" className="max-h-[620px] min-w-0 overflow-auto rounded-xl border border-slate-300/40 p-3 dark:border-white/10">
        {loadingDirectory && <p className="text-sm">Loading {loadingDirectory === "." ? "component" : loadingDirectory}…</p>}
        {directoryError && <p role="alert" className="text-sm text-red-700 dark:text-red-300">{directoryError}</p>}
        {directories["."]?.truncated && <p className="text-xs text-amber-700 dark:text-amber-300">Showing the first 500 root entries.</p>}
        {Object.entries(directories).filter(([path, listing]) => path !== "." && listing.truncated).map(([path]) =>
          <p key={path} className="text-xs text-amber-700 dark:text-amber-300">{path}: showing the first 500 entries.</p>)}
        {directories["."] && <Tree items={items} ariaLabel="Component files" selectionMode="single"
          selectedIds={selected ? [selected] : []} onSelectionChange={selectItem}
          expandedIds={expandedIds} onExpandedChange={setExpandedIds} tone={controlTone}
          emptyMessage="No files in this component." />}
      </nav>
      <section aria-label="File preview" className="max-h-[620px] min-h-52 min-w-0 overflow-auto rounded-xl border border-slate-300/40 p-4 dark:border-white/10">
        {selected && <h3 className="mb-4 break-all border-b border-slate-300/40 pb-3 text-sm font-semibold dark:border-white/10">{selected}</h3>}
        {loadingPreview ? <p>Opening file…</p> : previewError ? <p role="alert" className="text-red-700 dark:text-red-300">{previewError}</p> :
          !selected ? <p className="text-sm text-slate-500">Choose a file to preview it here.</p> :
          preview?.kind === "markdown" ? <MarkdownView content={preview.content || ""} /> :
          preview?.kind === "image" && preview.dataUrl ? <img src={preview.dataUrl} alt={selected} className="max-h-[540px] max-w-full object-contain" /> :
          preview?.kind === "code" ? <pre className="overflow-auto whitespace-pre font-mono text-sm"><code>{preview.content}</code></pre> :
          <p className="text-sm text-slate-600 dark:text-slate-400">{preview?.reason || "This file cannot be opened in the preview."}</p>}
      </section>
    </div>}
  </div></Panel>;
}
