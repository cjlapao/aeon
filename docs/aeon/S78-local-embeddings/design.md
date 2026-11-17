# S78 — Local embeddings: a model Aeon ships and runs itself (ONNX, in a worker the daemon manages)

**Status:** design only (2026-11-16). No code yet. Numbers marked **[measure]** are expectations that task T0 must replace
with measurements before anything is built on them.
**Revised 2026-11-16 (twice):** the first draft chose a third-party `llama.cpp` sidecar; the second linked ONNX into the daemon itself. The runtime is now **ONNX Runtime in a worker process the daemon starts and manages**: the same `aeon` binary, re-executed in a worker mode (L2/L3). It keeps the choice of ONNX, keeps the engine out of the daemon's address space, and adds no second binary to sign.
**Builds on:** S22 retrieval (embedding is a route, V16–V17), S18 provider breadth (the OpenAI-compatible adapter, the
three cost states), S54 platform status (the Search index tab and its Index button).
**New decisions begin at L1.**

## 1. Why

Searching the code and the history by meaning needs vectors, and vectors need an embedding model. Today (S22) that model
is a **route**: the person adds a model in Models with the *Embedding* capability, pointing at some server that answers an
embeddings endpoint. If there is none, the search index cannot be built and Index says so.

That is the right abstraction and it stays. What is missing is a default that works **with nothing to set up**:

- A new install has no embedding route, so search is keyword-only until the person finds a provider, a key and a model
  name. Most people never do.
- The model is small. A good small embedder is tens to a few hundred megabytes, runs on a laptop CPU, and needs no network
  after the download.
- The code and the history are the person's private material. Sending every chunk of a repository to a cloud endpoint to
  index it is a cost (money, V10) and a disclosure the person should choose, not inherit.
- Indexing is bulk, repetitive work. Local means no per-token cost, no rate limit, no outage.

The price is memory and disk while it runs, and a build that carries a native runtime. This design spends them
deliberately and keeps the cost bounded and visible.

## 2. The shape in one paragraph

When an embedding is needed, the daemon starts a **worker**: the `aeon` binary itself, re-executed in an embedding-worker
mode. The worker loads **ONNX Runtime** and a small ONNX embedding model, and answers embedding requests over a private
pipe to the daemon. The daemon owns its whole life (start, health, restart, stop on idle or shutdown). Aeon registers a
built-in **route** `local-embed` (provider *local*, capability `embedding`, unmetered) that appears in Models like any
other route; the embedding call reaches the worker through a local transport instead of the network, so the same client,
the same adapter registry and the same cost path serve it. The worker is the only process that ever loads the native
library. **Nothing in retrieval changes:** it already asks the route for vectors, stores
the route's model id and dimension in the index, and refuses a mismatch (V17). The new work is only: linking and shipping the runtime, obtaining a model (with consent), tokenising and pooling
(an ONNX file is only the network, §5a), keeping the engine's memory bounded, and showing the person what is happening.

## 3. Decisions

