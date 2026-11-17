package daemon

// charter.go — S61 T7 (GI8, P4): the charter doors over the ONE charter
// service. Four reads answer what is saved (running nothing — the
// charter's proposed arm is data here, the S56 hooks read it and this
// plane does not), and the one write appends a revision under the
// CONNECTION's person with source "person" — the source word the T1 CHECK
// admits for a socket save, never a guess at anything else.
//
// The wire body is the charter package's OWN canonical spelling
// (charter.Canonical of the parsed body), which is the byte law the
// digest computes over — re-marshalling raw column text here would be a
// second byte law.

import (
	"context"
	"strconv"

	"github.com/cjlapao/aeon/internal/autonomy"
	"github.com/cjlapao/aeon/internal/charter"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// charterDoors is the protocol.CharterService seam over the charter
// package. One service, two doors — CLI direct roads included.
type charterDoors struct {
	svc   *charter.Service
	store *store.Store
}

// newCharterDoors stands the seam up over the store; a storeless mount
// keeps the plane ABSENT (V13). The autonomy seam is the S56 plane's own
// live vocabulary — a charter body may name an autonomy word, and the
// enum is read from the plane that owns it, never cached (OD12).
func newCharterDoors(d *Daemon) *charterDoors {
	if d == nil || d.store == nil {
		return nil
	}
	return &charterDoors{
		svc: &charter.Service{
			Store: d.store,
			Autonomy: func() []string {
				v := autonomy.Vocabulary()
				out := make([]string, 0, len(v.Subjects))
				for _, s := range v.Subjects {
					if s.Kind == "action" {
						out = append(out, s.Name)
					}
				}
				return out
			},
		},
		store: d.store,
	}
}

// view converts one charter revision to the wire. The body is the
// package's canonical bytes; a body that cannot marshal is a broken row,
// and the plane says so instead of answering a half view.
func charterView(r charter.Revision) (protocol.CharterView, error) {
	body, err := charter.Canonical(r.Body)
	if err != nil {
		return protocol.CharterView{}, protocol.ErrCharterRefused.WithWhat("%v", err)
	}
	v := protocol.CharterView{
		RootID:         r.RootID,
		Revision:       r.Revision,
		Body:           body,
		Summary:        r.Summary,
		ActorKind:      r.ActorKind,
		ActorID:        r.ActorID,
		Source:         r.Source,
		SourceRef:      r.SourceRef,
		CreatedAt:      r.CreatedAt,
		TotalRevisions: r.TotalRevisions,
	}
	if d := charter.Describe(r); d.Summary != "" || len(d.Lines) > 0 {
		v.Describe = &protocol.CharterDescription{Summary: d.Summary, Lines: d.Lines}
	}
	if p := r.Body.Proposed; p != nil {
		v.Proposed = &protocol.CharterProposedWire{
			BudgetUsd:      p.BudgetUSD,
			Verify:         append([]string(nil), p.Verify...),
			EnvironmentRef: p.EnvironmentRef,
		}
	}
	return v, nil
}

func (c *charterDoors) Get(ctx context.Context, p protocol.CharterGetParams) (protocol.CharterView, error) {
	if p.Revision != nil && *p.Revision > 0 {
		// A revision out of history: Revisions() is the store's own list,
		// newest first; the requested number is found or refused by name.
		revs, err := c.svc.Revisions(ctx, p.RootID)
		if err != nil {
			return protocol.CharterView{}, protocol.ErrCharterRefused.WithWhat("%v", err)
		}
		for _, r := range revs {
			if r.Revision == *p.Revision {
				return charterView(r)
			}
		}
		return protocol.CharterView{}, protocol.ErrCharterRefused.WithWhat(
			"charter.get: %s has no revision %d — charter.revisions answers which revisions exist", p.RootID, *p.Revision)
	}
	r, err := c.svc.Get(ctx, p.RootID)
	if err != nil {
		return protocol.CharterView{}, protocol.ErrCharterRefused.WithWhat("%v", err)
	}
	return charterView(r)
}

func (c *charterDoors) List(ctx context.Context) (protocol.CharterListResult, error) {
	revs, err := c.svc.List(ctx)
	if err != nil {
		return protocol.CharterListResult{}, protocol.ErrCharterRefused.WithWhat("%v", err)
	}
	// Names ride the component rows; a root whose component was deleted
	// keeps its charter and answers with an empty name — the row is the
	// fact, a name is decoration the list does not invent.
	comps, err := c.store.ListComponents(store.ComponentFilter{IncludeArchived: true})
	if err != nil {
		return protocol.CharterListResult{}, protocol.ErrCharterRefused.WithWhat("%v", err)
	}
	names := make(map[string]string, len(comps))
	for _, comp := range comps {
		names[comp.ID] = comp.Name
	}
	out := protocol.CharterListResult{Charters: make([]protocol.CharterListEntry, 0, len(revs))}
	for _, r := range revs {
		name := names[r.RootID]
		entry := protocol.CharterListEntry{
			RootID:    r.RootID,
			Name:      name,
			Revision:  r.Revision,
			Summary:   charter.Describe(r).Summary,
			UpdatedAt: r.CreatedAt,
		}
		if r.Body.Proposed != nil {
			entry.HasProposed = true
		}
		out.Charters = append(out.Charters, entry)
	}
	return out, nil
}

func (c *charterDoors) Revisions(ctx context.Context, p protocol.CharterRevisionsParams) (protocol.CharterRevisionsResult, error) {
	limit := 20
	if p.Limit != nil && *p.Limit > 0 && *p.Limit <= 100 {
		limit = *p.Limit
	}
	revs, err := c.svc.Revisions(ctx, p.RootID)
	if err != nil {
		return protocol.CharterRevisionsResult{}, protocol.ErrCharterRefused.WithWhat("%v", err)
	}
	// Newest first (the store's order), paged BELOW the cursor revision.
	var page []charter.Revision
	for _, r := range revs {
		if p.Cursor != nil && r.Revision >= *p.Cursor {
			continue
		}
		if len(page) >= limit {
			break
		}
		page = append(page, r)
	}
	out := protocol.CharterRevisionsResult{}
	if len(page) == limit {
		// The page was full; anything strictly below its last revision is
		// the next page — the cursor is "page below this revision".
		out.NextCursor = strconv.Itoa(page[len(page)-1].Revision)
	}
	for _, r := range page {
		v, err := charterView(r)
		if err != nil {
			return protocol.CharterRevisionsResult{}, err
		}
		out.Revisions = append(out.Revisions, v)
	}
	return out, nil
}

func (c *charterDoors) Vocabulary(_ context.Context) (protocol.CharterVocabularyResult, error) {
	v := charter.Vocabulary(c.svc.Autonomy)
	out := protocol.CharterVocabularyResult{
		CheckKinds:   v.CheckKinds,
		Autonomy:     v.Autonomy,
		AutonomyNote: "the autonomy words are the S56 plane's live vocabulary; a charter body names one of these in proposed.autonomy or is refused — this plane grants nothing (GI8)",
	}
	out.Caps = protocol.CharterCaps{
		BodyBytes:   v.Caps.BodyBytes,
		Goal:        v.Caps.Goal,
		Item:        v.Caps.Item,
		List:        v.Caps.List,
		Acceptance:  v.Caps.Acceptance,
		Tags:        v.Caps.Tags,
		Tag:         v.Caps.Tag,
		Verify:      v.Caps.Verify,
		VerifyBytes: v.Caps.VerifyBytes,
		EnvRef:      v.Caps.EnvRef,
		Note:        v.Caps.Note,
	}
	return out, nil
}

func (c *charterDoors) Save(ctx context.Context, actor protocol.Actor, p protocol.CharterSaveParams) (protocol.CharterView, error) {
	// The charter package is the ONE validator: Parse refuses what the
	// schema does not carry, Validate refuses what the caps do not hold,
	// and the refusal's sentence rides the door's own voice (DB19).
	body, err := charter.Parse(p.Body)
	if err != nil {
		return protocol.CharterView{}, protocol.ErrCharterRefused.WithWhat("%v", err)
	}
	rev, ref, err := c.svc.Save(ctx, store.Actor{Kind: actor.Kind, ID: actor.ID}, p.RootID, body, p.Note, "person", "")
	if err != nil {
		return protocol.CharterView{}, protocol.ErrCharterRefused.WithWhat("%v", err)
	}
	if ref != nil {
		return protocol.CharterView{}, protocol.ErrCharterRefused.WithWhat("%s", ref.Sentence)
	}
	return charterView(rev)
}
