package protocol

import (
	"strings"
	"testing"
)

// D5 — asserted over a RUN. "Monotonic" is trivially true of one event; what
// matters is no gaps and no duplicates across a sequence.
func TestSeqIsMonotonicWithNoGapsOrDuplicatesOverTwentyEvents(t *testing.T) {
	log := NewEventLog()
	seen := map[uint64]bool{}
	var last uint64

	for i := 0; i < 20; i++ {
		ev, err := log.Append("loop.started", "/r", nil)
		if err != nil {
			t.Fatal(err)
		}
		if seen[ev.Seq] {
			t.Fatalf("seq %d was allocated twice", ev.Seq)
		}
		seen[ev.Seq] = true
		if ev.Seq <= last {
			t.Fatalf("seq went backwards or repeated: %d after %d", ev.Seq, last)
		}
		if last != 0 && ev.Seq != last+1 {
			t.Fatalf("gap in seq: %d after %d", ev.Seq, last)
		}
		last = ev.Seq
	}
	if log.Head() != 20 {
		t.Fatalf("head = %d, want 20", log.Head())
	}
}

// Concurrency is where "monotonic" quietly becomes false: allocate the number
// outside the lock and two emitters write the ring out of order, so the counter
// is monotonic and what a subscriber receives is not.
func TestSeqIsAllocatedUnderOneLockSoConcurrentEmittersCannotInterleave(t *testing.T) {
	log := NewEventLog()
	const n = 200
	done := make(chan uint64, n)
	for i := 0; i < n; i++ {
		go func() {
			ev, err := log.Append("loop.started", "/r", nil)
			if err != nil {
				t.Error(err)
			}
			done <- ev.Seq
		}()
	}
	seen := map[uint64]bool{}
	for i := 0; i < n; i++ {
		seq := <-done
		if seen[seq] {
			t.Fatalf("seq %d allocated twice under concurrency", seq)
		}
		seen[seq] = true
	}
	if log.Head() != n {
		t.Fatalf("head = %d, want %d", log.Head(), n)
	}
	// The ring itself must be in seq order, which is the part a counter test
	// would miss.
	events := log.Since(0, Filter{})
	for i := 1; i < len(events); i++ {
		if events[i].Seq <= events[i-1].Seq {
			t.Fatalf("the retained log is out of seq order at %d: %d after %d",
				i, events[i].Seq, events[i-1].Seq)
		}
	}
}

// D5 — three cursor positions, each replaying exactly the tail.
func TestAfterReplaysExactlyTheTailAndReportsFrom(t *testing.T) {
	log := NewEventLog()
	for i := 0; i < 20; i++ {
		if _, err := log.Append("loop.started", "/r", nil); err != nil {
			t.Fatal(err)
		}
	}

	for _, after := range []uint64{0, 5, 19} {
		t.Run("after="+itoa(int(after)), func(t *testing.T) {
			from, replay := log.Resume(&after, Filter{})
			if from != after+1 {
				t.Fatalf("from = %d, want %d", from, after+1)
			}
			want := 20 - int(after)
			if len(replay) != want {
				t.Fatalf("replayed %d events, want %d", len(replay), want)
			}
			// Exactly the tail: no gap, no duplicate, no reordering.
			for i, ev := range replay {
				if ev.Seq != after+1+uint64(i) {
					t.Fatalf("replay[%d].Seq = %d, want %d", i, ev.Seq, after+1+uint64(i))
				}
			}
		})
	}
}

// Not an error: it means "nothing yet". A client that saw more than the daemon
// has is a client talking to a restarted daemon, which O2 says will happen.
func TestAfterBeyondTheHeadIsNotAnErrorAndReportsHeadPlusOne(t *testing.T) {
	log := NewEventLog()
	for i := 0; i < 3; i++ {
		if _, err := log.Append("loop.started", "/r", nil); err != nil {
			t.Fatal(err)
		}
	}
	beyond := uint64(999)
	from, replay := log.Resume(&beyond, Filter{})
	if from != 4 {
		t.Fatalf("from = %d, want head+1 = 4", from)
	}
	if len(replay) != 0 {
		t.Fatalf("replayed %d events for a cursor beyond the head", len(replay))
	}
}

// Nil `after` means "from now" — no replay at all.
func TestNoAfterMeansStartAtTheHead(t *testing.T) {
	log := NewEventLog()
	for i := 0; i < 5; i++ {
		if _, err := log.Append("loop.started", "/r", nil); err != nil {
			t.Fatal(err)
		}
	}
	from, replay := log.Resume(nil, Filter{})
	if from != 6 || len(replay) != 0 {
		t.Fatalf("from = %d with %d replayed; want 6 and 0", from, len(replay))
	}
}

