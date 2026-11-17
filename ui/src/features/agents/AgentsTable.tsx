import { useEffect, useState } from "react";
import { useI18n } from "@cjlapao/ui-kit";
import {
  Button,
  EmptyState,
  Menu,
  Pill,
  Table,
  type MenuItem,
  type TableColumn,
  type TableSortState,
} from "../../components/kit";
import { rpcTyped } from "../../lib/bridge";
import type { RpcMethods } from "../../proto/contract";
import { AgentAvatar } from "./AgentFields";
import {
  INTERNAL_DELETE_REFUSAL,
  INTERNAL_EDIT_REFUSAL,
  refusalFor,
  rowReadOnly,
  type AgentRow,
} from "./agentsModel";

// The page's table: the kit's, fed by one daemon answer. It slices, sorts,
// filters and counts nothing — the pager is built from the echoed
// page/pageSize/total and the header from the echoed sort/dir (PG2; the ban is
// asserted by agentsRpc.guard.test.ts). `manualSorting` makes the kit report a
// clicked header without moving a row; `sortState` is never undefined, because
// the daemon always answers an order.
//
// The rows scroll INSIDE the card via the kit's own `fullHeight`: the card
// takes the height its parent offers and only the body's ScrollArea moves, so
// the pagination footer stays under the card and the page itself never grows a
// scrollbar. That parent offer is made above this file — the registry's
// `fill` flag (configRegistry), the shell's fill branch (PageLayout) and the
// screen's own flex column — and it is all CSS: a window resize changes the
// parent's height, and the body's height, the sticky header and the scroll
// thumb follow with no measurement anywhere.
//
// Each name cell carries the row's avatar. `agent.rows` answers no avatar
// fields, so each slug is asked of `agent.view` once (the daemon's own door for
// them — see useRowAvatars below); the row itself still renders from the ONE
// window answer.
//
// Clicking a row opens its view panel — the same door the row menu's View item
// opens, and the kit gives the row its pointer cursor for it. The ⋮ button is
// the one control inside a row: its click stops at the button (the kit's Menu
// calls the trigger's own handler before toggling), so opening the menu never
// opens a panel behind it.

/** A row's five verbs (CU3); the header's three are the screen's. */
export const ROW_VERBS = ["view", "edit", "duplicate", "export", "delete"] as const;
export type RowVerb = (typeof ROW_VERBS)[number];

/**
 * The avatar fields, from the one door that answers them: `agent.rows` carries
 * none of them, so a row's avatar is read from `agent.view` — one ask per slug,
 * the same shape loop-workspace's chat reads. Successes are kept (an avatar
 * asked for once is not asked for again this session); a failure is NOT
 * remembered, so the next visit to the page asks again rather than keeping a
 * row silently blank forever. `avatarEpoch` is the screen's write counter: any
 * successful write (the edit panel writes avatars too) clears the whole cache,
 * so the row repaints from a fresh answer instead of a stale face.
 */
type RowAvatarAnswer = RpcMethods["agent.view"]["result"];
const rowAvatars = new Map<string, RowAvatarAnswer>();
const rowAvatarsInFlight = new Set<string>();
let rowAvatarsGeneration = 0;

