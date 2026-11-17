package daemon

// intake_start.go — S61 T9 (GI6, GI15/GI16/GI18/GI20, P8): the two doors
// that OPEN and CLOSE an intake session.
//
//	intake.start   validates the name, lays a PRIVATE scratch directory
//	             (0700, under the state dir unless the person names one,
//	             never reused), files the row, and starts the interview
//	             loop through the SAME start door every other loop rides
//	             (startLoopWith — one door, no second implementation, P34)
//	             with the goal-intake blueprint, the scratch as workspace,
//	             attended PINNED true by the daemon (a person opened this
//	             session over the 0600 socket; the interview asks), and
//	             allow_outward false. Without a configured model route the
//	             door refuses BEFORE any row — an interview nobody can run
//	             is not half-started (GI16), and the sentence names
//	             `aeon routes set`.
//	intake.abandon closes a session that will not commit: it stops the
//	             loop (the loop-control door's Stop, under the abandoning
//	             person's own actor), rejects the intake's OPEN plan
//	             proposals through desk.Reject — the desk's own reject path
//	             with the person's reason, the same audit as any reject —
//	             and leaves the scratch workspace ON DISK. A person's door
//	             may close a session, not erase its notes.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

const (
	// intakeBlueprint is the loop the session runs — the seeded row
	// (intakeseed), named once here and nowhere else.
	intakeBlueprint = "goal-intake"
	// maxIntakeNameRunes is GI6's name discipline: the name files the row,
	// names the scratch dir, and reaches the loop's goal line — 1..120
	// runes, no control characters.
	maxIntakeNameRunes = 120
)

// Start is intake.start's daemon door (GI6/GI9): a person opens the
// session; every write the interview will produce stays a person's door.
func (s *intakeDoors) Start(ctx context.Context, actor protocol.Actor, p protocol.IntakeStartParams) (protocol.IntakeView, error) {
	name := strings.TrimSpace(p.Name)
	if r := []rune(name); len(r) < 1 || len(r) > maxIntakeNameRunes {
		return protocol.IntakeView{}, protocol.ErrIntakeRefused.WithWhat(
			"the intake name must be 1–%d characters, got %d — intake.list shows the names as filed", maxIntakeNameRunes, len(r))
	}
	for _, c := range name {
		if unicode.IsControl(c) {
			return protocol.IntakeView{}, protocol.ErrIntakeRefused.WithWhat(
				"the intake name carries control character %q — the name files a row and names a directory, and control bytes belong to neither", c)
		}
	}
	// GI16 first, before anything is written: an interview the daemon
	// cannot run is refused whole, with the sentence that says what to do.
	routes, err := s.store.ListRoutes()
	if err != nil {
		return protocol.IntakeView{}, protocol.ErrIntakeRefused.WithWhat("the routes could not be read: %v", err)
	}
	if len(routes) == 0 {
		return protocol.IntakeView{}, protocol.ErrIntakeRefused.WithWhat(
			"intake.start refuses: no model route is configured — run `aeon routes set` and start the interview again; nothing was filed")
	}
	scratch, err := s.scratchDir(p.ScratchPath, name)
	if err != nil {
		return protocol.IntakeView{}, err
	}
	in, ref, err := s.store.CreateIntake(name, scratch, store.Actor{Kind: actor.Kind, ID: actor.ID})
	if err != nil {
		return protocol.IntakeView{}, protocol.ErrIntakeRefused.WithWhat("%v", err)
	}
	if ref != nil {
		return protocol.IntakeView{}, protocol.ErrIntakeRefused.WithWhat("%s", ref.Sentence)
	}
	// The loop slug is the row's own id: one intake, one interview, and
	// the derivation (intake.propose resolves loop→row, GI17) needs no
	// second naming act.
	slug := "intake-" + strings.TrimPrefix(in.ID, "int-")
	attended := true // pinned by the daemon, never read from params (P31/D2)
	res, err := s.d.startLoopWith(ctx, protocol.LoopStartParams{
		Blueprint:     intakeBlueprint,
		Goal:          "Open the goal-intake interview: the person's want is " + fmt.Sprintf("%q", name) + " — interview in waves, ground the options, and file the charter draft and the plan through your own ask doors.",
		WorkspaceRoot: scratch,
		Slug:          slug,
		Attended:      &attended,
		// AllowOutward stays the zero false: the interview creates nothing
		// outward; the commit door is where a person chooses (GI12).
	}, startExtras{StartedBy: "intake"})
	if err != nil {
		// The row and its scratch stand — honest, abandonable, and the
		// sentence says so rather than pretending a clean refusal.
		return protocol.IntakeView{}, protocol.ErrIntakeRefused.WithWhat(
			"the interview loop would not start (%v) — intake %s is filed at %s and stands without a loop; intake.abandon closes it", err, in.ID, scratch)
	}
	loopSlug := mapString(res, "slug")
	if loopSlug == "" {
		loopSlug = slug
	}
	ref, err = s.store.SetIntakeLoop(in.ID, loopSlug)
	if err != nil {
		return protocol.IntakeView{}, protocol.ErrIntakeRefused.WithWhat(
			"intake %s was filed and its loop %s started, but the link refused: %v — the rows stand; name this in the audit", in.ID, loopSlug, err)
	}
	if ref != nil {
		return protocol.IntakeView{}, protocol.ErrIntakeRefused.WithWhat(
			"intake %s was filed and its loop %s started, but the link refused: %s — the rows stand; intake.abandon closes the session", in.ID, loopSlug, ref.Sentence)
	}
	return protocol.IntakeView{
		ID: in.ID, Name: in.Name, ScratchPath: in.ScratchPath,
		LoopSlug: loopSlug, RootID: in.RootID, CreatedAt: in.CreatedAt,
	}, nil
}