| # | Decision | Why |
|---|---|---|
| **L1** | **Local embedding is a route, not a second provider path.** `local-embed` is an ordinary route served by an ordinary OpenAI-compatible adapter; the retrieval code is untouched. | V16/I8: one cost path, one key path, one notion of a provider. A parallel in-process embedder would need its own cost, error and mismatch handling. |
| **L2** | **The engine runs in a worker process that the daemon manages; the worker is the `aeon` binary re-executed in worker mode, not a separate program.** | A native fault, a leak or a stuck inference is the worker's: the daemon survives and restarts it. The memory goes back to the OS when the worker exits. There is nothing new to sign, notarise or version: the worker is the same file as the daemon. See §4. |
| **L3** | **The engine is ONNX Runtime (CPU) running an ONNX embedding model, behind a small Go package `localembed` with a narrow interface (`Embed(texts) -> vectors, dim`), run by the worker.** | Many good small embedders ship as ONNX; the CPU runtime is mature. The narrow interface keeps the engine swappable. The native library is loaded at run time and only in worker mode, so the daemon never links or loads it. |
| **L4** | **Opt-in, never automatic.** A fresh install has no local model. Choosing it is an explicit act that names the download (what, from where, how big) and asks to proceed. | Downloading a file is a consent action; and "we fetched 140 MB without asking" is the wrong first impression. |
| **L5** | **Lazy start, idle stop.** The worker starts on the first embedding request and exits after a configurable idle period (default **[measure]** 10 minutes). The route reports `ready`, `starting` or `stopped`; asking while stopped starts it and waits. | The memory is only spent while indexing or searching, and a process exit returns all of it. A person who indexes once a week pays for minutes, not for a resident process. |
| **L6** | **The model and its dimension belong to the index.** Changing the local model is a different embedding space: the existing V17 refusal applies (priced, with the explicit rebuild), unchanged. | The failure V6 names as the worst here is two spaces in one index. L1 means we inherit the guard for free; we must not weaken it. |
| **L7** | **Local is `unknown` cost with a reason, never `$0`.** | M30/D11, already designed for a local embedding model. A claim of zero is a claim. |
| **L8** | **Everything loaded is verified first.** Pinned URLs and pinned SHA-256 for the model and the tokenizer files, checked after download and before first load; a mismatch deletes the file and refuses. The ONNX Runtime library is pinned by the Aeon release and checked at build. | A model file fed to a native parser is untrusted input from the network. The check is not skipped for "trusted" hosts. |
| **L9** | **No network surface.** The worker has no listener: it talks to the daemon over its inherited stdin/stdout pipe (framed messages), which no other process can reach. The local transport refuses any request whose target is not the `local-embed` route. | There is no port to guard and no token to leak: a smaller surface than a loopback server. |
| **L10** | **A cap on memory and threads, set by the person, with safe defaults.** Intra-op threads default to **half the cores**; the maximum sequence length and batch size are fixed to what embedding chunks need, not what the model allows. | An indexing pass must not make the machine unusable. The default favours the person's other work over index speed. |
| **L11** | **Search must still work when the model is absent, stopped or broken.** Retrieval degrades to keyword-only and says so (V9); it never blocks a query on starting a model. A pass (reindex) is the only caller allowed to wait for the child. | A search that hangs for 20 s while a model loads is worse than a keyword result now. Queries embed one short string; if the child is already up they use it, otherwise they fall back. |
| **L12** | **No telemetry, no phone-home, no auto-update.** The runtime version is pinned by the Aeon release; updating it is an Aeon release. | The point of local is that nothing leaves. |
| **L13** | **A worker failure is the worker's, never the daemon's.** A native fault, a Go panic, a hang past the per-call timeout, or an out-of-memory kill ends the worker; the daemon marks the engine `failed(reason)`, retries a bounded number of times, and keeps serving everything else, with search falling back to keyword (L11). The engine stays a conservative surface anyway: fixed input shapes, a verified model (L8), no user-supplied model paths. | This is what the worker buys over linking the runtime into the daemon, and the reason it is a process and not a goroutine. |
| **L14** | **Tokenising and pooling are part of the engine, written or vendored once, and pinned to each model's own configuration.** Each catalogue entry carries its tokenizer files and its pooling mode (mean, CLS), whether to normalise, and the query/document prefixes the model expects. | An ONNX file returns per-token states, not a sentence vector. A wrong tokenizer or pooling gives vectors that look fine and rank badly; the catalogue makes them data, tested against reference vectors. |

## 4. The runtime: what was weighed

