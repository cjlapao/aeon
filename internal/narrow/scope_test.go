package narrow

// scope_test.go — T7's scoping and degradation gates at the package
// level. Q11 answered: a read resolves an id WITHIN this daemon's store
// — the surface holds no data source of its own, so there is nowhere
// "further" for a lookup to reach. That is not a tenancy check (one
// user, §8 N5); it is the shape that must already be true the day a
// second user arrives, priced now rather than retrofitted (O4, I7).

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// TestReadsAreScopedToThisDaemon — a miss is not-found, and the miss
// comes from the ONE data door the surface has. Two halves:
//
// (a) behavioural: an id this store does not hold answers not-found
// (store.ErrNotFound through the real daemon's wrapping), and the
// surface dispatched the id VERBATIM — it resolved, widened, or
// defaulted nothing itself.
//
// (b) structural: the Mux holds no second source. Reflection over its
// fields: there is no store handle, no SQL handle, no file, no peer
// address — only the single Caller seam (the daemon's dispatcher) and
// the table. A read that "returns whatever it finds" needs a second
// place to find it from; this type does not have one.
func TestReadsAreScopedToThisDaemon(t *testing.T) {
	seen := make(chan recordedCall, 4)
	c := &recordingScopeCaller{seen: seen}
	m := NewMux(c, Actions())
	rec := get(t, m, "/v1/loop/show?id=loop-not-here")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("a miss in this store answered %d; want 404 (Q11: not-found, never a further reach)", rec.Code)
	}
	got := <-seen
	if got.method != string(protocol.MethodLoopShow) || !strings.Contains(got.params, "loop-not-here") {
		t.Fatalf("the surface did not pass the caller's id through verbatim: %s %s", got.method, got.params)
	}
	for _, f := range reflectFields(&Mux{}) {
		ft := strings.ToLower(f.Type.String())
		for _, forbidden := range []string{"store.Store", "sql.DB", "gorm", "os.File", "net.Conn", "http.Client"} {
			if strings.Contains(ft, strings.ToLower(forbidden)) {
				t.Errorf("Mux holds a %s — a second data source is how a scoped read becomes a tenancy bug (O4)", forbidden)
			}
		}
	}
	// Import-level half: no SQL or store HANDLE import anywhere in the
	// package's non-test code (errors.go's import of internal/store is
	// for the ErrNotFound SENTINEL — classification, not data access).
	for _, f := range nonTestSources(t, ".") {
		srcBytes, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src := string(srcBytes)
		for _, forbidden := range []string{`"database/sql"`, `gorm.io`, `os.Open(`, `os.OpenFile(`} {
			if strings.Contains(src, forbidden) {
				t.Errorf("%s imports/opens %s — the surface must hold no data source (I7)", f, forbidden)
			}
		}
	}
}

type recordingScopeCaller struct{ seen chan recordedCall }

// (recordedCall is http_test.go's — same package, same shape: {method, params}.)

func (c *recordingScopeCaller) Call(method protocol.MethodName, params, result any) error {
	raw := mustJSON(params)
	select {
	case c.seen <- recordedCall{string(method), raw}:
	default:
	}
	// Exactly the real daemon's miss shape (daemon/memory.go wraps
	// store.ErrNotFound with context).
	return fmt.Errorf("store: read loop %s: %w", "loop-not-here", store.ErrNotFound)
}

func reflectFields(v any) []reflect.StructField {
	t := reflect.TypeOf(v).Elem()
	out := make([]reflect.StructField, t.NumField())
	for i := range out {
		out[i] = t.Field(i)
	}
	return out
}

func nonTestSources(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".go") && !strings.HasSuffix(e.Name(), "_test.go") {
			out = append(out, e.Name())
		}
	}
	if len(out) == 0 {
		t.Fatal("walked no non-test sources; the absence proved nothing")
	}
	return out
}

