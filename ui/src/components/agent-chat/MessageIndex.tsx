import { useId, useState } from "react";
import { ScrollArea } from "../kit";
import "./messageIndex.css";

/** An opaque loop-scoped anchor, not a transcript offset or array index. */
export interface LoopMessageIndexEntry {
  id: string;
  targetItemId?: string;
  preview: string;
  at?: string;
  /** Host-defined human interaction type: goal, message, steer, answer, approval, etc. */
  kind?: string;
  /** Optional cached context for the hover/focus card. No fetch occurs on hover. */
  contextPreview?: string;
}

export interface MessageIndexRequest {
  before: number;
  after: number;
  signal: AbortSignal;
}

export interface MessageIndexTarget { targetItemId: string }

export interface LoopMessageIndexProps {
  /** Change when switching loops; pending requests and selection reset. */
  scopeKey: string;
  /** Ordered lightweight human-interaction metadata, independent of loaded ChatItems. */
  entries: readonly LoopMessageIndexEntry[];
  before?: number;
  after?: number;
  hasMore?: boolean;
  loadingMore?: boolean;
  onLoadMore?: () => void;
  /** Host loads/commits a bounded window, then resolves its rendered ChatItem anchor. */
  onRequestJump?: (entry: LoopMessageIndexEntry, request: MessageIndexRequest) => Promise<MessageIndexTarget>;
  /** Host restores/commits the live tail, then resolves an item in that window. */
  onRequestLatest?: (request: { signal: AbortSignal }) => Promise<MessageIndexTarget>;
  onActiveChange?: (id: string | null) => void;
}

export interface MessageIndexProps {
  entries: readonly LoopMessageIndexEntry[];
  activeId: string | null;
  pendingId: string | null;
  onJump: (entry: LoopMessageIndexEntry) => void;
  hasMore?: boolean;
  loadingMore?: boolean;
  onLoadMore?: () => void;
}

export function MessageIndex({ entries, activeId, pendingId, onJump, hasMore, loadingMore, onLoadMore }: MessageIndexProps) {
  const [hovered, setHovered] = useState<string | null>(null);
  const tooltipId = useId();
  const preview = entries.find((entry) => entry.id === hovered);
  const activePosition = entries.findIndex((entry) => entry.id === activeId);
  return <nav className="chat-message-index" aria-label="Loop interaction index">
    <ScrollArea className="chat-message-index-scroll" horizontal={false} contentClassName="chat-message-index-markers">
      {entries.map((entry, index) => <button key={entry.id} type="button" className="chat-message-index-marker"
        data-neighbor={activePosition >= 0 && Math.abs(index - activePosition) === 1 || undefined}
        aria-label={`Jump to ${entry.kind ?? "interaction"} ${index + 1}: ${entry.preview}`}
        aria-describedby={hovered === entry.id ? tooltipId : undefined}
        aria-current={activeId === entry.id ? "location" : undefined} aria-busy={pendingId === entry.id || undefined}
        onMouseEnter={() => setHovered(entry.id)} onMouseLeave={() => setHovered(null)}
        onFocus={() => setHovered(entry.id)} onBlur={() => setHovered(null)} onClick={() => onJump(entry)}>
        <span aria-hidden="true" />
      </button>)}
      {hasMore && onLoadMore && <button type="button" className="chat-message-index-more" disabled={loadingMore}
        aria-label="Load more interaction markers" onClick={onLoadMore}>{loadingMore ? "…" : "+"}</button>}
    </ScrollArea>
    {preview && <div id={tooltipId} className="chat-message-index-preview" role="tooltip">
      {preview.kind && <span className="chat-panel-muted">{preview.kind}</span>}
      <strong>{preview.preview}</strong>
      {preview.contextPreview && <p>{preview.contextPreview}</p>}
      {preview.at && <time dateTime={preview.at}>{preview.at}</time>}
    </div>}
  </nav>;
}
