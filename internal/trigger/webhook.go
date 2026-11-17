package trigger

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/cjlapao/aeon/internal/label"
)

// webhook.go — TR9, TR10 and TR11 made code: the signature proves WHO, the
// payload is a stranger, and no fourth door opens.
//
// # The order, and why it is THIS order
//
// signature → replay → overlap → governance → start.
//
// The signature first because a bad signature must cost the daemon nothing
// but a hash and a constant-time compare — no store read beyond the row
// that names the secret's ENV VAR, no gate touched, nothing started.
// Governance (T7) deliberately comes AFTER authentication (TR9's order in
// the design's own words, signature-then-policy) — but authentication is
// not the answer to "may this fire"; a verified delivery still faces the
// gates, and the test that proves it lives in T7 where the evaluator does.
//
// # The secret (C17)
//
// The row stores only the NAME of the environment variable
// (secret_env — T1's "named, never stored"); the value is read here with a
// single os.Getenv, and the only place it can go is the HMAC. It never
// reaches Fire, a record, a reason, or a spoken line (the tests read the
// secret's absence out of every artefact a delivery can leave). argv and
// config files are not secret sources — this package reads neither (the
// grep test holds the absence).
//
// # Replay (scope 3, mechanism and bound STATED)
//
// Key: the caller's delivery id when supplied, else the SHA-256 digest of
// the payload bytes. Bound: an in-process cache of the last 4,096 keys,
// plus a ±5-minute window on the supplied timestamp when one is present.
// In-process is honest and it is enough: a replay is a resend seconds or
// minutes old (the sender's retry), and a restart that "loses" the cache
// has also ended the resend storm — the digest catches anything that
// arrives inside the same process's memory. A delivery that arrives with
// NO timestamp relies on the digest alone (GitHub signs bodies without a
// timestamp param; refusing the honest sender would train senders to drop
// headers — the refusal lands on what the signature cannot prove).

// ReplayWindow bounds how stale a timestamped delivery may be.
const ReplayWindow = 5 * time.Minute

// replayBound caps the seen-delivery cache (4,096 digests ≈ 130 KB — the
// bound exists so a flood cannot grow the daemon's memory without limit;
// eviction is oldest-first).
const replayBound = 4096

type replayCache struct {
	mu    sync.Mutex
	seen  map[string]time.Time
	order []string
}

var webhookReplays = &replayCache{seen: map[string]time.Time{}}

// remember returns true when the key was already present (a replay) and
// records it either way — first sight answers, second sight refuses.
func (c *replayCache) remember(key string, at time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, dup := c.seen[key]; dup {
		return true
	}
	c.seen[key] = at
	c.order = append(c.order, key)
	for len(c.order) > replayBound {
		delete(c.seen, c.order[0])
		c.order = c.order[1:]
	}
	return false
}

// ResetWebhookReplays is the test seam; production never calls it (the
// cache's whole life is the process's life).
func ResetWebhookReplays() {
	webhookReplays = &replayCache{seen: map[string]time.Time{}}
}

// VerifyWebhookSignature is HMAC-SHA256 over the EXACT payload bytes, hex,
// constant-time, accepting GitHub's "sha256=" prefix as spelling — the same
// secret, the same bytes, the same result on both mountings (R-6), because
// stdio has no headers and the surface stays one protocol (P25).
func VerifyWebhookSignature(secret string, payload []byte, signature string) bool {
	if secret == "" || signature == "" {
		return false
	}
	sig := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(signature)), "sha256=")
	want, err := hex.DecodeString(sig)
	if err != nil || len(want) != sha256.Size {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return subtle.ConstantTimeCompare(want, mac.Sum(nil)) == 1
}

// WebhookAnswer is the discriminated answer a delivery gets — never a bare
// error: "did this fire?" asked through a POST deserves the same five
// answers the clock's records hold (TR4).
type WebhookAnswer struct {
	TriggerID string
	Outcome   Outcome
	Reason    string
	LoopID    string
	// HTTPStatus is where the projection maps the answer — the narrow
	// mounting renders it, this package does not know HTTP exists.
	HTTPStatus int
}

