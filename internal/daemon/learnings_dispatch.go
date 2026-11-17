package daemon

import (
	"github.com/cjlapao/aeon/internal/learnings"
	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/tools"
)

// initLearnings wires the learnings' daemon-side seam (S28 T4): the one
// internal/learnings instance the socket and the CLI speak through (P34's
// ordering: the capability rides the socket first; the CLI and the bench
// are secondary clients). The scope validation (design §3) and the relayed
// write-protection (M1) live in that one place — the seam's one Append — so
// a second authoring path cannot appear. A nil store is a no-op, loud at
// the handler (ErrStoreUnavailable), never a silent default.
//
// The embedding route is resolved here, exactly the way cmd/aeon's retrieval
// link resolves it (tools.EmbedRoute over the same protocol.Routes): the
// first route by id that speaks "embedding". With a route, learnings.query
// ranks the tag-filtered set by embedding similarity within it (D6); without
// one, SetRoute is never called, the store stays route==nil, and the set is
// returned unranked — the normal, working state (D5), not a fallback. A nil
// route is the normal state, so the guard is on the interface, not a
// fabricated default.
func (d *Daemon) initLearnings() {
	if d.Table == nil || d.store == nil {
		return
	}
	l := learnings.New(d.store)
	if d.Table.Routes != nil {
		if route := tools.EmbedRoute(d.Table.Routes); route != nil {
			l.SetRoute(route, learnings.ModelClientEmbedder{C: &model.Client{}})
		}
	}
	d.Table.Learnings = l
}
