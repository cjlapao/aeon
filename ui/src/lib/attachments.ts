/**
 * attachments.ts — the client side of S69 (AT2/AT8/AT11). One rule owns
 * every byte that leaves or enters through this file:
 *
 *   **Bytes once, refs forever.** A file crosses the socket exactly once,
 *   at `attachment.put`, and everything after — the send, the row, the
 *   event, the bubble — carries the store's own RECEIPT. The preview's
 *   `attachment.read` is for RENDERING only; a read never re-sends bytes,
 *   and a failed read renders the absence (ruling 3: visibly broken,
 *   naming document and revision), never a blank and never a fake icon.
 *
 * The receipt fields are never asserted from the client: the daemon files
 * what the STORE says (sanitised name, sniffed type, stored size, digest).
 * `putAttachment` sends the operator's declared type; the door's magic-byte
 * verdict overrules it, and its refusal sentence is what callers surface.
 */
import { rpc } from "./bridge";

/** The store's facts about one frozen attachment revision (S69 AT8). */
export interface AttachmentReceipt {
  name: string;
  document: string;
  rev: number;
  mediaType: string;
  size: number;
  sha256: string;
}

/** What a send carries: the two facts a client may legitimately know. */
export interface AttachmentRef {
  document: string;
  rev: number;
}

/** Where the loop lives — the put/read doors need the scope, and it is the
 * loop row's own, read from `loop.show`, never a flag somebody typed. */
export interface AttachmentScope {
  slug: string;
  componentRoot: string;
}

/** The four types the bubble previews inline (the store admits png/jpeg/
 * gif/webp/pdf; a pdf previews as a chip — the window does not embed it). */
const PREVIEWABLE = new Set(["image/png", "image/jpeg", "image/gif", "image/webp"]);

export function isPreviewable(mediaType: string): boolean {
  return PREVIEWABLE.has(mediaType);
}

