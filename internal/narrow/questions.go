package narrow

import "strings"

// questions.go — the questions boundary, in one file, saying so out loud.
//
// # This is permanent, not deferred (S19 T5; §8 N3)
//
// There is no future key, flag, scope, trusted caller, or "enterprise
// mode" for which the narrow surface answers a question or creates one.
// The next person who wants this will look for the flag first, so the
// flag's absence is the documented answer, here, where they will read it.
//
// **Answering is refusal by absence (N16, D6, D8).** A key authenticates
// *a caller*; it does not make the caller a human. A gate exists to reach
// a person — and if CI could answer a gate, the gate would be decoration.
// S11 already took the consistent position at the other door: an
// unattended loop *denies* an ask (G7, G14) rather than auto-answering;
// letting a bearer token answer would be the same mistake with a token
// attached. The answer action is therefore not on the table at all —
// absent, not restricted (the same reasoning as R6, asserted over the
// whole table rather than by name-checking one action).
//
// **Creating is refusal by absence too (N6).** A question the model never
// asked, answered by a human who assumed it did, puts a value into a
// transcript with false provenance — "a fabricated prompt, once answered,
// feeds a value into a transcript as though the model had asked for it."
// That is a worse failure than either half alone, so the surface cannot
// compose it from either side.
//
// **Reading is the point (O3).** A monitor needs id, kind, prompt, and
// the declared answer set — see protocol/memory.go's QuestionView.
//
// # Why the KIND is visible here, and why that is not S9's U14
//
// U14 forbids distinguishing the asking mechanism **to the person
// answering inline** — the decider must not be steered by whether a
// model, a gate, or a permission check asked. A monitor has the opposite
// need: the human deciding **whether to go and answer** benefits from
// knowing what is asking (a phase gate and a clarify question are
// different visits). Different audience, different surface — reporting
// what a loop is blocked on is not steering the answer.

// answerRefusalReason is the sentence the surface says when someone tries
// the obvious URL (404, not 403 — the action does not exist, N14's
// reasoning; the body still carries the reason and the alternative).
const answerRefusalReason = "Answering a question requires a person. A gate exists to reach one, " +
	"and a key authenticates a caller rather than a human."

// answerRefusalInstead is the alternative half (O3: refusals say what to
// do instead, not only what was refused).
const answerRefusalInstead = "Instead: answer in the app, or `aeon answer <id> <value>` from a terminal."

// createRefusalReason is N6's sentence, carried into the response body
// rather than only into this comment.
const createRefusalReason = "Questions are asked by the model or the loop driver, never posted. " +
	"A fabricated prompt, once answered, feeds a value into a transcript as though the model had asked for it."

// createRefusalInstead names the legitimate door (N6's alternative).
const createRefusalInstead = "Instead: run a step — questions arise from steps; " +
	"`aeon answer <id> <value>` answers what exists."

// answerVerbs are the words whose presence in an action name, tool name,
// or route path would mean "this surface answers questions". The check is
// over tokenized whole words, so aeon_questions_open (a noun) passes and
// aeon_question_answer (a verb) would not.
var answerVerbs = []string{"answer", "approve", "deny", "accept", "reject", "resolve", "dismiss", "submit"}

// createVerbs are the words whose presence would mean "this surface
// creates questions" (N6). "ask" is here deliberately: S11's ask is a
// command an attended loop runs, never a surface capability.
var createVerbs = []string{"create", "post", "new", "ask", "inject", "fabricate", "insert"}

// refusesQuestionAnswer reports whether a tool name, action name, socket
// method, or route path carries an answering verb (whole-token match).
func refusesQuestionAnswer(name string) bool { return carriesVerb(tokenize(name), answerVerbs) }

// refusesQuestionCreate reports the creating half (N6).
func refusesQuestionCreate(name string) bool { return carriesVerb(tokenize(name), createVerbs) }

func carriesVerb(tokens []string, verbs []string) bool {
	for _, t := range tokens {
		for _, v := range verbs {
			if t == v {
				return true
			}
		}
	}
	return false
}

// QuestionRefusalFor answers a question-shaped HTTP attempt with the
// refusal the table cannot answer with an action: the reason and the
// alternative (O3), returned as a not-found — the action does not exist
// (N14's reasoning; a 403 would advertise a door behind a lock).
//
// The shapes it catches are the OBVIOUS ones a caller reaches for: any
// non-GET verb under the questions routes (posting a question, or
// answering by POST), and any path carrying an answering verb
// (/v1/questions/12/answer and its synonyms). Everything else stays the
// plain not-found: the surface does not go hunting for intent it cannot
// see, and it never answers a question either way.
func QuestionRefusalFor(method, path string) (string, bool) {
	// Paths are tokenized on separators too — "12/approve" must yield
	// "approve" or the gate-vocabulary URL shape slips past the net.
	toks := tokenize(strings.ReplaceAll(path, "/", "_"))
	if carriesVerb(toks, answerVerbs) {
		return answerRefusalReason + " " + answerRefusalInstead, true
	}
	if strings.HasPrefix(path, "/v1/questions") && method != "GET" {
		return createRefusalReason + " " + createRefusalInstead, true
	}
	return "", false
}

// QuestionToolRefusal is the stdio twin (TestBothMountingsAgree asserts
// them separately — a mounting-specific gap is N18's failure): a client
// that guesses the tool name gets the same reason and the same
// alternative, inside the JSON-RPC error it already expects.
func QuestionToolRefusal(name string) (string, bool) {
	toks := tokenize(name)
	if carriesVerb(toks, answerVerbs) {
		return "no such tool — " + answerRefusalReason + " " + answerRefusalInstead, true
	}
	if carriesVerb(toks, createVerbs) && carriesVerb(toks, []string{"question", "questions"}) {
		return "no such tool — " + createRefusalReason + " " + createRefusalInstead, true
	}
	return "", false
}