function useRowAvatars(slugs: readonly string[], epoch: number): Record<string, RowAvatarAnswer> {
  const [, bump] = useState(0);
  const key = slugs.join("\u0000");
  useEffect(() => {
    rowAvatars.clear();
    rowAvatarsInFlight.clear();
    rowAvatarsGeneration += 1;
  }, [epoch]);
  useEffect(() => {
    let alive = true;
    const generation = rowAvatarsGeneration;
    for (const slug of slugs) {
      if (rowAvatars.has(slug) || rowAvatarsInFlight.has(slug)) continue;
      rowAvatarsInFlight.add(slug);
      rpcTyped("agent.view", { slug })
        .then((answer) => {
          if (generation !== rowAvatarsGeneration) return;
          rowAvatars.set(slug, answer);
          if (alive) bump((n) => n + 1);
        })
        .catch(() => undefined)
        .finally(() => {
          if (generation === rowAvatarsGeneration) rowAvatarsInFlight.delete(slug);
        });
    }
    return () => {
      alive = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, epoch]);
  const out: Record<string, RowAvatarAnswer> = {};
  for (const slug of slugs) {
    const answer = rowAvatars.get(slug);
    if (answer) out[slug] = answer;
  }
  return out;
}

export interface AgentsTableProps {
  /** The rows loaded so far, in the daemon's order. */
  rows: AgentRow[];
  /** How many rows the daemon says exist for this window. */
  total: number;
  /** The search term, for the empty state's sentence. */
  q: string;
  /** The first chunk is out. */
  loading: boolean;
  hasMore: boolean;
  /** Append the next chunk; resolves once it is in `rows`. */
  onLoadMore: () => Promise<void>;
  /** Changes when the rows are replaced (a new search or order) rather than appended to. */
  resetKey: string;
  sort: string;
  dir: string;
  /** A write is out: the controls go quiet. */
  busy: boolean;
  /** Writes are refused while the connection is not current (CU2). */
  canWrite: boolean;
  /** The reason a disabled control gives — the shell's own words, not a page-invented one. */
  reason: string | undefined;
  /** The screen's write counter: any change to it clears the per-slug avatar cache. */
  avatarEpoch: number;
  onSort: (sort: TableSortState | null) => void;
  onVerb: (verb: RowVerb, row: AgentRow) => void;
}

/** The page's table: the kit's, fed by the rows loaded so far. It slices, sorts and counts nothing. */
export function AgentsTable({ rows, total, q, loading, hasMore, onLoadMore, resetKey, sort, dir, busy, canWrite, reason, avatarEpoch, onSort, onVerb }: AgentsTableProps) {
  const { t } = useI18n();
  const avatars = useRowAvatars(rows.map((row) => row.slug), avatarEpoch);

  // One flexible agent column and a fixed pair. The cell wrapper is `w-0 min-w-full`: an
  // auto-layout table column takes its content's min-content width, and a `truncate` line's
  // min-content is the WHOLE line — without the zero-width wrapper a long name stretched the
  // table past the page. A row is the name, one line of what it does, and a mark only for what
  // is the exception: "Yours" says it is not shipped. The slug, the version and the signature
  // live on the agent's own page.
  const columns: TableColumn<AgentRow>[] = [
    {
      id: "name",
      header: t("agents.col.name"),
      sortable: true,
      render: (row) => {
        const avatar = avatars[row.slug];
        return (
          <div className="w-0 min-w-full py-0.5" data-testid={`agents-cell-name-${row.slug}`}>
            <div className="flex items-center gap-3">
              {/* The reserved slot keeps every row's text on one left edge whether or not the avatar has answered. */}
              <span className="flex h-9 w-9 shrink-0 items-center justify-center" data-testid={`agents-row-avatar-${row.slug}`}>
                {avatar ? <AgentAvatar agent={avatar} compact /> : null}
              </span>
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <span className="truncate font-medium">{row.name}</span>
                  {!row.internal && row.origin !== "import" && <Pill size="xs" tone="green" data-testid={`agents-cell-internal-${row.slug}`}>{t("agents.flag.yours")}</Pill>}
                  {row.origin === "import" && <Pill size="xs" tone="blue" data-testid={`agents-cell-imported-${row.slug}`}>{t("agents.flag.imported")}</Pill>}
                  {row.origin === "import" && !row.signed && <Pill size="xs" tone="amber" data-testid={`agents-cell-unsigned-${row.slug}`}>{t("agents.flag.unsigned")}</Pill>}
                </div>
                {row.description !== "" && <div className="truncate text-sm opacity-70" title={row.description}>{row.description}</div>}
              </div>
            </div>
          </div>
        );
      },
      groupValue: (row) => row.name,
    },
    {
      id: "updatedAt",
      header: t("agents.col.updatedAt"),
      sortable: true,
      width: 190,
      // The daemon's string, unformatted: a page that re-formatted a timestamp would be an
      // opinion about a value it was only asked to display.
      render: (row) => (
        <time dateTime={row.updatedAt} className="block truncate text-xs opacity-70" title={row.updatedAt}>
          {row.updatedAt}
        </time>
      ),
      sortValue: (row) => row.updatedAt,
      groupable: false,
    },
    {
      id: "actions",
      header: t("agents.col.actions"),
      width: 44,
      align: "right",
      isActionsColumn: true,
      hideable: false,
      groupable: false,
      render: (row) => <RowMenu row={row} busy={busy} canWrite={canWrite} reason={reason} onVerb={onVerb} />,
    },
  ];

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-2" data-testid="agents-table">
      <div className="min-h-0 flex-1">
        <Table<AgentRow>
          variant="outlined"
          data={rows}
          columns={columns}
          rowKey={(row) => row.slug}
          manualSorting
          corner="rounded-sm"
          sortState={{ columnId: sort, direction: dir === "desc" ? "desc" : "asc" }}
          onSortChange={onSort}
          loading={loading}
          loadingMessage={t("agents.table.loading")}
          emptyState={<AgentsEmpty q={q} total={total} />}
          onRowClick={(row) => onVerb("view", row)}
          fullHeight
          stickyActions
          // The kit asks for the next chunk as the body nears its end, one request at a time, and
          // offers "Load more" itself when a chunk adds nothing. `resetKey` says the rows were replaced.
          infiniteScroll={{ hasMore, onLoadMore, resetKey }}
        />
      </div>
      <div className="flex shrink-0 items-center justify-between gap-3 text-sm opacity-80" data-testid="agents-footer">
        <span data-testid="agents-count">{t("agents.footer.showing", { shown: rows.length, total })}</span>
      </div>
    </div>
  );
}