/** One place formats a byte count, so a chip never says "1048576 bytes". */
export function humanSize(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / (1024 * 1024)).toFixed(1)} MB`;
}

/** Base64 in chunks — a single String.fromCharCode over a 10 MiB byte
 * array blows the argument stack; 64 KiB per call is the measured-safe
 * chunk this box has never overflowed. */
function toBase64(bytes: Uint8Array): string {
  let out = "";
  const CHUNK = 65536;
  for (let i = 0; i < bytes.length; i += CHUNK) {
    out += String.fromCharCode(...bytes.subarray(i, i + CHUNK));
  }
  return btoa(out);
}

/** The declared type: what the browser guessed, or what the extension
 * says, or octet-stream — the door decides whether the bytes agree and
 * names BOTH sides when they do not. This file never claims a type it
 * verified. */
export function declaredType(file: File): string {
  if (file.type) return file.type;
  const ext = file.name.slice(file.name.lastIndexOf(".")).toLowerCase();
  const guessed: Record<string, string> = {
    ".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif",
    ".webp": "image/webp", ".pdf": "application/pdf", ".txt": "text/plain", ".md": "text/plain",
    ".markdown": "text/plain", ".log": "text/plain", ".csv": "text/plain", ".tsv": "text/plain",
    ".json": "text/plain", ".yaml": "text/plain", ".yml": "text/plain", ".toml": "text/plain",
    ".ini": "text/plain", ".env": "text/plain", ".sql": "text/plain", ".proto": "text/plain",
    ".go": "text/plain", ".ts": "text/plain", ".tsx": "text/plain", ".js": "text/plain",
    ".jsx": "text/plain", ".mjs": "text/plain", ".cjs": "text/plain", ".py": "text/plain",
    ".rb": "text/plain", ".rs": "text/plain", ".java": "text/plain", ".kt": "text/plain",
    ".c": "text/plain", ".h": "text/plain", ".cpp": "text/plain", ".hpp": "text/plain",
    ".cs": "text/plain", ".swift": "text/plain", ".sh": "text/plain", ".bash": "text/plain",
    ".zsh": "text/plain", ".html": "text/plain", ".htm": "text/plain", ".css": "text/plain",
    ".scss": "text/plain", ".xml": "text/plain", ".svg": "text/plain", ".tf": "text/plain",
  };
  return guessed[ext] ?? "application/octet-stream";
}

/** The capability a media type needs from a route, in the daemon's own
 * vocabulary (S77): the mirror of `model.RequiredCapability` — an image
 * needs `image`, a pdf needs `pdf`, text needs `file`. Anything else
 * needs nothing here because the store already refused it upstream. */
export function requiredCapability(mediaType: string): string | null {
  if (mediaType.startsWith("image/")) return "image";
  if (mediaType === "application/pdf") return "pdf";
  if (mediaType.startsWith("text/")) return "file";
  return null;
}

/** The shape this gate reads off a `routes.list` row: its id for the
 * sentence and its declared capabilities — possibly ABSENT, and absent
 * is not yes (P31: a route that never declared `image` cannot take one). */
export interface RouteCapabilityView {
  id?: string;
  capabilities?: readonly string[] | null;
}

/**
 * The launch gate (S77): which staged files CANNOT ride the chosen route,
 * named, before a byte is put. Empty means every file can ride. The
 * sentences mirror the daemon's refusal at `loop.start` — the button is
 * disabled on these words, never on a mystery.
 */
export function attachmentLaunchGate(files: readonly File[], route: RouteCapabilityView | undefined): string[] {
  const out: string[] = [];
  for (const file of files) {
    const media = declaredType(file);
    if (media === "application/octet-stream") {
      out.push(`${file.name}: the store admits images, pdfs and text — these bytes are none of those, so they cannot ride a launch or a loop`);
      continue;
    }
    const need = requiredCapability(media);
    if (!need) continue;
    if (!route?.capabilities?.includes(need)) {
      out.push(`${file.name} (${media}) needs the "${need}" capability, which route '${route?.id ?? "unchosen"}' does not declare — remove the file or choose a route that accepts it`);
    }
  }
  return out;
}

/** Put ONE file: the only call in the app where attachment bytes leave.
 * The daemon's refusals (cap, lying extension, off the allow-list, bound)
 * arrive as RPC errors whose sentences already name the file and the
 * limit — callers surface them verbatim, never rewritten. */
export async function putAttachment(scope: AttachmentScope, file: File): Promise<AttachmentReceipt> {
  const bytes = new Uint8Array(await file.arrayBuffer());
  const result = await rpc<{ receipt?: AttachmentReceipt }>("attachment.put", {
    slug: scope.slug, componentRoot: scope.componentRoot,
    name: file.name, mediaType: declaredType(file), data: toBase64(bytes),
  });
  if (!result?.receipt?.document) throw new Error(`attachment.put for ${file.name} returned no receipt`);
  return result.receipt;
}

/** Reads are for previews, so the SAME (document, rev) is fetched once —
 * a pinned revision is frozen, so the cached answer cannot go stale. A
 * FAILED read is evicted (a later render may retry honestly) and the
 * rejection is what the chip renders as its sentence. */
const readCache = new Map<string, Promise<string>>();

export function cachedAttachmentData(scope: AttachmentScope, document: string, rev: number): Promise<string> {
  const key = `${scope.slug}\u0000${scope.componentRoot}\u0000${document}\u0000${rev}`;
  const hit = readCache.get(key);
  if (hit) return hit;
  const pending = rpc<{ data?: string; mediaType?: string }>("attachment.read", {
    slug: scope.slug, componentRoot: scope.componentRoot, document, rev,
  }).then((r) => {
    if (!r?.data) throw new Error(`attachment.read ${document}@${rev} returned no data`);
    return `data:${r.mediaType ?? "application/octet-stream"};base64,${r.data}`;
  }).catch((cause: unknown) => {
    readCache.delete(key); // the next render asks again — a miss is retried, not cached forever
    throw cause;
  });
  readCache.set(key, pending);
  return pending;
}

/** Put a batch, in the operator's own order, collecting the references
 * the send will carry. Sequential on purpose: the door answers one file
 * at a time and the first refusal should stop the send before any half
 * list rides. Returns undefined for nothing-to-send so the caller can
 * keep the field ABSENT (never `[]` — AT2). */
export async function putAllAttachments(scope: AttachmentScope, files: readonly File[]): Promise<AttachmentRef[] | undefined> {
  if (files.length === 0) return undefined;
  const refs: AttachmentRef[] = [];
  for (const file of files) {
    const receipt = await putAttachment(scope, file);
    refs.push({ document: receipt.document, rev: receipt.rev });
  }
  return refs;
}
