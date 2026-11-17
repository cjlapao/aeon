import { Highlight, type PrismTheme } from "prism-react-renderer";

/**
 * A read-only, syntax-colored code block (JSON, YAML, Go, shell…).
 *
 * `prism-react-renderer` returns React elements, never an HTML string, so what
 * is shown is always text: nothing in `code` can become markup. The colors are
 * not baked into the theme — every token points at a `--code-*` CSS variable
 * (src/index.css), so the block follows the app's light and dark palette and
 * changes with it.
 */
export const CODE_THEME: PrismTheme = {
  plain: { color: "var(--code-fg)", backgroundColor: "transparent" },
  styles: [
    { types: ["comment", "prolog", "doctype", "cdata"], style: { color: "var(--code-comment)", fontStyle: "italic" } },
    { types: ["punctuation", "operator"], style: { color: "var(--code-punct)" } },
    { types: ["property", "tag", "attr-name", "key", "atrule"], style: { color: "var(--code-key)" } },
    { types: ["string", "char", "attr-value", "inserted"], style: { color: "var(--code-string)" } },
    { types: ["number", "boolean", "constant", "symbol"], style: { color: "var(--code-number)" } },
    { types: ["keyword", "selector", "builtin", "null"], style: { color: "var(--code-keyword)" } },
    { types: ["function", "class-name"], style: { color: "var(--code-fn)" } },
    { types: ["deleted"], style: { color: "var(--code-deleted)" } },
  ],
};

export function CodeBlock({
  code,
  language = "json",
  className = "",
  "data-testid": testId,
}: {
  code: string;
  language?: string;
  className?: string;
  "data-testid"?: string;
}) {
  const text = code.replace(/\n$/, "");
  return (
    <Highlight theme={CODE_THEME} code={text} language={language}>
      {({ tokens, getLineProps, getTokenProps }) => (
        <pre
          className={`aeon-code overflow-x-auto whitespace-pre-wrap break-words rounded-lg px-3 py-2 font-mono text-xs leading-relaxed ${className}`}
          data-testid={testId}
          data-language={language}
        >
          {tokens.map((line, i) => (
            <div key={i} {...getLineProps({ line })}>
              {line.map((token, key) => (
                <span key={key} {...getTokenProps({ token })} />
              ))}
            </div>
          ))}
        </pre>
      )}
    </Highlight>
  );
}
