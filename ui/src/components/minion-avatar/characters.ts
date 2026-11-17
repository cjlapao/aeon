export type MinionMood = 'idle' | 'hi' | 'thinking' | 'working' | 'stuck' | 'done';
export type MinionSmile = 'auto' | 'smile' | 'grin' | 'neutral' | 'smirk' | 'worried' | 'surprised';
export type MinionView = 'full' | 'face';
export type MinionColor = keyof typeof MINION_COLORS | (string & {});
export type MinionCharacter = {
  eyes: 1 | 2;
  hair: 'sprout' | 'tuft' | 'parted' | 'bald';
  body?: 'tall' | 'medium' | 'short';
};

export const MINION_CHARACTERS = {
  pip: { eyes: 2, hair: 'sprout', body: 'tall' },
  bop: { eyes: 1, hair: 'parted', body: 'medium' },
  noodle: { eyes: 2, hair: 'bald', body: 'short' },
} as const satisfies Record<string, MinionCharacter>;

export const MINION_COLORS = {
  yellow: '#ffd83d', green: '#87d77d', purple: '#ba9bfa', orange: '#ffb458',
  blue: '#83cafa', pink: '#f4a2c5',
} as const;

export const MINION_MOUTHS = {
  smile: { path: 'M83 141Q100 156 117 141', filled: false },
  grin: { path: 'M80 141Q100 148 120 141C117 164 83 164 80 141Z', filled: true },
  neutral: { path: 'M88 146C94 147.5 106 147.5 112 146', filled: false },
  smirk: { path: 'M86 148Q105 151 117 139', filled: false },
  worried: { path: 'M83 153Q100 136 117 153', filled: false },
  surprised: { path: 'M100 139C92 139 92 156 100 156C108 156 108 139 100 139Z', filled: true },
} as const;

export const MOOD_SMILES: Record<MinionMood, Exclude<MinionSmile, 'auto'>> = {
  idle: 'smile', hi: 'smile', thinking: 'smile', working: 'smile', stuck: 'worried', done: 'grin',
};

/** Keep the feet on one baseline and compensate the goggles for body stretching. */
export function characterGeometry(body: MinionCharacter['body'] = 'medium') {
  const SCALES = { tall: [.82, 1.1], medium: [1, 1], short: [1.03, .91] } as const;
  const [sx, sy] = SCALES[body ?? 'medium'];
  const uniform = Math.min(sx, sy);
  const side = 128 * Math.max(sx, sy);
  const cy = 98 * sy + 228 * (1 - sy);
  return {
    sx, sy,
    transform: `translate(${100 * (1 - sx)} ${228 * (1 - sy)}) scale(${sx} ${sy})`,
    eyeTransform: `translate(100 101) scale(${uniform / sx} ${uniform / sy}) translate(-100 -101)`,
    faceViewBox: `${100 - side / 2} ${cy - side / 2} ${side} ${side}`,
  };
}

/** Unknown names get stable features, without a shared registry or random render output. */
export function characterForName(name: string): MinionCharacter {
  const key = name.toLowerCase();
  if (Object.hasOwn(MINION_CHARACTERS, key)) {
    return MINION_CHARACTERS[key as keyof typeof MINION_CHARACTERS];
  }
  let hash = 0;
  for (const letter of name) hash = (hash * 31 + letter.charCodeAt(0)) >>> 0;
  // `hash % 3` is 0..2 by construction; the cast states that fact to the
  // checker, which cannot see it through the modulo.
  return { eyes: hash % 3 === 0 ? 1 : 2, hair: (['sprout', 'tuft', 'parted'] as const)[(hash % 3) as 0 | 1 | 2] };
}
