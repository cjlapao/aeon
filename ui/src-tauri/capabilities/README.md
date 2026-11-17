# `capabilities/` — what the window may ask the runtime for

## Why this directory exists, and what its absence cost

Tauri v2 gates the **core plugin** commands behind an access-control list. Commands the app defines itself
with `generate_handler!` are not gated — so with no capability at all:

| | |
| --- | --- |
| `invoke("connect")`, `invoke("rpc_call")` | **work** — they are this app's own commands |
| `listen("aeon://event")` | **denied** — `listen` belongs to the core `event` plugin |

That asymmetry is exactly what a stuck UI looks like: every call succeeds, every stream is silent. The
window sat at "Streaming…" and received nothing, and the generated ACL was literally `{}`.

It was found by elimination rather than by reading this file, which is why the file now explains itself:
a live test proved the daemon, the socket and the Rust reader all delivered the events, which left one hop.

## Why only `core:event:default`, plus three narrow notification permissions

Least privilege, and it is not ceremony here. The web layer's entire access to the outside world is:

- `invoke` of this app's own commands — **not ACL-gated**, so nothing is needed for them
- `listen` on two event channels — which is what `core:event:default` grants
  (`allow-listen`, `allow-unlisten`, `allow-emit`, `allow-emit-to`)
- Three named commands from `tauri-plugin-notification`, each granted individually rather than the
  plugin's own `notification:default` bundle — which also hands out `cancel`, `get-active`/`get-pending`,
  `list-channels`, `delete-channel`, `create-channel` and `batch`, none of which the OS-notification piece
  of S41 (`lib/osNotify.ts`) needs:
  - `notification:allow-is-permission-granted` — check without prompting
  - `notification:allow-request-permission` — the one-time OS prompt
  - `notification:allow-notify` — fire one notification

It does **not** grant filesystem, shell, HTTP, clipboard or window-management access. The window cannot
read a file or open a socket, which is the property `ui/test/no-socket-in-web-layer.test.ts` asserts from
the other side — and the reason the Tauri shell is the UI rather than optional packaging. The sidecar
daemon auto-start (`local_daemon.rs`) and the notification *send* are both reached only from Rust-side
setup code or this narrow three-command grant — never a broader door than the one feature that needed it.

**Adding a permission here widens what a compromised web layer could do.** If a page ever needs one, it
belongs in a reviewed diff with the reason written down, like a service pin (`AGENTS.md` §4).

## The window label

`"windows": ["main"]` matches the label Tauri gives the single window declared in `tauri.conf.json`. A
capability naming a window that does not exist grants nothing, silently — the same class of failure as
having no capability at all.
