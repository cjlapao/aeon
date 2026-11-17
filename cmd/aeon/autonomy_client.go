package main

// autonomy_client.go — S56 T9 (AL13). The two doors onto the one autonomy
// service, and the selection between them.
//
// # One engine, two doors (the S45 PM4 pattern, as seed_door.go keeps it)
//
// The autonomy engine is the daemon's service (internal/daemon/autonomy.go's
// NewAutonomyService — the SAME function the daemon wires the socket's ten
// autonomy.* methods with). The CLI is a CLIENT of it: `selectAutonomyDoor`
// dials the daemon and, on a successful handshake, speaks the ten methods
// over the socket (autonomySocketDoor). When there is NO daemon (the socket is not
// reachable — not merely a version or permission error) the CLI opens the
// SAME service directly over the store (autonomyDirectDoor) and says so, EXCEPT for
// grant: an allow is never widened offline (the audit must carry the
// peer-attested identity, which only the daemon can stamp), and grant
// refuses with a fixed sentence instead of falling back. Any other dial
// failure (a version mismatch, a permission error) REFUSES — it never falls
// back, because a live-but-disagreed daemon is a state the operator must see,
// not one the CLI routes around.
//
// # The direct door is the service, not a copy
//
// autonomyDirectDoor builds `daemon.NewAutonomyService` over a directly opened store
// and calls it — no second validator, no second mapper, no second sentence.
// The five grant-table needles (CreateAutonomyGrant, RevokeAutonomyGrant,
// RecordAutonomyUse, AutonomyLive, the pin's INSERT) appear nowhere in this
// package; the store's actor door (AL11 lock 3) is the backstop that keeps
// the cli actor to the restrictive writes. `TestDirectDoorUsesTheSameFunctions`
// parses this file to prove the door speaks the service and nothing else.
//
// # The actor is the connection or the cli — never a param
//
// The socket door's actor is stamped server-side from the connection's peer
// credential (the daemon's ActorFor seam; the CLI sends no actor in params —
// the protocol's identity-key check would refuse one). The direct door's
// actor is the fixed {cli, cli}: audited `actor_kind='cli'`, which the
// store admits for deny and revoke (restrictive) and refuses for allow
// (the backstop if the selection layer is ever bypassed).
//
// This file is the ONLY place in the CLI that imports internal/store and
// internal/daemon for the autonomy verbs: the verbs (autonomy.go) import
// internal/protocol (and the pure internal/autonomy for the generated help)
// only, so a verb cannot reopen the store (TestCLINeverImportsTheStore).

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// autonomyDirectLine is the fixed stderr line a fallback prints (AL13): a
// constant, never assembled from the error.
const autonomyDirectLine = "daemon not running — applied directly to the store"

// autonomyGrantOfflineRefusal is grant's offline refusal (AL13): the audit
// for a widening must carry the peer-attested identity, and only the daemon
// stamps one. A constant, printed verbatim.
const autonomyGrantOfflineRefusal = "daemon not running — grants are written through the daemon so the audit carries who asked; start the daemon, or use `aeon autonomy deny`/`revoke` to restrict"

// autonomyDoor is the two doors onto the one autonomy service: the seven
// reads and the three writes that the verbs speak through. Both doors answer
// the SAME service, so the wire the CLI reads cannot drift from the truth
// (TestCLIRefusalsVerbatimBothDoors pins it).
type autonomyDoor interface {
	list(ctx context.Context, p protocol.AutonomyListParams) (protocol.AutonomyListResult, error)
	show(ctx context.Context, p protocol.AutonomyShowParams) (protocol.AutonomyShowResult, error)
	vocabulary(ctx context.Context) (protocol.VocabularyView, error)
	effective(ctx context.Context, p protocol.AutonomyEffectiveParams) (protocol.EffectiveView, error)
	pinned(ctx context.Context, p protocol.AutonomyPinnedParams) (protocol.PinnedView, error)
	changes(ctx context.Context, p protocol.AutonomyChangesParams) (protocol.AutonomyChangesResult, error)
	uses(ctx context.Context, p protocol.AutonomyUsesParams) (protocol.AutonomyUsesResult, error)
	grant(ctx context.Context, p protocol.AutonomyGrantParams) (protocol.AutonomyGrantResult, error)
	deny(ctx context.Context, p protocol.AutonomyDenyParams) (protocol.AutonomyDenyResult, error)
	revoke(ctx context.Context, p protocol.AutonomyRevokeParams) (protocol.AutonomyRevokeResult, error)
}

// autonomyDirectActor is the direct door's audit identity (AL13): the cli is
// who a direct, offline write is. A constant, never a flag — a params actor
// would forge the audit identity of a write. The store admits it for deny and
// revoke and refuses it for allow (the backstop).
var autonomyDirectActor = protocol.Actor{Kind: "cli", ID: "cli"}