// webhookSecret reads the secret from the named environment variable. The
// two failure sentences are distinct on purpose: an operator who named no
// variable configured a door with no lock cylinder; an operator whose var
// is empty has a lock with no key. Both refuse; neither says anything about
// the secret's value (there is nothing to say, and the sentence never has
// one — C17 tested from the OUTSIDE).
func webhookSecret(envName string) (string, string) { // (secret, refusalReason)
	if envName == "" {
		return "", "the trigger row names no secret environment variable — an unauthenticated webhook is a door anyone can knock on"
	}
	v := os.Getenv(envName)
	if v == "" {
		return "", "the named secret environment variable is not set — delivery refused (the secret's name travels, its value never does)"
	}
	return v, ""
}

// DeliverWebhook is the whole answer to one delivery, gates in order. The
// payload enters the loop through Fire.With — labelled at the door with
// S12's own mechanism (label.Region, the same grammar as W23/MC7/AS12, not
// a fifth one): Q4's answer is that the VALUE enters through `with` and
// the LABEL rides inside the value, which is how the manifest carries it
// and S7's replay sees exactly what the model saw.
func (sc *Scheduler) DeliverWebhook(ctx context.Context, id string, payload []byte, signature, timestamp, deliveryID, event string, now time.Time) WebhookAnswer {
	answer := func(o Outcome, reason, loopID string, status int) WebhookAnswer {
		return WebhookAnswer{TriggerID: id, Outcome: o, Reason: reason, LoopID: loopID, HTTPStatus: status}
	}
	scheds, err := sc.Store.ActiveSchedules(ctx)
	if err != nil {
		return answer(OutcomeCouldNotSay, "the trigger table could not be read: "+err.Error(), "", 503)
	}
	var row *Schedule
	for i := range scheds {
		if scheds[i].ID == id && scheds[i].Kind == KindWebhook {
			row = &scheds[i]
			break
		}
	}
	if row == nil {
		// Nothing to record against (no row, no FK) — and the absence is
		// the answer, phrased so a 404 cannot read as "the webhook exists
		// and disliked you".
		return answer(OutcomeDidNotFire, "no webhook trigger "+id+" is registered (a schedule row of a different kind does not answer this door)", "", 404)
	}
	if row.Paused {
		f := webhookFire(*row, now, id, nil)
		sc.answer(ctx, *row, f, OutcomeDidNotFire, "the webhook is paused — a paused trigger answers nothing, and says so")
		return answer(OutcomeDidNotFire, "the webhook is paused", "", 409)
	}
	// 1. Signature (before everything else that costs).
	secret, refusal := webhookSecret(row.SecretEnv)
	if refusal != "" {
		f := webhookFire(*row, now, id, nil)
		sc.answer(ctx, *row, f, OutcomeDidNotFire, refusal)
		return answer(OutcomeDidNotFire, refusal, "", 401)
	}
	if !VerifyWebhookSignature(secret, payload, signature) {
		reason := "the signature did not verify — an unauthenticated delivery starts nothing (a valid signature would prove who sent it, never what they may cause)"
		f := webhookFire(*row, now, id, nil)
		sc.answer(ctx, *row, f, OutcomeDidNotFire, reason)
		return answer(OutcomeDidNotFire, reason, "", 401)
	}
	// 1b. The filter: a signed delivery of an event this webhook does not listen for is answered, not acted on, and answered
	// with a 200 so the sender does not read a deliberate "not for me" as a failure and retry it.
	if ok, why := MatchEventFilter(row.Listens, event, payload); !ok {
		f := webhookFire(*row, now, id, nil)
		sc.answer(ctx, *row, f, OutcomeDidNotFire, "not for this webhook: "+why)
		return answer(OutcomeDidNotFire, "not for this webhook: "+why, "", 200)
	}
	// 2. Replay.
	key := strings.TrimSpace(deliveryID)
	if key == "" {
		key = "digest:" + fmt.Sprintf("%x", sha256.Sum256(payload))
	} else {
		key = "delivery:" + key
	}
	if !timestampIsFresh(timestamp, now) {
		reason := "the delivery timestamp is outside the ±5-minute replay window — an old delivery is a resend, not an event"
		f := webhookFire(*row, now, id, nil)
		sc.answer(ctx, *row, f, OutcomeDidNotFire, reason)
		return answer(OutcomeDidNotFire, reason, "", 409)
	}
	if webhookReplays.remember(key, now) {
		reason := "this delivery was already answered — a payload delivered twice is refused the second time (TR-scope-3)"
		f := webhookFire(*row, now, id, nil)
		sc.answer(ctx, *row, f, OutcomeDidNotFire, reason)
		return answer(OutcomeDidNotFire, reason, "", 409)
	}
	// 3+4. The SAME gates the clock walks (overlap, then governance).
	if sc.Gate.Overlap != nil {
		if blocked, prev := sc.Gate.Overlap(*row, now); blocked {
			f := webhookFire(*row, now, id, nil)
			sc.answer(ctx, *row, f, OutcomeSkipped, "the previous run is still going (loop "+prev+") — an overlapping delivery is skipped and recorded (TR5)")
			return answer(OutcomeSkipped, "the previous run is still going (loop "+prev+")", "", 409)
		}
	}
	if sc.Gate.Access != nil {
		if allow, reason := sc.Gate.Access(ProvenanceWebhook, row.ID); !allow {
			// Scope 3's parenthesis, earned by the ORDER: the signature was
			// checked first and PASSED, so the refusal may honestly say so —
			// and must, because the sentence is the lesson: authentication
			// is not authorization (TR9). The parenthesis rides the record,
			// not just the HTTP body.
			reason = reason + " (signature verified — authentication is not authorization)"
			f := webhookFire(*row, now, id, nil)
			sc.answer(ctx, *row, f, OutcomeRefused, reason)
			return answer(OutcomeRefused, reason, "", 403)
		}
	}
	// 5. Start — the ONE door (T2's Starter), webhook provenance, payload
	// labelled at the door.
	with := map[string]string{
		"payload":       label.Region("webhook payload for trigger "+id+" (delivered over the network, sender verified by signature ONLY — signatures prove origin, not permission)", string(payload)),
		"payloadOrigin": "webhook:" + id,
	}
	f := webhookFire(*row, now, id, with)
	// The goal may carry a few fields of the payload (one line each, cut short): data in the sentence, never an instruction.
	f.Goal = RenderGoal(f.Goal, payload)
	loopID, err := sc.Starter.StartFromTrigger(ctx, f)
	if err != nil {
		reason := "the start was refused: " + err.Error()
		sc.answer(ctx, *row, f, OutcomeRefused, reason)
		return answer(OutcomeRefused, reason, "", 503)
	}
	f.Outcome, f.LoopID = OutcomeFired, loopID
	if err := sc.Store.RecordFire(ctx, f); err != nil {
		sc.speak("webhook %s: fired loop %s but the record failed: %v", id, loopID, err)
	}
	sc.speak("webhook %s: delivery signed, fresh, and started loop %s — the payload travels labelled (TR10)", id, loopID)
	return answer(OutcomeFired, "", loopID, 200)
}

func webhookFire(s Schedule, at time.Time, id string, with map[string]string) Fire {
	goal := s.Goal
	if goal == "" {
		goal = fmt.Sprintf("webhook delivery for %s at %s", id, at.UTC().Format(time.RFC3339))
	}
	return Fire{
		TriggerID: id, Blueprint: s.Blueprint, Workspace: s.Workspace, Goal: goal,
		With: with, Provenance: ProvenanceWebhook, Instant: at.UTC(), At: at.UTC(),
	}
}

func timestampIsFresh(raw string, now time.Time) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return true // no timestamp claimed: the digest carries the replay check (stated above)
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return false // a malformed timestamp is a sender that cannot be trusted to bound its own resends
	}
	d := now.Sub(t)
	if d < 0 {
		d = -d
	}
	return d <= ReplayWindow
}
