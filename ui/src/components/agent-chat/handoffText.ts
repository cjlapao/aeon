/**
 * A step that ends in a handoff emits its closing turn as a JSON object
 * (`{"status":"ok","summary":"…","open_questions":"None."}`). That object is
 * the orchestrator's record, not something a person should read, so the chat
 * shows the summary as prose and surfaces open questions only when there are
 * any. Text that is not a handoff object is returned unchanged.
 */

const NONE = /^\s*none\.?\s*$/i;

function unescape(raw: string): string {
  try { return JSON.parse(`"${raw}"`) as string; } catch { return raw; }
}

function body(text: string): string {
  const t = text.trim();
  const fenced = /^```(?:json)?\s*([\s\S]*?)\s*(?:```)?$/.exec(t);
  return fenced?.[1] ?? t;
}

function prose(o: Record<string, unknown>): string | undefined {
  if (typeof o.summary !== "string") return undefined;
  const open = typeof o.open_questions === "string" && !NONE.test(o.open_questions) ? o.open_questions : "";
  return open ? `${o.summary}\n\n**Still open:** ${open}` : o.summary;
}

// A closing fenced object that follows ordinary prose: the model said
// something, then ended with its handoff.
const TRAILING_FENCE = /^([\s\S]*?)```(?:json)?\s*(\{[\s\S]*\})\s*```\s*$/;

/** Prose for a handoff object, or `undefined` when `text` is not one. */
export function handoffProse(text: string, streaming = false): string | undefined {
  const t = body(text);
  // The first characters of a fenced object (```, ```json) are not text yet:
  // show nothing rather than a flash of backticks.
  if (streaming && /^`{1,3}(?:j(?:s(?:o(?:n)?)?)?)?\s*$/.test(text.trim())) return "";
  if (!t.startsWith("{")) {
    const m = TRAILING_FENCE.exec(text);
    if (!m) return undefined;
    try {
      const said = prose(JSON.parse(m[2] ?? "") as Record<string, unknown>);
      if (said === undefined) return undefined;
      const lead = (m[1] ?? "").trim();
      return lead ? `${lead}\n\n${said}` : said;
    } catch { return undefined; }
  }
  try {
    return prose(JSON.parse(t) as Record<string, unknown>);
  } catch {
    // Mid-stream the object is incomplete: show the summary written so far
    // rather than a flash of raw braces.
    if (!streaming) return undefined;
    const m = /"summary"\s*:\s*"((?:[^"\\]|\\.)*)/.exec(t);
    return m ? unescape(m[1] ?? "") : "";
  }
}
