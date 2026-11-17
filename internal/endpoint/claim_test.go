package endpoint

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Every test here binds a path under t.TempDir() (I6). A test that touched the
// real endpoint would collide with a developer's running daemon — and the
// failure would look like this code being wrong.
func tempEndpoint(t *testing.T) string {
	t.Helper()
	// Test names and checkout paths can exceed Unix socket address limits.
	// Keep the private directory short and remove it on test cleanup.
	dir, err := os.MkdirTemp("", "ae-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	// localrpc chmods the endpoint's parent to 0700, and so does anything
	// binding here; a path directly in /tmp fails for that reason.
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatalf("chmod temp dir: %v", err)
	}
	path := filepath.Join(dir, "rpc.sock")
	t.Setenv(endpointPathEnv, path)
	return path
}

func TestClaimSucceedsWhenNothingIsThere(t *testing.T) {
	tempEndpoint(t)
	if err := Claim(); err != nil {
		t.Fatalf("Claim on a free endpoint: %v", err)
	}
}

// The refusal that makes the task worth doing: a bind error from inside the
// framework is true and useless — it does not say another daemon is running.
func TestClaimRefusesWhenALiveHolderAnswers(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix socket")
	}
	path := tempEndpoint(t)

	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			c, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			_ = c.Close()
		}
	}()

	err = Claim()
	if err == nil {
		t.Fatal("Claim succeeded while a live holder was listening")
	}
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("error = %v, want ErrAlreadyRunning", err)
	}
	for _, want := range []string{path, "holder"} {
		if !contains(err.Error(), want) {
			t.Errorf("refusal does not mention %q:\n%s", want, err)
		}
	}
}

// On Linux the refusal should name the pid, because SO_PEERCRED is available —
// and a refusal that names the process is the difference between "something is
// running" and "stop pid 4213".
func TestRefusalNamesThePIDOnLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("SO_PEERCRED is a Linux affordance here")
	}
	path := tempEndpoint(t)
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			c, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			_ = c.Close()
		}
	}()

	err = Claim()
	if err == nil {
		t.Fatal("expected a refusal")
	}
	if !contains(err.Error(), "pid") {
		t.Fatalf("refusal does not name a pid:\n%s", err)
	}
}

// THE important one (O5). A crash leaves a socket file with no holder. If that
// refused, one bad shutdown would make the daemon unstartable — the opposite
// of what S7's recovery is for.
func TestClaimReclaimsAStaleEndpoint(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix socket")
	}
	path := tempEndpoint(t)

	// Bind and close without unlinking — exactly what a killed process leaves.
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	if ul, ok := ln.(*net.UnixListener); ok {
		ul.SetUnlinkOnClose(false)
	}
	if cerr := ln.Close(); cerr != nil {
		t.Fatalf("close: %v", cerr)
	}
	if _, serr := os.Stat(path); serr != nil {
		t.Fatalf("the stale socket was not left behind; the test proves nothing: %v", serr)
	}

	if err := Claim(); err != nil {
		t.Fatalf("Claim refused a stale endpoint: %v", err)
	}
	if _, serr := os.Stat(path); !os.IsNotExist(serr) {
		t.Fatal("the stale endpoint was not removed, so bind would still fail")
	}
}

func TestPathHonoursAnExplicitEndpoint(t *testing.T) {
	t.Setenv(endpointPathEnv, "/tmp/aeon-explicit/rpc.sock")
	got, err := Path()
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if got != "/tmp/aeon-explicit/rpc.sock" {
		t.Fatalf("Path() = %q, want the explicit value", got)
	}
}

// Without this, the endpoint resolves under the FRAMEWORK's namespace, and the
// claim would check a path the daemon never binds.
func TestPathUsesAeonNamespaceWhenUnset(t *testing.T) {
	t.Setenv(endpointPathEnv, "")
	t.Setenv(namespaceEnv, "")
	got, err := Path()
	if err != nil {
		t.Skipf("no resolvable endpoint path in this environment: %v", err)
	}
	if !contains(got, "aeon") {
		t.Fatalf("Path() = %q, want it under the aeon namespace", got)
	}
	if contains(got, "go-backend-scaffolding") {
		t.Fatalf("Path() = %q resolved under the framework's namespace", got)
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
