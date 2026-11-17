package protocol

// The surface fixtures (S47 T6). Generated, not typed: a fixture someone
// transcribed is a fixture that can agree with a bug. Run with
// S47_SURFACE_FIXTURES=<dir> to rewrite them from the live dispatch; the
// conformance test then compares a re-dispatch against what is on disk, so a
// classification that changes without the fixtures changing fails loudly.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type surfaceFixtureCase struct {
	name   string
	id     int
	method MethodName
	params string
	// unwiredSeam drops Table.NarrowMethods before the dispatch, which is the
	// only way to make the daemon honestly say `unknown`.
	unwiredSeam bool
}

func surfaceFixtureCases() []surfaceFixtureCase {
	return []surfaceFixtureCase{
		{name: "list-kind", id: 701, method: MethodSurfaceList, params: `{"kind":"diagnostic"}`},
		{name: "list-door-agent-service", id: 702, method: MethodSurfaceList, params: `{"door":"agentService"}`},
		{name: "list-q", id: 703, method: MethodSurfaceList, params: `{"q":"webhook"}`},
		{name: "list-no-match", id: 704, method: MethodSurfaceList, params: `{"q":"no-method-can-carry-this-word"}`},
		{name: "get-ok", id: 705, method: MethodSurfaceGet, params: `{"method":"loop.show"}`},
		{name: "get-unknown", id: 706, method: MethodSurfaceGet, params: `{"method":"loop.shwo"}`},
		{name: "get-actor-refused", id: 707, method: MethodSurfaceGet, params: `{"method":"loop.show","actor":{"kind":"agent","id":"atlas"}}`},
		{name: "list-narrow-seam-unwired", id: 708, method: MethodSurfaceList, params: `{"door":"narrow"}`, unwiredSeam: true},
	}
}

func dispatchSurfaceFixture(t *testing.T, c surfaceFixtureCase) (json.RawMessage, json.RawMessage) {
	t.Helper()
	tbl, conn := surfaceTestTable(t)
	if c.unwiredSeam {
		tbl.NarrowMethods = nil
	}
	req := map[string]any{"jsonrpc": "2.0", "id": c.id, "method": string(c.method)}
	if c.params != "" {
		var p any
		if err := json.Unmarshal([]byte(c.params), &p); err != nil {
			t.Fatal(err)
		}
		req["params"] = p
	}
	rawReq, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	resp := map[string]any{"jsonrpc": "2.0", "id": c.id}
	out, err := tbl.Dispatch(context.Background(), conn, c.method, json.RawMessage(c.params))
	if err != nil {
		var ae *Error
		if errors.As(err, &ae) {
			resp["error"] = map[string]any{"code": ae.Code, "message": ae.Message, "data": ae.Data}
		} else {
			// A shape refusal is the transport's own -32602 (D9: the shape of
			// params is a protocol-level fact, not an Aeon domain), which is how
			// ErrBadParams reaches a client.
			resp["error"] = map[string]any{"code": -32602, "message": err.Error()}
		}
	} else {
		resp["result"] = out
	}
	rawResp, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	return rawReq, rawResp
}

// TestZZSurfaceFixtures writes the fixtures when the env var names a directory,
// and otherwise CHECKS them: every case must still answer exactly what is on
// disk, and every file must carry a description a reader can learn from.
func TestZZSurfaceFixtures(t *testing.T) {
	dir := os.Getenv("S47_SURFACE_FIXTURES")
	cases := surfaceFixtureCases()
	if dir == "" {
		dir = filepath.Join("conformance", "surface")
	}
	if os.Getenv("S47_SURFACE_FIXTURES") != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, c := range cases {
			req, resp := dispatchSurfaceFixture(t, c)
			body := map[string]any{"description": "TODO", "method": string(c.method), "request": json.RawMessage(req), "response": json.RawMessage(resp)}
			b, err := json.MarshalIndent(body, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, c.name+".json"), append(b, '\n'), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		t.Skipf("wrote %d fixtures to %s", len(cases), dir)
	}
	for _, c := range cases {
		path := filepath.Join(dir, c.name+".json")
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		var onDisk struct {
			Description string          `json:"description"`
			Method      string          `json:"method"`
			Request     json.RawMessage `json:"request"`
			Response    json.RawMessage `json:"response"`
		}
		if err := json.Unmarshal(raw, &onDisk); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if onDisk.Description == "" || onDisk.Description == "TODO" {
			t.Errorf("%s carries no description — a fixture nobody can read is a fixture nobody can review", path)
		}
		if onDisk.Method != string(c.method) {
			t.Errorf("%s names method %q, the case names %q", path, onDisk.Method, c.method)
		}
		_, resp := dispatchSurfaceFixture(t, c)
		var want, got any
		if err := json.Unmarshal(onDisk.Response, &want); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(resp, &got); err != nil {
			t.Fatal(err)
		}
		w, _ := json.Marshal(want)
		g, _ := json.Marshal(got)
		if string(w) != string(g) {
			t.Errorf("%s no longer matches a live dispatch\n  on disk: %s\n  today:   %s", path, w, g)
		}
	}
}