| Option | For | Against | Verdict |
|---|---|---|---|
| **A. ONNX Runtime in a worker process (the `aeon` binary in worker mode), managed by the daemon** | The chosen engine, kept out of the daemon (L13); memory returned on exit (L5); no second binary to sign (the worker is the same file); no port (stdio pipe); many embedders ship as ONNX; the daemon manages the lifecycle itself. | A native shared library per platform to build, ship and pin; cgo in the build; a process to supervise (health, restart, orphan clean-up); tokenising and pooling are ours to do (L14); a worker mode in the `aeon` binary to maintain. | **Chosen (L2, L3).** |
| **B. ONNX Runtime linked into the daemon (in process)** | No supervision; lowest latency. | A native fault or a leak is the daemon's; memory is returned less completely than a process exit returns it; the library is loaded into the daemon for everyone. | Rejected: A gives the same engine with the isolation. |
| **C. `llama.cpp` server as a managed child, GGUF model** | Tokenisation and pooling come with it; an OpenAI-compatible endpoint out of the box. | A second, third-party binary to ship per platform, sign and notarise; a loopback port and token to guard. | The **fallback** if T0 shows tokenisation and pooling in Go cost more than they are worth (§5a). The `localembed` interface and the route do not change. |
| **D. A pure-Go inference library** | One static binary, no native dependency. | Transformer inference on a CPU is several times slower than a tuned runtime **[measure]**; narrow model coverage; we would own a numerics surface. | Rejected for now; the interface makes it a later swap. |
| **E. Ask the person to run Ollama/vLLM themselves** | Zero code. | It is what exists today and what this slice exists to remove. | Stays available as an ordinary route. |

The contract the rest of Aeon sees is the same whichever engine runs: a route whose embedding call returns vectors of a
declared dimension. That is what makes the choice reversible.

## 5. The model

One default, one or two alternatives, all GGUF, all pinned (L8). The catalogue is **data**, in a file the build embeds, so
adding a model is a file change, not a code change.

| Slot | Candidate | Dimension | Approx. size (Q8) | Notes |
|---|---|---|---|---|
| **Default** | a small English-centred retrieval embedder (the *bge-small* class) | 384 | tens of MB **[measure]** | Smallest and fastest; good enough for code and prose chunks; the right default for a laptop. |
| **Better** | a mid-size retrieval embedder with a longer context (the *nomic-embed-text* class) | 768 | roughly 140 MB **[measure]** | Handles longer chunks and does better on mixed code and prose. |
| **Multilingual** | a small multilingual embedder (the *e5-small* / *bge-m3* class) | 384–1024 | 120–600 MB **[measure]** | For history written in more than one language; opt-in because of size. |

