package protocol

import (
	"context"
	"encoding/json"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/cjlapao/aeon/internal/records"
)

// mission_test.go — S58 T6: the two claims the protocol layer owns about
// `mission.status` and the supervision tools.

// TestMissionHasNoCompositeScore walks the RESULT types by reflection:
// nothing in the overview may be a health score, a rating, or a green mark
// (SV11 — the rule exists because every dashboard eventually invents one).
// If a field named like a judgment appears, this test fails and the
// judgment has to go back to being the facts it was glued from.
func TestMissionHasNoCompositeScore(t *testing.T) {
	judgment := regexp.MustCompile(`(?i)score|health|rating|grade`)
	var walk func(reflect.Type, string)
	walk = func(typ reflect.Type, path string) {
		for typ.Kind() == reflect.Ptr || typ.Kind() == reflect.Slice || typ.Kind() == reflect.Map {
			typ = typ.Elem()
		}
		if typ.Kind() != reflect.Struct || path == "time.Time" {
			return
		}
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			name := path + "." + f.Name
			if judgment.MatchString(f.Name) {
				t.Errorf("%s — a composite judgment in the overview (SV11 forbids it; the surface renders facts)", name)
			}
			walk(f.Type, name)
		}
	}
	walk(reflect.TypeOf(MissionStatusResult{}), "MissionStatusResult")
	walk(reflect.TypeOf(MissionComponentStatus{}), "MissionComponentStatus")
	// And the JSON surface, tag for tag, for the record.
	b, err := json.Marshal(MissionStatusResult{})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, bad := range []string{"score", "health", "rating"} {
		if strings.Contains(strings.ToLower(string(b)), bad) {
			t.Errorf("the wire carries %q", bad)
		}
	}
}

// — the supervision family's parity (SV16) —

type supervInboxStub struct{}

func (supervInboxStub) InboxList(context.Context, InboxListParams) (InboxListResult, error) {
	return InboxListResult{}, nil
}
func (supervInboxStub) InboxSummary(context.Context, InboxSummaryParams) (InboxSummaryResult, error) {
	return InboxSummaryResult{}, nil
}

type supervMissionStub struct{}

func (supervMissionStub) MissionStatus(context.Context, MissionStatusParams) (MissionStatusResult, error) {
	return MissionStatusResult{}, nil
}

type supervClassStub struct{}

func (supervClassStub) NotifyClassList(context.Context) (NotifyClassListResult, error) {
	return NotifyClassListResult{}, nil
}
func (supervClassStub) NotifyClassUpdate(context.Context, Actor, NotifyClassUpdateParams) (NotifyClassItem, error) {
	return NotifyClassItem{}, nil
}
func (supervClassStub) NotifyVocabulary(context.Context) (NotifyVocabularyResult, error) {
	return NotifyVocabularyResult{}, nil
}

type supervChannelStub struct{}

func (supervChannelStub) NotifyChannelCreate(context.Context, Actor, NotifyChannelCreateParams) (NotifyChannelView, error) {
	return NotifyChannelView{}, nil
}
func (supervChannelStub) NotifyChannelUpdate(context.Context, Actor, NotifyChannelUpdateParams) (NotifyChannelView, error) {
	return NotifyChannelView{}, nil
}
func (supervChannelStub) NotifyChannelEnable(context.Context, Actor, string) (NotifyChannelView, error) {
	return NotifyChannelView{}, nil
}
func (supervChannelStub) NotifyChannelDisable(context.Context, Actor, string) (NotifyChannelView, error) {
	return NotifyChannelView{}, nil
}
func (supervChannelStub) NotifyChannelArchive(context.Context, Actor, NotifyChannelArchiveParams) (NotifyChannelView, error) {
	return NotifyChannelView{}, nil
}
func (supervChannelStub) NotifyChannelTest(context.Context, Actor, string) (NotifyTestResult, error) {
	return NotifyTestResult{}, nil
}
func (supervChannelStub) NotifySubscriptionSet(context.Context, Actor, NotifySubscriptionSetParams) (NotifySubscriptionView, error) {
	return NotifySubscriptionView{}, nil
}
func (supervChannelStub) NotifyChannelList(context.Context) (NotifyChannelListResult, error) {
	return NotifyChannelListResult{}, nil
}
func (supervChannelStub) NotifyChannelShow(context.Context, string) (NotifyChannelView, error) {
	return NotifyChannelView{}, nil
}
func (supervChannelStub) NotifyDeliveryList(context.Context, NotifyDeliveryListParams) (NotifyDeliveryListResult, error) {
	return NotifyDeliveryListResult{}, nil
}

