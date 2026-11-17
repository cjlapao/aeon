/** Words said and heard. Pure: no audio, no clock. */

/** What is worth saying aloud: markdown, code and links stripped to plain words. */
export function speakable(text: string): string {
  return text
    .replace(/```[\s\S]*?```/g, " ")
    .replace(/`([^`]*)`/g, "$1")
    .replace(/!?\[([^\]]*)\]\([^)]*\)/g, "$1")
    .replace(/[*_#>~]+/g, "")
    .replace(/\s+/g, " ")
    .trim();
}

const ABBREVIATIONS = /(?:\b(?:mr|mrs|ms|dr|vs|etc|e\.g|i\.e|no)|\b[a-z])\.$/i;

/**
 * Whole sentences out of text that is still arriving. A sentence ends at a period, question mark or exclamation mark
 * followed by space, or at a newline; the unfinished tail is returned to be completed by the next chunk. Speaking a
 * sentence at a time lets the first start long before the answer is complete.
 */
export function splitSentences(buffer: string): { sentences: string[]; rest: string } {
  const sentences: string[] = [];
  let start = 0;
  for (let i = 0; i < buffer.length; i += 1) {
    const ch = buffer[i]!;
    const next = buffer[i + 1];
    const endsHere = ch === "\n" || ((ch === "." || ch === "?" || ch === "!") && (next === undefined ? false : /\s/.test(next)));
    if (!endsHere) continue;
    const candidate = buffer.slice(start, i + 1).trim();
    if (ch === "." && ABBREVIATIONS.test(candidate)) continue;
    if (candidate) sentences.push(candidate);
    start = i + 1;
  }
  return { sentences, rest: buffer.slice(start) };
}

/** Everything left once the stream has ended: the finished sentences and the tail as a last sentence. */
export function flushSentences(buffer: string): string[] {
  const { sentences, rest } = splitSentences(buffer);
  const tail = rest.trim();
  return tail ? [...sentences, tail] : sentences;
}

const norm = (s: string) => s.toLowerCase().normalize("NFD").replace(/[̀-ͯ]/g, "").replace(/[^a-z0-9 ]+/g, " ").replace(/\s+/g, " ").trim();

function distance(a: string, b: string): number {
  const m = Array.from({ length: a.length + 1 }, (_, i) => [i, ...Array(b.length).fill(0)] as number[]);
  for (let j = 1; j <= b.length; j += 1) m[0]![j] = j;
  for (let i = 1; i <= a.length; i += 1) for (let j = 1; j <= b.length; j += 1) {
    m[i]![j] = Math.min(m[i - 1]![j]! + 1, m[i]![j - 1]! + 1, m[i - 1]![j - 1]! + (a[i - 1] === b[j - 1] ? 0 : 1));
  }
  return m[a.length]![b.length]!;
}

/**
 * Did the person say the wake word? A recogniser mishears a name ("Orion" as "Oryon" or "Orian"), so a word within one
 * edit of it counts when the word is long enough for that to mean something. `rest` is what was said after it.
 */
export function matchWakeWord(heard: string, wake: string): { matched: boolean; rest: string } {
  const target = norm(wake);
  const words = norm(heard).split(" ").filter(Boolean);
  if (!target) return { matched: false, rest: heard };
  const tolerance = target.length >= 5 ? 1 : 0;
  for (let i = 0; i < words.length; i += 1) {
    if (distance(words[i]!, target) <= tolerance) return { matched: true, rest: words.slice(i + 1).join(" ") };
  }
  return { matched: false, rest: heard };
}

const STOP = /^(orion )?(stop|stop it|stop talking|stop speaking|enough|that s enough|quiet|be quiet|silence|shush|shut up|cancel|never mind)( orion| please| now)?$/;

/** Is this utterance only an order to stop? Whole-phrase, so a sentence that merely contains "stop" is not one. */
export function isStopCommand(heard: string): boolean {
  return STOP.test(norm(heard));
}

const YES = /^(yes|yeah|yep|sure|ok|okay|confirm|confirmed|go ahead|do it|please do|proceed|affirmative)\b/;
const NO = /^(no|nope|cancel|stop|don't|do not|never mind|negative|abort)\b/;

/** A spoken answer to "shall I?": yes, no, or neither (which is not a yes). */
export function confirmation(heard: string): "yes" | "no" | "unclear" {
  const t = norm(heard);
  if (YES.test(t)) return "yes";
  if (NO.test(t)) return "no";
  return "unclear";
}
