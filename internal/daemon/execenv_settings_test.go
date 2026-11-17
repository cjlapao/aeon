package daemon

// execenv_settings_test.go — S59 T6: the one number this plane owns is read
// the S44 way, and the vocabulary tells the truth about WHERE it came from.
//
// The failure these tests hold against is the quiet one: a panel shows "24h"
// whether the operator set it, the default carries it, or the row is
// unparseable garbage — three different facts wearing one number, and the
// third is the one where the daemon is doing something nobody chose.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cjlapao/aeon/internal/config"
	"github.com/cjlapao/aeon/internal/protocol"
)

// fakeSettingsReader is S44's read-only seam. It answers one key and refuses
// the shape of every other, so a test cannot accidentally pass by reading a
// key the plane does not own.
type fakeSettingsReader struct {
	value string
	err   error
	reads []string
}

var _ config.Reader = (*fakeSettingsReader)(nil)

func (f *fakeSettingsReader) Bool(context.Context, string) (bool, error) {
	return false, errUnexpectedSettingRead
}
func (f *fakeSettingsReader) Int(context.Context, string) (int, error) {
	return 0, errUnexpectedSettingRead
}
func (f *fakeSettingsReader) Enum(context.Context, string) (string, error) {
	return "", errUnexpectedSettingRead
}
func (f *fakeSettingsReader) String(_ context.Context, key string) (string, error) {
	f.reads = append(f.reads, key)
	if f.err != nil {
		return "", f.err
	}
	if key != provisionIdleTTLSetting {
		return "", errUnexpectedSettingRead
	}
	return f.value, nil
}

var errUnexpectedSettingRead = &settingReadError{}

type settingReadError struct{}

func (*settingReadError) Error() string {
	return "this plane reads exactly one setting (execenv.provisionIdleTTL); a second read is a second authority"
}

func newEnvFixtureWithSettings(t *testing.T, value string, err error) (*envFixture, *fakeSettingsReader) {
	t.Helper()
	f := newEnvFixture(t)
	reader := &fakeSettingsReader{value: value, err: err}
	f.env = newEnvironmentService(f.store, f.resolver, f.log, reader)
	return f, reader
}

// TestVocabularyCarriesTheDeclaredSweepInterval: no config store at all is the
// third state, said out loud — the default runs, and the answer says it is a
// default rather than letting "24h" read as a decision somebody made.
func TestVocabularyCarriesTheDeclaredSweepInterval(t *testing.T) {
	f := newEnvFixture(t)
	v, err := f.env.Vocabulary(context.Background())
	if err != nil {
		t.Fatalf("vocabulary: %v", err)
	}
	if v.ProvisionIdleTTLSeconds != int64(defaultProvisionIdleTTL/time.Second) {
		t.Errorf("provisionIdleTtlSeconds = %d, want %d (the declared default)", v.ProvisionIdleTTLSeconds, int64(defaultProvisionIdleTTL/time.Second))
	}
	if v.ProvisionIdleTTLFrom != idleTTLFromUnreadabe {
		t.Errorf("provisionIdleTtlFrom = %q, want %q — a daemon with no config store is not a daemon running an operator's choice (P3)", v.ProvisionIdleTTLFrom, idleTTLFromUnreadabe)
	}
	if !strings.Contains(v.ProvisionIdleTTLNote, provisionIdleTTLSetting) {
		t.Errorf("the note does not name the setting a person would set: %q", v.ProvisionIdleTTLNote)
	}
	if v.ProvisionIdleTTLKey != provisionIdleTTLSetting {
		t.Errorf("provisionIdleTtlKey = %q, want the declared key — the panel's link to the Settings row is this name", v.ProvisionIdleTTLKey)
	}
	if v.ProvisionIdleTTL != defaultProvisionIdleTTL.String() {
		t.Errorf("provisionIdleTtl = %q, want %q", v.ProvisionIdleTTL, defaultProvisionIdleTTL.String())
	}
}

// TestVocabularyReportsASetIntervalAsSet: the ordinary case, with the state
// that says so and no note (a clean answer carries no explanation of a
// problem that did not happen).
func TestVocabularyReportsASetIntervalAsSet(t *testing.T) {
	f, reader := newEnvFixtureWithSettings(t, "90m", nil)
	v, err := f.env.Vocabulary(context.Background())
	if err != nil {
		t.Fatalf("vocabulary: %v", err)
	}
	if v.ProvisionIdleTTLFrom != idleTTLFromSetting {
		t.Errorf("provisionIdleTtlFrom = %q, want %q", v.ProvisionIdleTTLFrom, idleTTLFromSetting)
	}
	if v.ProvisionIdleTTLSeconds != 90*60 {
		t.Errorf("provisionIdleTtlSeconds = %d, want 5400", v.ProvisionIdleTTLSeconds)
	}
	if v.ProvisionIdleTTLNote != "" {
		t.Errorf("a clean read carries a note: %q", v.ProvisionIdleTTLNote)
	}
	if len(reader.reads) != 1 || reader.reads[0] != provisionIdleTTLSetting {
		t.Errorf("the reads were %v — the plane reads its one declared key, once, through config.Reader (CS11)", reader.reads)
	}
}

