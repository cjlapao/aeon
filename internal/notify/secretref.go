package notify

import (
	"fmt"
	"os"
	"strings"

	"github.com/cjlapao/aeon/internal/secrets"
)

// secretref.go — S58 T5 (SV7): the secret by reference, end to end.
//
// The rules the file exists to make structural:
//   - the value arrives ONLY as a file path on the daemon host (C17: never
//     a flag value, a param, stdin or a prompt — `--secret-file`);
//   - it is written to the secrets store FIRST and the row carries only
//     `secret:notify/<id>` (the route-key order, daemon/memory.go's
//     argument: a row whose secret failed to save is a lie about the
//     thing it claims to be able to do);
//   - the value never enters a log line, an error string, a result, an
//     audit summary or a row — the holder type below cannot leak it even
//     by `fmt.Stringer` accident (S19 T2's Key shape).

// SecretStore is the seam the channel service writes through. The real
// implementation is the daemon host's credentials directory (same
// mechanism as the cursor key and route keys: 0600 in a 0700, P26); a
// test mounts a map.
type SecretStore interface {
	Put(ref string, value []byte) error
	Get(ref string) ([]byte, error)
	Delete(ref string) error
}

// FileSecrets is the production SecretStore over internal/secrets.
type FileSecrets struct{}

// FileSecrets is the one SecretStore the daemon mounts.
func (FileSecrets) Put(ref string, value []byte) error {
	id, err := channelIDFromRef(ref)
	if err != nil {
		return err
	}
	return secrets.SaveChannelSecret(id, string(value))
}

func (FileSecrets) Get(ref string) ([]byte, error) {
	id, err := channelIDFromRef(ref)
	if err != nil {
		return nil, err
	}
	s, err := secrets.ReadChannelSecret(id)
	if err != nil {
		return nil, err
	}
	return []byte(s), nil
}

func (FileSecrets) Delete(ref string) error {
	id, err := channelIDFromRef(ref)
	if err != nil {
		return err
	}
	_, err = secrets.DeleteChannelSecret(id)
	return err
}

// ChannelSecretRef is the ref shape the store's rows carry.
func ChannelSecretRef(channelID string) string { return secrets.ChannelSecretRef(channelID) }

func channelIDFromRef(ref string) (string, error) {
	const prefix = "secret:notify/"
	if !strings.HasPrefix(ref, prefix) || len(ref) == len(prefix) {
		return "", fmt.Errorf("notify: %q is not a channel secret reference (the shape is secret:notify/<id>)", ref)
	}
	return strings.TrimPrefix(ref, prefix), nil
}

// Secret is a holder for a read secret value. It has no field that
// survives a format print: String and GoString redact, so a `%v` of a
// struct embedding it — in a log line, in a t.Errorf, anywhere — cannot
// carry the value.
type Secret struct{ v []byte }

// Bytes hands the value to the dispatcher, the one consumer allowed to
// hold it, at send time only.
func (s Secret) Bytes() []byte { return s.v }

// Present is what every view answers instead of the value.
func (s Secret) Present() bool { return len(s.v) > 0 }

func (s Secret) String() string { return "[redacted]" }

func (s Secret) GoString() string { return "notify.Secret{[redacted]}" }

// MarshalJSON refuses: the holder has no JSON form, so embedding it in a
// result struct makes "echo the secret" a compile-time shape, not a
// runtime forgetfulness.
func (s Secret) MarshalJSON() ([]byte, error) {
	return nil, fmt.Errorf("notify: a secret is never marshalled")
}

// ReadSecretFile reads the secret from the operator-named path. The file
// is the credential's only permitted carrier, so its mode is checked
// before its contents are trusted: 0600 or stricter, or it is not read
// (the cursor-key rule's spirit — a world-readable secret is already
// public; telling the operator is kinder than copying the exposure).
// ≤ 4 KiB, one trailing newline trimmed, empty refused; the buffer is
// zeroed after Put by the caller (see Service.useSecret).
func ReadSecretFile(path string) (Secret, error) {
	if path == "" {
		return Secret{}, fmt.Errorf("notify: no secret file was named")
	}
	fi, err := os.Stat(path)
	if err != nil {
		return Secret{}, fmt.Errorf("notify: read the secret file: %w", err)
	}
	if !fi.Mode().IsRegular() {
		return Secret{}, fmt.Errorf("notify: %s is not a regular file", path)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return Secret{}, fmt.Errorf("notify: %s is mode %04o — a secret file must be readable by its owner only (chmod go-rwx it), and its contents were not read", path, fi.Mode().Perm())
	}
	if fi.Size() == 0 {
		return Secret{}, fmt.Errorf("notify: %s is empty", path)
	}
	if fi.Size() > 4096 {
		return Secret{}, fmt.Errorf("notify: %s is %d bytes — a secret file is capped at 4096", path, fi.Size())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Secret{}, fmt.Errorf("notify: read the secret file: %w", err)
	}
	v := strings.TrimSuffix(string(raw), "\n")
	if v == "" {
		return Secret{}, fmt.Errorf("notify: %s holds only a newline", path)
	}
	return Secret{v: []byte(v)}, nil
}
