package store

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func receiptStep(t *testing.T) (*Store, int64) {
	t.Helper()
	s := openTemp(t)
	sc := seedLoop(t, s, "receipt", "/receipt-work")
	return s, seedStep(t, s, sc).ID
}

func TestReceiptRoundTrip(t *testing.T) {
	s, id := receiptStep(t)
	ctx := context.Background()
	digest := ArgDigest("bash", json.RawMessage(`{"cmd":"true"}`))
	if err := s.BeginReceipt(ctx, id, "u1", "bash", digest); err != nil {
		t.Fatal(err)
	}
	if err := s.CompleteReceipt(ctx, id, "u1", "", false); err != nil {
		t.Fatal(err)
	}
	got, err := s.Receipts(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].CompletedAt == nil || got[0].Result == nil || *got[0].Result != "" || got[0].IsError == nil || *got[0].IsError || got[0].ArgDigest != digest {
		t.Fatalf("completed empty receipt = %+v", got)
	}
}

func TestReceiptIncompleteReadsAsNull(t *testing.T) {
	s, id := receiptStep(t)
	ctx := context.Background()
	if err := s.BeginReceipt(ctx, id, "u1", "bash", "digest"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Receipts(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].CompletedAt != nil || got[0].Result != nil || got[0].IsError != nil {
		t.Fatalf("incomplete receipt = %+v", got)
	}
}

func TestReceiptSchemaFreshEqualsMigrated(t *testing.T) {
	fresh := openTemp(t)
	upgraded := upgradedStore(t)
	a, err := fresh.SchemaFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	b, err := upgraded.SchemaFingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if !a.Equal(b) {
		t.Fatalf("fresh and upgraded schemas differ: %s", strings.Join(a.Diff(b), "; "))
	}
	ledger, err := upgraded.AppliedMigrations()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range ledger {
		if m.Name == "0011_tool_receipt" && m.Status == "applied" {
			found = true
		}
	}
	if !found {
		t.Fatal("receipt migration missing from ledger")
	}
}

func TestReceiptCascadesWithStep(t *testing.T) {
	s, id := receiptStep(t)
	ctx := context.Background()
	if err := s.BeginReceipt(ctx, id, "u1", "bash", "digest"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.w.Exec(`DELETE FROM step WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	got, err := s.Receipts(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("cascade left receipts: %+v", got)
	}
}

func TestReceiptDoubleBeginFails(t *testing.T) {
	s, id := receiptStep(t)
	ctx := context.Background()
	if err := s.BeginReceipt(ctx, id, "u1", "bash", "digest"); err != nil {
		t.Fatal(err)
	}
	if err := s.BeginReceipt(ctx, id, "u1", "bash", "digest"); err == nil {
		t.Fatal("second begin overwrote an existing receipt")
	}
}

func TestArgDigestSeparatesToolFromInput(t *testing.T) {
	a := ArgDigest("a", json.RawMessage(`{"x":"b"}`))
	for _, pair := range []struct{ tool, input string }{{"ab", `{"x":""}`}, {"b", `{"x":"a"}`}, {"a", `{"x":"c"}`}} {
		if a == ArgDigest(pair.tool, json.RawMessage(pair.input)) {
			t.Fatalf("digest collision for %q %s", pair.tool, pair.input)
		}
	}
}