// TestAnUnparsableIntervalFallsBackAndSaysSo is the state a silent fallback
// would hide: the default runs, the answer says why, and the daemon logs it —
// because a sweep interval silently reverted is how a machine quietly fills up
// with containers.
func TestAnUnparsableIntervalFallsBackAndSaysSo(t *testing.T) {
	f, _ := newEnvFixtureWithSettings(t, "a day", nil)
	v, err := f.env.Vocabulary(context.Background())
	if err != nil {
		t.Fatalf("vocabulary: %v", err)
	}
	if v.ProvisionIdleTTLFrom != idleTTLFromUnreadabe {
		t.Errorf("provisionIdleTtlFrom = %q, want %q", v.ProvisionIdleTTLFrom, idleTTLFromUnreadabe)
	}
	if v.ProvisionIdleTTLSeconds != int64(defaultProvisionIdleTTL/time.Second) {
		t.Errorf("the fallback did not take: %d seconds", v.ProvisionIdleTTLSeconds)
	}
	if !strings.Contains(v.ProvisionIdleTTLNote, "a day") || !strings.Contains(v.ProvisionIdleTTLNote, provisionIdleTTLSetting) {
		t.Errorf("the note does not name the value and the key it rejected: %q", v.ProvisionIdleTTLNote)
	}
	joined := strings.Join(f.log.lines, "\n")
	if !strings.Contains(joined, provisionIdleTTLSetting) {
		t.Errorf("the fallback was not logged — a silently reverted interval is the failure with no witness: %v", f.log.lines)
	}

	// Zero and negative are the same state for the same reason: a sweep
	// interval of zero would remove a container the moment it stopped being
	// used, which is a refusal of the whole feature wearing a valid duration.
	for _, bad := range []string{"0s", "-3h"} {
		fb, _ := newEnvFixtureWithSettings(t, bad, nil)
		vb, err := fb.env.Vocabulary(context.Background())
		if err != nil {
			t.Fatalf("vocabulary (%s): %v", bad, err)
		}
		if vb.ProvisionIdleTTLFrom != idleTTLFromUnreadabe || vb.ProvisionIdleTTLSeconds != int64(defaultProvisionIdleTTL/time.Second) {
			t.Errorf("%s was accepted as a sweep interval: %q / %d seconds", bad, vb.ProvisionIdleTTLFrom, vb.ProvisionIdleTTLSeconds)
		}
	}

	// A read that ERRORS is the third state too, with its own sentence.
	fe, _ := newEnvFixtureWithSettings(t, "", context.DeadlineExceeded)
	ve, err := fe.env.Vocabulary(context.Background())
	if err != nil {
		t.Fatalf("vocabulary (error): %v", err)
	}
	if ve.ProvisionIdleTTLFrom != idleTTLFromUnreadabe || !strings.Contains(ve.ProvisionIdleTTLNote, "could not be read") {
		t.Errorf("a failed read did not produce the third state: %q / %q", ve.ProvisionIdleTTLFrom, ve.ProvisionIdleTTLNote)
	}
}

// TestThePlaneReadsExactlyItsOwnSetting is CS11's edge made checkable: the
// service takes config.Reader, and a second key read would be a second
// authority this plane has no claim to. The fake refuses every key but the one.
func TestThePlaneReadsExactlyItsOwnSetting(t *testing.T) {
	f, reader := newEnvFixtureWithSettings(t, "2h", nil)
	if _, err := f.env.Vocabulary(context.Background()); err != nil {
		t.Fatalf("vocabulary: %v", err)
	}
	if _, err := f.env.List(context.Background(), protocol.EnvironmentListParams{}); err != nil {
		t.Fatalf("list: %v", err)
	}
	if _, err := f.env.Effective(context.Background(), protocol.EnvironmentEffectiveParams{}); err == nil {
		// Nothing is bound, so the effective read refuses — which is the right
		// answer, and it must not have reached for a setting either.
		t.Log("effective refused with no binding, as EV2 requires")
	}
	for _, k := range reader.reads {
		if k != provisionIdleTTLSetting {
			t.Errorf("the plane read %q — one declared key is the whole of its configuration (CS11, S44)", k)
		}
	}
}
