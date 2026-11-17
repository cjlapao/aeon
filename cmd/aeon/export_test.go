package main

// export_test.go — S62 T8 (BK14/P5): the two doors, the direct door's fixed
// line, and the ledger's honesty.
//
// The parity claim is that `aeon export` answers from ONE implementation
// whichever door it speaks: the daemon's door (export.run over the socket,
// the ledger row written) and the direct door (the store opened read-only,
// no ledger row) both run the same export engine and render through the same
// printer. The two doors differ in exactly the two facts the door owns — the
// manifest's actor and generated_at, and the ledger's recorded flag — and in
// nothing else: every record file is byte-identical, and the wire answer is
// equal apart from the door's own dir and recorded.

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/daemon"
	"github.com/cjlapao/aeon/internal/export"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// exportTestStore opens an isolated, fresh store under a throwaway state dir
// (the standing rule: tests never point at the real store) and plants one
// agent row so the agents set is non-empty — a tree with one real file to
// compare, not just empty directories.
func exportTestStore(t *testing.T) *store.Store {
	t.Helper()
	isolateStoreEnv(t)
	agentHome(t)
	st, err := store.Open(store.Options{})
	if err != nil {
		t.Fatalf("open the temp store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.CreateAgentAudited(context.Background(), store.AgentActor{Kind: store.AgentActorUser, ID: "1000"}, store.Agent{Slug: "atlas", Name: "atlas", Version: "1"}, "2026-11-02T12:00:00Z"); err != nil {
		t.Fatalf("plant atlas: %v", err)
	}
	return st
}

// deadEndpoint points the CLI's dial at a socket nothing serves, so the
// socket-first verbs take their direct door — deterministically, without a
// real daemon having to be (or not be) running.
func deadEndpoint(t *testing.T) {
	t.Helper()
	t.Setenv("LOCALRPC_ENDPOINT_PATH", filepath.Join(t.TempDir(), "nothing.sock"))
}

// exportTree walks an export dir into a relpath->bytes map, so two trees can
// be compared file by file without depending on walk order.
func exportTree(t *testing.T, root string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return rerr
		}
		b, berr := os.ReadFile(p)
		if berr != nil {
			return berr
		}
		out[filepath.ToSlash(rel)] = b
		return nil
	})
	if err != nil {
		t.Fatalf("walk the export tree %s: %v", root, err)
	}
	return out
}

func exportTreeKeys(m map[string][]byte) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// TestExportParityDaemonAndCLI is behaviour 8 (P5): the two doors over one
// fresh store answer the same way. The socket door is the real daemon's table
// over an in-memory pipe (registered the way `aeon serve` wires it); the
// direct door is the real `aeon export --json` against a dead endpoint, which
// opens the same store read-only. The wire answers are equal apart from the
// door's own dir and recorded flag, and the trees are byte-identical apart
// from the manifest, whose actor and generated_at are the door's to stamp.
func TestExportParityDaemonAndCLI(t *testing.T) {
	st := exportTestStore(t)
	dirA := filepath.Join(t.TempDir(), "via-daemon")
	dirB := filepath.Join(t.TempDir(), "via-direct")

	// --- the socket door: the daemon's own table over an in-memory pipe ---
	// DaemonVersion is the binary's own Version, the same one the direct door
	// stamps: the manifest and the README carry it, and the parity claim is
	// that the doors differ only in the door's own actor and generated_at.
	d := daemon.New(daemon.Options{Store: st, DaemonVersion: Version})
	c := protocol.NewClient(parityPipeServer(t, d.Table), "parity-test", "0")
	t.Cleanup(func() { _ = c.Close() })
	if _, err := c.Connect(0); err != nil {
		t.Fatalf("handshake over the in-memory pipe: %v", err)
	}
	var socketRes protocol.ExportRunResult
	if err := c.Call(protocol.MethodExportRun, protocol.ExportRunParams{Dir: dirA}, &socketRes); err != nil {
		t.Fatalf("socket export.run: %v", err)
	}

	// --- the direct door: the real verb, the endpoint dead ---
	deadEndpoint(t)
	var out strings.Builder
	if err := cmdExport(&out, []string{"--dir", dirB, "--json"}); err != nil {
		t.Fatalf("direct `aeon export --json`: %v", err)
	}
	var directRes protocol.ExportRunResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(out.String())), &directRes); err != nil {
		t.Fatalf("decode the direct door's --json: %v (got %q)", err, out.String())
	}

	// The door's own facts: the socket door recorded the ledger's row; the
	// direct door did not. Each names the dir it filled.
	if !socketRes.Recorded {
		t.Fatalf("the socket door must have recorded its ledger row")
	}
	if directRes.Recorded {
		t.Fatalf("the direct door must not record a ledger row")
	}
	if socketRes.Dir != dirA || directRes.Dir != dirB {
		t.Fatalf("the door names its own dir: socket=%q direct=%q", socketRes.Dir, directRes.Dir)
	}

	// The shared answer: the sets and the file count are the door's to render
	// and the engine's to decide — so they are equal, byte for byte.
	if !reflect.DeepEqual(socketRes.Sets, directRes.Sets) {
		t.Fatalf("the two doors report different sets:\n socket: %v\n direct: %v", socketRes.Sets, directRes.Sets)
	}
	if socketRes.Files != directRes.Files {
		t.Fatalf("the two doors count different files: socket=%d direct=%d", socketRes.Files, directRes.Files)
	}

	// The tree is the stronger claim: every file the engine wrote is the same
	// bytes, except the manifest, whose actor and generated_at are the door's
	// to stamp.
	treeA := exportTree(t, dirA)
	treeB := exportTree(t, dirB)
	if got, want := exportTreeKeys(treeA), exportTreeKeys(treeB); !reflect.DeepEqual(got, want) {
		t.Fatalf("the two trees hold different files:\n socket: %v\n direct: %v", got, want)
	}
	for name, bytesA := range treeA {
		if name == "export.json" {
			continue // the manifest is the door's to stamp; checked below
		}
		bytesB, ok := treeB[name]
		if !ok {
			t.Fatalf("the direct tree is missing %s that the socket tree wrote", name)
		}
		if !reflect.DeepEqual(bytesA, bytesB) {
			t.Fatalf("the two trees differ in %s:\n socket: %s\n direct: %s", name, bytesA, bytesB)
		}
	}

	// The manifest: same set states and counts, the actor the door stamps.
	var manA, manB struct {
		Actor export.Actor `json:"actor"`
		Sets  []struct {
			Name  string `json:"name"`
			State string `json:"state"`
			Files int    `json:"files"`
			Rows  *int   `json:"rows"`
		} `json:"sets"`
	}
	if err := json.Unmarshal(treeA["export.json"], &manA); err != nil {
		t.Fatalf("decode the socket manifest: %v", err)
	}
	if err := json.Unmarshal(treeB["export.json"], &manB); err != nil {
		t.Fatalf("decode the direct manifest: %v", err)
	}
	if manA.Actor.Kind != "user" || manB.Actor.Kind != "cli" {
		t.Fatalf("the manifest stamps the door: socket actor=%+v direct actor=%+v", manA.Actor, manB.Actor)
	}
	if !reflect.DeepEqual(manA.Sets, manB.Sets) {
		t.Fatalf("the two manifests report different sets:\n socket: %v\n direct: %v", manA.Sets, manB.Sets)
	}
}

