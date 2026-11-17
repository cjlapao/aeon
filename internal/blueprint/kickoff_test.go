package blueprint

import "testing"

// kickoff_test.go pins the renderer (S39 T7): substitution works, absence is
// honest (empty in, empty out; absent Params key is a blank), the output is
// deterministic (a map may not leak its order into a first message), and an
// unknown field is an ERROR — the typo'd template must fail loudly at render
// even though validation was supposed to catch it, because StartRun can be
// reached with a pinned template older than any gate.

func TestRenderKickoffSubstitutes(t *testing.T) {
	got, err := RenderKickoff("Work on {{.Goal}} in {{.Project}}", KickoffData{Goal: "a", Project: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "Work on a in p" {
		t.Fatalf("got %q", got)
	}
}

func TestRenderKickoffEmptySourceIsEmpty(t *testing.T) {
	for _, src := range []string{"", "  \n"} {
		got, err := RenderKickoff(src, KickoffData{Goal: "ignored"})
		if err != nil || got != "" {
			t.Fatalf("source %q rendered %q (err %v), want empty and nil", src, got, err)
		}
	}
}

func TestRenderKickoffAbsentParamRendersEmpty(t *testing.T) {
	got, err := RenderKickoff("ticket {{.Params.ticket}} done", KickoffData{Goal: "g"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "ticket  done" {
		t.Fatalf("missingkey=zero lost: %q", got)
	}
}

func TestRenderKickoffIsDeterministic(t *testing.T) {
	d := KickoffData{Params: map[string]string{"a": "1", "b": "2", "c": "3"}}
	first, err := RenderKickoff("{{range $k, $v := .Params}}{{$k}}={{$v}};{{end}}", d)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		got, err := RenderKickoff("{{range $k, $v := .Params}}{{$k}}={{$v}};{{end}}", d)
		if err != nil || got != first {
			t.Fatalf("render %d differs (map order leaking into a first message?): %q vs %q (err %v)", i, got, first, err)
		}
	}
}

func TestRenderKickoffUnknownFieldErrors(t *testing.T) {
	if _, err := RenderKickoff("{{.Nope}}", KickoffData{}); err == nil {
		t.Fatal("a template naming a field that does not exist must error at render, not render <no value> silently")
	}
}
