// A window over a list the daemon already sent whole (S75): sort, then slice.
//
// `tools.list` and `agent.list` answer every row at once and filter on the
// daemon's side, so the page, the order and the page size are the screen's to
// keep — unlike the agents management table, whose window the daemon answers.
// The sort runs over the FULL list before the slice: sorting one page of a
// longer list would put the wrong rows on page one.

export type SortDir = "asc" | "desc";

export interface WindowState {
  page: number;
  pageSize: number;
  sort: string;
  dir: SortDir;
}

export interface WindowAnswer<T> {
  rows: T[];
  total: number;
  /** The page actually shown: the asked page clamped into 1…pages. */
  page: number;
}

export function initialWindow(sort: string, pageSize = 25): WindowState {
  return { page: 1, pageSize, sort, dir: "asc" };
}

export function windowOf<T>(
  rows: readonly T[],
  state: WindowState,
  sortValue: (row: T, column: string) => string | number,
): WindowAnswer<T> {
  const sorted = [...rows].sort((a, b) => {
    const x = sortValue(a, state.sort);
    const y = sortValue(b, state.sort);
    const c = typeof x === "number" && typeof y === "number" ? x - y : String(x).localeCompare(String(y));
    return state.dir === "desc" ? -c : c;
  });
  const pages = Math.max(1, Math.ceil(sorted.length / state.pageSize));
  const page = Math.min(Math.max(1, state.page), pages);
  const start = (page - 1) * state.pageSize;
  return { rows: sorted.slice(start, start + state.pageSize), total: sorted.length, page };
}
