package envtest

// sinkscan_test.go — the self-test (S59 T1). A scanner that ships green
// while it finds nothing is worse than no scanner: T6 will wire it into
// the daemon's end-to-end proof, and the one property it must have —
// that it FINDS what it is pointed at — is measured here, before it is
// trusted.
//
// A sentinel is planted in every sink kind the scanner covers (a table
// column, the raw database file that column's bytes end up in, the logger
// buffer, an event payload, a response body, an extra file), in several
// encodings, and the scanner is held to naming each one. The negative
// control — a clean store, nothing planted — must come back empty, so a
// scanner that cries wolf fails here too.

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/cjlapao/aeon/internal/database"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

const (
	canaryPlain = "s59-sinkscan-plain-sentinel-7741"
	canaryB64   = "s59-sinkscan-b64-sentinel-042"
)

func hasFinding(fs []Finding, sink, sentinel, form string) bool {
	for _, f := range fs {
		if f.Sink == sink && f.Sentinel == sentinel && f.Form == form {
			return true
		}
	}
	return false
}

func TestSinkScanFindsAPlantedSentinel(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(store.Options{Dir: dir, Env: func(string) string { return "" }})
	if err != nil {
		t.Fatalf("throwaway store: %v", err)
	}
	defer st.Close()

	// 1. A table column: one sentinel plain, its twin base64-encoded, in
	// the same throwaway table the scanner must name.
	database.RegisterExtensions()
	db, err := sql.Open(database.DriverName, st.Path())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE sinkscan_canary (v TEXT, w TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO sinkscan_canary VALUES (?, ?)`, canaryPlain, base64.StdEncoding.EncodeToString([]byte(canaryB64))); err != nil {
		t.Fatal(err)
	}
	db.Close()

	// 2. The other sinks, each in a different encoding. The event is an
	// envelope with a payload, the shape every daemon event has.
	logBuf := &bytes.Buffer{}
	logBuf.WriteString("a log line carrying " + canaryPlain + " ends here\n")
	events := []protocol.Event{{Kind: "sinkscan.canary",
		Payload: json.RawMessage(`{"command":"` + canaryPlain + `"}`)}}
	responses := [][]byte{[]byte(url.QueryEscape(canaryB64))}
	file := filepath.Join(t.TempDir(), "canary.txt")
	if err := os.WriteFile(file, []byte(hex.EncodeToString([]byte(canaryPlain))), 0o644); err != nil {
		t.Fatal(err)
	}

	sinks := Sinks{
		Store:     st,
		LogBuf:    logBuf,
		Events:    events,
		Responses: responses,
		Files:     []string{file},
	}
	fs := scan(sinks, []string{canaryPlain, canaryB64})

	// The table, by name and column — the sink naming the spec asks for.
	if !hasFinding(fs, "store:sinkscan_canary.v", canaryPlain, "plain") {
		t.Errorf("the planted plain sentinel was not found in store:sinkscan_canary.v: %v", fs)
	}
	if !hasFinding(fs, "store:sinkscan_canary.w", canaryB64, "base64") {
		t.Errorf("the planted base64 sentinel was not found in store:sinkscan_canary.w: %v", fs)
	}
	// The raw bytes of the state database file (the column's bytes live in
	// the file or its WAL, and the scan must cover both).
	rawFile := hasFinding(fs, "file:state.db", canaryPlain, "plain")
	rawWAL := hasFinding(fs, "file:state.db-wal", canaryPlain, "plain")
	if !rawFile && !rawWAL {
		t.Errorf("the raw state database bytes did not carry the planted sentinel in file:state.db or its WAL: %v", fs)
	}
	// The non-store sinks, in the encodings they were planted in.
	if !hasFinding(fs, "log", canaryPlain, "plain") {
		t.Errorf("the log buffer sink was not found: %v", fs)
	}
	if !hasFinding(fs, "events[0]", canaryPlain, "plain") {
		t.Errorf("the event sink was not found: %v", fs)
	}
	if !hasFinding(fs, "responses[0]", canaryB64, "percent") {
		t.Errorf("the response sink was not found (planted percent-encoded): %v", fs)
	}
	if !hasFinding(fs, "file:canary.txt", canaryPlain, "hex") {
		t.Errorf("the extra-file sink was not found (planted hex): %v", fs)
	}

	// The finding must name its offset: the offset of the plain canary in
	// the log line is where the line's text says it is.
	for _, f := range fs {
		if f.Sink == "log" && f.Sentinel == canaryPlain {
			got := logBuf.String()
			if want := "a log line carrying "; got[f.Offset-int64(len(want)):f.Offset] != want {
				t.Errorf("the finding's offset does not point at the hit: offset %d, prefix %q", f.Offset, got[f.Offset-int64(len(want)):f.Offset])
			}
			break
		}
	}
}

// The negative control: a clean store and empty sinks produce no finding.
// A scanner that reports hits on an empty database would make every T6
// proof meaningless, and this is where that failure is caught.
func TestSinkScanIsSilentOnCleanSinks(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(store.Options{Dir: dir, Env: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	logBuf := &bytes.Buffer{}
	logBuf.WriteString("nothing interesting in this line\n")
	fs := scan(Sinks{Store: st, LogBuf: logBuf}, []string{canaryPlain, canaryB64})
	if len(fs) != 0 {
		t.Fatalf("the scanner found what is not there: %v", fs)
	}
}
