import { useEffect, useMemo, useState } from "react";
import type { TableSortState } from "../../../components/kit";
import { windowOf, type SortDir, type WindowState } from "./clientWindow";

// The shared behaviour of the Tools page's three tables: the daemon answers a whole list, so
// the sort runs over all of it and the rows are revealed in steps as the table is scrolled
// (the Agents page's infinite scroll). The rows themselves are never filtered here.

export const STEP = 40;

export interface Revealed<T> {
  rows: T[];
  total: number;
  sort: { column: string; dir: SortDir };
  reset: string;
  infinite: { hasMore: boolean; onLoadMore: () => Promise<void>; resetKey: string };
  sortState: TableSortState;
  onSortChange: (next: TableSortState | null) => void;
}

export function useReveal<T>(
  data: readonly T[],
  resetKey: string,
  firstSort: string,
  sortValue: (row: T, column: string) => string | number,
): Revealed<T> {
  const [sort, setSort] = useState<{ column: string; dir: SortDir }>({ column: firstSort, dir: "asc" });
  const [shown, setShown] = useState(STEP);
  const reset = `${resetKey}|${sort.column}|${sort.dir}`;
  useEffect(() => setShown(STEP), [reset]);
  const win: WindowState = { page: 1, pageSize: shown, sort: sort.column, dir: sort.dir };
  const answer = useMemo(() => windowOf(data, win, sortValue), [data, shown, sort.column, sort.dir]); // eslint-disable-line react-hooks/exhaustive-deps
  return {
    rows: answer.rows,
    total: answer.total,
    sort,
    reset,
    infinite: { hasMore: answer.rows.length < answer.total, onLoadMore: async () => setShown((n) => n + STEP), resetKey: reset },
    sortState: { columnId: sort.column, direction: sort.dir },
    onSortChange: (next) =>
      setSort(next === null ? { column: firstSort, dir: "asc" } : { column: next.columnId, dir: next.direction === "desc" ? "desc" : "asc" }),
  };
}
