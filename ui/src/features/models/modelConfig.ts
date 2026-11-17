import { REASONING_EFFORTS, type RouteView, type SetRouteParams } from '../../lib/protocol';

export const CAPABILITIES = [
  { id: 'text', label: 'Text', description: 'Text prompts and responses' },
  { id: 'image', label: 'Image', description: 'Image input and understanding' },
  { id: 'tool-calling', label: 'Tool calling', description: 'Native structured tool calls' },
  { id: 'file', label: 'File', description: 'File input and understanding' },
  { id: 'pdf', label: 'PDF', description: 'PDF document understanding' },
  { id: 'thinking', label: 'Thinking', description: 'Reasoning output' },
  { id: 'thinking-effort', label: 'Thinking effort', description: 'Adjustable reasoning effort' },
  { id: 'embedding', label: 'Embedding', description: 'Turns text into vectors for the search index. A model with this is what Index uses.' },
  { id: 'asr', label: 'Speech to text', description: 'Transcribes speech. What Orion listens with.' },
  { id: 'tts', label: 'Text to speech', description: 'Speaks text aloud. What Orion talks with.' },
] as const;
export const EFFORT_LABELS: Record<string, string> = {
  none: 'No reasoning', minimal: 'Very low reasoning effort (some providers)', low: 'Minimal reasoning effort',
  medium: 'Moderate reasoning effort', high: 'Significant reasoning effort', xhigh: 'Extra high reasoning effort', max: 'Maximum reasoning effort',
};
export const MAX_ICON_BYTES = 256 * 1024;
export const routeIdForSlug = (slug: string) => slug.trim().replace(/[^a-zA-Z0-9_.-]+/g, '-').replace(/\.{2,}/g, '-').replace(/^\.+/, '').slice(0, 128);
export const modelName = (route: RouteView) => route.displayName || route.model;
export const availableEfforts = (route?: Pick<RouteView, 'reasoningEfforts'>) => route?.reasoningEfforts ?? REASONING_EFFORTS;

