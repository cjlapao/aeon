package daemon

import (
	"context"
	"time"

	"github.com/cjlapao/aeon/internal/blueprint"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/store"
)

// blueprint_doors.go — S48 T11 (BP12, P34). The CLI's OFFLINE door needs the
// daemon's blueprint services over a store it opened itself, and it must not
// own a copy of any of that logic: the exported constructors below hand out
// the very same adapters the daemon wires at boot, so "the same code offline"
// is a type-level fact rather than a promise. Nothing here is a new
// implementation — each is one line over the unexported constructor the
// daemon already uses (blueprint_read.go, blueprint_write.go), and the guard
// TestDirectDoorUsesTheSameWritePath reads this file as the evidence.
//
// The interfaces are the METHOD SETS the CLI's direct door calls, not new
// seams: reads is protocol.BlueprintReadService plus the two grown standing
// doors (List/Show, which the daemon answers through function fields because
// they predate the service), writes is the four human write doors. The
// catalog a full daemon hands lint stays nil on a direct door — the read
// service then states the unavailable, which is what a daemon without the
// tools catalog states too (V13: refuse, never invent an all-clear).

// BlueprintReadDoors is the read half the CLI's direct door speaks: the five
// T7 doors plus the list/show the daemon answers through its read service.
type BlueprintReadDoors interface {
	protocol.BlueprintReadService
	List(p protocol.BlueprintListParams) (protocol.BlueprintListResult, error)
	Show(p protocol.BlueprintShowParams) (protocol.BlueprintShowResult, error)
}

// BlueprintWriteDoors is the write half: the four human doors of BP9, over
// the ONE write path (store.BlueprintPutTx/BlueprintDeleteTx) the daemon's
// own writes go through. The actor arrives as an argument exactly as the
// protocol door hands it — the CLI stamps {cli, cli} and says so; the write
// service never reads an actor off a param.
type BlueprintWriteDoors interface {
	Save(ctx context.Context, actor protocol.Actor, p protocol.BlueprintSaveParams) (protocol.BlueprintSaveResult, error)
	Duplicate(ctx context.Context, actor protocol.Actor, p protocol.BlueprintDuplicateParams) (protocol.BlueprintSaveResult, error)
	Delete(ctx context.Context, actor protocol.Actor, p protocol.BlueprintDeleteParams) (protocol.BlueprintDeleteResult, error)
	Revert(ctx context.Context, actor protocol.Actor, p protocol.BlueprintRevertParams) (protocol.BlueprintSaveResult, error)
}

// NewBlueprintReadDoors hands out the daemon's read adapter over s. The
// shipped-record authority comes with it (newBlueprintReads' embedded
// source), so a diverged SYSTEM row reads diverged offline exactly as it
// does over the socket; the tools catalog does not, and lint says so.
func NewBlueprintReadDoors(s *store.Store) BlueprintReadDoors { return newBlueprintReads(s) }

// NewBlueprintWriteDoors hands out the daemon's ONE write service over s.
func NewBlueprintWriteDoors(s *store.Store) BlueprintWriteDoors { return newBlueprintWrites(s) }

// BlueprintValidateAnswer is the daemon's ONE blueprint.validate answer,
// factored out of the seam closure so the socket door and the offline door
// compute the same report from the same source (BP4: one validator,
// Analyze; BP12: one place decides what the source is). `src` is the
// daemon's own instance over the socket road and a store-backed source over
// the offline one; content wins over name, an unknown name is the named
// refusal the socket carries.
func BlueprintValidateAnswer(src blueprint.Source, p protocol.BlueprintValidateParams) (any, error) {
	raw := []byte(p.Content)
	if p.Content == "" {
		if src == nil {
			return nil, blueprint.NewErrUnknownBlueprint(p.Name)
		}
		got, ok, err := src.Get(p.Name)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, blueprint.NewErrUnknownBlueprint(p.Name)
		}
		raw = got
	}
	return blueprint.Analyze(raw, blueprintValidationOptions(p.Mode)), nil
}

// BlueprintOfflineLint answers lint for a person's own FILE with no daemon,
// no store and no tools catalog. It is the read service's own two answers,
// factored so the CLI cannot invent a third: content that cannot even load is
// refused with the daemon's sentence (lint reports, it does not judge —
// validity belongs to Analyze), and with no catalog the state is the explicit
// UNAVAILABLE with empty advisories, never an all-clear from absence (V13).
func BlueprintOfflineLint(content []byte) (protocol.BlueprintLintResult, error) {
	if _, err := blueprint.Load(content); err != nil {
		return protocol.BlueprintLintResult{}, blueprintRefused("diagnostics", "lint cannot read the blueprint: %v", err)
	}
	return protocol.BlueprintLintResult{
		Advisories:  []protocol.BlueprintAdvisory{},
		EvaluatedAt: time.Now().UTC().Format(time.RFC3339),
		Unavailable: "the tools catalog is not wired — policy advisories are unavailable",
	}, nil
}
