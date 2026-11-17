// Package vault is the variable manager: named values the app holds, handed to
// processes as environment variables and to agents through a tool — under one
// rule that does not bend. An agent never sees a variable unless a person marked
// it agent-visible, and nothing is agent-visible by default (a secret never is;
// a plain variable is only when the person leaves the box it arrives with).
//
// # What is stored, and where the key is
//
// Every value is AES-256-GCM ciphertext in the database; the 32-byte key is a
// file beside the store (paths.VaultKeyFile, mode 0600) that is in no backup of
// the database. The ciphertext is bound to its name and scope as additional
// data, so a row copied under another name does not decrypt. The store holds
// ciphertext and flags and never owns a key (store/env_var.go).
//
// # Who can read a value
//
//   - the daemon, to build a process environment: Resolve (named references only —
//     nothing is injected that a server or an environment did not ask for);
//   - a person, never: the UI and CLI read the LIST, which carries a secret's
//     name, flags and description but not its value;
//   - an agent: AgentGet, only for an agent-visible variable, and every such read
//     is a ledger row naming the agent.
//
// Resolution order is the project's own value (a ROOT component's override) and
// then the global one.
package vault

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cjlapao/aeon/internal/store"
)

const (
	keyBytes = 32
	// MaxValueBytes bounds one value: a token, a certificate, a connection string —
	// not a file store.
	MaxValueBytes  = 64 << 10
	maxDescription = 512
)

var nameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

// reservedNames are variables a process must never get from a person's list: the
// ones that change what the process IS, not what it knows.
var reservedNames = map[string]bool{
	"PATH": true, "HOME": true, "SHELL": true, "IFS": true, "PWD": true, "USER": true,
	"LD_PRELOAD": true, "LD_LIBRARY_PATH": true, "LD_AUDIT": true,
	"DYLD_INSERT_LIBRARIES": true, "DYLD_LIBRARY_PATH": true,
	"NODE_OPTIONS": true, "PYTHONPATH": true, "PYTHONSTARTUP": true, "BASH_ENV": true, "ENV": true,
}

// Variable is what a read returns. A secret's Value is ALWAYS empty here: the
// list tells you it exists and who may see it, never what it says.
type Variable struct {
	Name         string `json:"name"`
	Project      string `json:"project,omitempty"` // root component id; "" = global
	Description  string `json:"description,omitempty"`
	Secret       bool   `json:"secret"`
	AgentVisible bool   `json:"agentVisible"`
	Value        string `json:"value,omitempty"` // plain variables only
	UpdatedAt    string `json:"updatedAt"`
}

// SetInput is one set. Nil pointers keep what is stored (or take the default on a
// new variable), so a flags-only edit never has to re-enter the value.
type SetInput struct {
	Name         string
	Project      string
	Description  *string
	Value        *string
	Secret       *bool
	AgentVisible *bool
}

// Vault is the manager over a store and a key.
type Vault struct {
	st   *store.Store
	aead cipher.AEAD
}

// Open loads the key file, creating it (0600, in a 0700 directory) on first use.
// An existing file that cannot be read, or is the wrong size, is an error and is
// never replaced: minting a new key would silently orphan every stored value.
func Open(st *store.Store, keyPath string) (*Vault, error) {
	key, err := loadOrCreateKey(keyPath)
	if err != nil {
		return nil, err
	}
	return OpenWithKey(st, key)
}

// OpenWithKey builds a vault over a caller-held 32-byte key (tests).
func OpenWithKey(st *store.Store, key []byte) (*Vault, error) {
	if len(key) != keyBytes {
		return nil, fmt.Errorf("vault: the key must be %d bytes, got %d", keyBytes, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("vault: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("vault: %w", err)
	}
	return &Vault{st: st, aead: aead}, nil
}

func loadOrCreateKey(path string) ([]byte, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // our own path
	if err == nil {
		if len(raw) != keyBytes {
			return nil, fmt.Errorf("vault: key file %s is %d bytes, want %d — it is not replaced, because a new key would orphan every stored value", path, len(raw), keyBytes)
		}
		return raw, nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("vault: read key file %s: %w", path, err)
	}
	key := make([]byte, keyBytes)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("vault: generate key: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("vault: create %s: %w", filepath.Dir(path), err)
	}
	// O_EXCL: two starts racing must not each write a different key.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return loadOrCreateKey(path)
		}
		return nil, fmt.Errorf("vault: write key file %s: %w", path, err)
	}
	defer f.Close()
	if _, err := f.Write(key); err != nil {
		return nil, fmt.Errorf("vault: write key file %s: %w", path, err)
	}
	return key, nil
}

