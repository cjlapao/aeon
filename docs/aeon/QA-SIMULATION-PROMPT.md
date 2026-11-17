# QA simulation: one loop that exercises tasks, roadmap, learnings, the code graph and the inbox

This is a script for a real `dev-loop` run against `/home/cjlapao/code/aeon-qa-workspace`. The workspace is
a tiny Go module (`example.com/aeon-qa`, one `cmd/hello/main.go` that prints `hello`, no tests, no README),
so almost everything the loop does is visible: the code graph grows from a handful of nodes to a real
package structure, the plan produces tasks with dependencies, and the agents have plenty to learn and to
file.

The goal below is built to make the loop do four things on purpose:

1. **Ask questions.** It is deliberately ambiguous in four places, so the clarify and discovery lanes have
   something genuine to ask. The clarifier and the investigator ask with the `question` tool, one question at a
   time in the chat, and the one blueprint gate, `approve-plan`, stops for a person. (The relay gates
   `clarify-open-questions` and `discovery-open-questions` and the `finish-how` gate no longer exist in the
   dev-loop; the questions are asked by the agents themselves.)
2. **Produce a plan with several tasks and dependencies**, so the Tasks board shows cards in different
   columns and the Roadmap Gantt shows bars with links.
3. **Use the code graph and search before editing**, and record what it learns.
4. **Leave things for you to decide:** a proposal, a bug, a goal in review. These are what Home's "Needs you"
   feed shows.

## Before you start

Do these once, in the app, so the knowledge pages have something to show from the first minute.

1. Make sure `aeon-qa-workspace` is a registered component (Work → the component tree on the left). If it is
   not, add it from Home's composer or the Components page.
2. Configuration → Platform status → Graph roots → Maintain → Build the graph for the workspace root.
   Then the Search indexes tab → Reindex the `code` corpus. Both are cheap on a repository this small.
   Afterwards Work → the component → Knowledge → Code graph should show a few nodes (`main`), not "not built".
3. Make sure a route (model) is configured and that the loop is **attended**, because the questions have to be
   answered by you.
4. Optional but worth it: commit the stray `SUMMARY.md` and `SURVEY.md` first, or leave them. They are part of
   what the agents will notice and file.

Start the loop from Work → the component → Start a loop. Pick the `dev-loop` flow, standard mode, tech tag
`go`, and paste the goal below.

## The goal to paste

```
Turn the hello program into a small, tested greeting CLI, and leave the project in a state where the next
person can see what was done and why.

What I want, in my own words:

The program in cmd/hello should greet people. Right now it prints "hello". I want it to take a name and a
language, print the greeting in that language, and be able to print it in more than one format. I would also
like it to remember who it has greeted, so I can ask it for a history. Keep it to the Go standard library.

Things I have not decided and want you to ask me about before you plan: which languages to support and how
many, which output formats, whether the history should be kept at all, and if so where it lives and what
happens when the file is missing or damaged. Do not guess these. If something else genuinely changes the
design, ask about it too.

How I want the work done:

1. Before changing anything, use the code graph tool (explain the existing main, query the package nodes)
   and the search tool over the code corpus to see what is there, and say what they returned. Use them again
   when you wire the pieces together: before you call a function you wrote in another package, ask the graph
   who calls it and what it calls, and check that the chain you expect is the chain that exists.
2. Split the work into separate tasks that can be reviewed one by one, with real dependencies between them.
   I expect roughly: the greeting core, the language catalogue, the output formats, wiring them into
   cmd/hello, and the history feature, plus tests and a README. Create them as tasks, set their status as
   you go, and make the later ones depend on the earlier ones they need. A task that has nothing to do with
   the others should not wait for them.
3. Every task needs tests, written with the standard testing package, and a verification run you can show.
   Propose a verification gate for the project (go vet and go test ./...) so it is checked the same way every
   time. I will decide whether to accept it.
4. Whenever you learn something that the next person working here would trip over (a Go flag parsing
   behaviour, how this module is laid out, a decision I made in my answers and why), record it as a learning,
   with the reason. Prefer a few useful ones over many trivial ones.
5. File a bug for each real defect or gap you notice that is outside what I asked for. I already know of a few
   (no tests, no README, two stray files at the root). Add any others you find. Do not fix them in this loop.
6. Add three roadmap ideas that this work makes possible but that are NOT part of this loop (for example
   reading names from a file, coloured output, shell completion), and make them depend on each other or on
   this work where that is true.
7. Create one goal that describes the feature, link the work to it, and submit it for my review when the work
   is done. Do not mark it approved yourself.
8. At the end, tell me which of the above you did, and which you could not do and why.
```

## What to answer at each gate