// autonomyDial is the dial the selection uses; a var so a test can stand in a
// dial that refuses (a version fault) or answers "no daemon", without
// touching the real endpoint. Production is the one dial(0).
var autonomyDial = dial

// selectAutonomyDoor is the selection (S45 PM4, the seed pattern with the
// AL13 asymmetry): dial once; a success is the socket door. On "no daemon":
// a read, deny or revoke gets the direct door and the fixed stderr line;
// GRANT gets the fixed refusal — it never falls back. Any other dial
// failure (permission denied, protocol mismatch, version skew) is refused,
// never fallen back from: a fallback from an error that is not "nobody is
// home" would route around a daemon that exists.
func selectAutonomyDoor(verb string, errOut io.Writer) (autonomyDoor, error) {
	c, err := autonomyDial(0)
	if err != nil {
		if isNoDaemon(err) {
			if verb == "grant" {
				// The one verb that never falls back (AL13): widening
				// authority offline would leave an audit row whose identity
				// no kernel attested. The refusal is the fixed sentence.
				return nil, errors.New(autonomyGrantOfflineRefusal)
			}
			// "no daemon" is the ONLY fall back: the socket is not there, so
			// the CLI opens the same service directly and says so.
			fmt.Fprintln(errOut, autonomyDirectLine)
			return newAutonomyDirectDoorFunc(context.Background())
		}
		// A version mismatch, a permission error, any other fault: the daemon
		// is there and disagreeing, or the operator must fix a permission —
		// the CLI refuses rather than route around it (AL13).
		return nil, err
	}
	return autonomySocketDoor{c: c}, nil
}

// --- the socket door ------------------------------------------------------

// autonomySocketDoor speaks the ten autonomy.* methods over the daemon's socket. The
// actor for a write is stamped server-side (the connection's peer), so this
// door sends no actor.
type autonomySocketDoor struct {
	c *protocol.Client
}

func (d autonomySocketDoor) list(ctx context.Context, p protocol.AutonomyListParams) (protocol.AutonomyListResult, error) {
	var res protocol.AutonomyListResult
	if err := d.c.Call(protocol.MethodAutonomyList, p, &res); err != nil {
		return protocol.AutonomyListResult{}, err
	}
	return res, nil
}

func (d autonomySocketDoor) show(ctx context.Context, p protocol.AutonomyShowParams) (protocol.AutonomyShowResult, error) {
	var res protocol.AutonomyShowResult
	if err := d.c.Call(protocol.MethodAutonomyShow, p, &res); err != nil {
		return protocol.AutonomyShowResult{}, err
	}
	return res, nil
}

func (d autonomySocketDoor) vocabulary(ctx context.Context) (protocol.VocabularyView, error) {
	var res protocol.VocabularyView
	if err := d.c.Call(protocol.MethodAutonomyVocabulary, struct{}{}, &res); err != nil {
		return protocol.VocabularyView{}, err
	}
	return res, nil
}

func (d autonomySocketDoor) effective(ctx context.Context, p protocol.AutonomyEffectiveParams) (protocol.EffectiveView, error) {
	var res protocol.EffectiveView
	if err := d.c.Call(protocol.MethodAutonomyEffective, p, &res); err != nil {
		return protocol.EffectiveView{}, err
	}
	return res, nil
}

func (d autonomySocketDoor) pinned(ctx context.Context, p protocol.AutonomyPinnedParams) (protocol.PinnedView, error) {
	var res protocol.PinnedView
	if err := d.c.Call(protocol.MethodAutonomyPinned, p, &res); err != nil {
		return protocol.PinnedView{}, err
	}
	return res, nil
}

func (d autonomySocketDoor) changes(ctx context.Context, p protocol.AutonomyChangesParams) (protocol.AutonomyChangesResult, error) {
	var res protocol.AutonomyChangesResult
	if err := d.c.Call(protocol.MethodAutonomyChanges, p, &res); err != nil {
		return protocol.AutonomyChangesResult{}, err
	}
	return res, nil
}

func (d autonomySocketDoor) uses(ctx context.Context, p protocol.AutonomyUsesParams) (protocol.AutonomyUsesResult, error) {
	var res protocol.AutonomyUsesResult
	if err := d.c.Call(protocol.MethodAutonomyUses, p, &res); err != nil {
		return protocol.AutonomyUsesResult{}, err
	}
	return res, nil
}

func (d autonomySocketDoor) grant(ctx context.Context, p protocol.AutonomyGrantParams) (protocol.AutonomyGrantResult, error) {
	var res protocol.AutonomyGrantResult
	if err := d.c.Call(protocol.MethodAutonomyGrant, p, &res); err != nil {
		return protocol.AutonomyGrantResult{}, err
	}
	return res, nil
}

