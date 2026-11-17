package main

// agent_rows.go — S73 T2 and T5: the two paged DOORS of the agent plane,
// `agent.rows` and `agent.changes`, and the door half of the paging convention
// (docs/aeon/PAGING.md). The file's concern is "the doors that page", so T5's
// sibling joined it instead of opening a third file: the closed key vocabulary,
// the two error voices, and the map-before-the-struct walk that lets a refusal
// name the key it found are one body of reasoning, and a second copy of it in a
// second file is a second place to get wrong.
//
// # What lives here, as opposed to internal/store/agent_page.go
//
// The store answers a window of rows and a filtered count; this file decides
// what the wire asked for. That is three jobs, and each has a place it is done
// in once: the CLAMP and the defaults are `internal/paging` (the arithmetic is
// the convention's, not this method's), the SORT NAME resolves through the
// store's own bounds map (the columns belong to the table), and the JSON shape
// is here, because it is this method's contract.
//
// # Unknown keys are ignored. One closed set is refused.
//
// conformance/README.md's T3 is a protocol rule, not a style: an unknown field
// in params or in a result must be IGNORED, because a client that rejects one
// turns every additive change into a breaking one (`forward-compat.json` is the
// standing test of it, and it is un deletable). So this door does not use
// DisallowUnknownFields, however tempting that is — a future client may add
// `pageFromTheFuture` to this request and must succeed.
//
// What IS refused is a CLOSED vocabulary of keys whose silence would be a lie:
// `limit`, `cursor`, `offset`, `order`, `search`, and the actor pair. They are
// not unknown-key rejection, they are the same move S59 makes with its identity
// vocabulary (`rejectMCPIdentityKeys`) — a small named set, refused before
// semantic validation, because a client that sent `limit: 5000` and got a 20-row
// page back believes it asked for 20 when it asked for a different question
// entirely. Everything else is ignored, and the day a key deserves to become
// part of the convention it joins PG1's five in one place: the convention doc.
//
// # The two error voices
//
// A paging shape error (a bad `dir`, a `q` past the cap, a sort that is not
// published, a key of the wrong JSON type) is `protocol.ErrBadParams`, which
// dispatch maps to JSON-RPC −32602 — the voice the daemon already uses for
// "that is not one of the values" (internal/daemon/agent_mailbox.go:551 is a
// seam returning it). What the STORE failed at is a plain error, which is
// infrastructure and never dressed as a refusal, and what the domain refuses
// keeps the `refusal string` channel this family already answers with. A page
// past the end of the data is none of these: it is rows: [] and the honest
// total (PG3).

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/cjlapao/aeon/internal/paging"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// agentRowsParams is the wire's five keys, all optional. Pointers are not
// fussiness: `{"page": null}` and an absent `page` are different documents,
// and a paging read should answer both with the default rather than with 0.
type agentRowsParams struct {
	Page     *int    `json:"page"`
	PageSize *int    `json:"pageSize"`
	Sort     *string `json:"sort"`
	Dir      *string `json:"dir"`
	Q        *string `json:"q"`
}

// agentRowsResult is the answer: the rows, and the request as the daemon
// applied it, plus the filtered total. paging.Answer is embedded, so page,
// pageSize and total sit beside `agents` on the wire rather than nested under
// a word the UI would have to know.
type agentRowsResult struct {
	Agents []store.AgentRow `json:"agents"`
	paging.Answer
	Sort string `json:"sort"`
	Dir  string `json:"dir"`
	Q    string `json:"q"`
}

// pagedReadRefusedKeys is the closed vocabulary a paged read refuses by name.
// It is not "unknown keys": the keys below share one property, which is that
// they would be read as a paging question and silently dropped — so the caller
// believes it asked for something the daemon never did. Anything outside this
// set is ignored, per T3's forward-compat rule.
var pagedReadRefusedKeys = []string{"limit", "cursor", "offset", "order", "search", "actor", "actorKind"}

