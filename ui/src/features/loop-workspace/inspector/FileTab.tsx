import { DiffView } from "../../../components/DiffView";
import { ScrollArea } from "../../../components/kit";
import { MarkdownView } from "../../loops/components/MarkdownView";
import { useFilePreview } from "./useFilePreview";

/** One opened file: its diff when git has one, otherwise what it contains. */
export function FileTab({ componentRoot, path }: { componentRoot: string; path: string }) {
  const { loading, diff, preview, error } = useFilePreview(componentRoot, path);
  return <div className="space-y-3 p-4" data-testid="file-tab">
    <h3 className="break-all border-b border-slate-200 pb-2 font-mono text-sm font-semibold text-slate-900 dark:border-white/10! dark:text-slate-100!">{path}</h3>
    {loading ? <p className="text-sm text-slate-500">Opening file…</p>
      : error ? <p role="alert" className="text-sm text-red-700 dark:text-red-300!">{error}</p>
      : diff ? <DiffView diff={diff} path={path} />
      : preview?.kind === "markdown" ? <MarkdownView content={preview.content ?? ""} />
      : preview?.kind === "image" && preview.dataUrl ? <img src={preview.dataUrl} alt={path} className="max-h-[60vh] max-w-full object-contain" />
      : preview?.kind === "code" ? <ScrollArea variant="hover" className="max-h-[70vh]"><pre className="whitespace-pre font-mono text-xs text-slate-800 dark:text-slate-200!">{preview.content}</pre></ScrollArea>
      : <p className="text-sm text-slate-500">{preview?.reason || "This file cannot be previewed."}</p>}
  </div>;
}
