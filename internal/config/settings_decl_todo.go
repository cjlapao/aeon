package config

// settings_decl_todo.go — S75 declares the one number the loop's todo list owns:
// how many model calls an agent may go without touching an open list before the
// runtime reminds it (TD9). A count, not a duration: the unit of "went quiet" is
// a model call, and 0 turns the reminder off for an operator who finds it noisy.
func init() {
	minZero, maxCalls := 0, 100
	mustDeclare(Setting{
		Key:     "todo.nudgeAfter",
		Area:    "todo",
		Type:    TypeInt,
		Default: "8",
		Min:     &minZero,
		Max:     &maxCalls,
		Description: "Model calls an agent may make without touching its todo list, while the list has open items, " +
			"before the runtime adds a reminder to the transcript (S75). 0 turns the reminder off. " +
			"The reminder repeats at most once per window, and a step that tries to finish with items open " +
			"is sent back twice regardless of this value.",
	})
}
