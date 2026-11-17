package protocol

// skills.go — the app's door onto the Agent Skills catalog (S65 T3).
//
// This file adds JSON-RPC marshaling around internal/skills' read/install/
// remove surface and NOTHING else — the asyncworkers.go discipline,
// restated: the daemon wires ONE *skills.Service, every door reads through
// it, and a second query implementation is what the P34 amendment exists
// to prevent.
//
// # What the family carries, and what it deliberately does not
//
// Five methods: three reads (list, get, validate) and two writes (install,
// remove). The writes are the HUMAN door — install fetches from a remote
// and writes to disk, remove deletes — and like settings.set they are
// socket-only: none is Debug (they are product surfaces, not diagnostics)
// and none is named by the narrow surface's Actions() allow-list, which
// stays the single decision about what is projected (S44 CS9's precedent).
// There is no skills.refresh: freshness is a forced scan, which is
// exactly what validate does — two doors for one act would be two truths.
//
// # Absent, not refusing (V13)
//
// Nil seam keeps all five out of the table. A daemon assembled without the
// catalog has no skills to speak of; the methods have nothing to be the
// door OF.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/cjlapao/aeon/internal/skills"
)

// Skills is the daemon-injected catalog. The shape is the service's own
// methods restated — drift between seam and service is a compile failure
// at the daemon's single wiring point, not a runtime surprise.
type Skills interface {
	Scan(force bool) (*skills.ScanResult, error)
	Get(name string) (*skills.GetResult, error)
	GetUnder(path string) (*skills.GetResult, error)
	Install(ctx context.Context, req skills.InstallRequest) (*skills.InstallResult, error)
	Remove(name string) (*skills.RemoveResult, error)
}

// --- params ---------------------------------------------------------------

