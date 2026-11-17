# `webfetch` fixtures (S11 T16)

Recorded request/response captures for the `webfetch` tests. **Nothing in this
package's tests dials a socket**: `fixtureTransport` is a `http.RoundTripper`
that plays these files, so a test cannot reach a live host even by accident
(AGENTS.md §5).

Every host here is under `.test` (RFC 2606 — reserved, never routable), and
**no fixture contains a credential of any kind**: there is no `Authorization`,
no `Cookie`, no `user:pass@` and no API key anywhere in this directory, in the
request captures or in the response bodies. That is a checked property, not an
aspiration — `TestWebfetchFixturesCarryNoSecrets` walks every file.

## The format

One JSON file per scenario:

```json
{
  "description": "what this recording proves",
  "exchanges": [
    {
      "request":  {
        "method": "GET",
        "url":    "http://docs.test/guide",
        "header": {"Accept": ["text/plain, …"], "User-Agent": ["aeon-webfetch/1.0 …"]}
      },
      "response": {
        "status": 200,
        "header": {"Content-Type": ["text/plain; charset=utf-8"]},
        "body":   "the bytes returned",
        "chunked": false,
        "body_repeat": {"text": "0123456789", "times": 3}
      }
    }
  ]
}
```

- `request.header` is the **capture of the wire**, not an input: the transport
  asserts the headers it actually received equal it, key set and values. A
  regression that puts one more header on the wire (a `Cookie`, an
  `Accept-Encoding` the description does not mention) breaks that equality
  rather than passing unnoticed. Only the two permitted headers ever appear.
- `request` is matched by `method` + fully-resolved `url`. Matching is by URL
  and repeatable, because a redirect loop re-reads its one exchange; an
  unmatched request is a test failure with the URL named (and a 599 that no
  assertion treats as a plausible answer), never a fallback response.
- `response.chunked: true` reports an unknown `Content-Length` (-1), so the
  honest third state (P31) is exercised without a lying server.
- `response.content_length: N` states a length that is NOT the body's real
  size — how the two "the server's word and the count disagree" cases are
  pinned (the larger of the two is what gets reported).
- `response.abort_after: N` delivers N bytes and then fails the stream: the
  connection that ends by itself, which no fixture could produce on schedule.
- `response.body_repeat` expands `text` × `times` in memory so the 2 MiB cap
  can be crossed for real without a 2 MiB file on disk.

## Index

| File | What it pins |
| --- | --- |
| `plain-text.json` | the happy path: preamble, `status`/`content-type` header line, summary |
| `json-and-xml.json` | `application/json` and `application/xml` are on the allowlist |
| `truncated-exact.json` | the 2 MiB cap with a stated length — exact `of M bytes` |
| `truncated-unknown-length.json` | the cap with an unstated length — `of at least M bytes` |
| `stream-error.json` | a connection that drops mid-body — the kept bytes are all there was |
| `declared-longer-than-sent.json` | a server that states more than it sends: the stated length is reported |
| `declared-shorter-than-sent.json` | a lying `Content-Length` cannot shrink the reported size |
| `same-origin-redirects.json` | 5 same-origin hops followed, and every destination reported |
| `too-many-redirects.json` | the 6th hop refuses by count |
| `redirect-loop.json` | the same URL pointing at itself |
| `cross-host-redirect.json` | a different host refuses, and nothing is sent there |
| `scheme-change-redirect.json` | `http:` → `https:` on one host is an origin change |
| `port-change-redirect.json` | `:80` → `:8443` on one host is an origin change |
| `redirect-with-credentials.json` | a `Location` carrying `user:pass@` is refused |
| `content-type-refused.json` | `application/pdf` refused, offered type quoted |
| `no-content-type.json` | an absent `Content-Type` fails closed |
| `not-found.json` | a non-2xx status answers with the number and no body |
| `control-char-stripped.json` | the URL that actually goes on the wire when the argument carried a control character |
