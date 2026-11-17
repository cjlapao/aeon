package agentmgr

import (
	"strings"
	"testing"
)

func TestCheckTechNormalizesAndRefusesUnknownTags(t *testing.T) {
	out, err := checkTech(`{"tech":["TypeScript","react","react"],"x":1}`)
	if err != nil || !strings.Contains(out, `"tech":["react","typescript"]`) || !strings.Contains(out, `"x":1`) {
		t.Fatalf("normalized blob = %q, %v", out, err)
	}
	if _, err := checkTech(`{"tech":["go","cobol"]}`); err == nil || !strings.Contains(err.Error(), `"cobol"`) || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("an unknown tag must be refused by name: %v", err)
	}
	for _, untouched := range []string{"", "{}", `{"mode":"x"}`} {
		if got, err := checkTech(untouched); err != nil || got != untouched {
			t.Fatalf("%q must pass untouched: %q, %v", untouched, got, err)
		}
	}
}
