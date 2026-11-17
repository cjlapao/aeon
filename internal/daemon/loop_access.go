package daemon

import (
	"context"
	"fmt"
	"time"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
	"github.com/cjlapao/aeon/internal/tools"
)

// loopAccessService is the daemon's side of loop.access / loop.setAccess: it reads and writes the stored mode
// and publishes the change, so a watcher (the chat's switch, Home) sees it flip. The write is the human door's:
// the actor arrives from the connection (protocol), never from params.
type loopAccessService struct {
	st      *store.Store
	publish func(kind string, payload any)
	now     func() time.Time
}

func newLoopAccessService(st *store.Store, publish func(kind string, payload any)) *loopAccessService {
	return &loopAccessService{st: st, publish: publish, now: time.Now}
}

// loopAccessChanged is the event payload. It is filed under the loop's component so project-scoped
// subscribers receive it.
type loopAccessChanged struct {
	LoopID string   `json:"loopId"`
	Mode   string   `json:"mode"`
	Tools  []string `json:"tools"`
	By     string   `json:"by"`
	Root   string   `json:"-"`
}

func (e loopAccessChanged) EventComponentRoot() string { return e.Root }

func viewOf(id string, a store.LoopAccess, present bool) protocol.LoopAccessView {
	if !present {
		return protocol.LoopAccessView{ID: id, Mode: protocol.AccessAsk, Tools: []string{}, AskingTools: tools.DangerousTools()}
	}
	chosen := a.Tools
	if chosen == nil {
		chosen = []string{}
	}
	return protocol.LoopAccessView{ID: id, Mode: a.Mode, Tools: chosen, SetBy: a.SetByKind + ":" + a.SetByID, SetAt: a.SetAt, AskingTools: tools.DangerousTools()}
}

func (s *loopAccessService) Get(ctx context.Context, loopID string) (protocol.LoopAccessView, error) {
	if _, err := s.st.ReadLoopByID(loopID); err != nil {
		return protocol.LoopAccessView{}, protocol.ErrLoopControlRefused.WithWhat("loop %s is not known", loopID)
	}
	a, present, err := s.st.LoopAccessGet(ctx, loopID)
	if err != nil {
		return protocol.LoopAccessView{}, err
	}
	return viewOf(loopID, a, present), nil
}

func (s *loopAccessService) Set(ctx context.Context, actor protocol.Actor, p protocol.LoopSetAccessParams) (protocol.LoopAccessView, error) {
	l, err := s.st.ReadLoopByID(p.ID)
	if err != nil {
		return protocol.LoopAccessView{}, protocol.ErrLoopControlRefused.WithWhat("loop %s is not known", p.ID)
	}
	by := actor.Kind + ":" + actor.ID
	if p.Mode == protocol.AccessAsk {
		if err := s.st.LoopAccessClear(ctx, p.ID); err != nil {
			return protocol.LoopAccessView{}, err
		}
	} else {
		mode := store.LoopAccessFull
		if p.Mode == protocol.AccessTools {
			mode = store.LoopAccessTools
		}
		if err := s.st.LoopAccessSet(ctx, store.LoopAccess{
			LoopID: p.ID, Mode: mode, Tools: p.Tools, SetByKind: actor.Kind, SetByID: actor.ID, SetAt: s.now().UTC().Format(time.RFC3339),
		}); err != nil {
			return protocol.LoopAccessView{}, err
		}
	}
	if s.publish != nil {
		s.publish(protocol.KindLoopAccessChanged, loopAccessChanged{LoopID: p.ID, Mode: p.Mode, Tools: p.Tools, By: by, Root: l.ComponentRoot})
	}
	a, present, err := s.st.LoopAccessGet(ctx, p.ID)
	if err != nil {
		return protocol.LoopAccessView{}, fmt.Errorf("the mode was saved but could not be read back: %w", err)
	}
	return viewOf(p.ID, a, present), nil
}
