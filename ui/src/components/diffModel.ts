// A unified diff, read into rows a view can draw. Pure: no DOM, no highlighter.

export type DiffRow =
  | { kind: "hunk"; text: string }
  | { kind: "ctx"; oldNo: number; newNo: number; text: string }
  | { kind: "add"; newNo: number; text: string }
  | { kind: "del"; oldNo: number; text: string };

const HUNK = /^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/;

/** Rows of a unified diff, headers (`diff --git`, `index`, `---`, `+++`) left out. */
export function parseUnified(diff: string): DiffRow[] {
  const rows: DiffRow[] = [];
  let oldNo = 0;
  let newNo = 0;
  // A diff with no hunk header at all (a bare list of +/- lines) is read from its first line, numbered from 1.
  const lines = diff.replace(/\n$/, "").split("\n");
  const headerless = !lines.some((l) => HUNK.test(l));
  let inHunk = headerless;
  if (headerless) { oldNo = 1; newNo = 1; }
  for (const line of lines) {
    if (headerless && /^(diff --git|index |--- |\+\+\+ )/.test(line)) continue;
    const hunk = HUNK.exec(line);
    if (hunk) { oldNo = Number(hunk[1]); newNo = Number(hunk[2]); inHunk = true; rows.push({ kind: "hunk", text: line }); continue; }
    if (!inHunk) continue; // file headers before the first hunk
    if (line.startsWith("\\")) continue; // "\ No newline at end of file"
    if (line.startsWith("+")) rows.push({ kind: "add", newNo: newNo++, text: line.slice(1) });
    else if (line.startsWith("-")) rows.push({ kind: "del", oldNo: oldNo++, text: line.slice(1) });
    else rows.push({ kind: "ctx", oldNo: oldNo++, newNo: newNo++, text: line.startsWith(" ") ? line.slice(1) : line });
  }
  return rows;
}

/** A side-by-side row: the old line on the left, the new on the right, either absent. */
export type SplitRow = { kind: "hunk"; text: string } | { kind: "line"; left?: Extract<DiffRow, { kind: "ctx" | "del" }>; right?: Extract<DiffRow, { kind: "ctx" | "add" }> };

/** Pairs each run of removed lines with the run of added lines that follows it, line for line. */
export function toSplit(rows: readonly DiffRow[]): SplitRow[] {
  const out: SplitRow[] = [];
  for (let i = 0; i < rows.length;) {
    const row = rows[i]!;
    if (row.kind === "hunk") { out.push(row); i += 1; continue; }
    if (row.kind === "ctx") { out.push({ kind: "line", left: row, right: row }); i += 1; continue; }
    const dels: Extract<DiffRow, { kind: "del" }>[] = [];
    const adds: Extract<DiffRow, { kind: "add" }>[] = [];
    while (rows[i]?.kind === "del") dels.push(rows[i++] as Extract<DiffRow, { kind: "del" }>);
    while (rows[i]?.kind === "add") adds.push(rows[i++] as Extract<DiffRow, { kind: "add" }>);
    for (let k = 0; k < Math.max(dels.length, adds.length); k += 1) out.push({ kind: "line", left: dels[k], right: adds[k] });
  }
  return out;
}

const LANGUAGES: Record<string, string> = {
  go: "go", ts: "tsx", tsx: "tsx", js: "javascript", jsx: "jsx", json: "json", md: "markdown", css: "css", html: "markup",
  yml: "yaml", yaml: "yaml", sh: "bash", rs: "rust", py: "python", sql: "sql", toml: "toml",
};

/** The highlighter's language for a path, plain text when unknown. */
export function languageOf(path: string): string {
  const ext = path.includes(".") ? path.slice(path.lastIndexOf(".") + 1).toLowerCase() : "";
  return LANGUAGES[ext] ?? "plain";
}
