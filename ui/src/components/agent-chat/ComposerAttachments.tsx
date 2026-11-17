import { useEffect, useId, useRef, useState, type RefObject } from "react";
import { Modal, ScrollArea } from "../kit";
import { AppIcon } from "../icons";

/** Local draft data. The host uploads files; preview URLs are never sent. */
export interface ChatAttachment {
  id: string;
  file: File;
  relativePath?: string;
}

type PickedFile = { file: File; relativePath?: string };

async function readEntry(entry: FileSystemEntry): Promise<PickedFile[]> {
  if (entry.isFile) {
    const file = await new Promise<File>((resolve, reject) => (entry as FileSystemFileEntry).file(resolve, reject));
    return [{ file, relativePath: entry.fullPath.replace(/^\//, "") }];
  }
  if (!entry.isDirectory) return [];
  const reader = (entry as FileSystemDirectoryEntry).createReader();
  const entries: FileSystemEntry[] = [];
  // Directory readers can return batches; a single call can silently omit files.
  for (;;) {
    const batch = await new Promise<FileSystemEntry[]>((resolve, reject) => reader.readEntries(resolve, reject));
    if (!batch.length) break;
    entries.push(...batch);
  }
  return (await Promise.all(entries.map(readEntry))).flat();
}

export function useComposerAttachments({ targetRef, attachments, disabled, onChange, onError }: {
  targetRef: RefObject<HTMLElement | null>;
  attachments: readonly ChatAttachment[];
  disabled: boolean;
  onChange?: (attachments: readonly ChatAttachment[]) => void;
  onError: (message: string) => void;
}) {
  const [dragging, setDragging] = useState(false);
  const sequence = useRef(0);
  const prefix = useId();
  const current = useRef(attachments);
  current.current = attachments;
  const handlers = useRef({ disabled, onChange, onError });
  handlers.current = { disabled, onChange, onError };
  const commit = (next: ChatAttachment[]) => {
    current.current = next;
    handlers.current.onChange?.(next);
  };
  const add = (picked: readonly PickedFile[]) => {
    if (handlers.current.disabled) return;
    commit([...current.current, ...picked.map(({ file, relativePath }) => ({
      id: `${prefix}-attachment-${++sequence.current}`, file,
      relativePath: relativePath || file.webkitRelativePath || undefined,
    }))]);
  };
  const addRef = useRef(add);
  addRef.current = add;
  useEffect(() => {
    const target = targetRef.current;
    if (!target) return;
    let live = true;
    let depth = 0;
    const hasFiles = (event: DragEvent) => Array.from(event.dataTransfer?.types ?? []).includes("Files");
    const enter = (event: DragEvent) => {
      if (!hasFiles(event)) return;
      event.preventDefault();
      depth += 1;
      if (!handlers.current.disabled) setDragging(true);
    };
    const over = (event: DragEvent) => {
      if (!hasFiles(event)) return;
      event.preventDefault();
      if (event.dataTransfer) event.dataTransfer.dropEffect = handlers.current.disabled ? "none" : "copy";
    };
    const leave = () => { if (--depth <= 0) { depth = 0; setDragging(false); } };
    const drop = (event: DragEvent) => {
      if (!hasFiles(event)) return;
      event.preventDefault();
      depth = 0; setDragging(false);
      if (handlers.current.disabled || !event.dataTransfer) return;
      // Read entries/files synchronously before the browser protects the transfer.
      const entries = Array.from(event.dataTransfer.items ?? []).map((item) => ({
        entry: item.webkitGetAsEntry?.(), file: item.getAsFile(),
      }));
      const fallback = Array.from(event.dataTransfer.files);
      void (async () => {
        try {
          const picked = entries.length ? (await Promise.all(entries.map(({ entry, file }) =>
            entry ? readEntry(entry) : Promise.resolve(file ? [{ file }] : [])))).flat() : fallback.map((file) => ({ file }));
          if (live) addRef.current(picked);
        } catch (cause) {
          if (live) handlers.current.onError(`Could not read dropped files: ${cause instanceof Error ? cause.message : String(cause)}`);
        }
      })();
    };
    target.addEventListener("dragenter", enter);
    target.addEventListener("dragover", over);
    target.addEventListener("dragleave", leave);
    target.addEventListener("drop", drop);
    return () => {
      live = false;
      target.removeEventListener("dragenter", enter);
      target.removeEventListener("dragover", over);
      target.removeEventListener("dragleave", leave);
      target.removeEventListener("drop", drop);
    };
  }, [targetRef]);
  return { attachments, dragging, addFiles: (files: readonly File[]) => add(files.map((file) => ({ file }))),
    remove: (id: string) => { if (!handlers.current.disabled) commit(current.current.filter((item) => item.id !== id)); },
    clear: () => commit([]) };
}

function LocalImage({ file, className }: { file: File; className?: string }) {
  const [url, setUrl] = useState<string>();
  useEffect(() => {
    const next = URL.createObjectURL(file);
    setUrl(next);
    return () => URL.revokeObjectURL(next);
  }, [file]);
  return url ? <img src={url} alt={file.name} className={className} /> : null;
}

export function ComposerAttachments({ attachments, onRemove, disabled }: {
  attachments: readonly ChatAttachment[];
  onRemove: (id: string) => void;
  disabled: boolean;
}) {
  const [previewId, setPreviewId] = useState<string | null>(null);
  const preview = attachments.find((item) => item.id === previewId);
  return <>
    <ScrollArea className="chat-attachment-strip" horizontal={false} aria-label="Attachments">
      <div className="chat-attachment-items">{attachments.map((item) => <div className="chat-attachment" key={item.id}>
        {item.file.type.startsWith("image/") ? <button type="button" className="chat-attachment-image" aria-label={`Preview ${item.file.name}`}
          onClick={() => setPreviewId(item.id)}><LocalImage file={item.file} /></button> :
          <div className="chat-attachment-file"><AppIcon name="read" size={22} /><span>{item.relativePath ?? item.file.name}</span></div>}
        <span className="chat-attachment-name" title={item.relativePath ?? item.file.name}>{item.file.name}</span>
        <button type="button" className="chat-attachment-remove" aria-label={`Remove ${item.file.name}`} disabled={disabled}
          onClick={() => onRemove(item.id)}><AppIcon name="cross" /></button>
      </div>)}</div>
    </ScrollArea>
    {preview && <Modal isOpen onClose={() => setPreviewId(null)} title={preview.file.name} maxWidth="min(1000px, 95vw)" draggable={false}>
      <LocalImage file={preview.file} className="chat-image-preview" />
    </Modal>}
  </>;
}