Answer like a person who knows what they want but has not said it all. These answers are chosen so that the
loop makes different decisions, and so you can check that the plan reflects what you said.

| Where it asks | What to answer |
| --- | --- |
| Clarifier's questions (one per turn) | Languages: English, Portuguese and Spanish, and nothing else for now. Formats: plain text and JSON. History: yes. |
| Investigator's questions (one per turn) | History file: a JSON file in the user's config directory (`os.UserConfigDir`), created on first use. A missing file is not an error (empty history). A damaged file is an error the program reports, and it must not overwrite it. |
| `approve-plan` (first time) | Choose **revise**. This exercises the `on_reject` route back to the plan lane and shows a task count that changes. |
| `approve-plan` (second time) | Approve. |

Every answer is filed in the database as `answers.md` the moment it is given, gate answers included. After the
answer round, open Work → the loop's documents (or ask any agent to call `loop_context`): `answers.md` is there,
and the planner's context lists it under "Read these first".

If the loop asks anything else, answer it briefly and in character. If it asks nothing at all for the
clarify step, that is a finding worth writing down: the goal is meant to be ambiguous. If the planner reports
"blocked" naming unanswered questions, that is the loop working as designed: answer them and re-run the plan.

## What you should be able to see, and where

Work through this list while the loop runs and after it stops. Each line names the page and what is expected.
Anything that does not match is a finding.

**While it runs**

- **Home → Needs you.** The questions appear as soon as a gate opens, with an Answer button that opens the
  loop. The "Running now" count includes this loop.
- **The loop page.** The component menu is folded away for the width, and the Back button says where you came
  from (Home or the project) and goes back there.
- **Work → the component → Plan → Tasks.** Cards appear as the plan lands. Early on most are in "Not
  started"; as the loop works they move through In progress, Checking, In review and Landed. The tag on each
  card is the daemon's real status, not the column name. Open a card: the side panel shows the step ladder
  position, the assignee agent, the cost, and a link to the loop.
- **Work → Plan → Roadmap.** The three roadmap ideas appear as items, with bars that start on their
  creation day (labelled "not started") and links between the ones that depend on each other. The Gantt
  should not show a bar for the tasks; those are the Tasks board's.

**When the loop stops at `approve-plan`**

- The plan has a number of tasks. After you ask for the revision, the number goes up by one, and the new
  task shows the dependency you asked for. The earlier task cards are not duplicated.

**After it has finished (or is stopped on a question)**

- **Home → Needs you** now holds items beyond the questions:
  - a **proposal** for the verification gate (the agent proposed it; you decide, and Home sends you to the
    deciding page or, where there is none, shows the `aeon proposal commit` command);
  - **bugs waiting for validation**, one line per component, from the bugs the agents filed;
  - the **goal** waiting for your review, if the goal page lists it there.
- **Work → Plan → Goals.** The goal is in a "waiting for review" state. Open it and decide it yourself.
- **Work → Knowledge → Memory.** The learnings appear, each with the reason the agent gave. Check that they
  are about this project (the flag behaviour, the layout, your answers) and not generic Go advice. Open one:
  the provenance says an agent wrote it.
- **Work → Knowledge → Code graph.** It is no longer a single node. Search for the greeting function you
  asked for; its callers and callees should match the wiring the plan describes. The scope note says the
  graph is indexed for the repository.
- **Work → Knowledge → Search index.** After a Reindex, a search for the history file's name finds the
  storage code and not just the README.
- **Work → Guardrails → Triage.** The filed bugs are listed as unvalidated. Validate one; the Home count
  drops by one.
- **Work → Guardrails → Gates.** The gate the agent proposed shows once you accept it. Run it once with
  Try; it should pass. This is also the way to make a gate fail on purpose: add a failing test, run the
  gate, and Home's Needs you gets a "latest verification run failed" row until a later run passes.
- **Work → History.** The loop's record shows the lanes in order, including the revision of the plan.

**Two things this script cannot force, so look for them**

- Whether the agents actually used the graph before editing. Open the loop's transcript and look for
  `graph` calls (explain, path, query) before the first edit. If there are none, the prompt's first
  instruction was ignored, and that is the most important finding of the run.
- Whether the learnings are worth keeping. If they are all trivial, tighten step 4 of the goal and run
  again on a fresh branch.

## Running it again

The workspace is meant to be reused. To start over cleanly: delete the feature branch the loop created,
reset the workspace to the commit with `cmd/hello/main.go` printing `hello`, and rebuild the graph and the
search index so Knowledge shows the small starting state. The tasks, learnings, bugs and roadmap items from
the earlier run stay in the store, which is the point: run twice and the Tasks board, the Roadmap and Memory
should show the first run's history next to the second's.
