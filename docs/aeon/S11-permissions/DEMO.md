# S11 Permissions & the nine tools — the demo

**Stage 3 · Car, first of four.** *The bicycle could run a shell command only because you told it to, with a
warning on every use. This is the slice that makes `bash` trustworthy enough to grant.*

Use a repository you can throw away, with a bare `origin` you do not care about.

---

## 1. A step attempts `git push`, and the app asks

Start a loop whose handler carries `github-steward`'s real policy — the one that authors five of the corpus's seven
`ask` rules.

From the app: **New loop**, a blueprint whose step is granted `bash`, prompt it to *"push the current branch"*.

**You should see the step suspend and the thread ask** — inline, in the same place a gate asks:

```
  step   push        running
  ask    Run this command?

         git push origin feature/x

         Rule:  git push*        (ask)
         Agent: github-steward

         [ approve ]  [ deny ]
```

**Three things to check, and one of them is the whole point of the case.**

**The deciding rule is on screen.** `git push*`, and that it came from `github-steward`'s policy. A prompt reading
only *"approve `git push origin feature/x`?"* is a **FAIL** — the user needs to know they are overriding
`git push*`, because that is what tells them whether **the rule is wrong or the command is**. Those call for
opposite actions and the prompt is the only place to tell them apart.

**It renders through the same affordance as a gate.** S9's U14 says a human should not have to know which mechanism
is asking. This is a third kind of question — after a step question and a gate — and it looks like the others.

**But it carries more.** U14 forbids distinguishing the *mechanism*; it does not license withholding *what is being
decided*. Same door, richer payload.

Now approve.

**You should see** the command run, its output in the transcript, and the step continue.

---

## 2. Deny it — and prove nothing ran

Run the same loop again and **deny**.

**You should see** a tool error going back to the model, not a step failure:

```
  ask    Run this command?  git push origin feature/x
  you    deny
  step   push        running
         bash: denied by rule 'git push*'
```

The step **keeps going**. A denial is information the model can adapt to, not breakage (W6) — which is the point of
giving a model tools at all.

**Now prove the command never spawned.** Use a command with a visible trace instead:

> *"run: touch /tmp/aeon-proof-$(date +%s) && git push origin HEAD"*

Deny it, then:

```
ls /tmp/aeon-proof-* 2>/dev/null
```

**You should see nothing.** Not the push refused after the `touch` ran — **neither ran**.

That is G8: a command is split on `&&`, `||`, `;` and `|`, and **any segment denied denies the whole**. Otherwise
`cd sub && git push origin main` would pass because its first segment is harmless.

**Then check the receipt table:**

```
aeon debug receipts <step-id>
```