// The honest-truncation case, and the reason `from` exists at all: a cursor
// older than the ring resumes at the oldest retained seq and SAYS SO, so the
// client knows it missed the difference rather than assuming continuity.
func TestACursorOlderThanTheRingResumesAtTheOldestAndReportsTheGap(t *testing.T) {
	log := NewEventLogWithRetention(5)
	for i := 0; i < 20; i++ {
		if _, err := log.Append("loop.started", "/r", nil); err != nil {
			t.Fatal(err)
		}
	}
	// Retained: 16..20. A client resuming from 2 cannot be served 3..15.
	after := uint64(2)
	from, replay := log.Resume(&after, Filter{})

	if from == after+1 {
		t.Fatal("from reported 3, which the daemon cannot serve — a client would " +
			"assume continuity across a gap of thirteen events")
	}
	if from != 16 {
		t.Fatalf("from = %d, want the oldest retained seq 16", from)
	}
	if len(replay) != 5 {
		t.Fatalf("replayed %d, want the 5 retained", len(replay))
	}
	if replay[0].Seq != 16 {
		t.Fatalf("replay starts at %d, want 16", replay[0].Seq)
	}
	if log.Oldest() != 16 {
		t.Fatalf("Oldest = %d, want 16", log.Oldest())
	}
}

// The replay honours the filter, or a reconnecting client receives the whole
// machine's history and discards it — the same failure as an unfiltered
// subscription, arriving only on reconnect.
func TestReplayIsFiltered(t *testing.T) {
	log := NewEventLog()
	for i := 0; i < 5; i++ {
		if _, err := log.Append("loop.started", "/repo-a", nil); err != nil {
			t.Fatal(err)
		}
		if _, err := log.Append("step.started", "/repo-b", nil); err != nil {
			t.Fatal(err)
		}
	}
	zero := uint64(0)
	_, replay := log.Resume(&zero, Filter{ComponentRoot: "/repo-a", Kinds: []string{"loop.*"}})
	if len(replay) != 5 {
		t.Fatalf("replayed %d events, want the 5 matching ones", len(replay))
	}
	for _, ev := range replay {
		if ev.ComponentRoot != "/repo-a" || ev.Kind != "loop.started" {
			t.Fatalf("replay included %s from %s", ev.Kind, ev.ComponentRoot)
		}
	}
}

// S4 CLOSED S2's gap, and this guard flipped with it.
//
// # What this test used to be
//
// S2 shipped `seq` as an in-memory counter that reset on every daemon restart,
// and this test asserted that the SOURCE SAID SO — the words "IN-MEMORY" and
// "resets on restart" had to appear in events.go, because a limitation that
// lives only in a design document is a limitation nobody reads.
//
// S4's T8 made `seq` the `event` table's rowid. So the honest guard is now the
// inverse: the source must document that it is DURABLE and must no longer claim
// it resets. Deleting the test would have been the easy move and the wrong one
// — the thing worth guarding is that the code and the comment agree, in
// whichever direction that happens to be.
func TestSeqIsDocumentedAsDurableNowThatS4BacksIt(t *testing.T) {
	src, err := readSource("events.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"DURABLE",
		"S4",
		"event` table",
		"AUTOINCREMENT",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("events.go does not document %q — a cursor's durability is the one promise this "+
				"type makes to a client", want)
		}
	}
	// And it must not still tell a reader the old story. Two documents
	// disagreeing is worse than either, because a reader trusts whichever they
	// find first.
	for _, gone := range []string{"resets on restart", "until then the demo"} {
		if strings.Contains(src, gone) {
			t.Errorf("events.go still says %q, which S4 made untrue", gone)
		}
	}
}

// The deviation from N6 is recorded in the code, not only in a task file.
func TestTheRetentionDeviationFromN6IsRecordedInTheSource(t *testing.T) {
	src, err := readSource("events.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"N6", "bounded"} {
		if !strings.Contains(src, want) {
			t.Errorf("events.go does not explain the bounded ring against N6 (%q)", want)
		}
	}
}

// The four lifecycle kinds S2 contributed are all present.
//
// Asserted as a SUBSET rather than as the whole list: `Kinds()` grows with each
// slice, additively, and a test demanding exactly four would fail on every
// slice that adds an event kind — which is a test that gets deleted rather than
// understood. S3 adding `ask.*` is what first made that true.
func TestTheFourLifecycleKindsAreDeclared(t *testing.T) {
	declared := map[string]bool{}
	for _, k := range Kinds() {
		declared[k] = true
	}
	for _, want := range []string{
		"daemon.started", "client.connected", "client.disconnected", "subscription.created",
	} {
		if !declared[want] {
			t.Errorf("%q is not in Kinds()", want)
		}
	}
}

// Every declared kind is unique. A duplicate would make a filter match twice
// and a fixture-coverage check pass on half the set.
func TestEveryDeclaredKindIsUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, k := range Kinds() {
		if seen[k] {
			t.Errorf("kind %q is declared twice", k)
		}
		seen[k] = true
	}
	if len(Kinds()) < 6 {
		t.Errorf("Kinds() has %d entries; S2 contributed four and S3 two", len(Kinds()))
	}
}

func TestAnEventCarriesATimestamp(t *testing.T) {
	log := NewEventLog()
	ev, err := log.Append("daemon.started", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if ev.At == "" {
		t.Fatal("no timestamp")
	}
	if !strings.Contains(ev.At, "T") || !strings.HasSuffix(ev.At, "Z") {
		t.Errorf("At = %q, want RFC3339 in UTC", ev.At)
	}
}
