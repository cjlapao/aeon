package narrow

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	fmw_middleware "github.com/cjlapao/go-backend-scaffolding/framework/api/middleware"

	"github.com/cjlapao/aeon/internal/protocol"
)

// questions_test.go — T5's gates. The sharpest decision in the slice is
// that the surface can READ questions and can never ANSWER or CREATE one,
// asserted over the whole table rather than by name-checking one action
// (C6): the risk is the action added later whose name nobody thought to
// forbid. Both mountings are asserted SEPARATELY (TestBothMountingsAgree)
// — T4's set equality would catch an answer action on one mounting only;
// the failure that matters here is one present on BOTH.

// answerProbe is the extended verb set for these gates. It is deliberately
// WIDER than ForbiddenOperationalControl (which carries "answer" and is
// T1's line of defence): the creation family — fabricate, inject — is not
// operational control, it is provenance (N6), and it gets its own net.
var answerProbe = []string{"answer", "approve", "deny", "accept", "reject", "resolve", "dismiss", "submit"}
var createProbe = []string{"create", "post", "new", "ask", "inject", "fabricate", "insert"}

func carriesAny(tokens, probe []string) string {
	for _, t := range tokens {
		for _, v := range probe {
			if t == v {
				return v
			}
		}
	}
	return ""
}

// checkNoVerbOnAnySurface walks one table's THREE emitted surfaces — the
// table itself, the stdio tool list, the HTTP route table — and returns
// every surface string that carries one of the probe verbs. It is a
// function over tables so the gates can feed it POISONED tables: a check
// that only inspects the honest table has never shown it can catch the
// synonym nobody thought to forbid.
func checkNoVerbOnAnySurface(t *testing.T, table []Action, probe []string) []string {
	t.Helper()
	var bad []string
	// Surface one: the table (names and the socket methods they project).
	for _, a := range table {
		if v := carriesAny(tokenize(a.Name), probe); v != "" {
			bad = append(bad, fmt.Sprintf("table name %q carries %q", a.Name, v))
		}
		if v := carriesAny(tokenize(string(a.SocketMethod)), probe); v != "" {
			bad = append(bad, fmt.Sprintf("socket method %q carries %q", a.SocketMethod, v))
		}
	}
	// Surface two: what stdio EMITS (T3's lesson — assert the emitted
	// list, not the source of the projection).
	srv := NewServer(&countingCaller{}, table)
	for _, name := range srv.ToolNames() {
		if v := carriesAny(tokenize(name), probe); v != "" {
			bad = append(bad, fmt.Sprintf("stdio tool %q carries %q", name, v))
		}
	}
	// Surface three: what HTTP mounts.
	mux := NewMux(&countingCaller{}, table)
	for _, e := range mux.Entries() {
		if v := carriesAny(tokenize(strings.ReplaceAll(e.Path, "/", "_")), probe); v != "" {
			bad = append(bad, fmt.Sprintf("http path %q carries %q", e.Path, v))
		}
	}
	return bad
}

// TestNoActionAnswersAQuestion — the whole table, both mountings' emitted
// surfaces, NOT by name (D6, C6, I5). The poisoned half demonstrates the
// net catches what nobody thought to forbid: the entry below is spelled
// plausibly — a "read" of a question, whose socket method answers.
func TestNoActionAnswersAQuestion(t *testing.T) {
	if bad := checkNoVerbOnAnySurface(t, Actions(), answerProbe); len(bad) > 0 {
		t.Errorf("the surface can answer:\n%s", strings.Join(bad, "\n"))
	}
	// The net works, demonstrated — a plausible poisoned entry must be
	// caught on all three surfaces, not just the table.
	poisoned := append(Actions(), Action{
		Name:         "aeon_question_answer",
		SocketMethod: protocol.MethodName("loop.answer"),
		Kind:         KindRead,
		Cannot:       []string{"cannot answer — the lie this entry tells"},
	})
	caught := checkNoVerbOnAnySurface(t, poisoned, answerProbe)
	if len(caught) == 0 {
		t.Fatal("the poisoned answering entry passed — the enumeration catches nothing")
	}
	surfaces := map[string]bool{}
	for _, c := range caught {
		surfaces[strings.Fields(c)[0]] = true
	}
	for _, want := range []string{"table", "stdio", "http"} {
		if !surfaces[want] {
			t.Errorf("the poison was caught on the table and stdio but not the %s mounting: %v", want, caught)
		}
	}
	// And structurally: answering is a MUTATION, and the only mutation the
	// surface carries is the trigger — which cannot touch a question.
	for _, a := range Actions() {
		if a.Kind == KindTrigger && a.SocketMethod != protocol.MethodLoopStart {
			t.Errorf("a second trigger (%s) exists — every future answer-shaped door would arrive as one", a.SocketMethod)
		}
	}
}