export interface ModelDraft {
  id: string; provider: string; model: string; displayName: string; displayDescription: string; icon: string;
  capabilities: string[] | null; reasoningEfforts: string[] | null;
  baseUrl: string; apiKey: string; clearKey: boolean; maxOutputTokens: string; contextWindow: string;
  reasoningEffort: string; openaiEndpoint: string; audioApi: string;
  inputPrice: string; outputPrice: string; cacheReadPrice: string; cacheWritePrice: string;
  /** Declared unmetered on purpose (S18 O8) — the decision a blank price must not hide. */
  noPrice: boolean;
}
export function modelDraft(route?: RouteView, provider = 'anthropic'): ModelDraft {
  return {
    id: route?.id ?? '', provider: route?.provider ?? provider, model: route?.model ?? '',
    displayName: route ? modelName(route) : '', displayDescription: route?.displayDescription ?? '', icon: route?.icon ?? '',
    capabilities: route ? route.capabilities ?? null : ['text'], reasoningEfforts: route ? route.reasoningEfforts ?? null : [],
    baseUrl: route?.baseUrl ?? '', apiKey: '', clearKey: false, maxOutputTokens: String(route?.maxOutputTokens ?? 4096),
    contextWindow: route?.contextWindow ? String(route.contextWindow) : '', reasoningEffort: route?.reasoningEffort ?? '',
    openaiEndpoint: route?.openaiEndpoint ?? '', audioApi: route?.audioApi ?? '',
    inputPrice: String(route?.prices?.inputUsdPerMTok ?? ''), outputPrice: String(route?.prices?.outputUsdPerMTok ?? ''),
    cacheReadPrice: String(route?.prices?.cacheReadUsdPerMTok ?? ''), cacheWritePrice: String(route?.prices?.cacheWriteUsdPerMTok ?? ''),
    noPrice: route?.noPrice ?? false,
  };
}
/** The form's current values as the draft the daemon tests for one call (nothing is saved). A saved model's own id lets the daemon reuse its stored key when the key box is blank. */
export function voiceDraft(draft: ModelDraft, editing: boolean): Record<string, unknown> {
  return {
    ...(editing ? { id: draft.id.trim() } : {}), provider: draft.provider, model: draft.model.trim(), baseUrl: draft.baseUrl.trim() || undefined,
    apiKey: draft.clearKey ? undefined : draft.apiKey || undefined, audioApi: draft.audioApi || undefined, capabilities: draft.capabilities ?? [],
  };
}
export function modelParams(draft: ModelDraft): SetRouteParams {
  if (!draft.id.trim() || !draft.model.trim() || !draft.displayName.trim()) throw new Error('Route ID, slug and display name are required.');
  if (!/^[a-zA-Z0-9_-][a-zA-Z0-9_.-]*(\/[a-zA-Z0-9_-][a-zA-Z0-9_.-]*)*$/.test(draft.id.trim()) || draft.id.trim().length > 128 || draft.id.includes('..')) throw new Error('Route ID must use letters, digits, underscores, hyphens, single dots or a slash between parts, and no part can start with a dot.');
  if (!Number.isSafeInteger(Number(draft.maxOutputTokens)) || Number(draft.maxOutputTokens) <= 0) throw new Error('Output limit must be a positive whole number.');
  if (draft.contextWindow && (!Number.isSafeInteger(Number(draft.contextWindow)) || Number(draft.contextWindow) <= 0)) throw new Error('Context window must be a positive whole number.');
  const prices: Record<string, number> = {};
  for (const [field, value] of Object.entries({ inputUsdPerMTok: draft.inputPrice, outputUsdPerMTok: draft.outputPrice, cacheReadUsdPerMTok: draft.cacheReadPrice, cacheWriteUsdPerMTok: draft.cacheWritePrice } as const)) {
    if (value !== '') { const n = Number(value); if (!Number.isFinite(n) || n < 0) throw new Error('Prices must be non-negative numbers.'); prices[field] = n; }
  }
  // A price and "unmetered" are two decisions in one breath — refused here,
  // never precedence-resolved, the same discipline as `--no-price` beside
  // `--input-price` on the CLI and `apiKey`/`clearKey` before them.
  if (Object.keys(prices).length > 0 && draft.noPrice) throw new Error('A price and "no price — declared unmetered" say opposite things; change one.');
  return {
    id: draft.id.trim(), model: draft.model.trim(), provider: draft.provider,
    displayName: draft.displayName.trim(), displayDescription: draft.displayDescription.trim(), icon: draft.icon,
    ...(draft.capabilities !== null ? { capabilities: draft.capabilities } : {}),
    ...(draft.reasoningEfforts !== null ? { reasoningEfforts: draft.reasoningEfforts } : {}),
    baseUrl: draft.baseUrl.trim() || undefined, apiKey: draft.apiKey || undefined, clearKey: draft.clearKey || undefined,
    maxOutputTokens: Number(draft.maxOutputTokens), contextWindow: draft.contextWindow ? Number(draft.contextWindow) : undefined,
    reasoningEffort: draft.reasoningEffort || undefined, clearReasoningEffort: !draft.reasoningEffort || undefined,
    openaiEndpoint: draft.openaiEndpoint || undefined,
    audioApi: draft.audioApi || undefined,
    // Empty prices go as ABSENT, not `{}`: an empty PricesView is a non-nil
    // one, and the daemon would keep it as the saved prices — turning "I did
    // not touch pricing" into "prices erased".
    prices: Object.keys(prices).length ? prices : undefined,
    noPrice: draft.noPrice ? true : undefined,
  };
}
export async function readModelIcon(file: File): Promise<string> {
  if (!['image/png', 'image/jpeg', 'image/webp'].includes(file.type) || file.size > MAX_ICON_BYTES || file.size === 0) throw new Error('Choose a PNG, JPEG or WebP image up to 256 KiB.');
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result));
    reader.onerror = () => reject(new Error('The icon could not be read.'));
    reader.readAsDataURL(file);
  });
}
