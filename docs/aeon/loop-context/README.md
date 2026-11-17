# Loop context: every agent knows what the loop has, and where

Status: implemented, not yet seen in a running loop (2026-11-15). Owner: cjlapao.

## What went wrong

A dev-loop's planner reported that it could not plan: no `plan.md`, no task specs, and no answers to the open questions. The loop's `prompt.md` and `discovery.md` were on file. Three causes:

1. The clarifier and the investigator had no `question` tool, although the blueprint says their bodies carry it and the question-relay lanes were removed. They wrote "Open questions" into their documents and nobody was ever asked, so no answer existed anywhere.
2. Even when a person answers a step's question, the answer goes back to that one agent. The next agent starts with a fresh context and has no way to find what was decided.
3. A loop's files are in the database, but nothing told an agent that. An agent that does not know looks for `plan.md` in the workspace. Engineers also held no `doc_read`, so they could not open their own task spec.

## What changed

- **A loop index in every loop step's system context.** `LoopContextService.RenderFor` (`internal/daemon/loop_context.go`) renders, deterministically and capped at 12 KB: the loop (title, status, phase, mode, branches), every document (name, revision, bytes, who wrote it), every task (status, engineer, dependencies, title, and its spec file when `tasks/<id>.md` exists), the questions the person answered and any still open, the count of learnings, and the tools the agent holds with a one-line summary. It opens with the rule that the files are in the database and how to read them. It rides at compile (`StepRunner.Compile`) as the `Loop context` section, before the task frame, for every step that belongs to a loop, so it covers dev-loop and every other loop and chat.
- **A `loop_context` tool** returns the same index fresh, because a document written a minute ago is not in the text the step started with. Read-only (class safe, family document).
- **Every loop-bound step is granted `doc_read`, `doc_list` and `loop_context`** (`withLoopReadTools`, `internal/protocol/step.go`), added to the grant and to the frozen handler so a resumed step rebuilds the same set. They are read-only; nothing a person approved is widened.
- **The person's answers are filed as `answers.md`** the moment they are given (`fileLoopAnswers`, `internal/daemon/loop_answers.go`). The document is rebuilt from the question rows, lists each question with who asked it and the answer, leaves out permission questions, and writes nothing when nothing changed.
- **The bodies.** `intent-clarifier`, `investigator` and `legal-advisor` now hold `question` and are told to ask their genuine questions in one batch and fold the answers in, instead of leaving them in a document. The planner reads `answers.md`, plans to it, and reports blocked, naming the questions, when an open question that changes the plan has no answer.

## Second pass (same day)

- **Gate answers are filed too.** `answers.md` is built from every question of the loop, the blueprint's gates included (`store.AllLoopQuestions`), and rebuilt after a gate is answered. The plan gate's approve, revise or abort is in it, marked as the blueprint's.
- **A "Read these first" list in each step's index**, from the blueprint: the clarified brief and the answers, then the documents the lanes before this one declared they produce (a `tasks/` entry means the directory), and, in a build lane, this task's own spec named last. A document that does not exist yet is not listed.
- **Every loop-bound step can ask the person.** `question` joins the implicit grant, so the planner, the security engineer and every builder can ask instead of guessing. The step takes one question per turn and fails when a model sends two at once, so the bodies now say "one question per turn" (they said "one batch").
- **Every loop-bound step can use the code index.** `search` and `graph` are added to the grant when the daemon registered them (they are absent in a build without retrieval, and naming an unregistered tool would refuse the compile). The index tells the agent to use them before editing and to say what they returned. Before this, no agent body named either tool, which is why the investigator reported having "no code-graph tool".
- **Audit of every blueprint against the agent bodies.** Every agent a blueprint dispatches exists. Where a lane declares a `produces` and the agent holds no `doc_write`: `qa-spec` writes `docs/test_suites/` into the repository on purpose, and `research-map`'s `report` lane has an engineer writing `REPORT.md` with `write`, which the engine files into the database when the step ends. Both are left as they are.
- **The QA prompt** was corrected: the relay gates and `finish-how` it described no longer exist in the dev-loop.

## What is not done

- Agents outside a loop (plain `aeon step`, an ad-hoc ask) get no index; there is no loop to describe.
- A gate answer is only the chosen word (approve, revise, abort); there is no free-text feedback field on the plan gate, so "revise" carries no reason. Adding one is a separate change to the gate.
- The index lists learnings by count only. An agent that wants them uses `learning_query`.
- `ask_agent` (an agent asking another agent) is still not granted by default: it is on the dangerous list and asks first. An agent with a doubt can ask the person; asking a peer needs the operator to grant it.
- An unattended loop whose agent asks a question waits for a person (it shows in Home's needs-you list); nothing answers on its behalf.
- Not seen in a running loop: rebuild the daemon, run the QA loop, and check that the clarify step asks and the planner finds `answers.md`.

## Tests

`internal/daemon/loop_context_test.go` (the index and the answers document), `internal/protocol/loop_read_tools_test.go`, `internal/tools/loop_context_test.go`, `internal/runtime/replay_events_test.go` (the section and its position). The tool is registered in the class list, the tool metadata and the registry name lists.
