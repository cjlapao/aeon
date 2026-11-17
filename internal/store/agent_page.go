// agent_page.go — S73 T2 and T5: the paged windows the agents management page
// asks for, and the two adopters of the daemon's paging convention
// (docs/aeon/PAGING.md, properties PG1–PG5).
//
// # Why this is beside agent.go and not inside it
//
// agent.go owns the row — its shape, its writes, its single-row reads. This
// file owns one question about many rows: give me page N, in an order a person
// chose (or the one order the ledger offers), filtered by what they typed or by
// which agent, and tell me how many the filter leaves. A reader who came to
// change the row shape has no business reading a window query, and vice versa.
//
// T5 added the ledger's window to THIS file rather than opening a third one,
// and the reason is the same one that moved the row out of the window: a reader
// who came to change how a page answers — the count before the window, the
// clamp, the tiebreak, the empty-but-not-error — should find both examples in
// one place, because they are the same convention applied to two tables. The
// file is "the paged reads of the agent plane", which is one concern; a third
// window would be a fourth concern and would split.
//
// # The two numbers, and why they are two
//
// The window answers the rows; the COUNT answers "how many are there". The
// count is of the FILTERED set and it is taken BEFORE the window, in the same
// call with the same WHERE (PG3). A count without the filter is a number that
// disagrees with the table underneath it; a count of the window is the length
// of the answer wearing a different name.
//
// # Nothing the caller typed reaches the SQL text
//
// The order by is composed from paging.Request.Column — a lookup into
// AgentRowBounds.Sorts, whose values are the literals written below — plus one
// of two fixed direction words. The search term reaches SQL only as a bound
// parameter, escaped by paging.Like, with ESCAPE '\' agreeing with it. A sort
// the map does not hold is refused here, naming it, before any query runs
// (PG4). The ledger's window has no order to compose: `ORDER BY id DESC` is
// literal text, and its slug filter is a bound parameter — so the one place a
// caller's word could reach the SQL is the place that refuses to read one.

package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/cjlapao/aeon/internal/paging"
)

// AgentRow is one row of the management window: the nine columns a list can
// honestly hold. Deliberately NOT the Agent struct — `prompt`, `skills`,
// `metadata` and the dispatch columns stay out of the list, because the list is
// a chooser and a chooser that ships every row's content is a page that loads
// the whole store into the browser.
//
// The omission is load-bearing in the other direction too: agentmgr.Edit
// replaces nine content fields wholesale, so an edit form seeded from THIS row
// would blank the fields it never showed. The panel therefore opens a row with
// agent.view (S73 AR5), and this type's comment is where a future reader learns
// why that is not laziness.
type AgentRow struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version"`
	Internal    bool   `json:"internal"`
	Signed      bool   `json:"signed"`
	Origin      string `json:"origin"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
}

// AgentRowBounds is the convention's bounds for this window, and the ONE
// literal of it: the protocol door and the seam both normalise through here, so
// the words a client may sort by, the columns they mean, and the clamp they
// land on have a single source. The keys are the wire vocabulary; the values are
// columns of the `agent` table as migration 20261030130000_agent.sql declares
// them.
var AgentRowBounds = paging.Bounds{
	DefaultSize: 20,
	MaxSize:     100,
	DefaultSort: "name",
	MaxQuery:    paging.DefaultMaxQuery,
	Sorts: map[string]string{
		"name":      "name",
		"slug":      "slug",
		"version":   "version",
		"internal":  "internal",
		"signed":    "signed",
		"createdAt": "created_at",
		"updatedAt": "updated_at",
	},
}

// agentRowColumns mirrors AgentRow, in its order, as one SELECT list.
const agentRowColumns = `slug, name, description, version, internal, signed, origin, created_at, updated_at`

// AgentPage answers one window of the agent rows.
//
// `req` arrives already Normalised (paging.Request.Normalise) — this function
// takes the arithmetic as decided rather than re-deciding it, so the numbers in
// the response are the numbers that produced the rows. It returns the rows, the
// filtered total, and an error. An unknown sort name is that error, naming the
// name (PG4); an empty window is NOT an error (PG3).
func (s *Store) AgentPage(ctx context.Context, req paging.Request) ([]AgentRow, int, error) {
	col, dir, err := req.Column(AgentRowBounds)
	if err != nil {
		return nil, 0, err
	}

	// One WHERE, two queries. Building it once is how the count cannot drift
	// from the window it counts (PG3).
	var (
		where string
		args  []any
	)
	if pattern := paging.Like(req.Q); pattern != "" {
		where = `WHERE (slug LIKE ? ESCAPE '\' OR name LIKE ? ESCAPE '\' OR description LIKE ? ESCAPE '\')`
		// One escaped term, bound three times: the same search over the three
		// columns a person would expect a name search to reach.
		args = []any{pattern, pattern, pattern}
	}

	var total int
	if err := s.r.QueryRowContext(ctx, `SELECT count(*) FROM agent `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("store: agent page count: %w", err)
	}

	// The tiebreak is not decoration: two rows may share a name (a duplicate
	// does, AM2), and without a stable second key the database is free to order
	// them differently between two calls — which a person sees as rows moving
	// when they turn a page.
	query := fmt.Sprintf(`SELECT %s FROM agent %s ORDER BY %s %s, slug ASC LIMIT ? OFFSET ?`,
		agentRowColumns, where, col, dir)
	rows, err := s.r.QueryContext(ctx, query, append(append([]any{}, args...), req.PageSize, req.Offset())...)
	if err != nil {
		return nil, 0, fmt.Errorf("store: agent page: %w", err)
	}
	defer rows.Close()

	out := make([]AgentRow, 0, req.PageSize)
	for rows.Next() {
		var (
			a                AgentRow
			internal, signed int
		)
		if err := rows.Scan(&a.Slug, &a.Name, &a.Description, &a.Version,
			&internal, &signed, &a.Origin, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("store: agent page scan: %w", err)
		}
		a.Internal = internal == 1
		a.Signed = signed == 1
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("store: agent page rows: %w", err)
	}
	return out, total, nil
}

