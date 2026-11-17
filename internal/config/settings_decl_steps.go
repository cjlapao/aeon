package config

// settings_decl_steps.go declares how many model calls one step may make before it
// is stopped as blocked. It was a compiled constant (12) and a conversation that
// reads a few files and keeps a todo list reached it in seconds; a loop is meant
// to run until it is done, so the bound is now optional and 0 (none) by default.
// The compiled 12 remains only for a daemon with no settings store.
func init() {
	minZero, maxCalls := 0, 100000
	mustDeclare(Setting{
		Key:     "step.maxIterations",
		Area:    "step",
		Type:    TypeInt,
		Default: "0",
		Min:     &minZero,
		Max:     &maxCalls,
		Description: "The most model calls one step may make before it is stopped as blocked, naming this bound. " +
			"0 (the default) means no bound: a loop runs until it is done, and the wall-clock deadline still applies. " +
			"An agent's own bound (its metadata, maxIterations) wins over a blueprint step's max_iterations, which wins over this. " +
			"Every call is a request and costs money, so a bound limits spend as much as time. " +
			"Read when a step starts: a change applies to the next step, not to one already running.",
	})
	mustDeclare(Setting{
		Key:     "step.deadline",
		Area:    "step",
		Type:    TypeString,
		Default: "2h",
		Description: "How long one step may WORK before it is blocked, as a duration such as 90m or 2h; 0 means no deadline. " +
			"Time spent waiting for a person is not working time: a step that asked a question gets this window again when it is answered, " +
			"so a question may sit for hours or days. A value that will not parse falls back to 2h. " +
			"Read when a step starts: a change applies to the next step.",
	})
}
