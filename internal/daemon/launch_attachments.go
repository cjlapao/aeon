package daemon

// launch_attachments.go — S77: the CHOSEN attachments' daemon side, the
// sibling of chosen_skills.go (S68 B6). A person can START a loop with files
// attached; this is the half that decides, once, whether those files can ride
// and remembers the answer for every step of THIS launch.
//
// # The one moment the question is asked
//
// `startLaunchAttachments` runs at the start door, BEFORE any row exists — the
// same nothing-half-written rule the chosen skills and the link resolution
// follow. Two things must be true for an attachment to ride, and both are
// refused loudly (naming every offender) rather than resolved to silence:
//
//   - the reference RESOLVES: `{document, rev}` names a revision this exact
//     scope {slug, workspaceRoot} actually put. The protocol's
//     ResolveAttachmentRefs owns that rule (empty list, the caps, the store
//     lookup), so the start door cannot invent a receipt; and
//   - the start's route DECLARES the capability each type asks (model.
//     RequiredCapability: image / pdf / file). A launch aimed at a route that
//     cannot SEE the bytes refuses HERE — the person who attached a picture to
//     a text-only model hears that at the door, not three steps later inside a
//     model call that was never going to accept the block.
//
// # Why the cache exists, and why it is not the truth
//
// Like chosen skills, the DURABLE truth is the kickoff row: it carries the
// filed receipts. The cache is the restart-cheap fast path every dispatch
// consults through `launchAttachmentsFor`; a restart reloads a launch's
// receipts from its own kickoff row rather than rendering a step without them.
// `launchAttachmentParams` is the ONLY place a launch's attachments reach the
// wire — nothing attached leaves params byte-identical to a pre-S77 step.
//
// The step compile itself stays S69's shape byte for byte — a mid-loop
// message's own attachments are the ADAPTER's format honesty (S69's recorded
// ruling), and a per-step filter there would silently strip them on routes
// that declare no capabilities. The launch's receipts are filtered HERE, at
// their only road to the wire, against the STEP's route: a launch whose route
// moves later (a lane override, a pending-route change) never ships bytes the
// new route cannot carry, and no turn's own attachment is touched.