**You should see no receipt for the denied command.** A receipt means *"this may have started"* (S7's K31), so one
for something refused would make recovery report an unknown outcome for a command that provably never ran —
converting a clean denial into a step nobody can resolve. The verdict comes **before** the receipt (G15).

---

## 3. An unattended loop denies immediately, and does not wait

The same command, in a loop nobody is watching:

```
aeon loop start --unattended --blueprint <bp> "push the current branch"
```

**You should see it deny at once**, naming the rule, and **create no question**:

```
loop 2c19  blocked
  bash: denied by rule 'git push*' — ask resolved to deny (loop is unattended)
```

**Check that no question row was created:**

```
aeon loop show 2c19 --json | jq '.questions'
```

Empty. An unattended `ask` is not a question nobody answers — it is a denial, decided at once (G7).

**Now the part that matters more than it looks.** Open the app while that loop is running and confirm **its
attendedness does not change.** Then close the app on an *attended* loop mid-`ask` and confirm it **stays waiting**.

Attendedness is **declared at start and pinned**, like the blueprint (G14). It is not inferred from whether a
client is connected — because then the same command would be allowed or denied depending on whether someone
happened to have a window open, and a guard whose verdict depends on that is not reproducible.

---

## 4. The evaluator against all 366 real rules

```
aeon policy corpus --stats
```

**You should see the corpus as extracted from this repository's own agent definitions:**

```
38 of 75 agent files carry permission.bash
366 rules · 50 distinct patterns · 115 allow · 244 deny · 7 ask

  patterns with * mid-pattern         2   docker buildx build*--push*
                                          docker buildx bake*--push*
  patterns with no wildcard           3   pwd, gh, git branch --show-current
  patterns with conflicting verdicts  3
      *              allow, deny
      git push*      ask, deny
      gh pr merge*   allow, ask, deny
```

**The last three lines are the ones to read.** The same pattern gets **different verdicts in different agents** —
`gh pr merge*` is `allow`, `ask` **and** `deny`. So policy is irreducibly **per-agent** (G13), and a well-meaning
refactor extracting "the common denials" into a shared default would change behaviour for whichever agents
disagreed.

**Then the trap.** Ask the evaluator directly:

```
aeon policy check --agent docker-engineer-loop "gh pr merge 42"
```

**You should see `deny`** — against a policy whose only `gh` rule is the bare word `gh`.

A pattern with **no wildcard matches exactly** (G9). A matcher assuming a trailing `*` would match `gh pr merge`
against bare `gh` and permit it. **That is a silent over-permit, which is the worst failure direction a guard
has** — and it is the single most important thing the matcher gets right.

**And the honest limit:**

```
aeon policy check --agent github-steward 'bash -c "git push"'
```

**You should see `allow`**, and that is **correct behaviour, not a bug.** Segment splitting is not a shell grammar:
it does not understand quoting, `$( )`, backticks or heredocs. The corpus's denials target **accidents** — an agent
running `git push` when it should not — and a model deliberately obfuscating a denied command is a different threat
that no pattern matcher addresses.

**There is no sandbox.** The tool allow-list is the actual boundary: an agent without `bash` cannot run anything at
all. Nothing in the app claims otherwise, and if it ever does, that is the failure.

---

## 5. `edit` fails loudly on a stale string

```
aeon step "in README.md, replace the line 'Old title' with 'New title'"
```

Then run it again, unchanged.

**You should see the second attempt fail**, naming the string it could not find:

```
  edit: README.md — the string 'Old title' was not found
```

**Not** a fuzzy match, and **not** a silent no-op.

This is load-bearing beyond convenience. **S7 classified `edit` as `detectable` on re-run precisely because a
re-run fails loudly rather than patching twice.** A fuzzy implementation would make that classification wrong, and
crash recovery would then re-run an `edit` and corrupt a file while believing itself safe (W19).

---

## 6. One path check, all four file tools

Try each of these:

```
aeon step "write /etc/passwd with the text hello"
aeon step "write ../../../etc/passwd with the text hello"
aeon step "read the file at /home/someone-else/.ssh/id_rsa"
aeon step "grep for TODO under /"
```

**You should see all four refused**, each naming the path and the reason.

**And they should be refused by the same code.** W18: one enforcement point, not one per tool. W2's bug class is a
**re-typed absolute path** — the protocol records a real instance, `/home/cjlapao` written as `/home/cjayapao` —
and four implementations of the check are four chances for one of them to skip it.

Then the case that catches a naive implementation:

```
ln -s /etc /tmp/throwaway-repo/escape
aeon step "read escape/passwd"
```

**Refused.** A check on the *written* path passes this; a check on the *resolved* path does not.

---

## 7. `webfetch` sends no credential, ever

```
aeon step "fetch https://pkg.go.dev/net/http and summarise the ServeMux docs"
```

**You should see** the content fetched, size-capped, with the truncation reported.

Then, with a local listener that echoes the headers it received:

```
aeon step "fetch http://127.0.0.1:9099/whatever"
aeon debug last-fetch --headers
```

**You should see no `Authorization`, no `Cookie`, no bearer token — nothing credential-shaped.** W9: `webfetch` is
GET-only and never sends a credential header. Fetching library documentation is the use case across every agent
that declares it, and a POST is an exfiltration path nothing needs.

Confirm a POST is refused, and that a redirect to a **different host** is handled explicitly rather than followed
blindly.

---

## 8. `todo` leaves nothing behind

```
aeon step --allow-bash "make a checklist of the Go packages under internal, then work through it"
```

**You should see** the checklist in the transcript, updating as the step proceeds.

**Then confirm the store is untouched:**

```
sqlite3 ~/.local/state/aeon/state.db "select count(*) from task;"
aeon loop show <id> --json | jq '.tasks'
```

**Zero, and empty.** `todo` is a **within-step device** (W8): in memory for the step, visible in the transcript,
gone afterwards.

This mattered less before S8. Now there is a **real task ladder** in the store, and putting model-authored
checklists into it would make a loop's actual progress ambiguous — which is worse than useless, because the ladder
is what a human reads to know where things stand.

---

## 9. `--allow-bash` stops apologising

```
aeon step --allow-bash "run: go version"
```

**You should see no warning on stderr.**

Since S5, every use of that flag printed one, because `bash` had bounds but **no policy** and the flag's help said
so. That warning is now **retired** — the thing it warned about is what this slice built.

Check the help text too:

```
aeon step --help | grep -A2 allow-bash
```

**It should no longer say there is no command policy.** A warning that outlives its cause trains people to ignore
warnings.

---

## What this demo does **not** show

- **No sandbox.** Restated because this is the slice where someone would expect one. The tool allow-list is the
  boundary; the matcher is not.
- **No real agents.** **S13.** Policy attaches to handlers here; the 25 agent bodies and `by-tech-tag` arrive next,
  and they bring their own policy blocks with them.
- **No git or forge actions.** **S16.** The `git push` above is a `bash` command — which is exactly why it needs a
  guard.
- **No `search`.** S22, with the vector store. Nine tools, not ten.
- **No policy authoring UI.** Never promised for the alpha. Policy is data in an agent definition.
- **No network policy.** A denied `curl` is denied by pattern, not by a network namespace.

Next: **S13** fills the handler registry with 25 agent bodies and their policy, and `by-tech-tag` starts choosing
between them.