func (d autonomySocketDoor) deny(ctx context.Context, p protocol.AutonomyDenyParams) (protocol.AutonomyDenyResult, error) {
	var res protocol.AutonomyDenyResult
	if err := d.c.Call(protocol.MethodAutonomyDeny, p, &res); err != nil {
		return protocol.AutonomyDenyResult{}, err
	}
	return res, nil
}

func (d autonomySocketDoor) revoke(ctx context.Context, p protocol.AutonomyRevokeParams) (protocol.AutonomyRevokeResult, error) {
	var res protocol.AutonomyRevokeResult
	if err := d.c.Call(protocol.MethodAutonomyRevoke, p, &res); err != nil {
		return protocol.AutonomyRevokeResult{}, err
	}
	return res, nil
}

// --- the direct door ------------------------------------------------------

// autonomyDirectDoor speaks the one autonomy service over a directly opened store.
// It is the service, not a copy: no second validator, no second mapper. The
// only thing this door adds is the shape the verbs read (the seam's results
// mapped to the wire twins, the same field copies the socket handlers make)
// and the fixed cli actor for the writes.
type autonomyDirectDoor struct {
	s protocol.AutonomyService
}

// newAutonomyDirectDoorFunc is the direct-door construction; a var so a test
// can stand in a direct door over a fake service without opening a store
// (the fallback and parity tests). Production opens the real store and
// builds the daemon's own service over it.
var newAutonomyDirectDoorFunc = newAutonomyDirectDoor

func newAutonomyDirectDoor(ctx context.Context) (autonomyDoor, error) {
	// S62 T5: the safety net rides the direct door too — an autonomy verb
	// that opens an old store migrates it, and the copy precedes the first
	// migration. --no-backup is not exposed here: the flag is the db
	// verbs' (BK18).
	st, err := store.Open(storeOpenOptions(false, "cli"))
	if err != nil {
		return nil, err
	}
	_ = ctx
	return autonomyDirectDoor{s: daemon.NewAutonomyService(st)}, nil
}

// autonomyDoorRefusal renders the service's refusal as the SAME typed error
// the socket handlers render (protocol's autonomyRefused): code -31833, the
// sentence verbatim in `what`. One renderer for both doors — a second refusal
// sentence would be a second truth.
func autonomyDoorRefusal(ref *protocol.AutonomyRefusal) error {
	return protocol.ErrAutonomyRefused.WithWhat("%s", ref.Sentence)
}

func (d autonomyDirectDoor) list(ctx context.Context, p protocol.AutonomyListParams) (protocol.AutonomyListResult, error) {
	rootID, state := "", ""
	if p.RootID != nil {
		rootID = *p.RootID
	}
	if p.State != nil {
		state = *p.State
	}
	includeInactive := p.IncludeInactive != nil && *p.IncludeInactive
	grants, err := d.s.List(ctx, rootID, state, includeInactive)
	if err != nil {
		return protocol.AutonomyListResult{}, err
	}
	return protocol.AutonomyListResult{Grants: grants}, nil
}

func (d autonomyDirectDoor) show(ctx context.Context, p protocol.AutonomyShowParams) (protocol.AutonomyShowResult, error) {
	view, lastChange, usedCount, pinnedLoops, ref, err := d.s.Show(ctx, p.ID)
	if err != nil {
		return protocol.AutonomyShowResult{}, err
	}
	if ref != nil {
		return protocol.AutonomyShowResult{}, autonomyDoorRefusal(ref)
	}
	return protocol.AutonomyShowResult{
		Grant:       view,
		LastChange:  lastChange,
		UsedCount:   usedCount,
		PinnedLoops: pinnedLoops,
	}, nil
}

func (d autonomyDirectDoor) vocabulary(ctx context.Context) (protocol.VocabularyView, error) {
	v, err := d.s.Vocabulary()
	if err != nil {
		return protocol.VocabularyView{}, err
	}
	return v, nil
}

func (d autonomyDirectDoor) effective(ctx context.Context, p protocol.AutonomyEffectiveParams) (protocol.EffectiveView, error) {
	origin := protocol.OriginSchedule // the default a start without an origin is (the handler's)
	if p.Origin != nil {
		origin = *p.Origin
	}
	view, ref, err := d.s.Effective(ctx, p.RootID, origin, p.Labels)
	if err != nil {
		return protocol.EffectiveView{}, err
	}
	if ref != nil {
		return protocol.EffectiveView{}, autonomyDoorRefusal(ref)
	}
	return view, nil
}

func (d autonomyDirectDoor) pinned(ctx context.Context, p protocol.AutonomyPinnedParams) (protocol.PinnedView, error) {
	view, ref, err := d.s.Pinned(ctx, p.LoopID)
	if err != nil {
		return protocol.PinnedView{}, err
	}
	if ref != nil {
		return protocol.PinnedView{}, autonomyDoorRefusal(ref)
	}
	return view, nil
}