// SkillsListParams: {status?, source?, query?, limit?}. Every narrowing is
// optional; the page-mount question is "everything, honestly", and a
// screen that wants fewer narrows rather than the catalog answering less.
type SkillsListParams struct {
	Status string `json:"status,omitempty"`
	Source string `json:"source,omitempty"`
	Query  string `json:"query,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

// SkillsGetParams: {name} or {path}. Name addresses the winning entry;
// path addresses any entry including a shadowed one — the second copy of
// a name has the path as its only real identity.
type SkillsGetParams struct {
	Name string `json:"name,omitempty"`
	Path string `json:"path,omitempty"`
}

// SkillsValidateParams: {name?}. Empty validates (re-scans) the whole
// catalog; a name narrows the ANSWER, not the scan — the scan is the
// whole disk either way, and a partial scan would let a broken sibling
// hide.
type SkillsValidateParams struct {
	Name string `json:"name,omitempty"`
}

// SkillsInstallParams: {source, ref?, subpath?, force?}. Source is a
// local folder or a GitHub locator (internal/skills' grammar answers
// which). Force is the operator's explicit overwrite; its absence is a
// refusal, never a silent replace.
type SkillsInstallParams struct {
	Source  string `json:"source"`
	Ref     string `json:"ref,omitempty"`
	Subpath string `json:"subpath,omitempty"`
	Force   bool   `json:"force,omitempty"`
}

// SkillsRemoveParams: {name}. The managed root only — a name that lives
// solely in the user root refuses with the sentence saying so.
type SkillsRemoveParams struct {
	Name string `json:"name"`
}

// --- results --------------------------------------------------------------

// SkillsValidateResult is a forced re-scan's answer: the skills (filtered
// to name when one was named), the whole-catalog summary, and when the
// disk was read.
type SkillsValidateResult struct {
	Skills    []*skills.Skill `json:"skills"`
	Summary   skills.Summary  `json:"summary"`
	ScannedAt string          `json:"scannedAt"`
}

// --- registration -----------------------------------------------------------

// RegisterSkills puts the five on the table — or none of them (nil seam,
// V13). Returns whether the family is in the table so the daemon's wiring
// can be asserted rather than trusted.
func (t *Table) RegisterSkills() bool {
	if t.Skills == nil {
		return false
	}
	for _, m := range []struct {
		name   MethodName
		handle func(context.Context, *Session, json.RawMessage) (any, error)
	}{
		{MethodSkillsList, t.handleSkillsList},
		{MethodSkillsGet, t.handleSkillsGet},
		{MethodSkillsValidate, t.handleSkillsValidate},
		{MethodSkillsInstall, t.handleSkillsInstall},
		{MethodSkillsRemove, t.handleSkillsRemove},
	} {
		t.register(&Method{Name: m.name, RequiresInitialize: true, Handle: m.handle})
	}
	return true
}

func skillsRefused(method string) error {
	return ErrNotWired.WithWhat("skills.%s needs the skills catalog, and this daemon did not wire it — the catalog is absent because there is no catalog", method)
}

func (t *Table) handleSkillsList(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	if t.Skills == nil {
		return nil, skillsRefused("list")
	}
	var p SkillsListParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if s := p.Status; s != "" && s != "valid" && s != "warn" && s != "invalid" {
		return nil, ErrInvalidScope.WithWhat("skills.list refused status %q — the catalog's vocabulary is valid, warn, invalid (internal/skills' Status, and the bench renders exactly those three)", s)
	}
	if s := p.Source; s != "" && s != "user" && s != "managed" {
		return nil, ErrInvalidScope.WithWhat("skills.list refused source %q — the roots are user and managed, nothing else exists", s)
	}
	res, err := t.Skills.Scan(false)
	if err != nil {
		return nil, ErrSkillsUnavailable.WithWhat("skills.list: %v", err)
	}
	filtered := &skills.ScanResult{
		UserRoot:    res.UserRoot,
		ManagedRoot: res.ManagedRoot,
		ScannedAt:   res.ScannedAt,
		Summary:     res.Summary,
		// S66: the watch state rides the FILTERED answer too — filtering
		// skills is the handler's job; dropping the watcher's honesty
		// alongside the rows is how a projection lies.
		Watching:   res.Watching,
		WatchError: res.WatchError,
	}
	q := strings.ToLower(strings.TrimSpace(p.Query))
	for _, sk := range res.Skills {
		if p.Status != "" && string(sk.Status) != p.Status {
			continue
		}
		if p.Source != "" && string(sk.Source) != p.Source {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(sk.Name+" "+sk.Description), q) {
			continue
		}
		filtered.Skills = append(filtered.Skills, sk)
	}
	// limit AFTER filtering: the caller wants "fewest N of THESE", and
	// limiting first would make the page depend on disk order.
	if p.Limit > 0 && len(filtered.Skills) > p.Limit {
		filtered.Skills = filtered.Skills[:p.Limit]
	}
	if filtered.Skills == nil {
		filtered.Skills = []*skills.Skill{}
	}
	return filtered, nil
}

func (t *Table) handleSkillsGet(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	if t.Skills == nil {
		return nil, skillsRefused("get")
	}
	var p SkillsGetParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	name, path := strings.TrimSpace(p.Name), strings.TrimSpace(p.Path)
	if name == "" && path == "" {
		return nil, ErrInvalidScope.WithWhat("skills.get needs a name or a path — an empty ask matches nothing and answering it anyway is how a lookup lies")
	}
	var (
		res any
		err error
	)
	if path != "" {
		res, err = t.Skills.GetUnder(path)
	} else {
		res, err = t.Skills.Get(name)
	}
	if err != nil {
		if errors.Is(err, skills.ErrNotFound) {
			target := name
			if path != "" {
				target = path
			}
			return nil, ErrInvalidScope.WithWhat("skills.get found no skill %q — names come from skills.list (or from an install's answer), never invented", target)
		}
		return nil, ErrSkillsUnavailable.WithWhat("skills.get %s: %v", name, err)
	}
	return res, nil
}

func (t *Table) handleSkillsValidate(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	if t.Skills == nil {
		return nil, skillsRefused("validate")
	}
	var p SkillsValidateParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	res, err := t.Skills.Scan(true)
	if err != nil {
		return nil, ErrSkillsUnavailable.WithWhat("skills.validate: %v", err)
	}
	out := SkillsValidateResult{Summary: res.Summary, ScannedAt: res.ScannedAt.UTC().Format("2006-01-02T15:04:05Z07:00")}
	name := strings.TrimSpace(p.Name)
	for _, sk := range res.Skills {
		if name != "" && sk.Name != name {
			continue
		}
		out.Skills = append(out.Skills, sk)
	}
	if name != "" && len(out.Skills) == 0 {
		return nil, ErrInvalidScope.WithWhat("skills.validate found no skill named %q to re-validate — names come from skills.list", name)
	}
	if out.Skills == nil {
		out.Skills = []*skills.Skill{}
	}
	return out, nil
}

func (t *Table) handleSkillsInstall(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	if t.Skills == nil {
		return nil, skillsRefused("install")
	}
	var p SkillsInstallParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Source) == "" {
		return nil, ErrInvalidScope.WithWhat("skills.install needs a source — a local folder or a GitHub locator (owner/repo, owner/repo/sub/path@ref, or a full github.com URL)")
	}
	res, err := t.Skills.Install(ctx, skills.InstallRequest{
		Source: p.Source, Ref: p.Ref, Subpath: p.Subpath, Force: p.Force,
	})
	if err != nil {
		var ref *skills.ErrRefused
		if errors.As(err, &ref) {
			return nil, ErrSkillsRefused.WithWhat("skills.install refused: %v", ref)
		}
		return nil, ErrSkillsUnavailable.WithWhat("skills.install %s: %v", p.Source, err)
	}
	return res, nil
}

func (t *Table) handleSkillsRemove(ctx context.Context, _ *Session, params json.RawMessage) (any, error) {
	if t.Skills == nil {
		return nil, skillsRefused("remove")
	}
	var p SkillsRemoveParams
	if err := decodeParams(params, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.Name) == "" {
		return nil, ErrInvalidScope.WithWhat("skills.remove needs a name")
	}
	res, err := t.Skills.Remove(p.Name)
	if err != nil {
		var ref *skills.ErrRefused
		if errors.As(err, &ref) {
			return nil, ErrSkillsRefused.WithWhat("skills.remove refused: %v", ref)
		}
		return nil, ErrSkillsUnavailable.WithWhat("skills.remove %s: %v", p.Name, err)
	}
	return res, nil
}
