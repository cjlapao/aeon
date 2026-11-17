# UI source layout

`main.tsx` mounts the product router. `app/routes.ts` is the route and menu registry; `app/router.tsx` maps those routes to screens, and `app/routeAccess.ts` is the single access decision for route guards. Add future authentication checks there, using the route's `access` policy. `app/App.tsx` owns the persistent daemon session; `app/PageLayout.tsx` owns the side menu, header, and scrolling page body. Navigating between pages does not create another daemon session.

| Directory | Owns |
| --- | --- |
| `app/` | Router, route access, daemon session, and page layout |
| `features/loops/pages/` | Loop list, loop detail, step detail screens |
| `features/loops/components/` | Loop workflow, thread, artifacts, Markdown |
| `features/projects/`, `features/blueprints/`, `features/system/`, `features/settings/` | Their product screens and local components |
| `features/test-bench/` | The slice test bench and its tab pages |
| `components/` | Components shared across features, including kit wrappers |
| `lib/` | Daemon bridge, live state, settings, and other nonvisual services |
| `proto/` | Generated daemon contract |

The desktop app uses hash URLs, such as `#/loops/ID`, `#/settings`, and `#/bench/files`, so a saved page or test bench tab can be reopened. The test bench also works independently in component tests; in the product it receives the shell's connection and its selected tab comes from the URL.
