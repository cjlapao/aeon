package config

import "context"



// settings_decl_policy.go — the policy.* keys.
//
// permissions.unmatched decides what a tool call or a shell command with NO matching rule becomes:
// "ask" routes it to a person (the default — a tool nobody wrote a rule for is a question, not a
// wall), "deny" restores the strict mode where nothing unlisted runs. It only covers calls nobody
// ruled on: a deny rule still denies, an allow rule still allows, and an unattended loop, where
// nobody can answer, treats an ask as a deny.
func init() {
	mustDeclare(Setting{
		Key:         "permissions.unmatched",
		Area:        "permissions",
		Type:        TypeEnum,
		Default:     "ask",
		EnumValues:  []string{"ask", "deny"},
		Description: "What happens to a tool call or shell command that no policy rule covers: ask (a person is asked, the default) or deny (nothing unlisted runs). Rules always win over this default.",
	})
}

// SettingGetter is the one read PolicyUnmatchedAsks needs; *Store and the protocol's settings service both have it.
type SettingGetter interface {
	Get(ctx context.Context, key string) (View, error)
}

// PolicyUnmatchedAsks reports whether a tool call or shell command that no rule covers should ASK a person
// (the default) rather than be refused. A missing reader, an unreadable value, or "deny" answers false: the
// strict mode is the fail-closed one, so a setting that cannot be read never widens anything. The caller
// passes an untyped nil, never a nil *Store inside the interface.
func PolicyUnmatchedAsks(ctx context.Context, r SettingGetter) bool {
	if r == nil {
		return false
	}
	v, err := r.Get(ctx, "permissions.unmatched")
	return err == nil && v.Type == TypeEnum && v.Value == "ask"
}
