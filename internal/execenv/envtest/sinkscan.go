// Package envtest is the sentinel-scanning test support S59 builds once
// and the later tasks reuse: prove that a string the environment held —
// a credential, a path, a token — never made it into a place a model or a
// person can read it back.
//
// Only `*_test.go` files may import this package (T6 adds the guard that
// says so; the self-test beside it proves the scanner finds what it is
// meant to find before T6 can rely on it).
//
// The scan is deliberately exhaustive rather than clever: it reads every
// column of every table the store owns, the RAW bytes of the state
// database file and its WAL, the logger's buffer, the event payloads, the
// response bodies, and any extra files the caller names — and it looks for
// each sentinel in the forms a sink could have stored it in (plain,
// base64, base64url, hex, percent-encoded), because a sink that
// base64-encodes its values is still a sink that leaked.
package envtest

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/database"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// Sinks is everything a test wants scanned. Every field is optional; a
// nil/zero field is skipped. When Store is set, the raw bytes of its
// state database file (and its WAL, while one exists) are scanned
// automatically, on top of the per-table scan.
type Sinks struct {
	Store     *store.Store
	LogBuf    *bytes.Buffer
	Events    []protocol.Event
	Responses [][]byte
	Files     []string
}

// Finding is one place a sentinel (or one of its encodings) was found.
type Finding struct {
	Sink     string // "store:<table>.<col>" · "log" · "events[i]" · "responses[i]" · "file:<path>"
	Sentinel string // the sentinel it was checked against
	Form     string // "plain" · "base64" · "base64url" · "hex" · "percent"
	Offset   int64  // byte offset of the form within the scanned value
	Snippet  string // bytes around the hit, for the failure message
}

// Scan runs the scanner and fails the test on the first finding, naming
// the sink, the table and column where it lives, and the byte offset.
func Scan(t *testing.T, s Sinks, sentinels ...string) {
	t.Helper()
	for _, f := range scan(s, sentinels) {
		t.Fatalf("S59 sentinel %q reached %s (form %s) at byte offset %d:\n  %.120q",
			f.Sentinel, f.Sink, f.Form, f.Offset, f.Snippet)
	}
}

// scan is the non-fatal core: the self-test asserts on its findings
// directly, and a future caller that wants a report instead of a failure
// gets the same machine.
func scan(s Sinks, sentinels []string) []Finding {
	var findings []Finding
	check := func(sink, value string, at int64) {
		for _, sent := range sentinels {
			for name, form := range forms(sent) {
				if i := strings.Index(value, form); i >= 0 {
					findings = append(findings, Finding{
						Sink:     sink,
						Sentinel: sent,
						Form:     name,
						Offset:   at + int64(i),
						Snippet:  around(value, i, 60),
					})
				}
			}
		}
	}

	if s.Store != nil {
		path := s.Store.Path()
		scanStoreTable(s.Store, path, check)
		for _, f := range []string{path, path + "-wal"} {
			data, err := os.ReadFile(f)
			if err != nil {
				continue // the WAL legitimately does not exist between checkpoints
			}
			check("file:"+filepath.Base(f), string(data), 0)
		}
	}

	if s.LogBuf != nil {
		check("log", s.LogBuf.String(), 0)
	}

	for i, ev := range s.Events {
		body, err := json.Marshal(ev)
		if err != nil {
			continue
		}
		check(fmt.Sprintf("events[%d]", i), string(body), 0)
	}

	for i, resp := range s.Responses {
		check(fmt.Sprintf("responses[%d]", i), string(resp), 0)
	}

	for _, f := range s.Files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		check("file:"+filepath.Base(f), string(data), 0)
	}

	sort.Slice(findings, func(i, j int) bool { return findings[i].Sink < findings[j].Sink })
	return findings
}

// forms is the sentinel in every shape a sink could have stored it in.
func forms(sentinel string) map[string]string {
	b := []byte(sentinel)
	return map[string]string{
		"plain":     sentinel,
		"base64":    base64.StdEncoding.EncodeToString(b),
		"base64url": base64.URLEncoding.EncodeToString(b),
		"hex":       hex.EncodeToString(b),
		"percent":   url.QueryEscape(sentinel),
	}
}

// scanStoreTable reads every column of every table through a second,
// READ-ONLY connection: the scanner must not write to the store it audits
// (a scanner that checkpoints or locks the WAL would disturb the very
// state it is measuring). Tables that cannot be scanned (virtual-table
// shadows and the like) are skipped by the raw file scan, which covers
// their bytes.
//
// The connection opens through internal/database (DriverName, with the
// extensions registered first) exactly as the other non-store readers do
// (loop, tools, daemon tests): this package never imports the CGO driver
// itself, which TestDriverImportedOnlyByDatabaseAndStore holds to those
// two packages.
func scanStoreTable(st *store.Store, path string, check func(sink, value string, at int64)) {
	database.RegisterExtensions()
	db, err := sql.Open(database.DriverName, "file:"+path+"?mode=ro&_busy_timeout=5000")
	if err != nil {
		return
	}
	defer db.Close()

	var tables []string
	rows, err := db.Query("SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
	if err != nil {
		return
	}
	for rows.Next() {
		var name string
		if rows.Scan(&name) == nil {
			tables = append(tables, name)
		}
	}
	rows.Close()

	for _, table := range tables {
		cols, err := columns(db, table)
		if err != nil {
			continue
		}
		for _, col := range cols {
			var sb bytes.Buffer
			crows, err := db.Query(fmt.Sprintf(`SELECT "%s" FROM "%s"`, col, table))
			if err != nil {
				continue
			}
			for crows.Next() {
				var v any
				if crows.Scan(&v) == nil {
					sb.WriteString(stringify(v))
					sb.WriteByte(0) // one column value ends where the next one begins
				}
			}
			crows.Close()
			check("store:"+table+"."+col, sb.String(), 0)
		}
	}
}

func columns(db *sql.DB, table string) ([]string, error) {
	rows, err := db.Query(fmt.Sprintf(`PRAGMA table_info("%s")`, table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var cid, notnull, pk int
		var name, ctype string
		var dflt any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return nil, err
		}
		cols = append(cols, name)
	}
	return cols, rows.Err()
}

func stringify(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case []byte:
		return string(x)
	default:
		return fmt.Sprint(x)
	}
}

func around(s string, at, r int) string {
	lo, hi := at-r, at+r
	if lo < 0 {
		lo = 0
	}
	if hi > len(s) {
		hi = len(s)
	}
	return s[lo:hi]
}
