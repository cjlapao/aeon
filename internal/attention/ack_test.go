package attention

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// ack_test.go — S58 T3 at the derivation layer: what may be written, what
// may not, and that the silent cases (idempotence, expiry) write NOTHING.
// The end-to-end wire behaviour rides in daemon/ack_test.go; the row's
// append-only teeth are T1's store tests.

type rowWrite struct {
	Key, Op, Until, Kind, ID string
}

type fakeMarks struct {
	writes []rowWrite
	err    error
}

func (f *fakeMarks) AppendAttentionAck(key, op, until, kind, id string) (int64, error) {
	if f.err != nil {
		return 0, f.err
	}
	f.writes = append(f.writes, rowWrite{Key: key, Op: op, Until: until, Kind: kind, ID: id})
	return int64(len(f.writes)), nil
}

func fixedNow() time.Time { return time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC) }

func ackSvc(marks *fakeMarks, items []Item) *AckService {
	return &AckService{
		Write:     marks,
		Now:       fixedNow,
		LiveItems: func(context.Context) ([]Item, error) { return items, nil },
	}
}

func liveItem(key string, acked *Ack) Item {
	return Item{Key: key, Kind: "question", Severity: NeedsYou, Since: "2026-11-03T09:00:00Z", Acked: acked}
}

const liveKey = "questions:41:2026-11-03T09:00:00Z"

func TestAckWritesOneRowStampedWithThePeer(t *testing.T) {
	m := &fakeMarks{}
	wrote, err := ackSvc(m, []Item{liveItem(liveKey, nil)}).Ack(context.Background(), "user", "1000", liveKey)
	if err != nil || !wrote {
		t.Fatalf("ack = (%v, %v)", wrote, err)
	}
	if len(m.writes) != 1 {
		t.Fatalf("writes = %+v, want one", m.writes)
	}
	got := m.writes[0]
	if got != (rowWrite{Key: liveKey, Op: "ack", Kind: "user", ID: "1000"}) {
		t.Fatalf("row = %+v, want op=ack, no until, peer identity", got)
	}
}

func TestAckOfAGhostKeyRefused(t *testing.T) {
	m := &fakeMarks{}
	_, err := ackSvc(m, []Item{liveItem("questions:41:2026-11-03T09:00:00Z", nil)}).
		Ack(context.Background(), "user", "1000", "questions:9999:2026-11-03T00:00:00Z")
	if !errors.Is(err, ErrGhostKey) {
		t.Fatalf("ghost ack = %v, want ErrGhostKey", err)
	}
	if len(m.writes) != 0 {
		t.Fatalf("a ghost write reached the table: %+v", m.writes)
	}
}

func TestAckIsIdempotentNoRow(t *testing.T) {
	acked := &Ack{By: "user:1000", At: "2026-11-03T10:00:00Z"}
	m := &fakeMarks{}
	wrote, err := ackSvc(m, []Item{liveItem(liveKey, acked)}).Ack(context.Background(), "user", "1000", liveKey)
	if err != nil || wrote {
		t.Fatalf("second ack = (%v, %v), want (false, nil)", wrote, err)
	}
	if len(m.writes) != 0 {
		t.Fatalf("idempotent ack wrote %+v (P3: one row per CHANGING write)", m.writes)
	}
}

func TestUnackOfVisibleItemIsNoOp(t *testing.T) {
	m := &fakeMarks{}
	wrote, err := ackSvc(m, []Item{liveItem(liveKey, nil)}).Unack(context.Background(), "user", "1000", liveKey)
	if err != nil || wrote || len(m.writes) != 0 {
		t.Fatalf("unack of a visible item = (%v, %v, %+v), want silence", wrote, err, m.writes)
	}
}

func TestUnackOfHiddenItemWrites(t *testing.T) {
	m := &fakeMarks{}
	acked := &Ack{By: "user:1000", At: "2026-11-03T10:00:00Z"}
	wrote, err := ackSvc(m, []Item{liveItem(liveKey, acked)}).Unack(context.Background(), "user", "1000", liveKey)
	if err != nil || !wrote || len(m.writes) != 1 || m.writes[0].Op != "unack" {
		t.Fatalf("unack = (%v, %v, %+v)", wrote, err, m.writes)
	}
}

func TestSnoozeHorizonBounded(t *testing.T) {
	now := fixedNow()
	cases := []struct {
		name  string
		until time.Time
		bad   bool
	}{
		{"past", now.Add(-time.Hour), true},
		{"one second ago", now.Add(-time.Second), true},
		{"seven days plus a minute", now.Add(MaxSnooze + time.Minute), true},
		{"six days", now.Add(24 * 24 * time.Hour / 4), false}, // 6d
		{"exactly the horizon", now.Add(MaxSnooze), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := &fakeMarks{}
			_, err := ackSvc(m, []Item{liveItem(liveKey, nil)}).Snooze(context.Background(), "user", "1", liveKey, c.until)
			if c.bad {
				if !errors.Is(err, ErrSnoozeHorizon) {
					t.Fatalf("until %s: err = %v, want ErrSnoozeHorizon", c.until, err)
				}
				if len(m.writes) != 0 {
					t.Fatalf("a refused snooze wrote %+v", m.writes)
				}
				return
			}
			if err != nil {
				t.Fatalf("until %s inside the horizon refused: %v", c.until, err)
			}
			if len(m.writes) != 1 || m.writes[0].Op != "snooze" || m.writes[0].Until != c.until.UTC().Format(time.RFC3339) {
				t.Fatalf("row = %+v", m.writes)
			}
		})
	}
}

func TestKeyGrammarRefusesJunk(t *testing.T) {
	for _, junk := range []string{
		"", "questions", "questions:41", "questions:41:2026-11-03 09:00", // space-time is not RFC 3339
		"questions::2026-11-03T09:00:00Z", "Question:41:2026-11-03T09:00:00Z", // capitals are not source names
		" questions:41:2026-11-03T09:00:00Z", "questions:41:not-a-time",
	} {
		if err := ValidKey(junk); err == nil {
			t.Errorf("ValidKey(%q) accepted junk", junk)
		}
	}
	if err := ValidKey(liveKey); err != nil {
		t.Fatalf("the mint's own key failed its grammar: %v", err)
	}
}

func TestAckNeedsAnAttributedActor(t *testing.T) {
	m := &fakeMarks{}
	_, err := ackSvc(m, []Item{liveItem(liveKey, nil)}).Ack(context.Background(), "", "", liveKey)
	if err == nil || !strings.Contains(err.Error(), "attributed actor") {
		t.Fatalf("anonymous ack = %v, want a refusal naming the missing attribution", err)
	}
}