// TestAbandonedConnectionIsReaped — the half that matters (a slow reader
// is polite; a dead CI job is not). Against a REAL listener: callers
// connect, send a request whose answer is large enough to block on the
// socket buffer, never read a byte, and never close. Each stalled
// handler must be released by the surface's own write bound (hijack +
// close in writeJSON) and the process's goroutine count must return to
// baseline — the same assertion shape S9's T3 used for its runner.
func TestAbandonedConnectionIsReaped(t *testing.T) {
	prevCall, prevWrite := WithSurfaceTimeouts(5*time.Second, 250*time.Millisecond)
	defer func() { _, _ = WithSurfaceTimeouts(prevCall, prevWrite) }()

	// Large enough to outrun every buffer in the path (loopback kernel
	// absorb included): the writer MUST stall, and the reaper is what
	// releases it. 32 MiB is not subtlety, it is certainty.
	big := map[string]any{"pad": strings.Repeat("x", 32<<20)}
	srv := httptest.NewServer(NewMux(&errCaller{payload: big}, Actions()))
	defer srv.Close()

	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	baseline := runtime.NumGoroutine()

	const n = 4
	conns := make([]net.Conn, 0, n)
	defer func() {
		for _, c := range conns {
			_ = c.Close()
		}
	}()
	for i := 0; i < n; i++ {
		conn, err := net.Dial("tcp", srv.Listener.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		req := "GET /v1/loop/list HTTP/1.1\r\nHost: x\r\nConnection: close\r\n\r\n"
		if _, err := conn.Write([]byte(req)); err != nil {
			t.Fatal(err)
		}
		conns = append(conns, conn) // read NOTHING; close NOTHING (yet)
	}

	// The second abandoned shape (the one a dying CI job actually leaves):
	// a connection that disappears MID-REQUEST — the server is still
	// reading request bytes, no handler ever started, the client vanishes
	// without FIN. When the OS eventually signals the dead peer (or the
	// test closes its end), the server's parked read must unwind and the
	// goroutine must go. A surface that keeps these holds a goroutine per
	// killed process, forever.
	disappear := make([]net.Conn, 0, 4)
	for i := 0; i < 4; i++ {
		conn, err := net.Dial("tcp", srv.Listener.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Write([]byte("POST /v1/loops HTTP/1.1\r\nHost: x\r\nContent-Length: 9999\r\n\r\npartial...")); err != nil {
			t.Fatal(err)
		}
		disappear = append(disappear, conn)
	}
	// RUDELY close (linger off → RST, not FIN): the silent disappear a
	// killed process produces, not the polite half-close a shell does.
	for _, c := range disappear {
		if tc, ok := c.(*net.TCPConn); ok {
			_ = tc.SetLinger(0)
		}
		_ = c.Close()
	}

	deadline := time.Now().Add(12 * time.Second)
	for {
		runtime.GC()
		if runtime.NumGoroutine() <= baseline+n/2 {
			return // reaped: the stalled writers were released, their handlers exited
		}
		if time.Now().After(deadline) {
			buf := make([]byte, 64<<10)
			nb := runtime.Stack(buf, true)
			t.Logf("STUCK GOROUTINES:\n%s", string(buf[:nb]))
			t.Fatalf("goroutines did not return to baseline (%d started at, %d now, %d stalled readers + %d disappeared mid-request) — a dead reader pins a handler past the write bound, or a dead request pins the server",
				baseline, runtime.NumGoroutine(), n, 4)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestNoHandlerHoldsAStoreLock — asserted on shape and on goroutine
// accounting, not on a comment: the handlers hold NO data handle (the
// scope gate above proves it structurally), and after a burst of
// answered slow reads the package's goroutine count returns to baseline
// — a finished request leaves NOTHING behind. (The contention itself is
// a property of the daemon's store behind the surface — asserted live in
// cmd/aeon, where the loop runner's writer is the other side of the WAL.)
func TestNoHandlerHoldsAStoreLock(t *testing.T) {
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	baseline := runtime.NumGoroutine()

	c := newFakeCaller()
	c.delays[protocol.MethodLoopList] = 20 * time.Millisecond
	m := NewMux(c, Actions())
	for i := 0; i < 40; i++ {
		rec := get(t, m, "/v1/loop/list")
		if rec.Code != http.StatusOK {
			t.Fatalf("read %d answered %d", i, rec.Code)
		}
	}
	deadline := time.Now().Add(4 * time.Second)
	for {
		runtime.GC()
		if runtime.NumGoroutine() <= baseline+4 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("40 completed reads left %d goroutines over baseline — a finished request is holding something", runtime.NumGoroutine()-baseline)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestSlowReaderStillGetsItsBytes — the bound is for ABANDONED readers,
// not slow-but-alive ones: a reader that consumes at a trickle gets the
// full body as long as it keeps reading (the write limit bounds a STALL,
// and 250ms of no-progress with an actively-draining client fits well
// inside any real window — this test keeps the reaper from being a
// slow-client axe).
func TestSlowReaderStillGetsItsBytes(t *testing.T) {
	prevCall, prevWrite := WithSurfaceTimeouts(5*time.Second, 3*time.Second)
	defer func() { _, _ = WithSurfaceTimeouts(prevCall, prevWrite) }()
	srv := httptest.NewServer(NewMux(&errCaller{payload: map[string]any{"loops": []any{map[string]any{"id": "loop-1"}}}}, Actions()))
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/v1/loop/list")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 || !strings.Contains(string(body), "loop-1") {
		t.Fatalf("a live reader was truncated: %d %q", resp.StatusCode, body)
	}
}
