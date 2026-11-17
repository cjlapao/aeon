package daemon

import (
	"encoding/base64"
	"fmt"

	"github.com/cjlapao/aeon/internal/action"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cjlapao/aeon/internal/desk"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// componentSlug's regex MOVED to internal/desk (S61 T5): the intake_plan
// validator and this create door enforce one slug rule through the ONE
// shared function desk.ValidComponentSlug — desk is inside the daemon's
// import set, never the reverse, so the sharing runs one way and no cycle
// opens.
var componentImage = regexp.MustCompile(`^data:image/(?:png|jpeg|webp|gif);base64,`)

func validateComponentIcon(icon string) error {
	if icon == "" {
		return nil
	}
	if strings.HasPrefix(icon, "data:") {
		prefix := componentImage.FindString(icon)
		if prefix == "" {
			return fmt.Errorf("component icon must be a PNG, JPEG, WebP, or GIF image")
		}
		encoded := icon[len(prefix):]
		if len(encoded) > base64.StdEncoding.EncodedLen(256*1024) {
			return fmt.Errorf("component icon image exceeds 256 KiB")
		}
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || len(decoded) > 256*1024 {
			return fmt.Errorf("component icon image is invalid or exceeds 256 KiB")
		}
		return nil
	}
	if len(icon) > 64 || strings.ContainsAny(icon, "\r\n\x00") || strings.HasPrefix(icon, "http:") || strings.HasPrefix(icon, "https:") {
		return fmt.Errorf("component icon must be short text or an uploaded image")
	}
	return nil
}

type ComponentStore struct{ store *store.Store }

func NewComponentStore(s *store.Store) *ComponentStore { return &ComponentStore{store: s} }

func componentView(p store.Component, eff store.EffectiveComponent) protocol.ComponentView {
	return protocol.ComponentView{ID: p.ID, ParentID: p.ParentID, Name: p.Name, Slug: p.Slug, Root: p.Root,
		Remote: p.Remote, RemoteKey: p.RemoteKey, RepoKey: p.RepoKey, Owner: p.Owner,
		Icon: p.Icon, Tags: p.Tags, ShortDescription: p.ShortDescription, Description: p.Description,
		DefaultBranch: eff.DefaultBranch.Value, BranchPrefix: eff.BranchPrefix.Value, MaxParallelAgents: eff.MaxParallelAgents.Value,
		CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt}
}

// resolveComponentView reads the component and its resolved (own -> root ->
// default) inheritable values through effective.go, the ONE reader of the
// five inheritable columns (CT6). A resolution failure degrades to the
// column defaults, never a panic.
func (p *ComponentStore) viewOf(c store.Component) protocol.ComponentView {
	eff, err := p.store.EffectiveComponent(c.ID)
	if err != nil {
		eff = store.EffectiveComponent{ComponentID: c.ID}
	}
	return componentView(c, eff)
}

// validateMaxParallelAgents is the seam's PA14 range check (mirrors the
// schema's CHECK 1..16): a cap outside the range is a named refusal before
// any write, so the sentence names the number it refused (DB19). The store's
// CHECK is the backstop; this is the thing a person reads.
func validateMaxParallelAgents(n int) error {
	if n < 1 || n > 16 {
		return fmt.Errorf("maxParallelAgents must be between 1 and 16, got %d", n)
	}
	return nil
}

func (p *ComponentStore) CreateComponent(in protocol.ComponentCreateParams) (protocol.ComponentView, error) {
	in.Name, in.Slug, in.Root = strings.TrimSpace(in.Name), strings.TrimSpace(in.Slug), strings.TrimSpace(in.Root)
	if !desk.ValidComponentSlug(in.Slug) {
		return protocol.ComponentView{}, fmt.Errorf("component slug must use lowercase letters, numbers, and single hyphens")
	}
	if in.MaxParallelAgents > 0 {
		if err := validateMaxParallelAgents(in.MaxParallelAgents); err != nil {
			return protocol.ComponentView{}, err
		}
	}
	if err := validateComponentIcon(in.Icon); err != nil {
		return protocol.ComponentView{}, err
	}
	in.Root = filepath.Clean(in.Root)
	if err := validateLoopWorkspace(in.Root); err != nil {
		return protocol.ComponentView{}, err
	}
	tags := make([]string, 0, len(in.Tags))
	seen := map[string]bool{}
	for _, tag := range in.Tags {
		tag = strings.TrimSpace(tag)
		if tag != "" && !seen[tag] {
			tags = append(tags, tag)
			seen[tag] = true
		}
	}
	// S60 MQ15: the create seam carries the same prefix rule as the update
	// seam (the spec named component.update; refusing at BOTH is the only
	// way "refused where it is set" is true — recorded extension).
	if err := action.CheckBranchPrefix(in.BranchPrefix); err != nil {
		return protocol.ComponentView{}, err
	}
	w, err := p.store.CreateComponent(store.NewComponent{Name: in.Name, Slug: in.Slug, Root: in.Root,
		Remote: strings.TrimSpace(in.Remote), RemoteKey: strings.TrimSpace(in.RemoteKey),
		RepoKey: strings.TrimSpace(in.RepoKey), Owner: strings.TrimSpace(in.Owner), Icon: strings.TrimSpace(in.Icon), Tags: tags,
		ParentID: strings.TrimSpace(in.ParentID), ShortDescription: in.ShortDescription, Description: in.Description,
		DefaultBranch: nonEmptyStr(in.DefaultBranch), BranchPrefix: nonEmptyStr(in.BranchPrefix), MaxParallelAgents: nonZeroInt(in.MaxParallelAgents)})
	if err != nil {
		return protocol.ComponentView{}, err
	}
	if !w.OK() {
		return protocol.ComponentView{}, fmt.Errorf("component create refused: %s", w.Reason)
	}
	return p.viewOf(w.Component), nil
}

func (p *ComponentStore) UpdateComponent(in protocol.ComponentUpdateParams) (protocol.ComponentView, error) {
	in.ID, in.Name, in.Slug = strings.TrimSpace(in.ID), strings.TrimSpace(in.Name), strings.TrimSpace(in.Slug)
	if !desk.ValidComponentSlug(in.Slug) {
		return protocol.ComponentView{}, fmt.Errorf("component slug must use lowercase letters, numbers, and single hyphens")
	}
	if in.MaxParallelAgents > 0 {
		if err := validateMaxParallelAgents(in.MaxParallelAgents); err != nil {
			return protocol.ComponentView{}, err
		}
	}
	if err := validateComponentIcon(in.Icon); err != nil {
		return protocol.ComponentView{}, err
	}
	// S60 MQ15: an unsafe branch_prefix is refused WHERE IT IS SET, with
	// the construction rule's own sentence (action.CheckBranchPrefix is
	// the same door NewBranchNamesWith consults — no second spelling of
	// the rule). A junk prefix already stored pins no name and the loops
	// keep today's derivation; a NEW junk prefix never enters.
	if err := action.CheckBranchPrefix(in.BranchPrefix); err != nil {
		return protocol.ComponentView{}, err
	}
	tags := make([]string, 0, len(in.Tags))
	seen := map[string]bool{}
	for _, tag := range in.Tags {
		tag = strings.TrimSpace(tag)
		if tag != "" && !seen[tag] {
			tags = append(tags, tag)
			seen[tag] = true
		}
	}
	w, err := p.store.UpdateComponent(store.ComponentEdit{ID: in.ID, Name: in.Name, Slug: in.Slug,
		Remote: strings.TrimSpace(in.Remote), RemoteKey: strings.TrimSpace(in.RemoteKey),
		RepoKey: strings.TrimSpace(in.RepoKey), Owner: strings.TrimSpace(in.Owner), Icon: strings.TrimSpace(in.Icon), Tags: tags,
		ShortDescription: in.ShortDescription, Description: in.Description,
		DefaultBranch: nonEmptyStr(in.DefaultBranch), BranchPrefix: nonEmptyStr(in.BranchPrefix), MaxParallelAgents: nonZeroInt(in.MaxParallelAgents)})
	if err != nil {
		return protocol.ComponentView{}, err
	}
	if !w.OK() {
		return protocol.ComponentView{}, fmt.Errorf("component update refused: %s", w.Reason)
	}
	return p.viewOf(w.Component), nil
}

func (p *ComponentStore) ListComponents() ([]protocol.ComponentView, error) {
	stored, err := p.store.ListComponents(store.ComponentFilter{})
	if err != nil {
		return nil, err
	}
	out := make([]protocol.ComponentView, 0, len(stored))
	for _, comp := range stored {
		out = append(out, p.viewOf(comp))
	}
	return out, nil
}

func (p *ComponentStore) ShowComponent(id string) (protocol.ComponentView, error) {
	c, err := p.store.ReadComponent(strings.TrimSpace(id))
	if err != nil {
		return protocol.ComponentView{}, err
	}
	return p.viewOf(c), nil
}

func (p *ComponentStore) PromoteComponent(in protocol.ComponentPromoteParams) (protocol.ComponentView, error) {
	w, err := p.store.PromoteComponent(strings.TrimSpace(in.ID), store.PromoteInput{
		Name: strings.TrimSpace(in.Name), ShortDescription: in.ShortDescription,
		Description: in.Description, Icon: strings.TrimSpace(in.Icon)})
	if err != nil {
		return protocol.ComponentView{}, err
	}
	if !w.OK() {
		return protocol.ComponentView{}, fmt.Errorf("component promote refused: %s", w.Reason)
	}
	return p.viewOf(w.Component), nil
}

func (p *ComponentStore) AttachComponent(in protocol.ComponentAttachParams) (protocol.ComponentView, error) {
	w, err := p.store.AttachComponent(strings.TrimSpace(in.ID), strings.TrimSpace(in.ParentID))
	if err != nil {
		return protocol.ComponentView{}, err
	}
	if !w.OK() {
		return protocol.ComponentView{}, fmt.Errorf("component attach refused: %s", w.Reason)
	}
	return p.viewOf(w.Component), nil
}

func (p *ComponentStore) DetachComponent(id string) (protocol.ComponentView, error) {
	w, err := p.store.DetachComponent(strings.TrimSpace(id))
	if err != nil {
		return protocol.ComponentView{}, err
	}
	if !w.OK() {
		return protocol.ComponentView{}, fmt.Errorf("component detach refused: %s", w.Reason)
	}
	return p.viewOf(w.Component), nil
}

func (p *ComponentStore) ArchiveComponent(id string) (protocol.ComponentView, error) {
	w, err := p.store.ArchiveComponent(strings.TrimSpace(id))
	if err != nil {
		return protocol.ComponentView{}, err
	}
	if !w.OK() {
		return protocol.ComponentView{}, fmt.Errorf("component archive refused: %s", w.Reason)
	}
	return p.viewOf(w.Component), nil
}

func (p *ComponentStore) RestoreComponent(id string) (protocol.ComponentView, error) {
	w, err := p.store.RestoreComponent(strings.TrimSpace(id))
	if err != nil {
		return protocol.ComponentView{}, err
	}
	if !w.OK() {
		return protocol.ComponentView{}, fmt.Errorf("component restore refused: %s", w.Reason)
	}
	return p.viewOf(w.Component), nil
}

// nonEmptyStr is the protocol's string -> *string seam: an empty value means
// "inherit" (a child) or "keep" (an update), so it maps to nil; a value sets.
func nonEmptyStr(s string) *string {
	if s == "" {
		return nil
	}
	v := strings.TrimSpace(s)
	return &v
}

// nonZeroInt is the protocol's int -> *int seam: 0 means "inherit" (a child) or
// "keep" (an update), so it maps to nil; a value sets.
func nonZeroInt(n int) *int {
	if n == 0 {
		return nil
	}
	return &n
}
