# S76 — a reopened chat shows what the loop is still owed

Written 2026-11-07, built the same day from the operator's walk. Depends on S39 (the loop workspace), S75 (the todo panel) and S55 (loop control).

## The problem

The chat drew itself from the live event ring. The ring is bounded and a chat opened after the fact never saw `question.asked`, `loop.waiting` or `step.steer.queued`, so a loop waiting on a person showed a history with nothing to answer, an instruction queued before leaving was gone from view, and a blocked loop simply ended. A recovered question also stayed on screen after it was answered, with "question already answered" under it, because the synthetic card was added after the real `question.answered` event.

## What changed

1. **The store says what is owed.** On opening a loop the page reads, best-effort and independently: the block of questions the parked step asked (`loop.questions`, answered ones carrying their answers), a parked gate (`loop.show` already carries it), and queued instructions (`step.steers`). Each becomes the same event the daemon would have sent (`withRecovered`), so one mapper draws it and one answer path handles it; a fact the ring still holds is drawn once, from its own event. The block is ONE card ("1/3 answered"); answered questions rest answered and only the remaining ones can be answered. After an answer the page re-reads the store, and `question.*` and `step.steer.*` events re-read it too.
2. **A blocked loop says why and can be moved.** `LoopBlockedBanner` shows the loop's own reason and a Resume (with an optional note for the agent) that follows the daemon's verdict on `loop.control` (`eligibility.resume`), showing the daemon's refusal sentence when it says no.
3. **Avatar mood.** `agentMood` maps an agent's situation to the avatar's mood (stuck while a question, approval or gate waits on a person or the loop is blocked; thinking; working; idle; done for six seconds after the loop finishes, then idle). The chat's agent header shows the face view, the rail the full body.
4. **A spinner on the to-do being worked on** (the chat's own To-dos panel).

## Not done

Steers are read for the two newest steps only. The question block is the parked step's block; answered blocks of earlier steps return through the saved transcript as tool rows, as before. Resume offers no "raise the cap" path (that is `loop.raise`, needs a budget cap hit).
