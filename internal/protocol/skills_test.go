package protocol

// skills_test.go — the catalog's socket doors over a fake catalog:
// registration is conditional on the seam (V13), the family carries
// exactly five methods (no sixth door appears quietly — no "refresh",
// no "execute", no door onto nothing), refusals name what they refused,
// and answers pass through verbatim — the handler's whole job is
// marshaling (the asyncworkers_test.go proof shape, carried).

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/skills"
)

type fakeSkills struct {
	scanCalls  int
	forceCalls int
	lastScan   bool

	res     *skills.ScanResult
	getRes  *skills.GetResult
	getErr  error
	instRes *skills.InstallResult
	instErr error
	remRes  *skills.RemoveResult
	remErr  error
	lastReq skills.InstallRequest
	remName string
}

func (f *fakeSkills) Scan(force bool) (*skills.ScanResult, error) {
	f.scanCalls++
	if force {
		f.forceCalls++
	}
	f.lastScan = force
	if f.res == nil {
		f.res = &skills.ScanResult{
			UserRoot:    "/home/dev/.agents/skills",
			ManagedRoot: "/home/dev/.local/share/aeon/skills",
			ScannedAt:   time.Date(2026, 10, 30, 18, 0, 0, 0, time.UTC),
			Skills: []*skills.Skill{
				{Name: "pdf", Status: skills.StatusValid, Source: skills.SourceUser},
				{Name: "broken", Status: skills.StatusInvalid, Source: skills.SourceManaged},
			},
			Summary: skills.Summary{Valid: 1, Invalid: 1},
		}
	}
	return f.res, nil
}

func (f *fakeSkills) Get(name string) (*skills.GetResult, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.getRes != nil {
		return f.getRes, nil
	}
	return &skills.GetResult{Skill: &skills.Skill{Name: name, Status: skills.StatusValid}, Body: "body of " + name}, nil
}

func (f *fakeSkills) GetUnder(path string) (*skills.GetResult, error) {
	return f.Get(path)
}

func (f *fakeSkills) Install(ctx context.Context, req skills.InstallRequest) (*skills.InstallResult, error) {
	f.lastReq = req
	if f.instErr != nil {
		return nil, f.instErr
	}
	if f.instRes != nil {
		return f.instRes, nil
	}
	return &skills.InstallResult{Skill: &skills.Skill{Name: "pdf", Status: skills.StatusValid, Source: skills.SourceManaged}}, nil
}

func (f *fakeSkills) Remove(name string) (*skills.RemoveResult, error) {
	f.remName = name
	if f.remErr != nil {
		return nil, f.remErr
	}
	if f.remRes != nil {
		return f.remRes, nil
	}
	return &skills.RemoveResult{Name: name}, nil
}

func skillsTable(t *testing.T, f *fakeSkills) *Table {
	t.Helper()
	tab := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	tab.Skills = f
	if !tab.RegisterSkills() {
		t.Fatal("RegisterSkills refused with a wired seam")
	}
	return tab
}

func skillsCall(t *testing.T, tab *Table, name MethodName, params any) (any, error) {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	switch name {
	case MethodSkillsList:
		return tab.handleSkillsList(ctx, nil, raw)
	case MethodSkillsGet:
		return tab.handleSkillsGet(ctx, nil, raw)
	case MethodSkillsValidate:
		return tab.handleSkillsValidate(ctx, nil, raw)
	case MethodSkillsInstall:
		return tab.handleSkillsInstall(ctx, nil, raw)
	case MethodSkillsRemove:
		return tab.handleSkillsRemove(ctx, nil, raw)
	}
	t.Fatalf("unknown method %s", name)
	return nil, nil
}

func TestSkillsMethodsAbsentWithoutSeam(t *testing.T) {
	tab := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	if tab.RegisterSkills() {
		t.Fatal("RegisterSkills reported success with a nil seam")
	}
	for _, m := range tab.Methods() {
		if strings.HasPrefix(string(m), "skills.") {
			t.Fatalf("a storeless daemon registered %s — absent, not refusing (V13)", m)
		}
	}
}

func TestSkillsFamilyCarriesExactlyFive(t *testing.T) {
	tab := skillsTable(t, &fakeSkills{})
	want := map[MethodName]bool{
		MethodSkillsList: true, MethodSkillsGet: true, MethodSkillsValidate: true,
		MethodSkillsInstall: true, MethodSkillsRemove: true,
	}
	got := map[MethodName]bool{}
	for _, m := range tab.Methods() {
		if strings.HasPrefix(string(m), "skills.") {
			got[m] = true
		}
	}
	if len(got) != len(want) {
		t.Fatalf("family = %v, want exactly the five", got)
	}
	for m := range want {
		if !got[m] {
			t.Fatalf("family missing %s", m)
		}
	}
	// None is Debug: these are product surfaces (the narrow exclusion is
	// the allow-list's job — asserted in cmd/aeon/skills_guard_test.go).
	for _, m := range tab.Methods() {
		if strings.HasPrefix(string(m), "skills.") {
			if entry, ok := tab.methods[m]; ok && entry.Debug {
				t.Fatalf("%s is Debug-marked — the bench page needs it as a product surface", m)
			}
		}
	}
}