func aad(name, project string) []byte { return []byte(name + "\x00" + project) }

func (v *Vault) seal(name, project, value string) ([]byte, error) {
	nonce := make([]byte, v.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("vault: nonce: %w", err)
	}
	return v.aead.Seal(nonce, nonce, []byte(value), aad(name, project)), nil
}

func (v *Vault) open(name, project string, cipherText []byte) (string, error) {
	n := v.aead.NonceSize()
	if len(cipherText) < n {
		return "", fmt.Errorf("vault: stored value of %s is too short to be ciphertext", name)
	}
	plain, err := v.aead.Open(nil, cipherText[:n], cipherText[n:], aad(name, project))
	if err != nil {
		return "", fmt.Errorf("vault: stored value of %s does not decrypt — the key file is not the one that sealed it", name)
	}
	return string(plain), nil
}

// ValidateName is the grammar and the reserved list, as the sentence a person reads.
func ValidateName(name string) error {
	switch {
	case !nameRE.MatchString(name):
		return fmt.Errorf("refused: %q is not a variable name — letters, digits and _, not starting with a digit, at most 128", name)
	case strings.HasPrefix(strings.ToUpper(name), "AEON_"):
		return fmt.Errorf("refused: %s is in Aeon's own AEON_ namespace — those names are the daemon's, not application variables", name)
	case reservedNames[strings.ToUpper(name)]:
		return fmt.Errorf("refused: %s changes what a process IS rather than what it knows — a stored value would let a variable redirect or preload code, so the name is reserved", name)
	}
	return nil
}

// Set creates or updates one variable.
func (v *Vault) Set(ctx context.Context, in SetInput, actor store.Actor) (Variable, error) {
	if err := ValidateName(in.Name); err != nil {
		return Variable{}, err
	}
	if in.Value != nil {
		if *in.Value == "" || strings.ContainsRune(*in.Value, 0) {
			return Variable{}, fmt.Errorf("refused: a value is non-empty text with no NUL byte")
		}
		if len(*in.Value) > MaxValueBytes {
			return Variable{}, fmt.Errorf("refused: %s is %d bytes; a variable holds at most %d", in.Name, len(*in.Value), MaxValueBytes)
		}
	}
	existing, found, err := v.st.EnvVarGet(in.Name, in.Project)
	if err != nil {
		return Variable{}, err
	}
	if in.Project != "" {
		if root, rerr := v.st.RootOf(in.Project); rerr != nil || root != in.Project {
			return Variable{}, fmt.Errorf("refused: a project override hangs on a ROOT component, and %q is not one", in.Project)
		}
	}

	row := store.EnvVarRow{Name: in.Name, ProjectID: in.Project}
	if found {
		row.Description, row.Secret, row.AgentVisible = existing.Description, existing.Secret, existing.AgentVisible
	} else {
		row.Secret = true // a new variable is a secret until a person says otherwise
	}
	if in.Description != nil {
		if len(*in.Description) > maxDescription {
			return Variable{}, fmt.Errorf("refused: a description is at most %d bytes", maxDescription)
		}
		row.Description = *in.Description
	}
	if in.Secret != nil {
		row.Secret = *in.Secret
	}
	switch {
	case in.AgentVisible != nil:
		row.AgentVisible = *in.AgentVisible
	case !found:
		// What a person does not say stays closed for a secret. A plain
		// variable arrives open — it is the one the person said is not secret.
		row.AgentVisible = !row.Secret
	}
	if in.Value != nil {
		if row.Cipher, err = v.seal(in.Name, in.Project, *in.Value); err != nil {
			return Variable{}, err
		}
	}
	stored, err := v.st.EnvVarUpsert(ctx, row, actor, summaryOf(in.Name, found, row))
	if err != nil {
		return Variable{}, err
	}
	return v.view(stored)
}

func summaryOf(name string, existed bool, r store.EnvVarRow) string {
	verb := "created"
	if existed {
		verb = "updated"
	}
	return fmt.Sprintf("%s variable %s (secret=%t, agentVisible=%t)", verb, name, r.Secret, r.AgentVisible)
}

func (v *Vault) view(r store.EnvVarRow) (Variable, error) {
	out := Variable{Name: r.Name, Project: r.ProjectID, Description: r.Description,
		Secret: r.Secret, AgentVisible: r.AgentVisible, UpdatedAt: r.UpdatedAt}
	if !r.Secret {
		val, err := v.open(r.Name, r.ProjectID, r.Cipher)
		if err != nil {
			return Variable{}, err
		}
		out.Value = val
	}
	return out, nil
}

