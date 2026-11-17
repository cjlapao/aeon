import { useEffect, useMemo, useState } from "react";
import { EmptyState, Pill, Table, type TableColumn, type TableSortState } from "../../../components/kit";
import type { SkillView } from "../../../lib/skills";
import { windowOf, type WindowState } from "../tools/clientWindow";

// The catalog as the Agents page shows its rows: one table that fills the height and reveals more
// as it is scrolled. The daemon answers the whole catalog, so the order and the reveal are this
// table's; the rows, the verdicts and the issues are the daemon's, shown as it sent them.

const STEP = 40;
const TONES = { valid: "green", warn: "amber", invalid: "red" } as const;
const WORDS = { valid: "valid", warn: "valid, with notes", invalid: "refused" } as const;

export const skillKey = (s: SkillView): string => `${s.source}:${s.path}`;

function sortValue(s: SkillView, column: string): string {
  switch (column) {
    case "status": return s.status;
    case "source": return s.source;
    case "files": return String(s.files.scripts + s.files.references + s.files.assets).padStart(6, "0");
    default: return s.name;
  }
}

export interface SkillsTableProps {
  rows: SkillView[];
  loading: boolean;
  filtered: boolean;
  resetKey: string;
  onOpen: (skill: SkillView) => void;
}

export function SkillsTable({ rows, loading, filtered, resetKey, onOpen }: SkillsTableProps) {
  const [sort, setSort] = useState<{ column: string; dir: "asc" | "desc" }>({ column: "name", dir: "asc" });
  const [shown, setShown] = useState(STEP);
  const reset = `${resetKey}|${sort.column}|${sort.dir}`;
  useEffect(() => setShown(STEP), [reset]);
  const win: WindowState = { page: 1, pageSize: shown, sort: sort.column, dir: sort.dir };
  const answer = useMemo(() => windowOf(rows, win, sortValue), [rows, shown, sort.column, sort.dir]); // eslint-disable-line react-hooks/exhaustive-deps

  const columns: TableColumn<SkillView>[] = [
    {
      id: "name",
      groupable: false,
      header: "Skill",
      sortable: true,
      sortValue: (s) => s.name,
      render: (s) => (
        <div className="w-0 min-w-full py-1" data-testid={`skill-${s.name}`} data-source={s.source}>
          <code className="block truncate text-sm font-semibold">{s.name}</code>
          <div className="truncate text-xs opacity-70" title={s.description}>
            {s.description || "No description — see the issues in the skill."}
          </div>
        </div>
      ),
    },
    {
      id: "status",
      groupable: false,
      header: "Verdict",
      sortable: true,
      width: 170,
      render: (s) => (
        <span className="flex flex-wrap gap-1">
          <Pill size="sm" tone={TONES[s.status]} data-testid={`status-${s.name}`}>{WORDS[s.status]}</Pill>
          {s.shadowed && <Pill size="sm" tone="slate" data-testid={`shadowed-${s.name}`}>shadowed</Pill>}
        </span>
      ),
    },
    {
      id: "source",
      groupable: false,
      header: "Folder",
      sortable: true,
      width: 120,
      render: (s) => <Pill size="sm" tone="slate" data-testid={`source-${s.name}`}>{s.source === "managed" ? "managed" : "yours"}</Pill>,
    },
    {
      id: "files",
      groupable: false,
      header: "Files",
      sortable: true,
      width: 230,
      render: (s) => (
        <span className="block truncate text-xs opacity-80">
          {s.files.scripts} scripts · {s.files.references} references · {s.files.assets} assets
        </span>
      ),
    },
    {
      id: "issues",
      groupable: false,
      header: "Issues",
      width: 100,
      align: "right",
      render: (s) => <span className="text-xs opacity-80">{s.issues?.length ? s.issues.length : "—"}</span>,
    },
  ];

  return (
    <div className="flex min-h-[20rem] flex-1 flex-col gap-2" data-testid="skill-rows">
      <div className="min-h-[18rem] flex-1">
        <Table<SkillView>
          variant="outlined"
          data={answer.rows}
          columns={columns}
          rowKey={skillKey}
          manualSorting
          corner="rounded-sm"
          sortState={{ columnId: sort.column, direction: sort.dir }}
          onSortChange={(next: TableSortState | null) =>
            setSort(next === null ? { column: "name", dir: "asc" } : { column: next.columnId, dir: next.direction === "desc" ? "desc" : "asc" })
          }
          loading={loading}
          loadingMessage="Reading the skills…"
          emptyState={
            filtered
              ? <EmptyState title="No skill matches." data-testid="skills-empty-filter" />
              : <EmptyState title="No skills yet. Install one, or drop a folder with a SKILL.md into one of the two skill folders." data-testid="skills-empty" />
          }
          onRowClick={onOpen}
          fullHeight
          infiniteScroll={{ hasMore: answer.rows.length < answer.total, onLoadMore: async () => setShown((n) => n + STEP), resetKey: reset }}
        />
      </div>
      <div className="shrink-0 text-sm opacity-80" data-testid="skills-count">
        Showing {answer.rows.length} of {answer.total} skills
      </div>
    </div>
  );
}
