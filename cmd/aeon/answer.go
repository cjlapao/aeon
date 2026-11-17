package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/cjlapao/aeon/internal/model"
	"github.com/cjlapao/aeon/internal/protocol"
	"github.com/cjlapao/aeon/internal/runtime"
)

func cmdAnswer(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("answer", flag.ContinueOnError)
	fs.SetOutput(w)
	want := addProtocolVersionFlag(fs)
	positional, rest := splitPositional(args, 2)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if len(positional) != 2 {
		return fmt.Errorf("usage: aeon answer STEP_ID ANSWER (use \"\" for an empty answer)")
	}
	id, err := strconv.ParseInt(positional[0], 10, 64)
	if err != nil || id <= 0 {
		return cmdAnswerGate(w, positional[0], positional[1], *want)
	}
	c, err := dial(*want)
	if err != nil {
		return err
	}
	defer c.Close()
	var st protocol.StepView
	if err := c.Call(protocol.MethodStepShow, protocol.StepShowParams{ID: id}, &st); err != nil {
		return err
	}
	if err := validateAnswerChoice(id, st.Question, positional[1]); err != nil {
		return err
	}
	sub, err := c.Subscribe(protocol.Filter{Kinds: []string{"step.*", "model.*", "question.*"}}, nil)
	if err != nil {
		return err
	}
	_ = sub
	var accepted protocol.AnswerResponse
	questionID := int64(0)
	if st.Question != nil {
		questionID = st.Question.ID
	}
	if err := c.Call(protocol.MethodAnswer, protocol.AnswerParams{StepID: id, QuestionID: questionID, Answer: positional[1]}, &accepted); err != nil {
		return err
	}
	fmt.Fprintf(w, "step %d · resumed\n", id)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	for {
		select {
		case ev, ok := <-c.Events():
			if !ok {
				return fmt.Errorf("the connection closed while step %d was running; use `aeon step show %d` to inspect it", id, id)
			}
			if ev.Kind == "question.answered" {
				continue
			}
			var tagged struct {
				StepID int64 `json:"stepId"`
			}
			_ = json.Unmarshal(ev.Payload, &tagged)
			if tagged.StepID != 0 && tagged.StepID != id {
				continue
			}
			if ev.Kind == "step.persistence_failed" {
				var failure struct {
					Reason string `json:"reason"`
				}
				_ = json.Unmarshal(ev.Payload, &failure)
				return fmt.Errorf("step %d continued but its record could not be saved: %s", id, failure.Reason)
			}
			renderStepEvent(w, ev)
			if ev.Kind == "step.saved" || ev.Kind == "question.asked" {
				return printResumedStep(w, c, id)
			}
		case <-ctx.Done():
			fmt.Fprintf(w, "\nstep %d continues in the daemon; use `aeon step show %d` to inspect it.\n", id, id)
			return nil
		}
	}
}

func cmdAnswerGate(w io.Writer, id, answer string, want int) error {
	c, err := dial(want)
	if err != nil {
		return err
	}
	defer c.Close()
	var result struct {
		ID         string `json:"id"`
		Status     string `json:"status"`
		QuestionID int64  `json:"questionId"`
	}
	if err := c.Call(protocol.MethodLoopAnswer, protocol.LoopAnswerParams{ID: id, Answer: answer}, &result); err != nil {
		return err
	}
	fmt.Fprintf(w, "loop %s · gate %d answered · %s\n", result.ID, result.QuestionID, result.Status)
	return nil
}

func validateAnswerChoice(id int64, q *runtime.PendingQuestion, answer string) error {
	if q != nil && len(q.Options) > 0 && !contains(q.Options, answer) {
		return fmt.Errorf("%q is not one of the options for step %d: %s", answer, id, strings.Join(q.Options, " | "))
	}
	return nil
}

func printResumedStep(w io.Writer, c *protocol.Client, id int64) error {
	var st protocol.StepView
	if err := c.Call(protocol.MethodStepShow, protocol.StepShowParams{ID: id}, &st); err != nil {
		return err
	}
	var messages []model.Message
	if err := json.Unmarshal(st.Messages, &messages); err == nil {
		if note := changedFileNote(messages); note != "" {
			fmt.Fprintf(w, "  ⚠ %s\n", note)
		}
	}
	var manifest struct {
		Compactions []runtime.CompactionRecord `json:"compactions"`
	}
	_ = json.Unmarshal(st.ContextManifest, &manifest)
	fmt.Fprintln(w, resumedCostLine(st.Cost, manifest.Compactions))
	return nil
}

func changedFileNote(messages []model.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != model.RoleUser {
			continue
		}
		for _, b := range messages[i].Content {
			if b.Kind == model.BlockText && (strings.Contains(b.Text, "Changed while this step waited:") || strings.Contains(b.Text, "Missing while this step waited:")) {
				return firstLine(b.Text)
			}
		}
	}
	return ""
}

func resumedCostLine(cost protocol.CostView, records []runtime.CompactionRecord) string {
	line := fmt.Sprintf("cost  %s · 1 resume", renderCost(cost))
	if len(records) > 0 {
		var usd float64
		for _, r := range records {
			usd += r.Cost.USD
		}
		line += fmt.Sprintf(" · %d compaction(s) (+%s)", len(records), model.MoneyString(usd))
	}
	return line
}
