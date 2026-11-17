/**
 * Split a loop document into its YAML header and body.
 *
 * A deliberate mirror of the MCP's `parseFrontmatter` — same subset, same
 * rejection rules — because the two must agree on what counts as a header. If the
 * UI parsed something the writer would not, a document would render metadata that
 * the loop does not believe it has.
 *
 * The subset is flat `key: value` only. Anything it cannot represent (nesting, a
 * list item, an unterminated fence) invalidates the WHOLE header rather than being
 * partially shown: half a header is worse than none, because it looks complete.
 */
export interface ParsedDocument {
  data: Record<string, string>;
  body: string;
  had: boolean;
}

export function parseFrontmatter(text: string): ParsedDocument {
  if (!text.startsWith("---")) return { data: {}, body: text, had: false };

  // Tolerate CRLF without rewriting the body's own line endings.
  const nl = text.indexOf("\n");
  if (nl === -1 || text.slice(0, nl).trim() !== "---") {
    return { data: {}, body: text, had: false };
  }

  const lines = text.split("\n");
  let end = -1;
  for (let i = 1; i < lines.length; i++) {
    if (lines[i]!.trim() === "---") {
      end = i;
      break;
    }
  }
  if (end === -1) return { data: {}, body: text, had: false };

  const data: Record<string, string> = {};
  for (let i = 1; i < end; i++) {
    const line = lines[i]!;
    if (!line.trim() || line.trimStart().startsWith("#")) continue;
    const colon = line.indexOf(":");
    if (colon === -1 || /^\s/.test(line) || line.trimStart().startsWith("- ")) {
      return { data: {}, body: text, had: false };
    }
    const key = line.slice(0, colon).trim();
    let value = line.slice(colon + 1).trim();
    if (
      (value.startsWith('"') && value.endsWith('"') && value.length > 1) ||
      (value.startsWith("'") && value.endsWith("'") && value.length > 1)
    ) {
      try {
        value = value.startsWith('"') ? (JSON.parse(value) as string) : value.slice(1, -1);
      } catch {
        value = value.slice(1, -1);
      }
    }
    if (key) data[key] = value;
  }

  // Drop exactly one blank line after the fence, matching the writer.
  let bodyStart = end + 1;
  if (lines[bodyStart] === "" || lines[bodyStart] === "\r") bodyStart += 1;
  return { data, body: lines.slice(bodyStart).join("\n"), had: true };
}

/** True for a value the loop writes as an ISO instant. */
export function isIsoDate(value: string): boolean {
  return /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}/.test(value) && !Number.isNaN(Date.parse(value));
}