// TestNoActionCreatesAQuestion — same shape (N6). "ask" is in the probe
// deliberately: S11's ask is a command an attended loop runs, never a
// surface capability, and a tool named aeon_question_ask would read as
// that command wearing a hat.
func TestNoActionCreatesAQuestion(t *testing.T) {
	if bad := checkNoVerbOnAnySurface(t, Actions(), createProbe); len(bad) > 0 {
		t.Errorf("the surface can create questions:\n%s", strings.Join(bad, "\n"))
	}
	poisoned := append(Actions(), Action{
		Name:         "aeon_question_ask",
		SocketMethod: protocol.MethodName("question.ask"),
		Kind:         KindTrigger,
		Cannot:       []string{"cannot fabricate — the lie"},
	})
	if len(checkNoVerbOnAnySurface(t, poisoned, createProbe)) == 0 {
		t.Fatal("the poisoned fabricating entry passed")
	}
	// The legitimate noun survives the verb net: aeon_questions_open and
	// aeon_questions_pending are reads OF questions, not verbs ON them —
	// the net must not flag them, or the next person widens it away.
	for _, a := range Actions() {
		if strings.Contains(a.Name, "question") && a.Kind == KindRead {
			if v := carriesAny(tokenize(a.Name), createProbe); v != "" {
				t.Errorf("the read of questions %q was flagged by the %q verb — noun/verb confusion turns the net off", a.Name, v)
			}
		}
	}
}

// TestKeyedCallerCannotAnswerAGate — the obvious URL, WITH a good key: the
// answer attempt reaches the surface (auth passes — that is what the key
// is for) and the surface still has no action for it. 404 with the reason
// AND the alternative (D8, O3); the daemon is never consulted.
func TestKeyedCallerCannotAnswerAGate(t *testing.T) {
	secret := "aeon-t5-gate-key-9a8b7c6d"
	key, err := ResolveKey(writeKeyFile(t, secret))
	if err != nil {
		t.Fatal(err)
	}
	c := &countingCaller{}
	svc := NewHTTPService(c, Actions())
	// Auth reproduced through the framework's real middleware (the same
	// shape T4's gate uses); the SECURITY LEVEL is uniform across the
	// surface, so any mounted route's middleware is the same door.
	authRoute := svc.HTTPRoutes()[0]
	mw := fmw_middleware.NewAuthorizationPreMiddleware(NewValidator(key), nil, &authRoute)
	mux := svc.Mux()

	attempt := func(method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("Authorization", secret)
		rec := httptest.NewRecorder()
		if res := mw.Execute(rec, req); !res.Continue {
			t.Fatalf("a keyed caller was refused at auth on %s %s — this gate is about what happens AFTER the key works", method, path)
		}
		mux.ServeHTTP(rec, req)
		return rec
	}

	// The URLs a caller reaches for, in the order they would try them —
	// and the refusal is the RIGHT one for the shape: answering verbs get
	// the person sentence (N16), posting at the questions routes gets the
	// fabrication sentence (N6). Both carry reason and alternative (O3).
	for _, a := range []struct{ method, path, want string }{
		{http.MethodPost, "/v1/questions/12/answer", "requires a person"},
		{http.MethodPost, "/v1/question/answer", "requires a person"},
		{http.MethodPost, "/v1/questions", "never posted"},                // fabricating one outright
		{http.MethodPost, "/v1/questions/open", "never posted"},           // POSTing at the read route
		{http.MethodDelete, "/v1/questions/12", "never posted"},           // or deleting one
		{http.MethodPut, "/v1/questions/12/approve", "requires a person"}, // the gate-vocabulary shape
	} {
		rec := attempt(a.method, a.path)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s: want 404, got %d — the absence must answer", a.method, a.path, rec.Code)
			continue
		}
		body := rec.Body.String()
		if !strings.Contains(body, a.want) || !strings.Contains(body, "Instead") {
			t.Errorf("%s %s: the refusal must carry %q and the alternative, got %s", a.method, a.path, a.want, body)
		}
	}
	if c.count() != 0 {
		t.Errorf("answering attempts reached the daemon %d times — there is no action to reach, and none was created", c.count())
	}
}

// questionCaller answers question.pending with a real-shaped view so the
// readability gates assert the fields, not just the plumbing.
type questionCaller struct {
	mu    sync.Mutex
	calls int
}

