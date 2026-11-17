import { Component, type ErrorInfo, type ReactNode } from "react";
import { Panel } from "./kit";

/**
 * A render error in one tab must not blank the app.
 *
 * # Why this exists, and what it must not become
 *
 * The Store tab crashed on a fresh store — `TypeError: null is not an object
 * (evaluating 'info.pending.length')` — and the **whole page went blank**.
 * React unmounts the tree when a render throws, so the error reached the
 * console and the person reached an empty window. The cause was one null field;
 * the *experience* was "the app is broken and says nothing", which is a
 * different and worse failure.
 *
 * So this is not an apology screen. It **shows the error** — the message, and
 * the component that threw — because the person looking at it is the person who
 * has to report it, and a friendly sentence with the detail hidden in the
 * console is how a bug survives three releases.
 *
 * It deliberately does NOT:
 *
 *   - retry, which would loop on a deterministic render error
 *   - fall back to a partial view, which would make a crash look like data
 *   - swallow anything: `componentDidCatch` re-logs to the console, so whatever
 *     a developer was reading is still there
 *
 * It is per TAB rather than around the whole app, so the other tabs keep
 * working — the Status tab in particular, which is how you find out whether the
 * daemon is even there.
 */
interface Props {
  /** The tab's label, so the message can name what failed. */
  name: string;
  children: ReactNode;
}

interface State {
  error: Error | null;
  componentStack: string;
}

export class TabBoundary extends Component<Props, State> {
  state: State = { error: null, componentStack: "" };

  static getDerivedStateFromError(error: Error): Partial<State> {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    // Re-logged, not swallowed. The console is where a developer already looks,
    // and this boundary must not take that away.
    console.error(`the ${this.props.name} tab failed to render`, error, info.componentStack);
    this.setState({ componentStack: info.componentStack ?? "" });
  }

  render() {
    const { error, componentStack } = this.state;
    if (!error) return this.props.children;

    // The first frame of the component stack is the component that threw, which
    // is the single most useful line and the one nobody scrolls to find.
    const firstFrame = componentStack.trim().split("\n")[0]?.trim() ?? "";

    return (
      <Panel tone="red" data-testid="tab-boundary">
        <p className="text-sm font-medium">The {this.props.name} tab failed to render.</p>
        <p className="mt-2 text-sm">
          This is a bug in the app, not in the daemon: the call returned and the page could not draw it.
          The other views still work — <strong>Status</strong> in the test bench or <strong>System</strong> in the app will tell you whether the daemon is there.
        </p>
        <pre
          className="mt-3 overflow-x-auto rounded bg-black/10 p-2 text-xs"
          data-testid="tab-boundary-error"
        >
          {error.message}
          {firstFrame && `\n    in ${firstFrame}`}
        </pre>
        <p className="mt-2 text-xs opacity-70">
          Paste that when reporting it. A reload will not fix a render error, so this does not offer one.
        </p>
      </Panel>
    );
  }
}
