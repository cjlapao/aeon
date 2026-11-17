# The daemon's paging convention

Written 2026-11-06 by S73. The one implementation is `aeon/internal/paging`; the first two adopters are `agent.rows`
and `agent.changes`. This document is the text the package implements — a rule a reader can check, rather than fifty
copies of a habit.

## Why a page window, when the daemon already has cursors

Three shapes exist today and none of them is wrong for what it does. This convention does not retro-fit them, and it
does not pretend they are the same thing:

| Shape | Who answers this way today | What it is for |
| --- | --- | --- |
| `limit` only | `mcp.changes`, `inbox.list`, `tools.list`, `policy.events`, most of the ledger reads | "give me the newest N, and clamp a runaway number" |
| `limit` + `nextCursor` | `retro.list`, `charter.revisions` | walk forward from where you were, cheaply, under concurrent writes |
| `hasOlder` / `hasNewer` | `loop.transcript` | a window in the middle of a fixed sequence, panned both ways |

The page window — `page`/`pageSize` in, `page`/`pageSize`/`total` out — is what a **management table** needs and none
of those three can give it: the table has to render "page 3 of 17", let a person jump to page 9, and re-sort by a
column. A cursor cannot answer `total` without a `COUNT` anyway, and once you are counting, `page` is the honest unit.

So: **a new read that serves a table pages by this convention.** A new read that streams a feed may keep a cursor, and
says so in its own contract. Adopting this shape into an existing read is that read's own slice's decision, made for
its own reason (PG5 below).

## The five keys

| Key | Type | Default | The rule |
| --- | --- | --- | --- |
| `page` | integer | `1` | 1-based. Below 1 becomes 1. Past the last page answers `[]` with the honest `total` — never an error, never a quiet redirect. |
| `pageSize` | integer | `20` | Clamped into `1…100`. A value of 500 answers 100 **and says 100 in the response**. A wrong JSON type is refused, never coerced. |
| `sort` | string | the read's own default | A name from that read's closed allow-list. An unknown name is refused **naming it** and listing what is allowed. |
| `dir` | string | `"asc"` | Exactly `"asc"` or `"desc"`. Anything else is refused naming the value — `"newest"` is a guess about what the daemon means by order. |
| `q` | string | absent | A search term where the read has a search. Trimmed. Capped (200 characters by default) and a long one is refused naming the cap — never silently truncated. |

A key the read does not take is **ignored**, per `conformance/README.md`'s T3 rule (an unknown field must not break a
client — `forward-compat.json` is that rule's standing test). One **closed** vocabulary is refused by name, because its
silence would be a lie: `limit`, `cursor`, `offset`, `order`, `search`, and the actor pair. A client that sent
`limit: 5000` asked a different question, and the refusal says which. That is S59's identity-vocabulary move, not a
general unknown-key policy.

The answer carries the rows **and the request as the daemon applied it**, plus `total`:

```jsonc
{ "agents": [ … ], "page": 1, "pageSize": 100, "total": 37, "sort": "name", "dir": "asc", "q": "review" }
```

`total` is the count of the **filtered** set, taken before the `LIMIT`, in the same read. The UI computes nothing: the
kit's `Table` page state (`page`/`pageSize`/`total`) is the same three numbers, one field each way.

Two consequences worth stating because they are where implementations slip:

- **A search changes `total`.** The `COUNT` and the window share one `WHERE`. A count taken before the filter is a
  number that disagrees with the table underneath it.
- **The sort has a tiebreak.** `ORDER BY <column> <dir>, <the stable key>` — `agent.rows` breaks by `slug`. Two rows
  sharing a name otherwise shuffle across a page boundary, which looks like the daemon being random.

## Nothing the caller typed reaches SQL

`paging.Bounds.Sorts` is a `map[string]string` written by the plane: the key is the word a client may send, the value
is the SQL column. `Request.Column` resolves by lookup and refuses an unknown key. So the `ORDER BY` is composed from
literals in the daemon's own source and two fixed words, never from a request string.

`q` reaches SQL as a bound parameter and nothing else. `paging.Like` escapes `\`, `%` and `_` (the escape first) and
the SQL must carry `ESCAPE '\'` to agree with it; `Like("")` returns the empty string, not `"%%"`, so a caller that
forgot the empty check adds no condition rather than one that matches every row.

## Adopting it (the checklist)

A read adopts by doing all five, in the change that adds or reworks it:

1. Decode `page`/`pageSize`/`sort`/`dir` (+ `q` if the read searches) into `paging.Request`; a wrong JSON type refuses
   with `ErrBadParams` naming the key. Do **not** reject unknown keys (T3) — refuse only the closed vocabulary above.
2. `Normalise` against the plane's `Bounds`, and refuse only what `Normalise` refuses (a bad `dir`, a too-long `q`).
3. Resolve the sort with `Column`; put the returned literal and direction into the `ORDER BY` with the tiebreak.
4. Count the filtered set before the window; return `paging.NewAnswer(normalisedRequest, total)` beside the rows.
5. Pin the five behaviours with tests against a real store — page 2, the clamp echoed, the past-the-end empty, the
   unknown sort refused by name, and a search whose `%`/`_`/`\` match literally.

`internal/paging`'s own tests cover the arithmetic exhaustively; the plane's tests cover the SQL that consumes it. A
read that skips step 5 has adopted the words and not the rule.

**Where the pieces are allowed to live.** The package imports only the standard library — `fmt`, `sort`, `strings` —
pinned by `internal/paging/deps_test.go`, and that is what lets `internal/store` import it at all: the store's
internal-import allow-list (I1, `store_test.go`) is a closed set, and S73 T2 added `internal/paging` to it beside
`internal/mergerule` (S57) and `internal/match` (S49), three pure packages with the same one-way shape. The plane's
`Bounds` literal belongs **in the store**, next to the SQL whose columns its values name (`store.AgentRowBounds`), so
the door that refuses a wire word and the query that resolves it read one source. Paging imports no plane, no store, no
protocol; the plane imports paging; nothing in between.

## Who has adopted

| Read | Since | Notes |
| --- | --- | --- |
| `agent.rows` | S73 T2 | All five keys. Sort set: `name · slug · version · internal · signed · createdAt · updatedAt`, default `name asc`, tiebreak `slug asc`. |
| `agent.changes` | S73 T5 | `page`/`pageSize` and an optional `slug` filter. One order — newest first — so no `sort`/`dir`; no search, so no `q`. The second adopter is what makes the convention a shape rather than one file. |
| everyone else | — | The other 428 classified entries (measured 2026-11-06) answer exactly as they do today. A read joins this table in the slice that reworks it, and the row says what it took. |

**Confirmed at S73 T8's close-out**, from the shipped code rather than from the plan: the two rows above are the table's
whole content, and they are the two reads the app's agent page asks. The client's half of the convention is guarded in
`ui/src/features/agents/agentsRpc.guard.test.ts` — the page sends no key beyond `page`/`pageSize`/`sort`/`dir`/`q`
(`agent.changes` sends only the first two, and its History tab's invented-key scan proves the shape is caught), and it
computes nothing in the browser: the scan for `.sort(` / `.filter(` / `.slice(` / `localeCompare(` / `Math.ceil(` over
the page's own files is the guard's, and its teeth are shown by a planted canary in the same file. The page's window
label is the answer's `page`/`pageSize`/`total`, never the ask's — which is what PG3 means in the browser, and what
`AgentHistory.test.tsx`'s clamp case measures.

