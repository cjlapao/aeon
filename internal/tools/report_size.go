package tools

// report_size is S24 T5's one native tool: the sizing run's ONLY way to
// report, and a KNOWN SHAPE rather than prose the caller must parse
// (D9 — "reports through one native tool, called once"). One call, one
// shape: a RANGE with its BASIS — a bare number is refused at the tool
// (D8), and a second call in the same run is refused at the tool, not
// left to the handler's prompt as a polite request.
//
// The "same run" boundary is the toolset build, not a global: like
// `todo`, the closure is minted fresh per step by the registry (toolset
// per step, measured pattern), so a second step's sizing attempt —
// legitimately, after a failed run reverted the item to pending — gets a
// fresh closure without the first run's refusal sticking to it.
//
// The tool is a REGISTERED tool and a NON-GRANT (S11's rule): it enters
// `Registry()` and nobody's `DefaultGrant()`; only a handler body that
// names it — the sizing-estimator — ever holds it. It touches no store,
// imports nothing queue-shaped (CG5's structural exemption: there is no
// path from this file to appending a queue item), and reports into the
// step's HANDOFF, which is where the queue's own wiring reads it.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cjlapao/aeon/internal/model"
)

// SizeReport is the tool's one shape (D8): min, max, basis — all three,
// always. Exported because the queue's wiring validates the handoff
// against the same struct: two definitions of the contract would be two
// truths.
type SizeReport struct {
	SizeMin   int    `json:"sizeMin"`
	SizeMax   int    `json:"sizeMax"`
	SizeBasis string `json:"sizeBasis"`
}

func ReportSize() Tool {
	reported := false // per toolset build = per run (the todo.go pattern)
	return Tool{
		Class: RerunDetectable,
		Spec: model.ToolSpec{
			Name: "report_size",
			Description: "Report the size of the work being sized, exactly once: a range (sizeMin <= sizeMax, positive) and " +
				"the basis for it (what you looked at and why the range follows — never a bare number, D8). A second call in " +
				"the same run is refused: the first report is the report.",
			Parameters: json.RawMessage(`{
  "type": "object",
  "properties": {
    "sizeMin":   {"type": "integer", "minimum": 1, "description": "the low end of the range, in the task's own units"},
    "sizeMax":   {"type": "integer", "minimum": 1, "description": "the high end; must be >= sizeMin"},
    "sizeBasis": {"type": "string", "minLength": 1, "description": "what was inspected and why the range follows — the basis is part of the report, not optional colour (D8)"}
  },
  "required": ["sizeMin", "sizeMax", "sizeBasis"],
  "additionalProperties": false
}`),
		},
		Run: func(ctx context.Context, tc ToolContext, input json.RawMessage) Result {
			if reported {
				return Result{
					Content: "report_size was already called this run — the first report stands; a refused second call never overwrites it (D9)",
					IsError: true,
					Summary: "report_size refused · already reported",
				}
			}
			var rep SizeReport
			if err := json.Unmarshal(input, &rep); err != nil {
				return Result{
					Content: "report_size wants exactly {sizeMin, sizeMax, sizeBasis} as numbers and a string: " + err.Error(),
					IsError: true,
					Summary: "report_size refused · shape",
				}
			}
			// The same laws migration 0041 writes into queue_item's CHECK —
			// refused HERE too, because the model hears the refusal at the
			// tool call (where it can fix it), not four doors later at the
			// database. A lenient tool and a strict schema would just move
			// the failure to a worse place.
			if rep.SizeMin < 1 {
				return refuseSize("sizeMin must be at least 1 — a range that includes nothing sizes nothing")
			}
			if rep.SizeMax < rep.SizeMin {
				return refuseSize(fmt.Sprintf("sizeMax (%d) is below sizeMin (%d) — the range is inverted", rep.SizeMax, rep.SizeMin))
			}
			if strings.TrimSpace(rep.SizeBasis) == "" {
				return refuseSize("sizeBasis is empty — a range without its basis is D8's bare number wearing a hyphen")
			}
			reported = true
			return Result{
				Content: fmt.Sprintf("size reported: %d-%d — basis: %s", rep.SizeMin, rep.SizeMax, rep.SizeBasis),
				Summary: fmt.Sprintf("report_size %d-%d", rep.SizeMin, rep.SizeMax),
			}
		},
	}
}

func refuseSize(why string) Result {
	return Result{
		Content: "report_size refused: " + why + " — the shape is {sizeMin, sizeMax, sizeBasis}, a range with its basis, called once (D8/D9)",
		IsError: true,
		Summary: "report_size refused",
	}
}