// AgentChangeBounds is the ledger window's bounds. `Sorts` is empty on purpose:
// a history has ONE order — newest first — and a sort the daemon offers must be
// a sort the daemon can answer, so this read offers none and the door refuses
// `sort`/`dir` by name rather than accepting them and ignoring them. With no
// vocabulary, `DefaultSort` is empty too; nothing in this read calls
// paging.Request.Column, and the ORDER BY below is literal text.
var AgentChangeBounds = paging.Bounds{
	DefaultSize: 20,
	MaxSize:     100,
	MaxQuery:    paging.DefaultMaxQuery,
}

// AgentChangesPage answers one window of the agent ledger, newest first.
//
// `slug` narrows to one agent; the empty string means the whole ledger, which is
// what the History tab's "everything" toggle asks for and the only narrowing this
// read offers. No sort, no direction, no search term: those are refused at the
// door (AR6), and the shape stays honest about it — this function cannot ask for
// an order it does not implement.
//
// Two shapes are worth stating, because both are the truth rather than an error:
// a slug with no rows answers an empty window and a total of 0 (a person who
// deleted an agent and asks for its history may well be asking "did anybody touch
// this?"), and the unfiltered answer CONTAINS the `migrate` run's row, whose slug
// is NULL (AR1's one NULL) — so `AgentChange.Slug` is a `*string` all the way to
// the wire, and a slug-filtered query never invents that row. No join to `agent`:
// the ledger says what was true when it was written, and a slug whose row is gone
// still has a history (T5's own "do not").
func (s *Store) AgentChangesPage(ctx context.Context, slug string, req paging.Request) ([]AgentChange, int, error) {
	var (
		where string
		args  []any
	)
	if slug != "" {
		where = `WHERE slug = ?`
		args = []any{slug}
	}

	var total int
	if err := s.r.QueryRowContext(ctx, `SELECT count(*) FROM agent_change `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("store: agent changes count: %w", err)
	}

	// id is the ledger's own arrival order, and DESC is the only order this read
	// has: a person scanning a history reads the newest thing first. There is no
	// tiebreak to add — id is the primary key — which is also why no page can
	// shuffle between two calls.
	query := `SELECT id, op, slug, summary, actor_kind, actor_id, occurred_at
	          FROM agent_change ` + where + ` ORDER BY id DESC LIMIT ? OFFSET ?`
	rows, err := s.r.QueryContext(ctx, query, append(append([]any{}, args...), req.PageSize, req.Offset())...)
	if err != nil {
		return nil, 0, fmt.Errorf("store: agent changes: %w", err)
	}
	defer rows.Close()

	out := make([]AgentChange, 0, req.PageSize)
	for rows.Next() {
		var (
			c   AgentChange
			nul sql.NullString
		)
		if err := rows.Scan(&c.ID, &c.Op, &nul, &c.Summary, &c.ActorKind, &c.ActorID, &c.OccurredAt); err != nil {
			return nil, 0, fmt.Errorf("store: agent changes scan: %w", err)
		}
		if nul.Valid {
			v := nul.String
			c.Slug = &v
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("store: agent changes rows: %w", err)
	}
	return out, total, nil
}
