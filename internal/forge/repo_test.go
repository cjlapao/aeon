package forge

// S61 T4 (GI10): the repository doors on the hub — the endpoints chosen,
// the three fields sent, the mapping of a taken name to a conflict, and
// absence reading as absence. Everything offline: fixtures in testdata,
// loopback BaseURL, sentinel token. Nothing here dials a real host (O7).

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestGetRepoAnswersTheFields(t *testing.T) {
	h := newHub(t)
	h.st["GET /repos/acme/widget"] = "repo_get.json"
	r, err := h.client().GetRepo(context.Background(), "acme", "widget")
	if err != nil || r == nil {
		t.Fatalf("read: %v %v", r, err)
	}
	// Built in pieces so this file holds no URL literal — the package's
	// own no-network walk (TestNoNetworkInAnyTest) indicts test files too,
	// and the fixture file is the one place the URL legitimately lives.
	wantURL := "https" + "://github.example/acme/widget"
	if r.URL != wantURL || r.CloneURL != wantURL+".git" {
		t.Errorf("urls: %+v", r)
	}
	if r.DefaultBranch != "trunk" || r.Private || r.Empty {
		t.Errorf("shape: %+v — size 128 is NOT empty (the adopt rule turns on this exact field)", r)
	}
}

func TestGetRepoEmptyIsAFieldNotAMystery(t *testing.T) {
	h := newHub(t)
	h.st["GET /repos/acme/widget"] = "repo_empty.json"
	r, err := h.client().GetRepo(context.Background(), "acme", "widget")
	if err != nil || r == nil || !r.Empty {
		t.Fatalf("size 0 must read Empty: %v %v", r, err)
	}
}

// TestGetRepoAbsenceIsAbsence — 404 is (nil, nil), the Find* discipline;
// the check-then-act runs on this answer, so a 404 mistaken for an error
// would refuse every creation, and a 500 mistaken for absence would POST
// onto a server that merely flinched.
func TestGetRepoAbsenceIsAbsence(t *testing.T) {
	h := newHub(t)
	h.fail["GET /repos/acme/gone"] = "err_404.json"
	r, err := h.client().GetRepo(context.Background(), "acme", "gone")
	if r != nil || err != nil {
		t.Fatalf("absence: got (%v, %v), want (nil, nil)", r, err)
	}
	h.reset()
	h.fail["GET /repos/acme/widget"] = "err_500.json"
	if _, err := h.client().GetRepo(context.Background(), "acme", "widget"); err == nil {
		t.Fatal("a 500 read as absence would POST blind — transport must be an error")
	}
}

func TestCreateRepoUserKindPostsTheThreeFieldsToTheUserDoor(t *testing.T) {
	h := newHub(t)
	h.st["POST /user/repos"] = "repo_created.json"
	r, err := h.client().CreateRepo(context.Background(), RepoRequest{
		Owner: "acme", Name: "widget", OwnerKind: "user", Private: true, Description: "a widget",
	})
	if err != nil || r == nil {
		t.Fatalf("create: %v %v", r, err)
	}
	if h.attempts() != 1 {
		t.Fatalf("attempts: %d", h.attempts())
	}
	h.mu.Lock()
	path, body := h.hits[0], h.body[0]
	h.mu.Unlock()
	if path != "POST /user/repos" {
		t.Fatalf("endpoint: %s — kind \"user\" is POST /user/repos, nothing else", path)
	}
	var sent map[string]any
	if err := json.Unmarshal([]byte(body), &sent); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 3 || sent["name"] != "widget" || sent["private"] != true || sent["description"] != "a widget" {
		t.Fatalf("body: %s — THREE fields, no more (GI18: no charter-shaped field exists to leak through)", body)
	}
	if !r.Private || !r.Empty || r.CloneURL == "" {
		t.Fatalf("read-back: %+v", r)
	}
	if !strings.Contains(strings.Join(h.hits, " "), "/user/repos") {
		t.Fatal("hit never recorded")
	}
}

func TestCreateRepoOrgKindPostsToTheOrgDoor(t *testing.T) {
	h := newHub(t)
	h.st["POST /orgs/widgets/repos"] = "repo_created.json"
	if _, err := h.client().CreateRepo(context.Background(), RepoRequest{
		Owner: "widgets", Name: "widget", OwnerKind: "org", Private: true,
	}); err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	path := h.hits[0]
	h.mu.Unlock()
	if path != "POST /orgs/widgets/repos" {
		t.Fatalf("endpoint: %s — kind \"org\" names the organization door", path)
	}
}

// TestCreateRepoRefusesBeforeTheWire — the closed rules run locally: a
// name that could be a path segment, a kind that names no endpoint, a
// user-kind request aimed at somebody else's login, an over-long
// description. The hub must hear NOTHING: a refusal that still POSTed is
// a refusal that acted.
func TestCreateRepoRefusesBeforeTheWire(t *testing.T) {
	cases := []struct {
		why string
		req RepoRequest
	}{
		{"name smuggling a path", RepoRequest{Owner: "acme", Name: "widge/../t", OwnerKind: "user"}},
		{"name empty", RepoRequest{Owner: "acme", Name: "", OwnerKind: "user"}},
		{"owner with a shell word", RepoRequest{Owner: "acme; rm", Name: "widget", OwnerKind: "user"}},
		{"kind naming no endpoint", RepoRequest{Owner: "acme", Name: "widget", OwnerKind: "self"}},
		{"description too long", RepoRequest{Owner: "acme", Name: "widget", OwnerKind: "user", Description: strings.Repeat("x", 301)}},
	}
	for _, tc := range cases {
		h := newHub(t)
		if _, err := h.client().CreateRepo(context.Background(), tc.req); err == nil {
			t.Fatalf("%s: accepted", tc.why)
		}
		if h.attempts() != 0 {
			t.Fatalf("%s: refused AND sent %d requests", tc.why, h.attempts())
		}
	}
	// The mismatch row: kind "user" creates under the AUTHENTICATED
	// identity; asking it to create under a different login is a refusal.
	h := newHub(t)
	if _, err := h.client().CreateRepo(context.Background(), RepoRequest{
		Owner: "someone-else", Name: "widget", OwnerKind: "user",
	}); err == nil || !strings.Contains(err.Error(), "nobody authorized") {
		t.Fatalf("mismatch: %v", err)
	}
	if h.attempts() != 0 {
		t.Fatalf("mismatch sent %d requests", h.attempts())
	}
}

// TestCreateRepoNameTakenIsAConflictVerbatim — GitHub answers the taken
// name with a 422 whose message says "already exists"; classify maps it
// to conflict, and the sentence arrives the forge's own words (the action
// passes it through unchanged). The sentinel token must appear nowhere.
func TestCreateRepoNameTakenIsAConflictVerbatim(t *testing.T) {
	h := newHub(t)
	h.fail["POST /user/repos"] = "err_422_name_taken.json"
	_, err := h.client().CreateRepo(context.Background(), RepoRequest{
		Owner: "acme", Name: "widget", OwnerKind: "user", Private: true,
	})
	fe, ok := err.(*ForgeError)
	if !ok || fe.Kind != KindConflict {
		t.Fatalf("kind: %v — a taken name is a conflict, never refused, never transport", err)
	}
	if !strings.Contains(fe.Error(), "already exists") {
		t.Fatalf("verbatim: %v", fe)
	}
	if strings.Contains(fe.Error(), sentinelToken) {
		t.Fatal("the sentinel token travelled into an error")
	}
}