// Abandon is intake.abandon's daemon door (GI13): the fifth human write on
// the plane. Stop the loop under the abandoning person, reject the OPEN
// plans through the desk's own path, keep the scratch.
func (s *intakeDoors) Abandon(ctx context.Context, actor protocol.Actor, p protocol.IntakeAbandonParams) (protocol.IntakeAbandonResult, error) {
	row, err := s.store.GetIntake(p.IntakeID)
	if err != nil {
		return protocol.IntakeAbandonResult{}, protocol.ErrIntakeRefused.WithWhat("%v", err)
	}
	out := protocol.IntakeAbandonResult{IntakeID: row.ID, Intake: protocol.IntakeView{
		ID: row.ID, Name: row.Name, ScratchPath: row.ScratchPath,
		LoopSlug: row.LoopSlug, RootID: row.RootID, CreatedAt: row.CreatedAt,
	}}
	if row.LoopSlug != "" {
		closed, err := s.stopIntakeLoop(ctx, actor, row.LoopSlug, p.Reason)
		if err != nil {
			return protocol.IntakeAbandonResult{}, err
		}
		out.LoopClosed = closed
	} else {
		out.LoopClosed = true // no loop was ever attached — nothing to close
	}
	// Reject the intake's OPEN plans through desk.Reject — the same path
	// proposal.reject walks, the same audit, the abandoning person as the
	// deciding actor. Only intake_plan rows that NAME this intake are
	// touched; a charter_save proposal is about a root, not a session, and
	// outlives it honestly (the person still decides it at its own door).
	open, err := s.store.ListProposals("proposed")
	if err != nil {
		return protocol.IntakeAbandonResult{}, protocol.ErrIntakeRefused.WithWhat("the open proposals could not be read: %v", err)
	}
	for _, pr := range open {
		if pr.Kind != string(desk.KindIntakePlan) || !planNamesIntake(pr.Payload, row.ID) {
			continue
		}
		if _, err := desk.Reject(ctx, s.rejectDeps(), pr.ID, "intake abandoned: "+p.Reason,
			desk.Actor{Kind: actor.Kind, ID: actor.ID}); err != nil {
			return protocol.IntakeAbandonResult{}, protocol.ErrIntakeRefused.WithWhat(
				"open proposal %d would not reject (%v) — the abandon stops there; the loop was %s", pr.ID, err, closedWord(out.LoopClosed))
		}
		out.RejectedProposals = append(out.RejectedProposals, fmt.Sprintf("%d", pr.ID))
	}
	return out, nil
}

// stopIntakeLoop parks the interview loop through the loop-control door
// (the SAME StopLoop the commit path's closeLoop uses), under the person
// abandoning. A loop the store says already stands still (not running,
// not waiting, not blocked) is closed with nothing to do — abandon is
// allowed to finish what a crash started.
func (s *intakeDoors) stopIntakeLoop(ctx context.Context, actor protocol.Actor, slug, reason string) (bool, error) {
	// The control doors speak loop IDS; a slug is unique only with its
	// root — resolve by walking the slug's rows (2026-11-05, measured in
	// intake_start_test: the door that stops a loop by its slug stops
	// nothing).
	loops, lerr := s.store.LoopsBySlug(slug)
	if lerr != nil || len(loops) == 0 {
		return false, protocol.ErrIntakeRefused.WithWhat(
			"the interview loop %s could not be resolved to a loop row (%v) — abandon refuses to call the session closed on a loop it cannot see", slug, lerr)
	}
	svc := s.d.newLoopControlService()
	_, ref, err := svc.Stop(ctx, actor, loops[len(loops)-1].ID, "intake abandoned (S61): "+reason)
	if err != nil {
		return false, protocol.ErrIntakeRefused.WithWhat("the interview loop would not stop: %v", err)
	}
	if ref == nil {
		return true, nil
	}
	// The control core refuses to stop a loop that is not running — and
	// that refusal is EXACTLY the case an abandon must pass through: the
	// interview most often sits `waiting` on an unanswered question (its
	// step is parked by the suspension, no process behind it, P2), or it
	// reached a terminal. Only a loop the store still calls `running` is
	// genuinely live, and abandon refuses rather than racing it — a
	// person stops that loop first, through loop.stop, the door that
	// names what stands in the way.
	loops, lerr = s.store.LoopsBySlug(slug)
	if lerr != nil || len(loops) == 0 {
		return false, protocol.ErrIntakeRefused.WithWhat("the interview loop %s refused stop (%s, kind %s) and could not be read: %v", slug, ref.Detail, ref.Kind, lerr)
	}
	last := loops[len(loops)-1]
	if last.Status == "running" {
		return false, protocol.ErrIntakeRefused.WithWhat(
			"the interview loop %s refused stop (%s, kind %s) — abandon refuses with the loop still live; loop.stop names what stands in the way", slug, ref.Detail, ref.Kind)
	}
	return true, nil // waiting / blocked / terminal: already stood still — the abandon's intent holds
}

