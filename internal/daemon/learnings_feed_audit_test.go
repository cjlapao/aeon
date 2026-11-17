package daemon

// S52 T1 — the triage feed writes audited: the hook stamps itself
// {system, "triage-feed"} and the audit line is the proof the automatic
// memory is attributable. observed is exactly what a system kind may say
// (it saw the attempt conclude); the audit says WHICH hook filed it.

import (
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/triage"
)

func TestFeedFilesAnAuditedSystemAdd(t *testing.T) {
	s := openTriageFeedStore(t)
	d := &Daemon{store: s}
	got, ref := d.feedValidationLearning(triageFeedBug(), triage.ValidationCouldNotTell)
	if ref != nil {
		t.Fatalf("the feed refused: %s", ref.Reason)
	}
	lines, err := s.ListLearningChanges(got.ID)
	if err != nil {
		t.Fatalf("changes: %v", err)
	}
	if len(lines) != 1 || lines[0].Op != "add" || lines[0].ActorKind != "system" || lines[0].ActorID != "triage-feed" {
		t.Fatalf("audit after a feed write = %+v, want the one add by system/triage-feed", lines)
	}
	if !strings.Contains(lines[0].Summary, "observed") {
		t.Fatalf("summary %q does not say the feed filed an observation", lines[0].Summary)
	}
}