// TestExportDirectDoorFixedLine pins the direct door's three facts at once
// (behaviour 6/7's door half, and the checkpoint's fixed-line contract): the
// stderr line is exactly the one sentence and nothing else, no ledger row is
// written (only the daemon's door records runs), and the manifest is stamped
// {cli, cli}.
func TestExportDirectDoorFixedLine(t *testing.T) {
	st := exportTestStore(t)
	deadEndpoint(t)
	dir := filepath.Join(t.TempDir(), "direct")

	// The direct door must not leave a ledger row; count what is there before,
	// and the data tables must be read (the agents row survives untouched).
	rowsBefore, err := st.ListBackupRuns(store.BackupRunFilter{Kind: "export"})
	if err != nil {
		t.Fatalf("list the export runs before: %v", err)
	}
	if len(rowsBefore) != 0 {
		t.Fatalf("the fresh store already holds %d export rows", len(rowsBefore))
	}
	if _, ok, err := st.AgentBySlug("atlas"); err != nil || !ok {
		t.Fatalf("the planted agent is missing before the run: %v", err)
	}

	var out strings.Builder
	stderr := captureStderr(t, func() {
		if err := cmdExport(&out, []string{"--dir", dir, "--json"}); err != nil {
			t.Fatalf("direct `aeon export --json`: %v", err)
		}
	})

	// The fixed line: exactly one line, exactly the sentence.
	if want := exportDirectLine + "\n"; stderr != want {
		t.Fatalf("the direct door's stderr is not the fixed line:\n got:  %q\n want: %q", stderr, want)
	}

	// No ledger row: the direct door owns the manifest, not the ledger.
	rowsAfter, err := st.ListBackupRuns(store.BackupRunFilter{Kind: "export"})
	if err != nil {
		t.Fatalf("list the export runs after: %v", err)
	}
	if len(rowsAfter) != 0 {
		t.Fatalf("the direct door wrote %d ledger rows — only the daemon's door records runs", len(rowsAfter))
	}

	// Read-only: the data the export read is untouched.
	if _, ok, err := st.AgentBySlug("atlas"); err != nil || !ok {
		t.Fatalf("the direct door changed the data it read: %v", err)
	}

	// The manifest is stamped {cli, cli} — the direct door's own origin.
	var man struct {
		Actor export.Actor `json:"actor"`
	}
	b, err := os.ReadFile(filepath.Join(dir, "export.json"))
	if err != nil {
		t.Fatalf("read the manifest: %v", err)
	}
	if err := json.Unmarshal(b, &man); err != nil {
		t.Fatalf("decode the manifest: %v", err)
	}
	if man.Actor.Kind != "cli" || man.Actor.ID != "cli" {
		t.Fatalf("the manifest stamps the direct door: got %+v, want {cli cli}", man.Actor)
	}

	// And it really ran: the JSON answer names the dir and the agents row.
	var res protocol.ExportRunResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(out.String())), &res); err != nil {
		t.Fatalf("decode the --json: %v", err)
	}
	if res.Dir != dir || res.Recorded {
		t.Fatalf("the --json is not the direct door's: dir=%q recorded=%v", res.Dir, res.Recorded)
	}
}