// camel turns a snake_case tool input key into its wire tag ("root_id" →
// "rootId") — the one translation the daemon's supervision door performs.
func camel(s string) string {
	parts := strings.Split(s, "_")
	for i := 1; i < len(parts); i++ {
		if parts[i] != "" {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	return strings.Join(parts, "")
}

// TestRecordsParitySupervisionFamily checks the twin relation both ways
// for the five supervision specs: every spec's method is a REAL registered
// door (registered by the plane's own registrar over its seam, never by
// the records projection — the records.go skip-list owns that), every spec
// is a safe read, no write door is twinned to a tool, and every declared
// input property exists on the method's wire params (and vice versa for
// the ones the agent is allowed to send).
func TestRecordsParitySupervisionFamily(t *testing.T) {
	tbl := newTestTable(t)
	tbl.Inbox = supervInboxStub{}
	if !tbl.RegisterInbox() {
		t.Fatal("RegisterInbox refused a wired seam")
	}
	tbl.Mission = supervMissionStub{}
	if !tbl.RegisterMission() {
		t.Fatal("RegisterMission refused a wired seam")
	}
	tbl.NotifyClass = supervClassStub{}
	if !tbl.RegisterNotifyClasses() {
		t.Fatal("RegisterNotifyClasses refused a wired seam")
	}
	tbl.NotifyChannel = supervChannelStub{}
	if !tbl.RegisterNotifyChannels() {
		t.Fatal("RegisterNotifyChannels refused a wired seam")
	}
	registered := map[MethodName]bool{}
	for _, m := range tbl.Methods() {
		registered[m] = true
	}
	// No write of this plane — a person's act, never a tool (SV12/SV7).
	forbiddenTwins := map[string]bool{
		string(MethodInboxAck):              true,
		string(MethodInboxSnooze):           true,
		string(MethodInboxUnack):            true,
		string(MethodNotifyClassUpdate):     true,
		string(MethodNotifyChannelCreate):   true,
		string(MethodNotifyChannelUpdate):   true,
		string(MethodNotifyChannelEnable):   true,
		string(MethodNotifyChannelDisable):  true,
		string(MethodNotifyChannelArchive):  true,
		string(MethodNotifyChannelTest):     true,
		string(MethodNotifySubscriptionSet): true,
	}

	// declared properties per spec, read from the spec's own schema.
	props := func(sp records.Spec) []string {
		var shape struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal(sp.Schema, &shape); err != nil {
			t.Fatalf("%s schema: %v", sp.Name, err)
		}
		out := make([]string, 0, len(shape.Properties))
		for k := range shape.Properties {
			out = append(out, k)
		}
		return out
	}
	// the wire params each method accepts (json tag names), for the specs
	// that take a scope or a filter; the two notify lists take nothing.
	tagNames := func(v any) map[string]bool {
		out := map[string]bool{}
		typ := reflect.TypeOf(v)
		for i := 0; i < typ.NumField(); i++ {
			tag := typ.Field(i).Tag.Get("json")
			name := strings.Split(tag, ",")[0]
			if name != "" && name != "-" {
				out[name] = true
			}
		}
		return out
	}
	paramsFor := map[string]map[string]bool{
		"inbox.list":          tagNames(InboxListParams{}),
		"inbox.summary":       tagNames(InboxSummaryParams{}),
		"mission.status":      tagNames(MissionStatusParams{}),
		"notify.class.list":   map[string]bool{},
		"notify.channel.list": map[string]bool{},
	}

	wantNames := []string{"inbox_list", "inbox_summary", "mission_status", "notify_class_list", "notify_channel_list"}
	seen := []string{}
	catalog := records.Catalog()
	for i := range catalog {
		sp := catalog[i]
		if sp.Family != "supervision" {
			continue
		}
		seen = append(seen, sp.Name)
		if sp.Mutates {
			t.Errorf("%s mutates — the supervision family ships READS only (SV16)", sp.Name)
		}
		if sp.Class != "safe" {
			t.Errorf("%s is class %q, want safe — a supervision read is a re-read (N8)", sp.Name, sp.Class)
		}
		if !registered[MethodName(sp.Method)] {
			t.Errorf("%s names twin %q, which the wired table does not register — the doors drifted", sp.Name, sp.Method)
		}
		if forbiddenTwins[sp.Method] {
			t.Errorf("%s is twinned to %q — a human-door write reached the catalog (SV12)", sp.Name, sp.Method)
		}
		for _, p := range props(sp) {
			// The tool layer speaks snake_case and the wire speaks camel:
			// daemon/records_supervision.go translates, so parity means the
			// CAMEL form of the declared property is what the door accepts.
			if !paramsFor[sp.Method][camel(p)] {
				t.Errorf("%s declares input property %q, which its method's params do not accept — a property that is silently ignored is a lie the schema tells", sp.Name, p)
			}
		}
	}
	if !reflect.DeepEqual(seen, wantNames) {
		t.Fatalf("the supervision family holds %v, want exactly %v in catalog order", seen, wantNames)
	}
	// The other direction for the five DOORS themselves: a supervision door
	// is answered by the plane's service, not by the records projection —
	// a projection answer would scope by nobody's root. Probed by shape:
	// the projection skips the family, so a table built WITHOUT the plane
	// seams must find none of the five names.
	bare := newTestTable(t)
	have := map[MethodName]bool{}
	for _, m := range bare.Methods() {
		have[m] = true
	}
	for _, name := range []MethodName{MethodInboxList, MethodInboxSummary, MethodMissionStatus, MethodNotifyClassList, MethodNotifyChannelList} {
		if have[name] {
			t.Errorf("%s stands on an unwired build — the plane's registrar owns the name (V13), and the records projection must not claim it", name)
		}
	}
}