func (d autonomyDirectDoor) changes(ctx context.Context, p protocol.AutonomyChangesParams) (protocol.AutonomyChangesResult, error) {
	rootID, grantID, limit := "", "", 0
	if p.RootID != nil {
		rootID = *p.RootID
	}
	if p.GrantID != nil {
		grantID = *p.GrantID
	}
	if p.Limit != nil {
		limit = *p.Limit
	}
	changes, err := d.s.Changes(ctx, rootID, grantID, limit)
	if err != nil {
		return protocol.AutonomyChangesResult{}, err
	}
	return protocol.AutonomyChangesResult{Changes: changes}, nil
}

func (d autonomyDirectDoor) uses(ctx context.Context, p protocol.AutonomyUsesParams) (protocol.AutonomyUsesResult, error) {
	rootID, grantID, loopID, limit := "", "", "", 0
	if p.RootID != nil {
		rootID = *p.RootID
	}
	if p.GrantID != nil {
		grantID = *p.GrantID
	}
	if p.LoopID != nil {
		loopID = *p.LoopID
	}
	if p.Limit != nil {
		limit = *p.Limit
	}
	uses, err := d.s.Uses(ctx, rootID, grantID, loopID, limit)
	if err != nil {
		return protocol.AutonomyUsesResult{}, err
	}
	return protocol.AutonomyUsesResult{Uses: uses}, nil
}

func (d autonomyDirectDoor) grant(ctx context.Context, p protocol.AutonomyGrantParams) (protocol.AutonomyGrantResult, error) {
	// The selection never reaches here: grant refuses offline before the
	// door is built (selectAutonomyDoor). The backstop, if it were reached,
	// is the store's actor door — the cli actor is refused for allow — so
	// the direct door speaks the service exactly as the socket does.
	out, ref, err := d.s.Grant(ctx, autonomyDirectActor, protocol.AutonomyGrantRequest{
		NodeID:     p.RootID,
		Subject:    p.Subject,
		Conditions: derefAutonomyConditions(p.Conditions),
		ExpiresAt:  derefAutonomyTime(p.ExpiresAt),
		Note:       p.Note,
		DryRun:     p.DryRun,
	})
	if err != nil {
		return protocol.AutonomyGrantResult{}, err
	}
	if ref != nil {
		return protocol.AutonomyGrantResult{}, autonomyDoorRefusal(ref)
	}
	return protocol.AutonomyGrantResult{Grant: out.View, Confirmation: out.Confirmation}, nil
}

func (d autonomyDirectDoor) deny(ctx context.Context, p protocol.AutonomyDenyParams) (protocol.AutonomyDenyResult, error) {
	out, ref, err := d.s.Deny(ctx, autonomyDirectActor, protocol.AutonomyDenyRequest{
		NodeID:  p.NodeID,
		Subject: p.Subject,
		Note:    p.Note,
		DryRun:  p.DryRun,
	})
	if err != nil {
		return protocol.AutonomyDenyResult{}, err
	}
	if ref != nil {
		return protocol.AutonomyDenyResult{}, autonomyDoorRefusal(ref)
	}
	return protocol.AutonomyDenyResult{Grant: out.View, Confirmation: out.Confirmation}, nil
}

func (d autonomyDirectDoor) revoke(ctx context.Context, p protocol.AutonomyRevokeParams) (protocol.AutonomyRevokeResult, error) {
	out, ref, err := d.s.Revoke(ctx, autonomyDirectActor, protocol.AutonomyRevokeRequest{
		ID:     p.ID,
		Reason: p.Reason,
		DryRun: p.DryRun,
	})
	if err != nil {
		return protocol.AutonomyRevokeResult{}, err
	}
	if ref != nil {
		return protocol.AutonomyRevokeResult{}, autonomyDoorRefusal(ref)
	}
	return protocol.AutonomyRevokeResult{Grant: out.View, Confirmation: out.Confirmation, AffectedLoops: out.Affected}, nil
}

// derefAutonomyConditions renders the grant's conditions pointer: a present
// object is the caller's values verbatim, an absent one is the seam's whole-
// object default ({RequireBudget:true}) — the same distinction the socket's
// handler applies, so both doors send the service one shape.
func derefAutonomyConditions(c *protocol.AutonomyConditions) protocol.AutonomyConditions {
	if c == nil {
		return protocol.AutonomyConditions{RequireBudget: true}
	}
	return *c
}

func derefAutonomyTime(s *string) *time.Time {
	if s == nil {
		return nil
	}
	t, err := time.Parse(time.RFC3339, *s)
	if err != nil {
		// The verb already refused a non-RFC3339 spelling at parse; a direct
		// door reached with one is a caller bug, spoken as a refusal rather
		// than a panic.
		return nil
	}
	return &t
}