// decodeAgentRowsParams decodes the five keys, refuses the closed set, and
// leaves every other field alone (T3). The params are walked as a map first so
// the refusal can name the key rather than guess it from a decode error.
func decodeAgentRowsParams(params []byte) (paging.Request, error) {
	trimmed := bytes.TrimSpace(params)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		// `{}`, an absent params block and `null` all mean "the defaults".
		return paging.Request{}, nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &raw); err != nil {
		return paging.Request{}, protocol.ErrBadParams{Err: fmt.Errorf(
			"agent.rows' parameters are an object of page, pageSize, sort, dir and q — %v", err)}
	}
	for _, key := range pagedReadRefusedKeys {
		if _, sent := raw[key]; sent {
			return paging.Request{}, protocol.ErrBadParams{Err: fmt.Errorf(
				"%q is not a parameter of agent.rows — this read pages by page, pageSize, sort, dir and q, and %s",
				key, refusedKeyAlternative(key))}
		}
	}
	var p agentRowsParams
	if err := json.Unmarshal(trimmed, &p); err != nil {
		return paging.Request{}, protocol.ErrBadParams{Err: fmt.Errorf(
			"agent.rows' parameters are the wrong shape — %v", err)}
	}
	req := paging.Request{}
	if p.Page != nil {
		req.Page = *p.Page
	}
	if p.PageSize != nil {
		req.PageSize = *p.PageSize
	}
	if p.Sort != nil {
		req.Sort = *p.Sort
	}
	if p.Dir != nil {
		req.Dir = *p.Dir
	}
	if p.Q != nil {
		req.Q = *p.Q
	}
	return req, nil
}

// agentRows answers agent.rows through the store's window. It is a method
// body, not a handler: protocol holds the one seam and passes the params
// through, exactly as it does for the rest of this family.
func agentRows(ctx context.Context, st *store.Store, params json.RawMessage) (any, string, error) {
	req, err := decodeAgentRowsParams(params)
	if err != nil {
		return nil, "", err
	}
	req, err = req.Normalise(store.AgentRowBounds)
	if err != nil {
		return nil, "", protocol.ErrBadParams{Err: err}
	}
	// Resolved HERE as well as in the store, and deliberately twice: the sort
	// name is a wire vocabulary, so the door refuses a name outside it with
	// the bad-params voice, while the store keeps its own check because it is
	// the one that knows the columns. A door that trusted its store to
	// classify a client's input would answer −32603 for what is the caller's
	// typo.
	if _, _, err := req.Column(store.AgentRowBounds); err != nil {
		return nil, "", protocol.ErrBadParams{Err: err}
	}

	rows, total, err := st.AgentPage(ctx, req)
	if err != nil {
		return nil, "", err
	}
	return agentRowsResult{
		Agents: rows,
		Answer: paging.NewAnswer(req, total),
		Sort:   req.Sort,
		Dir:    req.Dir,
		Q:      req.Q,
	}, "", nil
}

// refusedKeyAlternative names the question the refused key was asking, so the
// refusal is a fix and not just a no. A refusal that cannot be acted on is a
// dead end, and the daemon's error voice has three parts for a reason.
func refusedKeyAlternative(key string) string {
	switch key {
	case "limit":
		return "pageSize is how many rows a page holds (20 by default, 100 at most)"
	case "cursor":
		return "this read pages by page number and answers the total, so a cursor is not what it can offer"
	case "offset":
		return "page is 1-based and the daemon does the arithmetic"
	case "order":
		return "dir carries \"asc\" or \"desc\", and sort names the column"
	case "search":
		return "q is the search term"
	case "actor", "actorKind":
		return "the actor is the connection's peer credential, never a parameter (and a read stamps nobody)"
	}
	return "the five keys named above are all this read takes"
}

// ---------------------------------------------------------------------------
// S73 T5 — the ledger's door. `agent.changes` is the convention's second
// adopter, and the reason this half sits below the first rather than in a new
// file is the file's own concern: "the doors that page". Two of them now.
// ---------------------------------------------------------------------------

// agentChangesParams is the wire's three keys, all optional. `slug` is a
// pointer for the same reason `page` is: `{"slug": null}`, an absent `slug` and
// `{"slug": ""}` all mean "the whole ledger", and a history read should answer
// all three the same way rather than treating one as a filter on the empty
// string — which would match no row and report an empty history for every
// agent, the most convincing wrong answer this plane could give.
type agentChangesParams struct {
	Page     *int    `json:"page"`
	PageSize *int    `json:"pageSize"`
	Slug     *string `json:"slug"`
}

