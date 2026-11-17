package container

// W2-1 (S59 T12's container re-walk) — the two inventory reads have to speak
// the handle format that Create stored. W2-1's FIX (this file's first shape)
// then became the regression it was written to prevent, and this pin is what
// says so.
//
// `docker run -d` prints the full 64-hex, and that is what lands in the row's
// handle. Both inventory reads used to ask docker for `{{.ID}}` — the 12-char
// SHORT id — so `live[h.ID]` and `seen[r.Handle]` could never match. The
// measured consequences were not cosmetic: a loop's second command was refused
// `provision-live-exists` (so the warm container `execenv.provisionIdleTTL`
// promises was never reused), and a restart reported its own running container
// as "gone" and removed it as an orphan — `0 adopted, 1 removed`. This package's
// suite could not see it because the test fake echoes what Create was given.
//
// W2-1 "fixed" that by asking both reads for `{{.FullID}}` — a field the engine
// does not HAVE. Measured on rootless docker 29.8.0: `docker ps --format
// '{{.FullID}}|…'` dies with `failed to execute template: … can't evaluate
// field FullID in type *formatter.ContainerContext`, and `docker network ls
// --format '{{.FullID}}|…'` dies with `… can't evaluate field FullID in type
// *network.networkContext` (exit 0 on an empty lane — the template only runs
// per row, which is why nobody noticed the network half until a row existed).
// List failed outright, the daemon could not see the container it had just
// started, and the loop was bricked by the same `provision-live-exists` refusal
// W2-1 had set out to kill.
//
// The lesson for this pin: the old assertion — "the argv contains
// `{{.FullID}}`" — was SATISFIED by a form the engine rejects. Checking for one
// magic token proves the token is there, not that the engine accepts the
// command. So this pins each read's WHOLE argv against the exact sequence,
// measured end-to-end on the real engine against real rows (see
// realtimetest_test.go for the engine half): `--no-trunc` present (the flag
// that makes `{{.ID}}` print the full 64-hex), `{{.ID}}` present, `{{.FullID}}`
// absent. Any drift in either direction fails here before it bricks a loop.

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

func captureArgv(t *testing.T, run func(r *CLIRuntime)) []string {
	t.Helper()
	var got []string
	r := &CLIRuntime{
		Bin: "docker",
		Call: func(_ context.Context, cmd *exec.Cmd) ([]byte, error) {
			got = cmd.Args
			return []byte(""), nil
		},
	}
	run(r)
	return got
}

func TestInventoryReadsAskForTheFullHandle(t *testing.T) {
	cases := []struct {
		name     string
		run      func(r *CLIRuntime)
		wantArgv string // the whole command, byte for byte, as the engine sees it
	}{
		{"container list",
			func(r *CLIRuntime) { _, _ = r.List(context.Background(), LabelManaged+"=1") },
			"docker ps --all --no-trunc --filter label=aeon.managed=1 --format {{.ID}}|{{.Names}}|{{.State}}"},
		{"network list",
			func(r *CLIRuntime) { _, _ = r.NetworkList(context.Background(), LabelManaged+"=1") },
			"docker network ls --no-trunc --filter label=aeon.managed=1 --format {{.ID}}|{{.Name}}"},
	}
	for _, c := range cases {
		argv := captureArgv(t, c.run)
		joined := strings.Join(argv, " ")
		// Refuse to pass on an empty argv: a pin that sees no command passes on
		// nothing, which is how W2-1's suite stayed green while the engine refused.
		if joined == "" {
			t.Fatalf("%s never reached the engine seam — the pin would pass on nothing", c.name)
		}
		if joined != c.wantArgv {
			t.Errorf("%s does not send the whole pinned argv\n got: %s\nwant: %s\n"+
				"the whole sequence is pinned, not one token: --no-trunc is what makes {{.ID}} answer the "+
				"full 64-hex Create stored (dropping it re-opens W2-1), and {{.FullID}} is a field the "+
				"engine rejects outright — W2-1's pin passed while docker answered "+
				"can't evaluate field FullID (W2-1 regression)", c.name, joined, c.wantArgv)
		}
		if !strings.Contains(joined, "--no-trunc") {
			t.Errorf("%s drops --no-trunc: %s — without it the engine answers the 12-char short id and nothing matches the stored 64-hex", c.name, joined)
		}
		if !strings.Contains(joined, "{{.ID}}") {
			t.Errorf("%s does not ask for the id at all: %s — rows store the 64-hex Create returned, so nothing will ever match (W2-1)", c.name, joined)
		}
		if strings.Contains(joined, "{{.FullID}}") {
			t.Errorf("%s asks for the form the engine refuses: %s — docker 29.8.0 fails the template with can't evaluate field FullID, which is the W2-1 regression", c.name, joined)
		}
	}
}

