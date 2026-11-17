package daemon

// notify_classes.go — S58 T4: the event-class doors (SV4, SV13).
//
// The service behind notify.class.list, notify.class.update and
// notify.vocabulary. It is thin on purpose: reads pass through to the
// store; the update validates the TEMPLATE against the closed placeholder
// set (notify.ValidateTemplate — the SV6 security door, fired before any
// row is touched) and then hands the change to the store's one audited
// write (UpdateNotifyClass: row + version bump + one notify_change row,
// one transaction). The actor arrived validated as a person from the
// protocol handler; identity never re-reads from params here.
//
// There is no create and no delete: kinds are a closed list and rows are
// the shipped set (the seed engine authors rows; SV4). Disabling is the
// Remove right, and an edited system row keeps its origin — the seed
// LEDGER's fingerprint is what records the divergence, so a later re-seed
// skips it (P8: never overwrite an operator).

import (
	"context"
	"fmt"

	"github.com/cjlapao/aeon/internal/attention"
	"github.com/cjlapao/aeon/internal/notify"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// NotifyClassService is the daemon's NotifyClassService. The resolver
// rides along only to share the store wiring — the doors never call it
// (the resolver reads at delivery time, not at the door).
type NotifyClassService struct {
	store    *store.Store
	resolver *notify.Resolver
}

// NotifyClassList reads the whole class table.
func (svc *NotifyClassService) NotifyClassList(ctx context.Context) (protocol.NotifyClassListResult, error) {
	rows, err := svc.store.ListNotifyClasses()
	if err != nil {
		return protocol.NotifyClassListResult{}, err
	}
	out := protocol.NotifyClassListResult{Classes: make([]protocol.NotifyClassItem, 0, len(rows))}
	for _, r := range rows {
		out.Classes = append(out.Classes, classItem(r))
	}
	return out, nil
}

// NotifyClassUpdate applies one person's edit. The template refusals are
// the typed ErrClassTemplate (−31925): the sentence names what the
// template asked for and what the closed set is. An unknown class is a
// bad-params refusal naming the row that is not there — the list shows
// what IS. A no-op (every named field already holds its value) returns
// the row and writes nothing, audit included (LC11).
func (svc *NotifyClassService) NotifyClassUpdate(ctx context.Context, actor protocol.Actor, p protocol.NotifyClassUpdateParams) (protocol.NotifyClassItem, error) {
	_, found, err := svc.store.GetNotifyClass(p.Kind, p.Variant)
	if err != nil {
		return protocol.NotifyClassItem{}, err
	}
	if !found {
		return protocol.NotifyClassItem{}, protocol.ErrBadParams{Err: classNotFound(p.Kind, p.Variant)}
	}
	if p.Title != nil {
		if verr := notify.ValidateTemplate(*p.Title, notify.MaxTitleTemplateBytes); verr != nil {
			return protocol.NotifyClassItem{}, protocol.ErrClassTemplate.WithWhat("%v", verr)
		}
	}
	if p.Body != nil {
		if verr := notify.ValidateTemplate(*p.Body, notify.MaxBodyTemplateBytes); verr != nil {
			return protocol.NotifyClassItem{}, protocol.ErrClassTemplate.WithWhat("%v", verr)
		}
	}
	row, err := svc.store.UpdateNotifyClass(p.Kind, p.Variant, p.Tone, p.Enabled, p.Title, p.Body, actor.Kind, actor.ID)
	if err != nil {
		return protocol.NotifyClassItem{}, err
	}
	return classItem(row), nil
}

// NotifyVocabulary is the plane's descriptor (SV13): every closed list
// and constant, from the code that keeps them. The app renders these and
// holds no copy (SV18).
func (svc *NotifyClassService) NotifyVocabulary(ctx context.Context) (protocol.NotifyVocabularyResult, error) {
	rows, err := svc.store.ListNotifyClasses()
	if err != nil {
		return protocol.NotifyVocabularyResult{}, err
	}
	var kinds []string
	seen := map[string]bool{}
	for _, r := range rows {
		if !seen[r.Kind] {
			seen[r.Kind] = true
			kinds = append(kinds, r.Kind)
		}
	}
	severities := make([]string, 0, 5)
	for _, s := range attention.Severities() {
		severities = append(severities, string(s))
	}
	return protocol.NotifyVocabularyResult{
		Kinds:        kinds,
		Severities:   severities,
		Placeholders: append([]string(nil), notify.Placeholders...),
		Tones:        []string{"info", "success", "warning", "danger"},
		// The dispatcher's closed enums (SV8) — named here before T5
		// builds the sender so the descriptor and the delivery log
		// agree from their first day.
		ErrorKinds:   []string{"transport", "timeout", "status", "refused-address", "secret-missing", "url-invalid"},
		ChannelKinds: []string{"webhook"},
		Limits: protocol.NotifyLimits{
			TitleMaxBytes:     notify.MaxTitleTemplateBytes,
			BodyMaxBytes:      notify.MaxBodyTemplateBytes,
			DedupMinutes:      10,
			DefaultMaxPerHour: 30,
			MaxPerHourFloor:   1,
			MaxPerHourCeiling: 1000,
			SendTimeoutSecs:   10,
			Attempts:          3,
			QueueDepth:        256,
		},
	}, nil
}

func classItem(r store.NotifyClass) protocol.NotifyClassItem {
	return protocol.NotifyClassItem{
		Kind: r.Kind, Variant: r.Variant, Tone: r.Tone,
		TitleTemplate: r.TitleTemplate, BodyTemplate: r.BodyTemplate,
		Enabled: r.Enabled, Origin: r.Origin, Version: r.Version, UpdatedAt: r.UpdatedAt,
	}
}

func classNotFound(kind, variant string) error {
	name := kind
	if variant != "" {
		name = kind + "/" + variant
	}
	return fmt.Errorf("notify.class.update names class %q, which is not in the table — notify.class.list shows the classes (rows arrive from the shipped set, not from this door)", name)
}
