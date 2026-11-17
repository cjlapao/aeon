package secrets

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Channel secrets, on disk, one file each — S58 T5 (SV7).
//
// This is the route-key mechanism, unchanged: same directory, same
// permissions (0600 in a 0700), same C17 rule (a credential comes from a
// dedicated file path — never a flag, a parameter, the database, or a log).
// A notify channel's row carries only the REFERENCE
// (`secret:notify/<channel-id>`, the same string this package's file is
// named for); the value crosses into the dispatcher at send time and
// nowhere else. T0 read this package's API and recorded it: a per-thing
// accessor here is a three-function addition, which is what this file is.

// ChannelSecretRef is the reference a channel row carries for its secret.
// The ref is derivable from the id and the id from the ref — one mapping,
// nothing to forget.
func ChannelSecretRef(channelID string) string {
	return "secret:notify/" + channelID
}

// ChannelSecretPath is where a channel's secret lives.
func ChannelSecretPath(channelID string) (string, error) {
	if err := validChannelID(channelID); err != nil {
		return "", err
	}
	dir, err := credentialsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "notify-"+channelID+".key"), nil
}

// SaveChannelSecret writes a channel's secret, 0600. An empty value is
// refused rather than silently skipped: for a channel there is no
// "keep the existing secret" reading of an empty input — the read path
// already reports absence honestly.
func SaveChannelSecret(channelID, value string) error {
	if value == "" {
		return fmt.Errorf("the secret for channel %q is empty", channelID)
	}
	path, err := ChannelSecretPath(channelID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create the credentials directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(value+"\n"), 0o600); err != nil {
		return fmt.Errorf("write the secret for channel %q: %w", channelID, err)
	}
	return nil
}

// ReadChannelSecret reads a channel's secret, or "" when there is none —
// absence is a real state (the channel is not sendable; `enable` says so),
// not an error. A file that exists and cannot be read IS an error, for the
// route-key reason: "no secret" and "a secret I cannot reach" send
// somebody to different fixes.
func ReadChannelSecret(channelID string) (string, error) {
	path, err := ChannelSecretPath(channelID)
	if err != nil {
		return "", err
	}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read the secret for channel %q: %w", channelID, err)
	}
	return strings.TrimSpace(string(raw)), nil
}

// DeleteChannelSecret removes a channel's secret, reporting whether one
// existed. Archive keeps the secret on purpose (archive is reversible in
// the operator's head; deletion is not offered) — so this is called only
// by an operator act that replaces the secret.
func DeleteChannelSecret(channelID string) (bool, error) {
	path, err := ChannelSecretPath(channelID)
	if err != nil {
		return false, err
	}
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("remove the secret for channel %q: %w", channelID, err)
	}
	return true, nil
}

// validChannelID is validRouteID's discipline for a channel id: the store
// mints them (`nc-<newID()>`), so they are short, lower-case and path-safe
// by birth; anything else reaching a path join is refused, not sanitised.
func validChannelID(id string) error {
	if id == "" {
		return fmt.Errorf("a channel id is required")
	}
	if len(id) > 128 {
		return fmt.Errorf("the channel id %q is too long to be a file name", id)
	}
	for i, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		case r == '.' && i > 0 && id[i-1] != '.':
		default:
			return fmt.Errorf("the channel id %q contains %q, which a file name may not", id, r)
		}
	}
	if strings.HasPrefix(id, ".") {
		return fmt.Errorf("the channel id %q may not start with a dot", id)
	}
	return nil
}