// rejectDeps is desk.Reject's daemon wiring beside proposal_decide.go's —
// the row read, the store's decision triple, the required reason. The
// in-force seam and the remove-target resolver serve COMMIT paths; a
// reject applies nothing (desk.Reject consults no effect registry), so
// this door hands it exactly what the reject path touches.
func (s *intakeDoors) rejectDeps() desk.DecideDeps {
	return desk.DecideDeps{
		Get: func(id int64) (desk.Row, bool, error) {
			r, found, err := s.store.GetProposal(id)
			if err != nil {
				return desk.Row{}, false, protocol.ErrStoreUnavailable.WithWhat("proposal %d could not be read: %s", id, err.Error())
			}
			if !found {
				return desk.Row{}, false, nil
			}
			return desk.Row{
				ID: r.ID, Kind: r.Kind, Payload: r.Payload, Status: r.Status,
				CreatedAt: r.CreatedAt, CreatedByKind: r.CreatedByKind, CreatedByID: r.CreatedByID,
				DecidedAt: r.DecidedAt, DecidedByKind: r.DecidedByKind, DecidedByID: r.DecidedByID,
				DecideReason: r.DecideReason, AppliedError: r.AppliedError,
			}, true, nil
		},
		Store:          decisionStore{s.store},
		ReasonRequired: true, // the reason the person typed for the reject (PM7.5)
	}
}

// scratchDir lays the private interview directory: 0700, under
// <stateDir>/intake named after the intake unless the person names an
// absolute path, and NEVER an existing directory (T7's MkdirAll era is
// over — a second session pointed at a first one's notes is T9's to
// refuse: the scratch is the interview's memory, and mixing memories is
// how an agent files a plan about the wrong project).
func (s *intakeDoors) scratchDir(explicit, name string) (string, error) {
	if explicit != "" {
		if !filepath.IsAbs(explicit) {
			return "", protocol.ErrIntakeRefused.WithWhat(
				"the scratch workspace must be an absolute path — %q is relative, and a relative promise is a promise about nothing", explicit)
		}
		if err := os.Mkdir(explicit, 0o700); err != nil {
			if os.IsExist(err) {
				return "", protocol.ErrIntakeRefused.WithWhat(
					"the scratch workspace %s already exists — a scratch dir is one session's memory; start a fresh one or name a different path", explicit)
			}
			return "", protocol.ErrIntakeRefused.WithWhat("creating the scratch workspace: %v", err)
		}
		if err := os.Chmod(explicit, 0o700); err != nil {
			return "", protocol.ErrIntakeRefused.WithWhat("securing the scratch workspace: %v", err)
		}
		return explicit, nil
	}
	root := filepath.Join(filepath.Dir(s.store.Path()), "intake")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", protocol.ErrIntakeRefused.WithWhat("creating the intake root: %v", err)
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		suffix := scratchSuffix()
		path := filepath.Join(root, fmt.Sprintf("%s-%s", store.Slugify(name), suffix))
		if err := os.Mkdir(path, 0o700); err == nil {
			return path, nil
		} else if !os.IsExist(err) {
			return "", protocol.ErrIntakeRefused.WithWhat("creating the scratch workspace: %v", err)
		} else {
			lastErr = err
		}
	}
	return "", protocol.ErrIntakeRefused.WithWhat(
		"the scratch workspace refused to be created private (%v) — the name collides with existing intake scratch; a different name, or an explicit path, breaks the tie", lastErr)
}

// scratchSuffix is 6 hex bytes of randomness — uniqueness for a scratch
// directory, not identity, not a secret.
func scratchSuffix() string {
	var b [3]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failing is a broken platform, not a branch to hide;
		// the zero suffix still lands under Mkdir's exist-refusal.
		return "000000"
	}
	return hex.EncodeToString(b[:])
}

// mapString reads one string field out of startLoopWith's answer map.
func mapString(m map[string]any, k string) string {
	if m == nil {
		return ""
	}
	v, _ := m[k].(string)
	return v
}

func closedWord(closed bool) string {
	if closed {
		return "stopped"
	}
	return "left standing"
}

// planNamesIntake peeks at a proposal payload for the intake it plans —
// the one link the abandon walk follows (mirror of internal/intake's read
// side; a payload that does not even decode is not about this intake).
func planNamesIntake(payload, intakeID string) bool {
	var peek struct {
		IntakeID string `json:"intakeId"`
	}
	if err := json.Unmarshal([]byte(payload), &peek); err != nil {
		return false
	}
	return peek.IntakeID == intakeID
}
