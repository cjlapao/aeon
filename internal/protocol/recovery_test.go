package protocol

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestRequestDuringRecovery(t *testing.T) {
	table := NewTable(NewSessions(), NewSubscriptions(), NewEventLog())
	table.Memory = nilMemory{}
	table.Recovering = func(id int64) bool { return id == 7 }
	for _, tc := range []struct {
		name string
		run  func() (any, error)
	}{
		{"step.show", func() (any, error) {
			return table.handleStepShow(context.Background(), nil, json.RawMessage(`{"id":7}`))
		}},
		{"answer", func() (any, error) {
			return table.handleAnswer(context.Background(), nil, json.RawMessage(`{"stepId":7,"answer":"yes"}`))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.run()
			var got *Error
			if !errors.As(err, &got) || got.Code != ErrStepRecovering.Code || got.Data.What != "step 7 is recovering" {
				t.Fatalf("error=%v", err)
			}
		})
	}
}
