package main

// policy_client_test.go — S45 T8 (PM4). The door tests: what the daemon
// answers and what the offline fallback answers must be THE SAME BYTES
// (modulo the fixed offline-notice line and instants), the fallback must
// fire ONLY for "nobody is home", and the audit must say who stood where
// (the peer on the socket, `cli` offline — never a flag).
//
// The method is deliberately blunt: every verb runs TWICE as a subprocess —
// once against a REAL spawned daemon (its own state dir, its own endpoint),
// once with no daemon at all (a fresh state dir, an endpoint path that does
// not exist) — and stdout is compared byte for byte (only named
// normalizations: instants, and the actor pair, which the two doors are
// REQUIRED to stamp differently — that difference is asserted by its own
// test, never hidden). Error texts compare through stderr after the fixed
// notice is stripped: a refusal printed through the socket and through the
// boundary must be one sentence.

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// doorRun runs the real binary with exactly the given env and returns the
// two streams separately — the parity claim is per-stream (stderr is where
// the offline notice and the SEC-014 warning speak; stdout is the answer).
func doorRun(t *testing.T, env []string, args ...string) (string, string, error) {
	t.Helper()
	if aeonBinary == "" {
		t.Skip("the binary was not built")
	}
	cmd := exec.Command(aeonBinary, args...)
	cmd.Env = env
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// doorEnv builds a subprocess env: the process env minus provider keys (I7
// — a developer's key must never ride a test subprocess) and minus the
// store/endpoint variables an earlier test may have left, plus this door's
// pins.
func doorEnv(stateHome, endpoint string) []string {
	base := make([]string, 0, 64)
	for _, kv := range os.Environ() {
		switch {
		case strings.HasPrefix(kv, "ANTHROPIC_API_KEY="),
			strings.HasPrefix(kv, "OPENAI_API_KEY="),
			strings.HasPrefix(kv, "AEON_ANTHROPIC_BASE_URL="),
			strings.HasPrefix(kv, "AEON_OPENAI_BASE_URL="),
			strings.HasPrefix(kv, "XDG_STATE_HOME="),
			strings.HasPrefix(kv, "LOCALRPC_ENDPOINT_PATH="),
			strings.HasPrefix(kv, "DATABASE_SQLITE_STORAGE_PATH="),
			strings.HasPrefix(kv, "DATABASE_SQLITE_FILE_NAME="),
			strings.HasPrefix(kv, "DATABASE_SQLITE_DSN="):
			continue
		}
		base = append(base, kv)
	}
	return append(base,
		"XDG_STATE_HOME="+stateHome,
		"LOCALRPC_ENDPOINT_PATH="+endpoint,
		"DATABASE_SQLITE_STORAGE_PATH=",
		"DATABASE_SQLITE_FILE_NAME=",
		"DATABASE_SQLITE_DSN=",
	)
}

// doorNormalizations name every difference the two doors are REQUIRED to
// have, and only those: instants (two runs, two clocks) and the actor pair
// (the peer uid on the socket versus cli offline — asserted by name in
// TestDirectDoorAuditsItselfAsCli, so naming it here is not hiding it).
var doorNormalizations = []struct {
	re   *regexp.Regexp
	repl string
}{
	{regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?Z`), "<T>"},
	{regexp.MustCompile(`\buser:\d+\b`), "<ACTOR>"},
	// The direct door decides as {user, cli} (T5's CLI ruling) where the
	// socket's decider is the peer — the same REQUIRED difference family,
	// named in TestDirectDoorAuditsItselfAsCli for the authoring rows.
	{regexp.MustCompile(`\buser:cli\b`), "<ACTOR>"},
	{regexp.MustCompile(`\bcli:cli\b`), "<ACTOR>"},
	// Column padding follows the value's length, and the two doors' actor
	// VALUES differ by ruling (above) — so the runs of alignment spaces
	// between fields normalize too. Tokens, order and line structure stay
	// byte-strict; only the padding that the differing actor names CAUSE is
	// forgiven. (The goldens still pin the true bytes; this guard pins the
	// doors against each other.)
	{regexp.MustCompile(` {2,}`), " "},
}

func doorNormalize(s string) string {
	for _, n := range doorNormalizations {
		s = n.re.ReplaceAllString(s, n.repl)
	}
	return s
}

// stripNotice removes the fixed offline line from a stderr capture, which
// is the one line the two doors are permitted to differ by.
func stripNotice(stderr string) string {
	return strings.ReplaceAll(stderr, policyDirectNotice+"\n", "")
}

// bothDoors is the fixture: one real daemon on its own endpoint/state dir,
// and one offline environment on a fresh state dir with a nonexistent
// endpoint. run() speaks a verb on one road; parity() speaks it on both and
// fails on the first byte that differs.
type bothDoors struct {
	t          *testing.T
	socketEnv  []string
	offlineEnv []string
	offlineDir string
	f          *daemonFixture
}

func newBothDoors(t *testing.T) *bothDoors {
	t.Helper()
	f := startDaemon(t)
	home := t.TempDir()
	// f.env() copies os.Environ(), and THIS test process may have run the
	// production dial() (whose appenv.Apply() os.Setenvs the store paths —
	// the ordering constraint AGENTS.md names, executed inside a test
	// binary). Stripping and re-pinning keeps the fixture's store its own
	// no matter what a sibling test's dial set. (Recorded as a finding: the
	// daemon fixture is order-sensitive to any in-process dial, and only
	// this suite's socket-arm test performs one.)
	socket := doorEnv(f.stateDir, f.endpoint)
	offline := doorEnv(home, filepath.Join(home, "nothing.sock"))
	// The daemon applies the shipped default set (permissions.firstParty) at start, so its policy table is
	// not empty. The offline road starts on a fresh store; apply the same set there so the two roads begin
	// from the same rows and the parity tests keep comparing what the verbs SAY, not what one store lacks.
	if _, errOut, err := doorRun(t, offline, "seed", "apply", "--set", "permissions.firstParty"); err != nil {
		t.Fatalf("seed the offline road with the default set: %v\n%s", err, errOut)
	}
	return &bothDoors{
		t:          t,
		f:          f,
		socketEnv:  socket,
		offlineEnv: offline,
		offlineDir: home,
	}
}

// parity runs the verb on both roads and compares stdout (normalized),
// stderr (normalized, notice stripped) and the exit outcome. wantErr says
// whether BOTH roads must fail. The comparison is the whole claim of
// PM4's "print what the wire said" — the offline door is a relay of the
// same sentences, not an echo of its own.
func (b *bothDoors) parity(wantErr bool, args ...string) {
	b.t.Helper()
	b.t.Run(strings.Join(args, " "), func(t *testing.T) {
		sOut, sErr, sErrErr := doorRun(t, b.socketEnv, args...)
		oOut, oErr, oErrErr := doorRun(t, b.offlineEnv, args...)
		if (sErrErr != nil) != wantErr {
			t.Fatalf("socket road exit mismatch: err=%v wantErr=%v\nstdout:\n%s\nstderr:\n%s", sErrErr, wantErr, sOut, sErr)
		}
		if (oErrErr != nil) != wantErr {
			t.Fatalf("offline road exit mismatch: err=%v wantErr=%v\nstdout:\n%s\nstderr:\n%s", oErrErr, wantErr, oOut, oErr)
		}
		if got, want := doorNormalize(oOut), doorNormalize(sOut); got != want {
			t.Errorf("stdout differs between the roads (%s)\n socket:\n%s\n offline:\n%s", strings.Join(args, " "), sOut, oOut)
		}
		if got, want := doorNormalize(stripNotice(oErr)), doorNormalize(sErr); got != want {
			t.Errorf("stderr differs between the roads (%s)\n socket:\n%s\n offline (notice stripped):\n%s", strings.Join(args, " "), sErr, oErr)
		}
	})
}

// paritySet compares the SET of normalized lines rather than the byte
// order — the named relaxation for the one legitimate ordering difference
// between the doors (the seed engines' insertion order; see its call site).
// Line content, count and the trailing depth/cap/oldest line stay strict
// after their own instants normalize.
func (b *bothDoors) paritySet(args ...string) {
	b.t.Helper()
	b.t.Run(strings.Join(args, " "), func(t *testing.T) {
		sOut, _, sErrErr := doorRun(t, b.socketEnv, args...)
		oOut, _, oErrErr := doorRun(t, b.offlineEnv, args...)
		if (sErrErr != nil) != (oErrErr != nil) {
			t.Fatalf("exit outcomes differ: socket=%v offline=%v", sErrErr, oErrErr)
		}
		lines := func(s string) []string {
			out := []string{}
			for _, l := range strings.Split(s, "\n") {
				l = strings.TrimSpace(doorNormalize(l))
				l = strings.TrimSpace(regexp.MustCompile(`^\d+ `).ReplaceAllString(l, ""))
				if l != "" {
					out = append(out, l)
				}
			}
			sort.Strings(out)
			return out
		}
		got, want := lines(oOut), lines(sOut)
		if len(got) != len(want) {
			t.Fatalf("line counts differ (%d offline vs %d socket)\n socket:\n%s\n offline:\n%s", len(got), len(want), sOut, oOut)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("line set differs at %d:\n socket: %s\n offline: %s\n(socket:\n%s\noffline:\n%s)", i, want[i], got[i], sOut, oOut)
			}
		}
	})
}

// must is a setup call that must succeed on one named road.
func (b *bothDoors) must(road string, args ...string) string {
	b.t.Helper()
	env := b.socketEnv
	if road == "offline" {
		env = b.offlineEnv
	}
	out, stderr, err := doorRun(b.t, env, args...)
	if err != nil {
		b.t.Fatalf("setup on the %s road failed: aeon %s: %v\nstdout:\n%s\nstderr:\n%s", road, strings.Join(args, " "), err, out, stderr)
	}
	return out
}

// --- the selection ---------------------------------------------------------------

func TestSelectPolicyDoorFallsBackOnlyWhenNobodyIsHome(t *testing.T) {
	defer func() { policyDial = dial }()

	t.Run("no endpoint is the direct door, and it says the fixed line first", func(t *testing.T) {
		policyDial = func(int) (*protocol.Client, error) {
			return nil, fmt.Errorf("no daemon at /x/nothing.sock: dial unix: connect: no such file or directory")
		}
		var errOut strings.Builder
		d, err := selectPolicyDoor(&errOut)
		if err != nil {
			t.Fatalf("a missing endpoint must fall back, not refuse: %v", err)
		}
		if _, ok := d.(*policyDirectDoor); !ok {
			t.Fatalf("the fallback door is %T, not the direct door", d)
		}
		if got := errOut.String(); got != policyDirectNotice+"\n" {
			t.Errorf("the fallback said %q, want the fixed line %q", got, policyDirectNotice+"\n")
		}
	})

	t.Run("a permission refusal REFUSES — it does not fall back", func(t *testing.T) {
		policyDial = func(int) (*protocol.Client, error) {
			return nil, &net.OpError{Op: "dial", Net: "unix", Err: os.ErrPermission,
				Source: nil, Addr: &net.UnixAddr{Name: "/x/aeon.sock", Net: "unix"}}
		}
		var errOut strings.Builder
		_, err := selectPolicyDoor(&errOut)
		// The net.OpError text contains "permission denied" but NOT "no daemon
		// at" — isNoDaemon's own ruling (seed_door.go): a daemon that exists
		// and locks you out is a state to SEE, never one to route around.
		if err == nil {
			t.Fatal("a permission refusal fell back — the CLI routed around a daemon that exists")
		}
		if errOut.Len() != 0 {
			t.Errorf("a refusal must not print the fallback notice, got %q", errOut.String())
		}
	})

	t.Run("a handshake fault REFUSES — no fallback", func(t *testing.T) {
		policyDial = func(int) (*protocol.Client, error) {
			return nil, errors.New("handshake refused: daemon protocol version 9 is newer than this client's 8")
		}
		var errOut strings.Builder
		if _, err := selectPolicyDoor(&errOut); err == nil {
			t.Fatal("a version-skew handshake refused to fall back — expected a refusal, got a door")
		} else if errOut.Len() != 0 {
			t.Errorf("the refusal printed the fallback notice: %q", errOut.String())
		}
	})

	t.Run("a live daemon is the socket door, silent", func(t *testing.T) {
		f := startDaemon(t)
		policyDial = dial
		// dial() ends in appenv.Apply(), which os.Setenvs the store paths
		// PROCESS-WIDE (that is its production purpose — config reads them
		// at init). Snapshot and put them back so no sibling test inherits
		// a mutated environment from this arm.
		before := map[string]string{}
		for _, k := range []string{"XDG_STATE_HOME", "DATABASE_SQLITE_STORAGE_PATH", "DATABASE_SQLITE_FILE_NAME", "DATABASE_SQLITE_DSN", "LOCALRPC_ENDPOINT_PATH"} {
			before[k] = os.Getenv(k)
		}
		t.Cleanup(func() {
			for k, v := range before {
				if v == "" {
					_ = os.Unsetenv(k)
				} else {
					_ = os.Setenv(k, v)
				}
			}
		})
		t.Setenv("LOCALRPC_ENDPOINT_PATH", f.endpoint)
		var errOut strings.Builder
		d, err := selectPolicyDoor(&errOut)
		if err != nil {
			t.Fatalf("the live daemon refused the dial: %v", err)
		}
		if _, ok := d.(*policySocketDoor); !ok {
			t.Fatalf("the live road answered with %T, not the socket door", d)
		}
		if errOut.Len() != 0 {
			t.Errorf("the socket road must say nothing; it said %q", errOut.String())
		}
	})
}

// --- the both-roads parity ---------------------------------------------------------

func TestPolicyVerbsSpeakByteIdenticalThroughBothDoors(t *testing.T) {
	b := newBothDoors(t)

	// --- authoring ---
	// (--spec carries the state the tool_access schema demands; --tool is
	// the toolName sugar and never writes state — the boundary refuses to
	// default it, so a stored spec says only what the operator typed.)
	b.parity(false, "policy", "add", "--type", "tool_access", "--id", "par-deny",
		"--target", "agent:bob", "--action", "deny", "--source", "operator",
		"--tool", "bash", "--spec", `{"state":"deny"}`)
	b.parity(false, "policy", "add", "--type", "command_access", "--id", "par-broad",
		"--target", "all-agents", "--action", "allow", "--source", "operator", "--command", "git*")
	b.parity(false, "policy", "add", "--type", "tool_access", "--id", "par-loop",
		"--target", "loop:nightly", "--action", "deny", "--source", "operator",
		"--tool", "ls", "--spec", `{"state":"deny"}`)
	// PL1's refusal is the boundary's sentence — through the socket it
	// travels as the wire's `what`, offline as the *Reject's Reason: one
	// sentence, compared byte for byte.
	b.parity(true, "policy", "add", "--type", "tool_access", "--id", "par-pl1",
		"--target", "agent:bob", "--action", "allow", "--source", "agent", "--tool", "bash")

	// --- reads ---
	b.parity(false, "policy", "list")
	b.parity(false, "policy", "list", "--type", "tool_access")
	b.parity(false, "policy", "list", "--type", "route_access")
	b.parity(false, "policy", "get", "par-deny")
	b.parity(true, "policy", "get", "par-never-existed")
	b.parity(false, "policy", "vocabulary")

	// --- verdicts ---
	b.parity(false, "policy", "why", "--agent", "bob", "--tool", "bash")
	b.parity(false, "policy", "why", "--type", "command_access", "--agent", "bob",
		"--command", "git status", "--tool", "bash") // SEC-050's pair: the note must ride both roads
	b.parity(false, "policy", "why", "--agent", "bob", "--loop", "nightly", "--tool", "bash") // DEF-038 warn
	b.parity(false, "policy", "why", "--type", "route_access", "--agent", "bob")              // D10 note
	b.parity(true, "policy", "why", "--agent", "bob", "--at", "yesterday")
	b.parity(false, "policy", "in-force", "--agent", "bob")
	b.parity(false, "policy", "in-force", "--agent", "nobody-here")
	b.parity(true, "policy", "in-force")
	b.parity(true, "policy", "in-force", "--agent", "bob", "--loop", "nightly")

	// --- events before any refusal-event exists (the third state) ---
	b.parity(false, "policy", "events")
	b.parity(false, "policy", "events", "--subject", "agent:nobody-here")

	// --- the audit, before the seed's provenance rows (which the two roads
	// audit through different engines — see the Notes' deviation) ---
	b.parity(false, "policy", "changes")
	b.parity(false, "policy", "changes", "--policy", "par-deny")
	b.parity(true, "policy", "changes", "--policy", "not an id!")

	// --- remove, then remove again (idempotence says so the same way) ---
	b.parity(false, "policy", "remove", "--id", "par-loop")
	b.parity(false, "policy", "remove", "--id", "par-loop")

	// --- seed-corpus: unknown agent, dry run, real run (T12's one engine) ---
	// The seed door's T12 rule — permissions for an agent nobody has is not
	// seeding — reads the AGENT TABLE, so both roads are given the same
	// truth first: git-loop exists as a row on both stores (staged directly;
	// agent creation is the agent-management door's job, not this verb's).
	for _, env := range [][]string{b.socketEnv, b.offlineEnv} {
		st := openDoorStore(t, env)
		if err := st.CreateAgentAudited(context.Background(), store.AgentActor{Kind: store.AgentActorUser, ID: "1000"}, store.Agent{Slug: "git-loop", Name: "git-loop"}, "2026-10-20T12:00:00Z"); err != nil {
			t.Fatalf("stage the git-loop agent: %v", err)
		}
		_ = st.Close()
	}
	b.parity(true, "policy", "seed-corpus")
	b.parity(true, "policy", "seed-corpus", "--agent", "agent-that-does-not-exist")
	b.parity(false, "policy", "seed-corpus", "--agent", "git-loop", "--dry-run")
	b.parity(false, "policy", "seed-corpus", "--agent", "git-loop")
	// The seed's authored events now stand in the audit. The COMPARISON is
	// by line SET, not bytes-in-order: the two engines legitimately insert
	// in different orders (the daemon's policyseed engine goes hash-first,
	// the offline desk runner goes corpus-first — T12's two homes, each
	// pinned by its own tests), so the row NUMBERS cannot match while the
	// CONTENT must. Every other read in this suite is byte-strict; this is
	// the one named relaxation, and it still refuses a rule seeded on one
	// road and not the other.
	b.paritySet("policy", "events")
	b.paritySet("policy", "events", "--subject", "agent:git-loop")

	// --- add --help is vocabulary-GENERATED (live on the socket road,
	// compiled-engine offline): same bytes, both roads. ---
	b.parity(false, "policy", "add", "--help")

	// The fixed line is offline-only, first, and exactly the constant.
	if _, oErr, _ := doorRun(t, b.offlineEnv, "policy", "list"); !strings.HasPrefix(oErr, policyDirectNotice+"\n") {
		t.Errorf("the offline road did not open with the fixed line %q; stderr was:\n%s", policyDirectNotice+"\n", oErr)
	}
	if _, sErr, _ := doorRun(t, b.socketEnv, "policy", "list"); strings.Contains(sErr, policyDirectNotice) {
		t.Errorf("the socket road printed the offline notice; it belongs to the fallback only:\n%s", sErr)
	}
}

func TestProposalVerbsSpeakByteIdenticalThroughBothDoors(t *testing.T) {
	b := newBothDoors(t)

	// Stage the same rows directly into both stores (the create door is the
	// agent/UI side by design — the CLI has no create verb; the parity here
	// is over the four verbs the CLI DOES speak). Flags come FIRST: the
	// golden-pinned T5 ruling is that a flag after the id reads as extra
	// positionals and the verb refuses its own input (proposal-reject-
	// reason-after-id), so a parity scenario spells commands the way an
	// operator must.
	payload := `{"id":"par-committed","type":"tool_access","target":{"kind":"agent","id":"bob"},"action":"deny","source":"operator","spec":{"toolName":"webfetch","state":"deny"}}`
	b.must("socket", "policy", "list") // force both stores to exist first (offline migrates quietly)
	b.f.waitStartupQuiet()             // the staging below is a second writer on the daemon's store
	for _, road := range []string{"socket", "offline"} {
		env := b.socketEnv
		if road == "offline" {
			env = b.offlineEnv
		}
		st := openDoorStore(t, env)
		if _, err := st.CreateProposal("policy_add", payload, store.Actor{Kind: "user", ID: "parity"}, "2026-10-20T12:00:00Z"); err != nil {
			t.Fatalf("stage the policy_add proposal on the %s road: %v", road, err)
		}
		if _, err := st.CreateProposal("policy_remove", `{"id":"par-gone"}`, store.Actor{Kind: "user", ID: "parity"}, "2026-10-20T12:00:00Z"); err != nil {
			t.Fatalf("stage the policy_remove proposal on the %s road: %v", road, err)
		}
		if _, err := st.CreateProposal("policy_remove", `{"id":"par-gone-too"}`, store.Actor{Kind: "user", ID: "parity"}, "2026-10-20T12:00:00Z"); err != nil {
			t.Fatalf("stage the second policy_remove proposal on the %s road: %v", road, err)
		}
		_ = st.Close()
	}

	b.parity(false, "proposal", "list")
	b.parity(false, "proposal", "list", "--status", "proposed")
	b.parity(false, "proposal", "list", "--status", "committed")
	b.parity(false, "proposal", "show", "1")
	b.parity(false, "proposal", "show", "2")
	b.parity(true, "proposal", "show", "999")
	b.parity(true, "proposal", "commit")
	b.parity(true, "proposal", "commit", "not-a-number")
	b.parity(true, "proposal", "reject", "--reason", "gone", "999")

	// PM7.1: a commit carries the digest the read showed. The wrong digest
	// refuses at both roads with one sentence (the desk's, verbatim through
	// the wire's `what`); the right one commits at both.
	b.parity(true, "proposal", "commit", "--digest", "deadbeef", "1")
	b.parity(false, "proposal", "commit", "1")
	b.parity(true, "proposal", "commit", "1") // decided rows are immutable history — the CLI's own sentence, both roads
	b.parity(false, "proposal", "reject", "--reason", "not now", "2")
	b.parity(true, "proposal", "reject", "--reason", "again", "2")

	// PM7.5's recorded asymmetry, asserted rather than averaged away: the
	// socket requires a reason where a human presses the button; the CLI
	// door keeps TD5's optional reason. Same command, two sanctioned
	// answers — the one difference between the doors a person can see.
	if _, sErr, sErrErr := doorRun(t, b.socketEnv, "proposal", "reject", "3"); sErrErr == nil ||
		!strings.Contains(sErr, "requires a reason") {
		t.Errorf("the socket door accepted a reason-less reject (PM7.5 says it must refuse): err=%v stderr=%s", sErrErr, sErr)
	}
	if _, _, oErrErr := doorRun(t, b.offlineEnv, "proposal", "reject", "3"); oErrErr != nil {
		t.Errorf("the offline door refused a reason-less reject (TD5 says the CLI's reason is optional): %v", oErrErr)
	}

	// The committed proposal's effect landed — and the list reads it the same.
	b.parity(false, "proposal", "list", "--status", "committed")
	b.parity(false, "policy", "list")
}

// TestSocketCommitAgentImportCommitsLikeEveryKind is the daemon-side half of
// the S45 T10 ruling — superseding TestSocketCommitRefusesAgentImportByName,
// which pinned the by-name refusal that stood only until S43 merged (and
// pinned the CLI's then-routing of the kind to the direct door at either
// road). Over the REAL daemon now: the kind commits digest-bound, the
// effect runs through the one injected seam map (the same registry entry
// the CLI's offline road runs — one implementation, PM4), the consent print
// comes back as DATA on the result (the daemon captured it; it streams to
// no terminal), the row carries the PEER stamp, and not one policy_change
// row is written — the decision's record is the row's decided_by_* fields.
func TestSocketCommitAgentImportCommitsLikeEveryKind(t *testing.T) {
	f := startDaemon(t)
	dir := writeAgentPackage(t, "groot", nil)
	payload := `{"pkgPath":"` + filepath.ToSlash(dir) + `"}`
	st := openDoorStore(t, f.env())
	row, err := st.CreateProposal("agent_import", payload, store.Actor{Kind: "user", ID: "parity"}, "2026-10-20T12:00:00Z")
	if err != nil {
		t.Fatalf("stage the agent_import proposal: %v", err)
	}
	_ = st.Close()

	c, err := dialTo(t, f.endpoint)
	if err != nil {
		t.Fatalf("dial the test daemon: %v", err)
	}
	defer func() { _ = c.Close() }()
	var res protocol.ProposalCommitResult
	// The REAL digest — PM7.1 binds the commit to what was read on every
	// kind, T10's path included.
	err = c.Call(protocol.MethodProposalCommit, protocol.ProposalCommitParams{ID: row.ID, Digest: desk.ProposalPayloadDigest(payload)}, &res)
	if err != nil {
		t.Fatalf("the socket refused an agent_import commit (T10 closed that door): %v", err)
	}
	if !res.EffectApplied || res.Proposal.Status != "committed" {
		t.Fatalf("half a commit: %+v", res)
	}
	if res.Proposal.DecidedByKind != "user" || res.Proposal.DecidedByID != fmt.Sprintf("%d", os.Getuid()) {
		t.Errorf("decided_by = %s:%s — the socket door stamps the kernel peer (PM7.3)", res.Proposal.DecidedByKind, res.Proposal.DecidedByID)
	}
	// The consent print reached the RESULT (S37 AA4 as data) — and the
	// before/after pair speaks for the agent through S37's own renderers.
	joined := strings.Join(res.Warnings, "\n")
	if !strings.Contains(joined, "imported groot as DISABLED") {
		t.Errorf("the consent print never rode the socket result: warnings=%+v", res.Warnings)
	}
	if res.Before.Target != "agent:groot" || len(res.Before.Answers) == 0 {
		t.Errorf("before-view names no agent preview: %+v", res.Before)
	}
	if res.After.Target != "agent:groot" || len(res.After.Answers) == 0 || !strings.Contains(res.After.Answers[0].Render, "imported groot as DISABLED") {
		t.Errorf("after-view is not the import body's print: %+v", res.After)
	}
	// The row and the audit, re-read from the daemon's store: committed by
	// the peer, and ZERO policy_change rows — T10's audit rule.
	st2 := openDoorStore(t, f.env())
	defer func() { _ = st2.Close() }()
	stored, found, err := st2.GetProposal(row.ID)
	if err != nil || !found || stored.Status != "committed" || stored.DecidedByKind != "user" {
		t.Fatalf("the row after the socket commit: %+v found=%v err=%v", stored, found, err)
	}
	n, err := st2.PolicyChangeCount(context.Background())
	if err != nil || n != 0 {
		t.Fatalf("agent_import wrote %d policy_change rows (err %v) — the authoring audit is policy-only", n, err)
	}
}

// --- the audit says who stood where ---------------------------------------------------

func TestDirectDoorAuditsItselfAsCliSocketDoorStampsPeer(t *testing.T) {
	b := newBothDoors(t)
	b.must("socket", "policy", "add", "--type", "tool_access", "--id", "audit-socket",
		"--target", "agent:bob", "--action", "deny", "--source", "operator",
		"--tool", "bash", "--spec", `{"state":"deny"}`)
	b.must("offline", "policy", "add", "--type", "tool_access", "--id", "audit-offline",
		"--target", "agent:bob", "--action", "deny", "--source", "operator",
		"--tool", "bash", "--spec", `{"state":"deny"}`)
	b.must("offline", "policy", "remove", "--id", "audit-offline")

	socketRows := doorChanges(t, b.socketEnv, "audit-socket")
	if len(socketRows) != 1 {
		t.Fatalf("the socket add audited %d rows, want 1", len(socketRows))
	}
	if socketRows[0].ActorKind != "user" || socketRows[0].ActorID == "cli" || socketRows[0].ActorID == "" {
		t.Errorf("the socket door's audit is %s:%s — the daemon must stamp the PEER (user + uid) from the connection",
			socketRows[0].ActorKind, socketRows[0].ActorID)
	}
	if !strings.HasPrefix(socketRows[0].Summary, "added tool_access deny for agent:bob") {
		t.Errorf("the socket audit summary drifted from the fixed template: %q", socketRows[0].Summary)
	}

	offlineRows := doorChanges(t, b.offlineEnv, "audit-offline")
	if len(offlineRows) != 2 { // the add and the remove
		t.Fatalf("the offline door audited %d rows, want 2 (add + remove)", len(offlineRows))
	}
	for _, r := range offlineRows {
		if r.ActorKind != "cli" || r.ActorID != "cli" {
			t.Errorf("the direct door's audit is %s:%s — the offline door names itself cli (T3's ruling; it never impersonates a peer)",
				r.ActorKind, r.ActorID)
		}
	}
	if offlineRows[0].Op != "remove" { // newest first
		t.Errorf("the offline audit order drifted: %s first, want remove", offlineRows[0].Op)
	}
}

// TestChangesNamesTheUnauditedGapBothRoads is DEF-041's third state through
// the new verb: a stored rule with no audit row is NAMED by the listing,
// and the line is one sentence at both roads.
func TestChangesNamesTheUnauditedGapBothRoads(t *testing.T) {
	b := newBothDoors(t)
	// Simulate the crash gap: the row is in the store (the write committed)
	// and its policy_change row never landed. Written through the raw
	// accessor on purpose — the boundary would have written the audit.
	for _, env := range [][]string{b.socketEnv, b.offlineEnv} {
		st := openDoorStore(t, env)
		if err := st.PolicyAdd(context.Background(), store.PolicyRow{
			ID: "gap-row", Type: "tool_access", TargetKind: "agent", TargetID: "bob",
			Action: "deny", Source: "operator", Subject: "agent:bob",
			SpecJSON: `{"toolName":"bash","state":"deny"}`, CreatedAt: "2026-10-20T12:00:00Z",
		}); err != nil {
			t.Fatalf("stage the gap row: %v", err)
		}
		_ = st.Close()
	}
	b.parity(false, "policy", "changes")
	out, _, _ := doorRun(t, b.offlineEnv, "policy", "changes")
	if !strings.Contains(out, `unaudited  policy "gap-row"`) {
		t.Fatalf("the listing did not name the unaudited gap:\n%s", out)
	}
}

// --- the file-level claim: verbs parse and print, the door opens -------------------

func TestPolicyAndProposalFilesOpenNoStoreBeyondTheDoor(t *testing.T) {
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join("..", "..", "aeon", "cmd", "aeon", name))
		if err != nil {
			// tests run from the package dir; the file sits beside them.
			b, err = os.ReadFile(name)
			if err != nil {
				t.Fatalf("read %s: %v", name, err)
			}
		}
		return string(b)
	}
	proposal := read("proposal.go")
	if strings.Contains(proposal, "openStoreDirectly(") || strings.Contains(proposal, "internal/store") {
		t.Error("proposal.go reaches the store by itself — after S45 T8 every store touch goes through policy_client.go's doors")
	}
	p := read("policy.go")
	// The ONE sanctioned direct open is the boundary seam itself
	// (openPolicyBoundaryDirectly); no verb may open another.
	if n := strings.Count(p, "openStoreDirectly("); n != 1 {
		t.Errorf("policy.go calls openStoreDirectly %d times — exactly the boundary seam (1) is sanctioned; verbs dial the door", n)
	}
}

// --- helpers --------------------------------------------------------------------------

// openDoorStore opens the store a given subprocess env would resolve — the
// fixture's way of staging rows the same store the subprocesses read.
func openDoorStore(t *testing.T, env []string) *store.Store {
	t.Helper()
	restore := map[string]string{}
	for _, kv := range env {
		k, v, ok := strings.Cut(kv, "=")
		switch k {
		case "XDG_STATE_HOME", "DATABASE_SQLITE_STORAGE_PATH", "DATABASE_SQLITE_FILE_NAME", "DATABASE_SQLITE_DSN":
			if !ok {
				continue
			}
			restore[k] = os.Getenv(k)
			if v == "" {
				_ = os.Unsetenv(k)
			} else {
				_ = os.Setenv(k, v)
			}
		}
	}
	st, err := store.Open(store.Options{})
	for k, v := range restore {
		if v == "" {
			_ = os.Unsetenv(k)
		} else {
			_ = os.Setenv(k, v)
		}
	}
	if err != nil {
		t.Fatalf("open the door store: %v", err)
	}
	return st
}

func doorChanges(t *testing.T, env []string, policyID string) []store.PolicyChange {
	t.Helper()
	st := openDoorStore(t, env)
	defer func() { _ = st.Close() }()
	rows, err := st.PolicyChangeList(context.Background(), policyID, 50)
	if err != nil {
		t.Fatalf("read the audit: %v", err)
	}
	return rows
}

// dialTo is the test's own client dial (the production dial resolves the
// endpoint from the env this process does NOT control; the fixture names
// it directly).
func dialTo(t *testing.T, endpoint string) (*protocol.Client, error) {
	t.Helper()
	conn, err := net.Dial("unix", endpoint)
	if err != nil {
		return nil, err
	}
	c := protocol.NewClient(conn, ClientName, Version)
	if _, err := c.Connect(0); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return c, nil
}