/** A row's five verbs, disabled where the daemon would refuse — with its reason, not ours. */
function RowMenu({
  row,
  busy,
  canWrite,
  reason,
  onVerb,
}: {
  row: AgentRow;
  busy: boolean;
  canWrite: boolean;
  reason: string | undefined;
  onVerb: (verb: RowVerb, row: AgentRow) => void;
}) {
  const { t } = useI18n();
  const label = t("agents.row.menu", { slug: row.slug });
  const readOnly = rowReadOnly(row);
  // CU6: a read-only row's menu says WHY in the daemon's own words (the guard
  // pins them to the Go source); a menu item has no tooltip prop, so
  // `description` — the kit's per-row second line — carries the reason.
  const editReason = readOnly ? refusalFor(INTERNAL_EDIT_REFUSAL, row.slug) : reason;
  const deleteReason = readOnly ? refusalFor(INTERNAL_DELETE_REFUSAL, row.slug) : reason;

  const items: MenuItem[] = [
    { id: "view", label: t("agents.row.view"), icon: "EyeOpen", disabled: busy, command: () => onVerb("view", row) },
    {
      id: "edit",
      label: t("agents.row.edit"),
      icon: "Edit",
      disabled: busy || !canWrite || readOnly,
      description: editReason,
      command: () => onVerb("edit", row),
    },
    {
      // Duplicate is the one write a first-party row allows (AM1).
      id: "duplicate",
      label: t("agents.row.duplicate"),
      icon: "Clone",
      disabled: busy || !canWrite,
      description: readOnly ? undefined : reason,
      command: () => onVerb("duplicate", row),
    },
    { id: "export", label: t("agents.row.export"), icon: "Export", disabled: busy, command: () => onVerb("export", row) },
    { id: "gap", type: "separator" },
    {
      id: "delete",
      label: t("agents.row.delete"),
      icon: "Trash",
      danger: true,
      disabled: busy || !canWrite || readOnly,
      description: deleteReason,
      command: () => onVerb("delete", row),
    },
  ];

  return (
    // The kit's Menu portals its popup, but React bubbles through the
    // component tree — a click on a menu item would otherwise keep bubbling
    // out of here and into the row's own click, opening the view panel on top
    // of the verb just chosen. The popup's clicks stop at this boundary (in
    // the bubble phase, so the item's own command still runs); everything
    // else, the trigger included, passes through untouched.
    <span
      className="block"
      onClick={(event) => {
        if ((event.target as HTMLElement).closest('[role="menu"]')) event.stopPropagation();
      }}
    >
      <Menu
        aria-label={label}
        placement="left"
        width={320}
        items={items}
        trigger={
          <Button
            size="xs"
            variant="ghost"
            iconOnly
            leadingIcon="DotsVertical"
            aria-label={label}
            data-testid={`agents-row-menu-${row.slug}`}
            onClick={(event) => {
              // The row's own click opens the view panel; the Menu merges this
              // handler ahead of its own toggle, so the menu opens here and the
              // row click never starts.
              event.stopPropagation();
            }}
          />
        }
      />
    </span>
  );
}

/** The two honest nothings (CU3): no match for the term, and no rows at all. */
function AgentsEmpty({ q, total }: { q: string; total: number }) {
  const { t } = useI18n();
  if (q !== "") return <EmptyState title={t("agents.empty.search", { q })} data-testid="agents-empty-search" />;
  if (total > 0) return null;
  return <EmptyState title={t("agents.empty.none")} data-testid="agents-empty-none" />;
}