// TestNetworkListArgvIsTheSameReadPinsTheWording keeps the builder the crash
// path uses honest: it is the read that makes a network findable after a
// restart, and a short id there means the loop's network is an orphan forever.
// It is pinned to the exact argv for the same reason as above — W2-1's
// `{{.FullID}}` satisfied a token-only pin while the engine refused the read.
func TestNetworkListArgvIsTheSameReadPinsTheWording(t *testing.T) {
	joined := strings.Join(networkListArgv("docker", "aeon.managed=1"), " ")
	want := "docker network ls --no-trunc --filter label=aeon.managed=1 --format {{.ID}}|{{.Name}}"
	if joined != want {
		t.Errorf("networkListArgv regressed\n got: %s\nwant: %s", joined, want)
	}
}

// TestNetworkCreateAndRmArgvAreWhatTheEngineAccepts pins the two network WRITES by
// their whole argv, for the reason this file exists at all: W2-1's pin asserted one
// token and stayed green on a command the engine refused outright. These two carry
// the handle decision — `create` is given the NAME (the engine builds the network
// from it, so the name belongs in the create argv) and `rm` is given the ID the
// create printed (measured on rootless docker 29.8.0: `docker network rm <64-hex>`
// exits 0 and prints the id back). If either drifts, the ledger holds a handle no
// engine command can speak, which is the defect class this file is the pin for.
func TestNetworkCreateAndRmArgvAreWhatTheEngineAccepts(t *testing.T) {
	const id = "9c45c297fe4a79779c586498779769d10f634bd2ce59a2542c3b2469af5a972c" // measured stdout of `network create`
	cases := []struct {
		name     string
		got      string
		wantArgv string
	}{
		{"network create",
			strings.Join(networkArgv("docker", "aeon-loop-a-net", "loop-a"), " "),
			"docker network create --label aeon.managed=1 --label aeon.loop=loop-a aeon-loop-a-net"},
		{"network rm",
			strings.Join(networkRmArgv("docker", id), " "),
			"docker network rm " + id},
	}
	for _, c := range cases {
		if c.got == "" {
			t.Fatalf("%s built no argv at all — a pin that sees no command passes on nothing, which is how W2-1's suite stayed green", c.name)
		}
		if c.got != c.wantArgv {
			t.Errorf("%s does not send the whole pinned argv\n got: %s\nwant: %s", c.name, c.got, c.wantArgv)
		}
		if strings.Contains(c.got, "{{.FullID}}") {
			t.Errorf("%s asks for a template field the engine refuses: %s", c.name, c.got)
		}
	}
	// The create stays a user-defined bridge: `--driver host` would put a loop's
	// services on the host's stack (EV13), and there is no parameter that does it.
	if strings.Contains(cases[0].got, "--driver") {
		t.Errorf("the network create argv chooses its own driver: %s — host networking from a loop spec is EV13 refused", cases[0].got)
	}
	// The removal names the handle, whole: a truncation here is a removal that
	// matches nothing while the row reads `removed`.
	if !strings.HasSuffix(cases[1].got, id) {
		t.Errorf("the network rm argv does not end with the stored handle: %s", cases[1].got)
	}
}
