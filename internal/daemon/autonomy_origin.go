package daemon

// autonomy_origin.go — S56 AL7: the closed set of start origins that consult
// the policy at pin time. The origins themselves are autonomy.Origin (the
// pure core owns them); this file is the daemon's mapping from a fire's
// provenance to the origin, plus the one fact the slice must hold — that
// OriginScheduler exists but has no caller here (it is reserved for S27).

import (
	"github.com/cjlapao/aeon/internal/autonomy"
	"github.com/cjlapao/aeon/internal/trigger"
)

// autonomyOriginFor maps a fire's provenance to the start origin (AL7). The
// schedule and webhook origins are the clock's two fires; anything else is an
// event (the dispatcher's path). Scheduler is deliberately absent — it has no
// caller in this slice, so the map never names it.
func autonomyOriginFor(provenance string) autonomy.Origin {
	switch provenance {
	case trigger.ProvenanceTrigger:
		return autonomy.OriginSchedule
	case trigger.ProvenanceWebhook:
		return autonomy.OriginWebhook
	default:
		return autonomy.OriginEvent
	}
}

// schedulerOrigin exists so the constant is referenced by the test that pins
// it (S27 is its only expected user; it must not reach a start in this
// slice). It is not called by any start path here.
var _ = autonomy.OriginScheduler