func (c *questionCaller) Call(method protocol.MethodName, params, result any) error {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	if method != protocol.MethodQuestionPending {
		return NewMsg(ErrTransport, "unexpected method " + string(method))
	}
	if p, ok := result.(*any); ok {
		*p = map[string]any{
			"pending": true,
			"question": map[string]any{
				"id": 12, "stepId": 5, "kind": "gate",
				"prompt":  "The plan renames the public API. Continue?",
				"answers": []string{"yes", "no"},
			},
		}
	}
	return nil
}

func (c *questionCaller) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// TestQuestionsAreReadable — id, kind, prompt and answer set ALL present
// through the mounted route. The monitor's whole point is that the
// reporting reaches the level of detail a person needs to decide to walk
// over and answer.
func TestQuestionsAreReadable(t *testing.T) {
	c := &questionCaller{}
	mux := NewMux(c, Actions())
	req := httptest.NewRequest(http.MethodGet, "/v1/questions/open", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("the question read: want 200, got %d %s", rec.Code, rec.Body.String())
	}
	var res struct {
		Pending  *bool `json:"pending"`
		Question *struct {
			ID      *int64    `json:"id"`
			Kind    *string   `json:"kind"`
			Prompt  *string   `json:"prompt"`
			Answers *[]string `json:"answers"`
		} `json:"question"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("the answer did not decode: %v — %s", err, rec.Body)
	}
	if res.Pending == nil || !*res.Pending {
		t.Fatal("pending is not true — the projection dropped the daemon's answer")
	}
	q := res.Question
	if q == nil {
		t.Fatal("the question is missing")
	}
	if q.ID == nil || *q.ID != 12 {
		t.Error("id absent — a monitor cannot point a human at the thing to answer")
	}
	if q.Kind == nil || *q.Kind != "gate" {
		t.Error("kind absent")
	}
	if q.Prompt == nil || !strings.Contains(*q.Prompt, "Continue") {
		t.Error("prompt absent — `waiting on a gate` without it is not an answer someone can act on")
	}
	if q.Answers == nil || len(*q.Answers) != 2 {
		t.Error("the declared answer set absent — the monitor must show what the asker offered")
	}
	if c.count() != 1 {
		t.Errorf("question.pending dispatched %d times, want 1", c.count())
	}
}

// TestKindIsVisibleToAMonitor — the kind is ON the wire, and the reason
// this does not contradict S9's U14 is recorded where code readers will
// meet it (the comment lives in questions.go and protocol/memory.go; this
// test pins that it exists, because a comment that can vanish silently is
// not a record).
func TestKindIsVisibleToAMonitor(t *testing.T) {
	src, err := os.ReadFile("questions.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(src)
	if !strings.Contains(text, "U14") {
		t.Error("questions.go must carry the U14 distinction — different audience, different surface")
	}
	if !strings.Contains(text, "whether to go and answer") {
		t.Error("the comment must say WHY the monitor benefits (the DECIDER about to answer must not see the distinction; the person deciding WHETHER to go must)")
	}
	// And the field is genuinely carried: the fixture states the vocabulary.
	view := protocol.QuestionView{ID: 1, Kind: "gate", Prompt: "p", Answers: []string{"yes"}}
	raw, _ := json.Marshal(view)
	for _, want := range []string{`"kind":"gate"`, `"answers":["yes"]`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("QuestionView does not carry %s", want)
		}
	}
}

// TestAbsentNotForbidden — 404, not 403 (N14's reasoning). A 403 would
// describe a door that exists and is locked, which is a lie about this
// surface, and a lie that INVITES key-hunting: locked doors get picked.
func TestAbsentNotForbidden(t *testing.T) {
	c := &countingCaller{}
	mux := NewMux(c, Actions())
	for _, a := range []struct{ method, path string }{
		{http.MethodPost, "/v1/questions/1/answer"},
		{http.MethodPost, "/v1/questions"},
		{http.MethodGet, "/v1/questions/answer"},
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(a.method, a.path, nil))
		if rec.Code == http.StatusForbidden {
			t.Errorf("%s %s answered 403 — an action that does not exist cannot be forbidden", a.method, a.path)
		}
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s: want 404, got %d", a.method, a.path, rec.Code)
		}
		if body := rec.Body.String(); strings.Contains(body, "403") || strings.Contains(strings.ToLower(body), "forbidden") {
			t.Errorf("the body reads as a forbidden door: %s", body)
		}
		var err struct {
			Error struct {
				Kind string `json:"kind"`
			} `json:"error"`
		}
		if json.Unmarshal(rec.Body.Bytes(), &err) != nil || err.Error.Kind != "not-found" {
			t.Errorf("the discriminated kind must be not-found, got %s", rec.Body)
		}
	}
}

// TestBothMountingsAgree — asserted SEPARATELY on purpose. T4's set
// equality catches an answer action on ONE mounting (the sets would
// differ); the failure that matters here is an answer action present on
// BOTH — which equality happily certifies. So each mounting is refused in
// its own protocol's terms, here, individually.
func TestBothMountingsAgree(t *testing.T) {
	// stdio: a guessed tool name, over the real line loop.
	c := &countingCaller{}
	cReq, sReq := io.Pipe()
	cOut, sOut := io.Pipe()
	srv := NewServer(c, Actions())
	go func() { _ = srv.Serve(context.Background(), cReq, sOut) }()
	go func() {
		fmt.Fprintln(sReq, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"aeon_question_answer","arguments":{"id":12,"answer":"yes"}}}`)
		fmt.Fprintln(sReq, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"aeon_question_create","arguments":{"prompt":"did the model ask this?"}}}`)
	}()
	deadline := time.After(10 * time.Second)
	seen := map[int]map[string]any{}
	dec := json.NewDecoder(bufio.NewReader(cOut))
	for len(seen) < 2 {
		got := make(chan struct{})
		go func() {
			var m map[string]any
			if err := dec.Decode(&m); err == nil {
				if id, _ := m["id"].(float64); id > 0 {
					seen[int(id)] = m
				}
			}
			close(got)
		}()
		select {
		case <-got:
		case <-deadline:
			t.Fatal("the stdio server stopped answering")
		}
	}
	for _, tc := range []struct {
		id   int
		want []string
	}{{1, []string{"requires a person", "Instead"}}, {2, []string{"fabricated prompt", "Instead"}}} {
		m := seen[tc.id]
		if m == nil {
			t.Fatal("missing reply")
		}
		e, _ := m["error"].(map[string]any)
		if e == nil {
			t.Fatalf("stdio answered an answering tool call without an error: %v", m)
		}
		msg, _ := e["message"].(string)
		for _, w := range tc.want {
			if !strings.Contains(msg, w) {
				t.Errorf("stdio refusal for tool call %d must carry %q, got %q", tc.id, w, msg)
			}
		}
	}
	// The read tool still works on stdio while the refusals answer —
	// absence of answering is not absence of reading.
	//
	// HTTP: the same two guesses, in the protocol the CI actually speaks.
	hc := &countingCaller{}
	mux := NewMux(hc, Actions())
	for _, a := range []struct{ method, path string }{
		{http.MethodPost, "/v1/questions/12/answer"},
		{http.MethodPost, "/v1/questions"},
	} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(a.method, a.path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("http %s %s: want 404 (the answer is absent on THIS mounting too), got %d", a.method, a.path, rec.Code)
		}
	}
	if c.count()+hc.count() != 0 {
		t.Errorf("neither mounting dispatched anything to the daemon — no answering action exists to reach on EITHER side (P25's two transports share one table, and the table has no such thing)")
	}
}

// TestPermanenceIsRecorded — the decision is permanent (§8 N3), not
// deferred, and the record is a comment: the next person to want this
// will look for the flag first, so the flag's ABSENCE is what they must
// find, in the file they will open. This test greps for the words the
// argument rests on — a comment that vanishes silently is not a record.
func TestPermanenceIsRecorded(t *testing.T) {
	for _, f := range []string{"questions.go", "actions.go"} {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		text := strings.ToLower(string(src))
		if !strings.Contains(text, "permanent") {
			t.Errorf("%s must record the permanence of the questions boundary", f)
		}
	}
	src, _ := os.ReadFile("questions.go")
	text := string(src)
	for _, want := range []string{"permanent, not deferred", "N16", "N6", "authenticates", "decoration"} {
		if !strings.Contains(text, want) {
			t.Errorf("questions.go must carry %q — the argument travels with the code", want)
		}
	}
	// And the table entries themselves say it, where a tools/list reader
	// sees it: the cannot lines on the question reads carry "permanent".
	found := 0
	for _, a := range Actions() {
		if a.Kind == KindRead && strings.Contains(a.Name, "questions") {
			joined := strings.Join(a.Cannot, " ")
			if !strings.Contains(joined, "permanent") {
				t.Errorf("%s's cannot lines must state the permanence (N14: legibility, not just absence)", a.Name)
			}
			found++
		}
	}
	if found != 2 {
		t.Errorf("expected both question reads (pending, open) to carry it; found %d", found)
	}
}
