// jsdom has none of the layout/animation APIs the ui-kit relies on, so
// components that observe or animate in an effect (NetworkGraph is the one in
// Aeon's bench) throw "ResizeObserver is not defined" the moment they mount.
//
// These are no-op polyfills: the assertions that matter here (the four honest
// states, and the pure buildGraphNetwork mapping) never depend on real
// layout, and the found case only needs the component to MOUNT without
// throwing. They set a global only when it is absent, so a real browser
// environment is never touched, and they are assigned at module load so they
// are in place before any test file's modules evaluate.
import { configure } from "@testing-library/react";

const g = globalThis as unknown as Record<string, unknown>;

if (!("ResizeObserver" in g)) {
  g.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
}

if (!("IntersectionObserver" in g)) {
  g.IntersectionObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
    takeRecords() {
      return [];
    }
  };
}

if (typeof g.requestAnimationFrame !== "function") {
  g.requestAnimationFrame = (cb: (t: number) => void) => setTimeout(() => cb(Date.now()), 0);
  g.cancelAnimationFrame = (id: number) => clearTimeout(id);
}

// testing-library's async queries (`findBy*`, `waitFor`) default to a 1s
// budget. Under the parallel load of the full suite — every worker jsdom,
// every file mounting real component trees — the settling that takes 300ms
// alone takes minutes-per-CPU-slice here, and honest assertions time out as
// flakes. A 4s budget keeps genuine failures honest (they still fail, in 4
// seconds of retry) and absorbs the contention.
configure({ asyncUtilTimeout: 4000 });