import (
	"fmt"
	"strings"

	"github.com/cjlapao/aeon/internal/loop"
	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// startLaunchAttachments is the start door's attachment check: references in
// (nil means nothing attached, and returns nothing), the store's receipts out.
// Absent refs are the pre-S77 shape — no route is even resolved, so a start
// that attaches nothing pays nothing and cannot trip the capability gate.
func (d *Daemon) startLaunchAttachments(routeName string, refs []protocol.AttachmentRef, slug, componentRoot string) ([]store.AttachmentReceipt, error) {
	if refs == nil {
		return nil, nil
	}
	if d.store == nil {
		return nil, protocol.ErrStoreUnavailable
	}
	// Resolve first — the store's facts, or the named refusal. No capability
	// is consulted for bytes that do not exist; the offender list is the
	// resolver's, verbatim (the same sentence `loop.message` would say).
	resolved, err := protocol.ResolveAttachmentRefs(storeAttachmentResolver{d.store}, slug, componentRoot, refs)
	if err != nil {
		return nil, err
	}
	if len(resolved) == 0 {
		return nil, nil
	}
	// The route that will speak this launch's steps is resolved by the SAME
	// rule the first step uses (Table.RouteForLaunch), so a default-single
	// route and a named route are judged the way the step would judge them.
	route, rerr := d.Table.RouteForLaunch(routeName)
	if rerr != nil {
		// No route to check against means the launch cannot prove its bytes
		// will be seen; a start that attaches files to an unroutable model is
		// refused naming that, never silently stripped. ErrBadParams (not the
		// route code itself) because the SENTENCE is the answer here — the
		// same shape the chosen-skills check beside it uses.
		return nil, protocol.ErrBadParams{Err: fmt.Errorf("the launch attaches files, so its route must be checkable: %w", rerr)}
	}
	var offenders []string
	for _, r := range resolved {
		req := model.RequiredCapability(r.MediaType)
		if req != "" && !route.DeclaresCapability(req) {
			offenders = append(offenders, fmt.Sprintf("%s (%s) needs the %q capability, which route '%s' does not declare", r.Name, r.MediaType, req, route.ID))
		}
	}
	if len(offenders) > 0 {
		return nil, protocol.ErrBadParams{Err: fmt.Errorf(
			"this launch attaches files its route cannot carry: %s — attach nothing, or start on a route that declares the capability",
			strings.Join(offenders, "; "))}
	}
	receipts := make([]store.AttachmentReceipt, 0, len(resolved))
	for _, r := range resolved {
		receipts = append(receipts, store.AttachmentReceipt{
			Name: r.Name, Document: r.Document, Rev: r.Rev,
			MediaType: r.MediaType, Size: r.Size, SHA256: r.SHA256,
		})
	}
	return receipts, nil
}

// rememberLaunchAttachments files what a launch attached, keyed by loop id —
// the fast path over the kickoff row's receipts.
func (d *Daemon) rememberLaunchAttachments(loopID string, receipts []store.AttachmentReceipt) {
	d.launchAttsMu.Lock()
	defer d.launchAttsMu.Unlock()
	if d.launchAtts == nil {
		d.launchAtts = map[string][]store.AttachmentReceipt{}
	}
	if len(receipts) == 0 {
		delete(d.launchAtts, loopID)
		return
	}
	d.launchAtts[loopID] = receipts
}

// forgetLaunchAttachments is the restart door: a restart is a NEW launch, and
// the old launch's attachments do not cross into it (the chosen-skills rule,
// carried — the next launch attaches what IT attaches).
func (d *Daemon) forgetLaunchAttachments(loopID string) {
	d.launchAttsMu.Lock()
	defer d.launchAttsMu.Unlock()
	delete(d.launchAtts, loopID)
}

// launchAttachmentsFor answers "what did this launch attach", consulting the
// cache first and the durable kickoff row after a restart. A launch that
// attached nothing returns nil — the byte-identical pre-S77 step.
func (d *Daemon) launchAttachmentsFor(st loop.LoopState) []store.AttachmentReceipt {
	if d == nil || d.store == nil {
		return nil
	}
	d.launchAttsMu.Lock()
	c, ok := d.launchAtts[st.ID]
	d.launchAttsMu.Unlock()
	if ok {
		return c
	}
	// The cache miss IS a restart (a live launch holds its entry). Rebuild
	// from THIS launch's kickoff row only — receipts filed on an earlier
	// run's kickoff are that run's history, never this step's.
	receipts := d.launchAttachmentReceipts(st)
	d.launchAttsMu.Lock()
	if d.launchAtts == nil {
		d.launchAtts = map[string][]store.AttachmentReceipt{}
	}
	d.launchAtts[st.ID] = receipts
	d.launchAttsMu.Unlock()
	return receipts
}

// launchAttachmentReceipts reads the receipts off the row that IS this launch:
// run 1's LINKED kickoff row (S77 links it in CreatePinnedLoop's one
// transaction). Deliberately NARROWER than chosen_skills.go's fallback to
// "the thread's first human row": chosen NAMES can lean on thread order
// because a wrong row costs a sentence of advice; attached BYTES cost a
// megabyte every step and re-tell somebody else's turn as the launch's work.
// No link (a loop born before S77) means no launch attachments — honest
// absence, never a guess.
func (d *Daemon) launchAttachmentReceipts(st loop.LoopState) []store.AttachmentReceipt {
	if run, err := d.store.CurrentRun(st.ID); err == nil && run.RunSeq == 1 && run.KickoffMessageID != nil {
		msg, err := d.store.ReadChatMessage(*run.KickoffMessageID)
		if err == nil && msg.Author == "human" {
			return msg.Attachments
		}
	}
	return nil
}

// launchAttachmentParams attaches a launch's receipts to step params — the
// ONLY place they reach the wire, beside the chosen skills that ride the same
// steps. Nothing attached, nothing attached: the params stay byte-identical to
// every pre-S77 step. The receipts travel as references (Name/Document/Rev/…),
// never bytes — the ONE expansion at the compile boundary turns them into
// content blocks, exactly as a message turn's do.
func (d *Daemon) launchAttachmentParams(st loop.LoopState, params *protocol.StepParams) {
	c := d.launchAttachmentsFor(st)
	if len(c) == 0 {
		return
	}
	// The carriage check runs against the STEP's route, best-effort: an
	// unresolvable route keeps every receipt (the start door already checked
	// the launch's own route, and dropping bytes on a lookup failure would
	// invent an absence the operator never asked for).
	var route model.Route
	var routeOK bool
	if r, rerr := d.Table.RouteForLaunch(params.Route); rerr == nil {
		route, routeOK = r, true
	}
	views := make([]protocol.AttachmentReceiptView, 0, len(c))
	for _, r := range c {
		if req := model.RequiredCapability(r.MediaType); routeOK && req != "" && !route.DeclaresCapability(req) {
			continue // the route moved after the launch: its words ride, this file does not
		}
		views = append(views, protocol.AttachmentReceiptView{
			Name: r.Name, Document: r.Document, Rev: r.Rev,
			MediaType: r.MediaType, Size: r.Size, SHA256: r.SHA256,
		})
	}
	if len(views) == 0 {
		return
	}
	params.Attachments = views
	params.ComponentRoot = st.ComponentRoot
}
