package policyseed

import (
	"bytes"
	"testing"
)

func TestSourceReturnsIndependentRecords(t *testing.T) {
	first, second := Source(), Source()
	want := bytes.Clone(second[0].Content)
	first[0].Content[0] ^= 1
	first[0].Key = "changed"
	third := Source()
	if !bytes.Equal(second[0].Content, want) || !bytes.Equal(third[0].Content, want) || third[0].Key == "changed" {
		t.Fatal("mutating one source result changed another caller's records")
	}
}