// List is every variable, secrets without their values.
func (v *Vault) List(ctx context.Context) ([]Variable, error) {
	rows, err := v.st.EnvVarList()
	if err != nil {
		return nil, err
	}
	out := make([]Variable, 0, len(rows))
	for _, r := range rows {
		view, err := v.view(r)
		if err != nil {
			return nil, err
		}
		out = append(out, view)
	}
	return out, nil
}

// Delete removes one variable in one scope.
func (v *Vault) Delete(ctx context.Context, name, project string, actor store.Actor) (bool, error) {
	return v.st.EnvVarDelete(ctx, name, project, actor)
}

// effective picks the row that applies to work in a root: the root's own
// override, else the global one.
func (v *Vault) effective(name, root string) (store.EnvVarRow, bool, error) {
	if root != "" {
		if r, ok, err := v.st.EnvVarGet(name, root); err != nil || ok {
			return r, ok, err
		}
	}
	return v.st.EnvVarGet(name, "")
}

// Resolve answers the value a process named `name` gets when it runs in `root`
// (a root component id, or "" for global). It is the daemon's door and nobody
// else's: the callers are the places that were configured to ask by name.
func (v *Vault) Resolve(name, root string) (string, bool) {
	r, ok, err := v.effective(name, root)
	if err != nil || !ok {
		return "", false
	}
	val, err := v.open(r.Name, r.ProjectID, r.Cipher)
	if err != nil {
		return "", false
	}
	return val, true
}

// AgentVariable is what an agent is told exists: a name, why, and whether it is a
// secret — never a value in a listing.
type AgentVariable struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Secret      bool   `json:"secret"`
}

// AgentList is the agent-visible variables in force for work in `root`, each once.
func (v *Vault) AgentList(root string) ([]AgentVariable, error) {
	rows, err := v.st.EnvVarList()
	if err != nil {
		return nil, err
	}
	best := map[string]store.EnvVarRow{}
	for _, r := range rows {
		if r.ProjectID != "" && r.ProjectID != root {
			continue
		}
		if cur, ok := best[r.Name]; !ok || (cur.ProjectID == "" && r.ProjectID != "") {
			best[r.Name] = r
		}
	}
	var out []AgentVariable
	for _, r := range best {
		if r.AgentVisible { // judged on the row that APPLIES: an override that hides it hides it
			out = append(out, AgentVariable{Name: r.Name, Description: r.Description, Secret: r.Secret})
		}
	}
	sortAgentVars(out)
	return out, nil
}

// AgentGet returns a value to an agent — only for an agent-visible variable — and
// writes the ledger row that says which agent read it. Everything else answers the
// same sentence, so "hidden" and "not there" cannot be told apart.
func (v *Vault) AgentGet(ctx context.Context, name, root string, actor store.Actor) (string, string) {
	notVisible := fmt.Sprintf("no variable named %s is visible to agents", name)
	r, ok, err := v.effective(name, root)
	if err != nil {
		return "", "the variable store could not be read"
	}
	if !ok || !r.AgentVisible {
		return "", notVisible
	}
	val, err := v.open(r.Name, r.ProjectID, r.Cipher)
	if err != nil {
		return "", "the variable could not be decrypted"
	}
	if err := v.st.EnvVarRecordRead(ctx, r.Name, r.ProjectID, actor); err != nil {
		return "", "the read could not be recorded, so it was refused"
	}
	return val, ""
}

func sortAgentVars(v []AgentVariable) {
	for i := 1; i < len(v); i++ {
		for j := i; j > 0 && v[j].Name < v[j-1].Name; j-- {
			v[j], v[j-1] = v[j-1], v[j]
		}
	}
}

// RootFor answers the root component (the project) a workspace belongs to, or ""
// when the workspace is not a registered component — in which case only global
// variables apply. Callers hold a working directory, never a component id.
func RootFor(st *store.Store, workspaceRoot string) string {
	if st == nil || workspaceRoot == "" {
		return ""
	}
	c, err := st.ComponentForRoot(workspaceRoot)
	if err != nil {
		return ""
	}
	root, err := st.RootOf(c.ID)
	if err != nil {
		return ""
	}
	return root
}

// Has reports whether a variable of this name exists in any scope — enough for a
// binding to be honest about what it points at.
func (v *Vault) Has(name string) bool {
	rows, err := v.st.EnvVarList()
	if err != nil {
		return false
	}
	for _, r := range rows {
		if r.Name == name {
			return true
		}
	}
	return false
}
