/**
 * The policy gate's verdict sentence, split for display.
 *
 * The daemon renders a decision ONCE, as one sentence (§4d's one formatter):
 * `allowed: agent:surveyor · "reads" · target all-agents · source operator ·
 * no expiry`. The chat shows that sentence's parts as chips — the quoted rule
 * name pulled forward — and never rewords them, so what the person reads is
 * still exactly what the gate said. Anything that does not split cleanly is
 * shown whole.
 */
export interface PolicyParts {
  /** The rule that decided — the sentence's quoted name. */
  rule?: string;
  /** Every other part, in the sentence's order, verbatim. */
  details: string[];
}

const VERDICT_PREFIX = /^\s*(allowed|allow|refused|denied|deny|ask|asked)\s*:\s*/i;

export function parsePolicySentence(sentence: string | undefined): PolicyParts {
  const text = (sentence ?? "").replace(VERDICT_PREFIX, "").trim();
  if (!text) return { details: [] };
  const parts = text.split(/\s+·\s+/).map((p) => p.trim()).filter(Boolean);
  let rule: string | undefined;
  const details: string[] = [];
  for (const part of parts) {
    const quoted = /^"([^"]+)"$/.exec(part);
    if (quoted && rule === undefined) rule = quoted[1];
    else details.push(part);
  }
  return { rule, details };
}
