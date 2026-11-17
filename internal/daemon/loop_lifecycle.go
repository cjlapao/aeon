package daemon

import (
	"context"
	"errors"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// notRunning refuses to touch a loop that is running or has a runner on it: stopping it is the person's first step,
// and a runner writing to rows being removed would be a race with no good outcome.
func (d *Daemon) notRunning(l store.Loop) error {
	if l.Status == "running" || (d.loopRunner != nil && d.loopRunner.Active(l.ID)) {
		return protocol.ErrLoopControlRefused.WithWhat("loop %s is running — stop it first", l.Slug)
	}
	return nil
}

func (d *Daemon) readLoopForLifecycle(id string) (store.Loop, error) {
	l, err := d.store.ReadLoopByID(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return store.Loop{}, protocol.ErrLoopControlRefused.WithWhat("loop %s is not known", id)
		}
		return store.Loop{}, err
	}
	return l, nil
}

// setLoopArchived is `loop.setArchived`: hide a loop or show it again. Nothing under it changes.
func (d *Daemon) setLoopArchived(ctx context.Context, p protocol.LoopSetArchivedParams) (protocol.LoopSetArchivedResult, error) {
	l, err := d.readLoopForLifecycle(p.ID)
	if err != nil {
		return protocol.LoopSetArchivedResult{}, err
	}
	if p.Archived {
		if err := d.notRunning(l); err != nil {
			return protocol.LoopSetArchivedResult{}, err
		}
	}
	if err := d.store.SetLoopArchived(ctx, p.ID, p.Archived, p.Note); err != nil {
		return protocol.LoopSetArchivedResult{}, err
	}
	res := protocol.LoopSetArchivedResult{ID: p.ID, Archived: p.Archived}
	if hidden, err := d.store.ArchivedLoops(ctx); err == nil {
		res.ArchivedAt = hidden[p.ID]
	}
	d.publishLoop("loop.visibility", map[string]any{"loopId": p.ID, "archived": p.Archived, "componentRoot": l.ComponentRoot})
	return res, nil
}

func tombstoneView(t store.LoopTombstone) protocol.LoopTombstoneView {
	return protocol.LoopTombstoneView(t)
}

// deleteLoop is `loop.delete`: the loop, its steps, chat and documents go; its totals and the work linked to it stay.
// The slug typed by the person is checked here, so no client can delete by accident.
func (d *Daemon) deleteLoop(ctx context.Context, actor protocol.Actor, p protocol.LoopDeleteParams) (protocol.LoopDeleteResult, error) {
	l, err := d.readLoopForLifecycle(p.ID)
	if err != nil {
		return protocol.LoopDeleteResult{}, err
	}
	if p.Confirm != l.Slug {
		return protocol.LoopDeleteResult{}, protocol.ErrLoopControlRefused.WithWhat("delete refused: confirm must be the loop's slug %q", l.Slug)
	}
	if err := d.notRunning(l); err != nil {
		return protocol.LoopDeleteResult{}, err
	}
	tomb, err := d.store.DeleteLoopKeepingStats(ctx, p.ID, actor.Kind+":"+actor.ID, p.Note)
	if err != nil {
		return protocol.LoopDeleteResult{}, err
	}
	d.publishLoop("loop.deleted", map[string]any{"loopId": p.ID, "slug": l.Slug, "componentRoot": l.ComponentRoot})
	return protocol.LoopDeleteResult{Tombstone: tombstoneView(tomb)}, nil
}

// listLoopTombstones is `loop.tombstones`.
func (d *Daemon) listLoopTombstones(ctx context.Context, p protocol.LoopTombstonesParams) (protocol.LoopTombstonesResult, error) {
	rows, err := d.store.ListLoopTombstones(ctx, p.ComponentRoot)
	if err != nil {
		return protocol.LoopTombstonesResult{}, err
	}
	out := protocol.LoopTombstonesResult{Tombstones: make([]protocol.LoopTombstoneView, 0, len(rows)), Detached: []protocol.DetachedLinkView{}, Closed: []protocol.WorkClosureView{}}
	for _, r := range rows {
		out.Tombstones = append(out.Tombstones, tombstoneView(r))
	}
	links, err := d.store.ListAllDetachedLinks(ctx, p.ComponentRoot)
	if err != nil {
		return protocol.LoopTombstonesResult{}, err
	}
	for _, l := range links {
		out.Detached = append(out.Detached, protocol.DetachedLinkView(l))
	}
	closed, err := d.store.ListWorkClosures(ctx, p.ComponentRoot)
	if err != nil {
		return protocol.LoopTombstonesResult{}, err
	}
	for _, c := range closed {
		out.Closed = append(out.Closed, protocol.WorkClosureView(c))
	}
	return out, nil
}

// listLoopDocuments is `loop.documents`: the loop's internal files, newest first.
func (d *Daemon) listLoopDocuments(_ context.Context, p protocol.LoopDocumentsParams) (protocol.LoopDocumentsResult, error) {
	l, err := d.readLoopForLifecycle(p.ID)
	if err != nil {
		return protocol.LoopDocumentsResult{}, err
	}
	docs, err := d.store.ListDocuments(store.Scope{LoopSlug: l.Slug, ComponentRoot: l.ComponentRoot})
	if err != nil {
		return protocol.LoopDocumentsResult{}, err
	}
	out := protocol.LoopDocumentsResult{Slug: l.Slug, ComponentRoot: l.ComponentRoot, Documents: make([]protocol.LoopDocumentView, 0, len(docs))}
	for _, doc := range docs {
		out.Documents = append(out.Documents, protocol.LoopDocumentView{File: doc.File, Rev: doc.Rev, Bytes: doc.Bytes, SHA256: doc.SHA256,
			WrittenBy: doc.WrittenBy, WrittenAt: doc.WrittenAt, OwnerGoal: doc.OwnerGoal})
	}
	return out, nil
}
