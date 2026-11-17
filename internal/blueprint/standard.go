package blueprint

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/cjlapao/aeon/internal/paths"
)

//go:embed blueprints/*.yaml
var standards embed.FS

// research-map joins at S14: the slice's keystone (a step that asks another
// agent's skill) has to be STARTABLE to be walkable — DEMO §4 runs it, and
// the pin preflight is what makes "not configured" a refusal at loop start
// rather than a hang at the first step.
var standardNames = []string{"two-step", "dev-loop", "development-map-pipeline", "development-map-direct", "proposal-brief", "conversational-map", "tag-only", "research-map", "brainstorm", "goal-to-brief", "chat", "roadmap-plan"}
var blueprintName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

func StandardNames() []string { return append([]string(nil), standardNames...) }

// LegacyNames includes standard sources and user-created copies in the state
// directory. S48 T3 renamed it with the Legacy prefix (BP2): the DATABASE is
// the source of truth, and this file reader survives only for the T4 importer
// and tests — a stray caller fails to compile and is found.
func LegacyNames() ([]string, error) {
	dir, err := paths.StoreDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(filepath.Join(dir, "blueprints"))
	if os.IsNotExist(err) {
		return StandardNames(), nil
	}
	if err != nil {
		return nil, err
	}
	names := StandardNames()
	seen := make(map[string]bool, len(names))
	var custom []string
	for _, name := range names {
		seen[name] = true
	}
	for _, entry := range entries {
		name := strings.TrimSuffix(entry.Name(), ".yaml")
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !strings.HasSuffix(entry.Name(), ".yaml") || !blueprintName.MatchString(name) || seen[name] {
			continue
		}
		custom = append(custom, name)
	}
	sort.Strings(custom)
	return append(names, custom...), nil
}

// LegacyNamedPath resolves a blueprint name inside Aeon's state directory.
// It never accepts a path. Legacy-prefixed by S48 T3 — importer + tests only.
func LegacyNamedPath(name string) (string, error) {
	if !blueprintName.MatchString(name) {
		return "", fmt.Errorf("invalid blueprint name %q", name)
	}
	if contains(standardNames, name) {
		return LegacyResolve(name)
	}
	dir, err := paths.StoreDir()
	if err != nil {
		return "", err
	}
	file := filepath.Join(dir, "blueprints", name+".yaml")
	if info, err := os.Lstat(file); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("blueprint %q is a symlink", name)
	} else if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	return file, nil
}

// LegacyResolve returns an editable path for a standard blueprint,
// materializing the embedded source on first use. Only known pristine older
// copies are upgraded; a person's edited source remains theirs. An explicit
// path is used as-is. Legacy-prefixed by S48 T3 — the importer, the CLI
// `path` verb (retired at T11) and tests; production readers use the Source.
func LegacyResolve(name string) (string, error) {
	if strings.ContainsAny(name, "/\\") || strings.HasSuffix(name, ".yaml") && filepath.IsAbs(name) {
		return filepath.Abs(name)
	}
	if !contains(standardNames, name) {
		return LegacyNamedPath(name)
	}
	dir, err := paths.StoreDir()
	if err != nil {
		return "", err
	}
	file := filepath.Join(dir, "blueprints", name+".yaml")
	if _, err := os.Stat(file); err == nil {
		if err := upgradePristineStandard(name, file); err != nil {
			return "", err
		}
		return file, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	raw, err := standards.ReadFile("blueprints/" + name + ".yaml")
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		return "", err
	}
	f, err := os.OpenFile(file, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if os.IsExist(err) {
		return file, nil
	}
	if err != nil {
		return "", err
	}
	if _, err = f.Write(raw); err != nil {
		f.Close()
		return "", err
	}
	if err = f.Close(); err != nil {
		return "", err
	}
	return file, nil
}

// The first materialized two-step source offered revise but had no reject
// edge. A loop pinned from it cannot rework. Match its exact bytes so custom
// blueprints are never silently replaced by a future embedded revision.
const legacyTwoStepWithoutReworkSHA256 = "ab10e66980f167b64001bbc4bf95628ac22c4ea0d7860eefc121257907ec079e"
const legacyTwoStepWithReworkSHA256 = "4f73e0eef6f4218b28fa78c2cb7732010e0a52ae592c15580b4551e23d491554"

// The 2026-10-26 investigation: a box ran `make dev-daemon` — go run, so a
// freshly compiled daemon — and the app still reported the retired
// forge.closeOpenIssuesAndPRs as `unknown action`. The daemon was right and
// the file it reads was stale: Resolve prefers the state-directory copy, and
// with the hatch below knowing only two-step, a pristine copy materialized
// from an older embedded revision could never catch up. Each entry is the
// sha256 of a committed embedded revision (git show <rev>:… | sha256sum),
// never a guess: only bytes a release actually shipped are recognised as
// pristine, so a person's edit — even one descended from a shipped
// revision — is never replaced.
// ValidBlueprintName is the file/row name grammar exported for the
// adoption walk (S48 T4) — the one grammar the writers also enforce, plus
// the schema's 64-char bound.
func ValidBlueprintName(name string) bool {
	return len(name) >= 1 && len(name) <= 64 && blueprintName.MatchString(name)
}

// PristineHashes lists the recognised pristine hashes for a standard name
// — adoption's classifier reads the list here (S48 T4) because the one
// truth about what shipped stays in this file; T5 retires the accessor
// when the seed set owns the hashes.
func PristineHashes(name string) []string {
	return append([]string(nil), pristineUpgrades[name]...)
}

var pristineUpgrades = map[string][]string{
	"two-step": {legacyTwoStepWithoutReworkSHA256, legacyTwoStepWithReworkSHA256},
	"dev-loop": {"8af923d7a96748ed61dbaf9147138753da8ce8076174ffce4ec00143fc88e6ed"},
}

func upgradePristineStandard(name, file string) error {
	pristine, known := pristineUpgrades[name]
	if !known {
		return nil
	}
	current, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(current))
	recognised := false
	for _, want := range pristine {
		if digest == want {
			recognised = true
			break
		}
	}
	if !recognised {
		return nil
	}
	updated, err := standards.ReadFile("blueprints/" + name + ".yaml")
	if err != nil {
		return err
	}
	if bytes.Equal(current, updated) {
		return nil
	}
	tmp, err := os.CreateTemp(filepath.Dir(file), ".blueprint-upgrade-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(updated); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), file)
}