Choosing among them is T0's job, on **Aeon's own corpus** (this repository's code and its docs/history), by one measure:
*did the right file appear in the top k for a hand-made set of 30 questions*. The sizes above are expectations, not
commitments. The default must clear a bar set before the comparison (L-Q1), not after.

The dimension is stored with the index (V16/L6). **Do not** truncate or pad vectors to make two models "fit" one index.

## 5a. Tokenising and pooling (L14)

An ONNX embedding model is the network only. Around it the engine must:

1. **Tokenise** with the model's own vocabulary (WordPiece or SentencePiece, per model) and truncate to the fixed maximum
   length. A Go tokenizer library may be used or vendored; which one is a T1 decision, taken on whether it reproduces the
   reference tokenisation for the catalogue's models **byte for byte** on a test set.
2. **Run** the session on the padded batch.
3. **Pool** the token states (mean over the attention mask, or the first token, per the model's card) and **normalise** when
   the model expects it.
4. **Apply the model's prefixes** (some embedders want a different prefix for a query than for a document) so a query and a
   chunk land in one space.

Each catalogue entry records these as data, and a test compares the engine's vectors with **reference vectors** produced
once by the model's own reference implementation, within a stated tolerance. A mismatch is a failing test, not a quiet
ranking regression.

## 6. Memory and disk, bounded

Resident memory while the model is loaded is, roughly, the model file plus the runtime's arena and working buffers **[measure]**; for the default this
is expected to be on the order of a few hundred MB, and for the larger candidates up to about a gigabyte. The rules that
keep this honest:

- **Not running when idle** (L5). The idle timeout stops the worker and the OS takes back all of its memory; the platform
  status says whether it is up.
- **One worker, shared.** All components share the one worker; concurrent passes are batched through it, not given a
  process each.
- **Disk:** the model file lives once under the state directory (`<state>/models/<id>/`), checksummed, removable from the
  UI (which also drops the indexes built with it, after the V17 explanation, not silently).
- **A visible number.** The settings card shows what the worker is using *right now* (its resident size, as the OS reports it)
  beside the model's file size, so "it costs memory" is a figure, not a feeling.

## 7. Lifecycle

```
absent ──(person chooses, confirms the download)──> downloading ──(verified)──> installed
installed ──(an embedding is needed)──> starting ──(worker ready)──> ready ──(idle timeout)──> stopped ──(needed)──> starting
any ──(person removes)──> absent
starting/ready ──(worker exits, hangs, or answers wrongly)──> failed(reason) ──(next need, bounded retry)──> starting
```

- **Start:** check free memory against the model's expected footprint first (refuse with both figures rather than let the OS
  kill something); spawn the worker (`aeon` re-executed in embedding-worker mode, with the model path, the thread count and
  the fixed shapes passed as arguments, and a minimal environment); it loads the runtime and the model, runs one warm-up
  inference, and reports ready over the pipe. A worker not ready within the timeout is killed and the state is `failed` with
  its last stderr lines.
- **Requests:** framed messages over the pipe (texts in, vectors and dimension out), batched, each with a timeout. A reply
  with the wrong count or dimension is a worker fault, not a result.
- **Supervision:** one watcher per worker; exit, crash, hang and OOM-kill are all `failed(reason)`, never a silent restart
  loop. A bounded retry (three, with backoff) then stays `failed` until the person acts.
- **Daemon shutdown:** the worker is stopped with the daemon (a closed pipe tells it to exit; a kill after a short grace) and
  never orphaned; the daemon records the worker's pid and start time and, at its next start, kills a leftover from a crash
  that is provably its own, and no other process.
- **Where it lives in the daemon:** the supervisor is a daemon service beside the async-worker manager (S22) and reuses its
  ledger for the install/download passes; the embedding worker itself is a long-lived child, not a ledger job.

## 8. Daemon surface

New methods, read first, all through the same discipline as the rest of the platform surface (P34):

| Method | Kind | Does |
|---|---|---|
| `embedding.local.status` | read | The state (`absent`/`downloading`/`installed`/`starting`/`ready`/`stopped`/`failed`), the model id and dimension, sizes, the worker's resident memory when up, the idle timeout, the last failure's reason. |
| `embedding.local.catalog` | read | The models the build knows: id, dimension, file size, source host, the checksum, the license. |
| `embedding.local.install` | control (human door) | `{model}` — starts the verified download of the model and tokenizer as an async worker (S22's ledger), progress reported; the confirm shows the exact URLs and sizes first. |
| `embedding.local.remove` | control (human door) | `{model}` — stops the worker, deletes the files; says which indexes were built with it and what V17 requires of them. |
| `embedding.local.start` / `.stop` | control | Explicit start/stop, for people who want it up before a pass or down now. |
| `embedding.local.configure` | control (human door) | `{idleMinutes, threads}`, within the bounds in L10. |

The **route** is not a method: on `installed`, the daemon makes `local-embed` appear in `routes.list` (marked built-in, not
editable, not deletable from Models; removable only through `embedding.local.remove`). Retrieval's route pick
(`tools.EmbedRoute`, first by id with the `embedding` capability) must prefer an explicit person's choice over the built-in:
a route the person added wins over `local-embed`, so adding a cloud model never gets silently ignored.

## 9. What the person sees

- **Knowledge → Search index:** if no embedding route exists, the page offers **Use a local model** beside the explanation
  it shows today. One click opens a card: the model, its size, where it comes from, what it will use while running, and
  **Download and enable** / **Cancel**.
- **A local-model card** in Models (or Settings → Search) with the state, the memory figure, **Start**, **Stop**,
  **Remove**, the idle timeout and the thread count. Failure shows the worker's last lines, not a code.
- **The Index confirm** already shows the plan; for `local-embed` the cost line reads **"unknown — runs on this machine"**,
  and gains an estimate of *time* (chunks ÷ measured throughput **[measure]**), because for a local model time, not money,
  is the price.
- **Search while the worker is stopped** says "keyword results (the local model is stopped)", with **Start** (L11).

## 10. Security and privacy

- Download hosts and checksums are pinned in the build (L8); the downloader follows no redirect to another host, rejects a
  size beyond the catalogue's, and writes to a temp file renamed only after the check.
- The model file is parsed by a native library, so it is treated as untrusted input until verified (L8), and parsed only in
  the worker; the worker accepts only catalogue models, never a path the person types (L13).
- The worker runs as the person, with no listener (L9), a minimal environment (no API keys or tokens) and no access to the
  daemon's database.
- A model's licence is shown in the card and recorded with the install; a model whose licence the build cannot
  redistribute is **downloaded from its host**, never bundled.
- The embedding input is the person's code and history; nothing about this slice sends it anywhere. A test asserts the
  local transport refuses any target other than the `local-embed` route.

## 11. Packaging

- The worker is the **`aeon` binary**: a hidden subcommand (`aeon embedding-worker`) selects worker mode, so there is
  nothing extra to build, sign, notarise or version. In worker mode only, it loads the ONNX Runtime shared library at run
  time (the Go binding uses cgo for this, which the build already needs for sqlite-vec).
- The ONNX Runtime shared library is **not committed**. A build step fetches the pinned release for each target (macOS
  arm64/x64, Linux x64/arm64, Windows x64), verifies its checksum, and it ships beside the binary. It is a few tens of MB per
  platform **[measure]**, so it is bundled rather than downloaded: the *model* is the opt-in download (L4), the runtime that
  can run it is not.
- macOS: the library is signed and notarised with the app's identity; Windows: signed. Linux: a checksum is the contract.
- GPU execution providers are **out of scope** for the first version. The CPU path is the supported one; a later slice may
  add CoreML or DirectML behind the same engine interface.

## 12. Failure modes and what each does

| Failure | Behaviour |
|---|---|
| Download interrupted | Resumes from the partial file if the host supports it; else restarts; never leaves a half file that looks installed. |
| Checksum mismatch | File deleted, install refused, the expected and the received digest in the message. |
| Worker will not start or load (missing library, bad model, shape mismatch) | `failed(reason)` with the worker's stderr tail; the route reports unavailable; retrieval is keyword-only (L11). |
| Native fault, hang or OOM-kill inside the worker | The worker ends; the daemon marks it `failed`, retries (bounded), and carries on (L13). A pass's already-written chunks stand and it resumes (S22 O5). |
| Machine low on memory | The start is refused with the figure it needed and the figure free, before the child loads, rather than letting the OS kill something. |
| Model removed while an index uses it | Refused unless the person accepts dropping those indexes (V17's explanation, stated). |
| The runtime is older than the model's operator set needs | The catalogue pairs each model with the minimum runtime version; the pairing is checked at install. |

## 13. Testing

- **No test downloads a model or loads a real runtime by default.** Unit and integration tests use a **fake engine** behind
  the `localembed` interface and a **fake worker** (a test binary honouring the same arguments and pipe protocol).
- Tokenisation and pooling are tested against **reference vectors** and reference token ids committed as fixtures, with
  a stated tolerance (§5a).
- The supervisor is tested against the fake worker for start, ready timeout, crash, hang, wrong-count reply, idle stop,
  kill-on-shutdown and orphan clean-up.
- The downloader is tested against a local server: interruption, resume, checksum failure, wrong size, redirect to another
  host (refused).
- **One opt-in suite** (`-tags localembed`, not in CI by default) runs the real worker with the real runtime and the default
  model on a fixture repository, asserts the vectors match the references and the quality bar from L-Q1, and records the
  worker's memory while up and after it stops.
- Replay fixtures for the status methods follow the existing conformance pattern.

## 14. Tasks

| # | Task | Depends | Notes |
|---|---|---|---|
| **T0** | **Measure and de-risk.** (a) Pick the default model on Aeon's own corpus with the 30-question set. (b) Measure the worker's resident memory, throughput (chunks/s), start time and idle-stop memory return on one Linux and one macOS machine. (c) Prove a Go ONNX binding loads in worker mode and runs the candidates, and choose the tokenizer library on byte-for-byte agreement with the reference. Record in `T0-findings.md`. | — | Replaces every **[measure]** here. If tokenisation and pooling in Go prove costly, switch the engine to `llama.cpp` (§4 C) before T1; the rest of the design stands. |
| **T1** | The `localembed` package: the interface, the tokenizer, the pooling and prefixes (§5a), the session wrapper with fixed shapes. Reference-vector tests. | T0 | The engine core, no process or daemon wiring. |
| **T2** | The worker mode: the `aeon embedding-worker` subcommand, the framed pipe protocol, the runtime load, the warm-up. Fake-engine tests. | T1 | |
| **T3** | The catalogue (embedded data file) and the pinned-download library: fetch, size cap, host pin, checksum, temp-and-rename, resume. | T0 | Reusable by any later download. |
| **T4** | The supervisor: spawn, ready, health, timeout, idle stop, crash and hang handling, free-memory check, orphan clean-up, memory figure. Fake-worker tests. | T2 | A daemon service beside the async-worker manager. |
| **T5** | The route: `local-embed` appears in `routes.list` when installed; the local transport onto the supervisor; the route pick prefers a person's route; the adapter and retrieval need no change (L1). | T4 | A test proves retrieval is untouched. |
| **T6** | `embedding.local.*` methods, conformance fixtures, surface table entries, `protogen`. | T5 | Human doors for install/remove/configure. |
| **T7** | Search degrades to keyword while the worker is stopped (L11) and says so; a pass waits for the start, with a bounded timeout. | T5 | |
| **T8** | The UI: the offer on the Search index tab, the card, the confirm with the download and the time estimate, the stopped/failed states. | T6 | Uses kit components; reads status, writes through the doors. |
| **T9** | Packaging: the build step that fetches and verifies the runtime library per target, cgo and cross-compilation, signing and notarisation notes. | T2 | |
| **T10** | The opt-in real-runtime suite and the close-out walk: install, index this repository, search, stop, remove — on a real machine. | all | Records findings in the slice, as every close-out does. |

## 15. Open questions

- **Q1.** What bar does the default model have to clear on the 30-question set, and who writes the questions? (Proposal:
  top-5 hit rate at least that of the cloud default on the same set, minus a stated tolerance; the questions are written
  from real searches in this repository's history, not invented for the comparison.)
- **Q2.** The ONNX Runtime library is bundled and the model downloaded (§11). If installer size matters more, the library could
  also be fetched on first use; the design works either way.
- **Q3.** Should an embedding model be **selectable per project**? V16 says the model belongs to the index, and indexes are
  per project root, so it is possible; it also means more than one model installed. Proposal: one installed model at a time
  in the first version.
- **Q4.** Is ONNX Runtime's licence, the Go binding's and the tokenizer library's, and each candidate model's licence
  compatible with how Aeon is distributed? Needs a read of the actual licences before T1; this document does not assume
  the answer.
- **Q5.** GPU: CoreML on macOS or DirectML on Windows would make indexing much faster for some complexity. Out of scope here.
- **Q6.** Worker transport: a stdio pipe (L9) is the simplest and has no surface; a Unix socket / named pipe would let a second
  consumer share the worker. Proposal: stdio now, revisit only if a second consumer appears.

## 16. What this does not change

Retrieval's chunking, the corpora, the hybrid ranking, the staleness rules, the V17 refusal, the async-worker ledger, the
Index button and its plan/confirm flow, and the cloud and self-hosted routes all stay as they are. A person with an
embedding route of their own sees no difference except that the built-in is there if they ever want it.