func TestSkillsListFiltersAndRefusesVocabulary(t *testing.T) {
	tab := skillsTable(t, &fakeSkills{})

	res, err := skillsCall(t, tab, MethodSkillsList, SkillsListParams{Status: "invalid"})
	if err != nil {
		t.Fatal(err)
	}
	lr := res.(*skills.ScanResult)
	if len(lr.Skills) != 1 || lr.Skills[0].Name != "broken" {
		t.Fatalf("status filter: %+v", lr.Skills)
	}
	if lr.Summary.Valid != 1 || lr.Summary.Invalid != 1 {
		t.Fatalf("summary must be the whole scan's, not the page's: %+v", lr.Summary)
	}

	if _, err := skillsCall(t, tab, MethodSkillsList, SkillsListParams{Status: "broken"}); err == nil {
		t.Fatal("a made-up status must refuse with the vocabulary")
	} else {
		var e *Error
		if !errors.As(err, &e) || e.Code != ErrInvalidScope.Code {
			t.Fatalf("a made-up status is the named-scope refusal: %v", err)
		}
		if !strings.Contains(e.Data.What, "valid") || !strings.Contains(e.Data.What, "warn") {
			t.Fatalf("refusal must name the vocabulary: %q", e.Data.What)
		}
	}

	res, err = skillsCall(t, tab, MethodSkillsList, SkillsListParams{Query: "PDF"})
	if err != nil {
		t.Fatal(err)
	}
	if lr := res.(*skills.ScanResult); len(lr.Skills) != 1 || lr.Skills[0].Name != "pdf" {
		t.Fatalf("query filter: %+v", lr.Skills)
	}
}

func TestSkillsGetNeedsAnAddress(t *testing.T) {
	tab := skillsTable(t, &fakeSkills{})
	if _, err := skillsCall(t, tab, MethodSkillsGet, SkillsGetParams{}); err == nil {
		t.Fatal("an empty ask must refuse — it matches nothing and answering it is a lie")
	}
	f := &fakeSkills{getErr: skills.ErrNotFound}
	tab = skillsTable(t, f)
	_, err := skillsCall(t, tab, MethodSkillsGet, SkillsGetParams{Name: "ghost"})
	var e *Error
	if !errors.As(err, &e) || e.Code != ErrInvalidScope.Code {
		t.Fatalf("a miss is the named-scope refusal, not a fault: %v", err)
	}
	if !strings.Contains(e.Data.What, "ghost") {
		t.Fatalf("the refusal must name the thing refused: %q", e.Data.What)
	}
}

func TestSkillsValidateForcesTheScan(t *testing.T) {
	f := &fakeSkills{}
	tab := skillsTable(t, f)
	if _, err := skillsCall(t, tab, MethodSkillsList, SkillsListParams{}); err != nil {
		t.Fatal(err)
	}
	if f.lastScan {
		t.Fatal("list must answer the cache")
	}
	res, err := skillsCall(t, tab, MethodSkillsValidate, SkillsValidateParams{})
	if err != nil {
		t.Fatal(err)
	}
	if !f.lastScan {
		t.Fatal("validate must force the re-scan — it IS the freshness door")
	}
	vr := res.(SkillsValidateResult)
	if len(vr.Skills) != 2 || vr.Summary.Invalid != 1 {
		t.Fatalf("validate answers the whole catalog when no name is named: %+v", vr)
	}
	if _, err := skillsCall(t, tab, MethodSkillsValidate, SkillsValidateParams{Name: "ghost"}); err == nil {
		t.Fatal("validating a name that does not exist must refuse, not return an empty list")
	}
}

func TestSkillsInstallRefusalTravelsVerbatim(t *testing.T) {
	f := &fakeSkills{instErr: &skills.ErrRefused{Field: "name", Detail: `a managed skill named "pdf" already exists at /x — re-run with force to replace it`}}
	tab := skillsTable(t, f)
	_, err := skillsCall(t, tab, MethodSkillsInstall, SkillsInstallParams{Source: "/home/dev/downloads/pdf"})
	var e *Error
	if !errors.As(err, &e) || e.Code != ErrSkillsRefused.Code {
		t.Fatalf("an installer refusal is the skills refusal code, not a fault: %v", err)
	}
	if !strings.Contains(e.Data.What, "re-run with force") {
		t.Fatalf("the installer's own sentence must travel verbatim: %q", e.Data.What)
	}
	if f.lastReq.Source == "" {
		t.Fatal("the request did not reach the service")
	}

	// And a disk/network fault is the OTHER code — the discrimination is
	// the whole point of DB19.
	f2 := &fakeSkills{instErr: errors.New("git clone timed out")}
	tab = skillsTable(t, f2)
	_, err = skillsCall(t, tab, MethodSkillsInstall, SkillsInstallParams{Source: "acme/skills"})
	if !errors.As(err, &e) || e.Code != ErrSkillsUnavailable.Code {
		t.Fatalf("an infrastructure fault is not a refusal (DB19): %v", err)
	}
}

func TestSkillsRemoveNamesItsTarget(t *testing.T) {
	f := &fakeSkills{}
	tab := skillsTable(t, f)
	res, err := skillsCall(t, tab, MethodSkillsRemove, SkillsRemoveParams{Name: "pdf"})
	if err != nil {
		t.Fatal(err)
	}
	if f.remName != "pdf" {
		t.Fatalf("remove did not reach the service: %q", f.remName)
	}
	if _, ok := res.(*skills.RemoveResult); !ok {
		t.Fatalf("unexpected result %T", res)
	}
	if _, err := skillsCall(t, tab, MethodSkillsRemove, SkillsRemoveParams{}); err == nil {
		t.Fatal("an empty name must be refused")
	}
}