// agentChangesResult is the answer: the ledger's window, the two paging keys as
// the daemon applied them, the filtered total, and the slug the window was
// narrowed to (echoed, so a caller that asked for one agent and got a page
// cannot be shown someone else's history by a routing bug).
//
// `slug` on a ROW is a `*string` and stays one on the wire: AR1's `migrate` run
// has no single agent to name, its ledger row carries NULL, and only the
// whole-ledger window shows it. The app's type gets that nullability from this
// contract rather than from a guess — which is what the fixture carrying it is
// for.
type agentChangesResult struct {
	Changes []store.AgentChange `json:"changes"`
	paging.Answer
	Slug string `json:"slug"`
}

// ledgerRefusedKeys is the first door's closed set plus the three keys this
// read has no way to honour. That is the same lie-avoidance, sharpened: a
// history has ONE order and ONE filter, so `sort`, `dir` and `q` are not
// paging keys here, they are questions with no answer — and a door that
// accepted them would report a history that looks searched and is not.
// Everything OUTSIDE this set is still ignored, unconditionally (T3).
var ledgerRefusedKeys = []string{
	"limit", "cursor", "offset", "order", "search", "actor", "actorKind",
	"sort", "dir", "q",
}

// decodeAgentChangesParams is the ledger door's shape step: the same walk as
// its sibling's (map first, so the refusal names the key it found), a wider
// closed set, and three keys decoded.
func decodeAgentChangesParams(params []byte) (paging.Request, string, error) {
	trimmed := bytes.TrimSpace(params)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return paging.Request{}, "", nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &raw); err != nil {
		return paging.Request{}, "", protocol.ErrBadParams{Err: fmt.Errorf(
			"agent.changes' parameters are an object of page, pageSize and slug — %v", err)}
	}
	for _, key := range ledgerRefusedKeys {
		if _, sent := raw[key]; sent {
			return paging.Request{}, "", protocol.ErrBadParams{Err: fmt.Errorf(
				"%q is not a parameter of agent.changes — this read pages by page and pageSize and narrows by slug, and %s",
				key, ledgerRefusedKeyAlternative(key))}
		}
	}
	var p agentChangesParams
	if err := json.Unmarshal(trimmed, &p); err != nil {
		return paging.Request{}, "", protocol.ErrBadParams{Err: fmt.Errorf(
			"agent.changes' parameters are the wrong shape — %v", err)}
	}
	req := paging.Request{}
	if p.Page != nil {
		req.Page = *p.Page
	}
	if p.PageSize != nil {
		req.PageSize = *p.PageSize
	}
	slug := ""
	if p.Slug != nil {
		slug = *p.Slug
	}
	return req, slug, nil
}

// agentChanges answers agent.changes through the store's ledger window.
func agentChanges(ctx context.Context, st *store.Store, params json.RawMessage) (any, string, error) {
	req, slug, err := decodeAgentChangesParams(params)
	if err != nil {
		return nil, "", err
	}
	// The bounds carry no sort vocabulary, and Normalise still owns the clamp,
	// the defaults and the two refusals it makes (a `dir` that is not a
	// direction, a `q` past the cap). A request reaching here with a sort has
	// already been refused above; this call is what makes the echo honest.
	req, err = req.Normalise(store.AgentChangeBounds)
	if err != nil {
		return nil, "", protocol.ErrBadParams{Err: err}
	}
	rows, total, err := st.AgentChangesPage(ctx, slug, req)
	if err != nil {
		return nil, "", err
	}
	return agentChangesResult{
		Changes: rows,
		Answer:  paging.NewAnswer(req, total),
		Slug:    slug,
	}, "", nil
}

// ledgerRefusedKeyAlternative is refusedKeyAlternative for the read that has
// one order: the same three-part error voice (what you sent, why it is not
// here, what to send instead), with the middle clause changed because the
// answer to "can I sort it?" is not "no, use sort" — it is "it has one order".
func ledgerRefusedKeyAlternative(key string) string {
	switch key {
	case "limit":
		return "pageSize is how many rows a page holds (20 by default, 100 at most)"
	case "cursor":
		return "this read pages by page number and answers the total, so a cursor is not what it can offer"
	case "offset":
		return "page is 1-based and the daemon does the arithmetic"
	case "sort", "dir", "order":
		return "a history has one order — newest first — and a sort the daemon cannot honour is not offered"
	case "search", "q":
		return "slug narrows to one agent; this read searches no text"
	case "actor", "actorKind":
		return "the actor is the connection's peer credential, never a parameter (and a read stamps nobody)"
	}
	return "page, pageSize and slug are all this read takes"
}
