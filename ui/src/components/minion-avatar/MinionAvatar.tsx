'use client';

import type { CSSProperties, HTMLAttributes, PointerEvent } from 'react';
import {
  characterForName, characterGeometry, MINION_COLORS, MINION_MOUTHS, MOOD_SMILES,
  type MinionCharacter, type MinionColor, type MinionMood, type MinionSmile, type MinionView,
} from './characters';
import { useMinionAnimation } from './useMinionAnimation';
import './MinionAvatar.css';
import { CatBody, CatEyes, CatExtremities, CatPaw } from './CatFeatures';
import { CAT_COAT_COLORS, catCoatForName, type AvatarType, type CatCoat } from './catCharacters';

export interface MinionAvatarProps extends Omit<HTMLAttributes<HTMLDivElement>, 'color' | 'children'> {
  /** Built-ins: pip, bop, noodle. Other names produce stable features. */
  name?: string;
  type?: AvatarType;
  catCoat?: CatCoat;
  /** Zero-based color variation within the selected cat coat, wrapped across four palettes. */
  colorVariant?: number;
  color?: MinionColor;
  mood?: MinionMood;
  view?: MinionView;
  /** Independent of mood. auto uses that mood's default expression. */
  smile?: MinionSmile;
  size?: number;
  paused?: boolean;
  /** 1 = normal, 2 = twice as fast. Clamped to 0.25–4. */
  speed?: number;
  eyeTracking?: boolean;
  attentionWaves?: boolean;
  /** Random quiet interval between stuck waves, in milliseconds. */
  attentionInterval?: readonly [number, number];
  character?: Partial<MinionCharacter>;
  /** Custom SVG path in the original 200 × 260 character coordinates. */
  mouthPath?: string;
  mouthFilled?: boolean;
  overallsColor?: string;
  gloveColor?: string;
  feetColor?: string;
  shadowColor?: string;
  gogglesColor?: string;
  showThoughtBubble?: boolean;
  /** Set when nearby text already identifies the character. */
  decorative?: boolean;
}

const DEFAULT_INTERVAL = [9000, 16000] as const;
const HAIR = {
  sprout: 'M96 45Q95 23 84 13M98 45Q99 22 101 12M100 45Q106 22 119 16M97 45Q90 24 77 19M99 45Q109 27 126 23',
  tuft: 'M92 44q-15-13-22-9m27 8q-3-22 8-24m-2 24q16-18 23-13',
  parted: 'M97 44Q78 22 57 64M96 44Q75 28 55 68M94 45Q74 34 54 72M103 44Q122 22 144 64M104 44Q125 28 146 68M106 45Q126 34 147 72',
  bald: '',
};

function Glove({ cat = false }: { cat?: boolean }) {
  if (cat) return <CatPaw />;
  return <>
    <path className="glove" d="M-7-5Q0-7 7-5L9 2Q13 5 14 10L16 16Q17 20 14 21Q11 22 10 18L8 14V23Q8 27 5 27Q2 27 2 23V14H0V26Q0 30-3 30Q-6 30-6 26V14H-8V23Q-8 27-11 27Q-14 27-14 23V9Q-14 3-9 1Z" />
    <path className="glove-seam" d="M-7-3H6M-8 8Q0 4 7 8" />
  </>;
}

export function MinionAvatar({
  name = 'pip', type = 'minion', catCoat, colorVariant = 0, color = 'yellow', mood = 'idle', view = 'full', smile = 'auto',
  size = 128, paused = false, speed = 1, eyeTracking = true, attentionWaves = true,
  attentionInterval = DEFAULT_INTERVAL, character, mouthPath, mouthFilled,
  overallsColor = '#3984f6', gloveColor, feetColor, shadowColor, gogglesColor = '#b8bec8',
  showThoughtBubble = true, decorative = false, className = '', style,
  onPointerMove, onPointerLeave, 'aria-label': ariaLabel, ...rest
}: MinionAvatarProps) {
  const features = { ...characterForName(name), ...character };
  const isCat = type === 'cat';
  const coat = catCoat ?? catCoatForName(name);
  const variant = Number.isFinite(colorVariant) ? ((Math.trunc(colorVariant) % 4) + 4) % 4 : 0;
  // variant is folded to 0..3 on the line above; the cast states that to
  // the checker (each coat ships exactly four palettes).
  const [fur, stripe] = CAT_COAT_COLORS[coat][variant as 0 | 1 | 2 | 3];
  const geometry = characterGeometry(isCat ? 'medium' : features.body);
  const rate = Number.isFinite(speed) ? Math.min(4, Math.max(.25, speed)) : 1;
  const width = Number.isFinite(size) ? Math.max(16, size) : 128;
  const { ref, stopped } = useMinionAnimation({
    mood, paused, speed: rate, attention: attentionWaves, interval: attentionInterval,
  });
  const expressionName = smile === 'auto' ? MOOD_SMILES[mood] : smile;
  const expression = MINION_MOUTHS[expressionName];
  const filled = mouthFilled ?? expression.filled;
  const skin = Object.hasOwn(MINION_COLORS, color)
    ? MINION_COLORS[color as keyof typeof MINION_COLORS] : color;
  const variables = {
    '--skin': skin, '--overalls': overallsColor, '--gloves': gloveColor,
    '--feet': feetColor, '--shadow': shadowColor,
    '--fur': Object.hasOwn(MINION_COLORS, color) ? fur : color, '--stripe': stripe,
    '--goggles': gogglesColor, '--tempo': 1 / rate, width,
    ...style,
  } as CSSProperties;

  function track(event: PointerEvent<HTMLDivElement>) {
    onPointerMove?.(event);
    if (!eyeTracking || stopped) return;
    const box = event.currentTarget.getBoundingClientRect();
    const x = Math.max(-7, Math.min(7, ((event.clientX - box.left) / box.width - .5) * 14));
    const y = Math.max(-5, Math.min(5, ((event.clientY - box.top) / box.height - .4) * 10));
    event.currentTarget.style.setProperty('--look-x', `${x}px`);
    event.currentTarget.style.setProperty('--look-y', `${y}px`);
  }

  return <div {...rest} ref={ref} className={`minion-avatar ${className}`.trim()}
    data-type={type} data-coat={coat} data-mood={mood} data-view={view} data-paused={stopped} style={variables}
    role={decorative ? undefined : 'img'} aria-hidden={decorative || undefined}
    aria-label={decorative ? undefined : ariaLabel ?? (isCat ? `${name}, ${coat} cat, ${mood}` : `${name}, ${mood}`)}
    onPointerMove={track} onPointerLeave={event => {
      onPointerLeave?.(event);
      if (!eyeTracking || stopped) return;
      event.currentTarget.style.setProperty('--look-x', '0px');
      event.currentTarget.style.setProperty('--look-y', '0px');
    }}>
    {mood === 'thinking' && showThoughtBubble ? <div className="thought" aria-hidden="true">
      <div className="thought-window">{[0, 1, 2].map(i => <span key={i} className="thought-dot"
        style={{ '--dot-delay': `${-i * .18}s` } as CSSProperties} />)}</div>
    </div> : null}
    <svg viewBox={view === 'face' ? (isCat ? '20 12 160 160' : geometry.faceViewBox) : '0 0 200 260'} aria-hidden="true">
      <ellipse cx="100" cy="236" rx={49 * geometry.sx} ry="7" fill="var(--shadow)" opacity="var(--shadow-opacity)" />
      <g className="character-build" transform={geometry.transform}>
      <g className="creature">
        {isCat ? <CatExtremities /> : <path d="M77 211v15h-15m61-15v15h15" fill="none" stroke="var(--feet)" strokeWidth="13" strokeLinecap="round" />}
        <g className="resting-left"><path className="arm-skin" d="M55 151q-19 13-15 29" />
          <g transform="translate(40 180) scale(-.8 .8)"><Glove cat={isCat} /></g></g>
        <g className="arm"><path className="arm-skin" d="M145 151Q154.5 157.5 158.25 164.75" />
          <g className="elbow"><path className="arm-skin forearm" d="M158.25 164.75Q162 172 160 180" />
            <g className="wrist"><g transform="translate(160 180) scale(.8)"><Glove cat={isCat} /></g></g>
          </g>
        </g>
        {isCat ? <CatBody coat={coat} /> : <>
        <rect className="body-fill" x="48" y="43" width="104" height="176" rx="49" />
        <path d={HAIR[features.hair]} fill="none" stroke="#25272c" strokeWidth={features.hair === 'parted' ? 1.5 : 2} strokeLinecap="round" />
        <path d="M48 167h104v13q0 39-39 39H87q-39 0-39-39Z" fill="var(--overalls)" />
        <path d="M61 157l14 29h50l14-29" fill="none" stroke="var(--overalls)" strokeWidth="12" />
        <rect x="79" y="181" width="42" height="25" rx="6" fill="#25272c" opacity=".14" />
        <path d="M49 101h102" stroke="#25272c" strokeWidth="14" />
        </>}
        <g className="goggles" transform={geometry.eyeTransform}>
        {isCat ? <CatEyes /> : (features.eyes === 1 ? [100] : [77, 123]).map(x => <g key={x}>
          <circle cx={x} cy="101" r={features.eyes === 1 ? 28 : 25} fill="var(--goggles)" />
          <circle cx={x} cy="101" r={features.eyes === 1 ? 23 : 20} fill="#fff" />
          <g className="eye"><g className="pupil"><circle cx={x} cy="102" r="10" fill="#25272c" />
            <circle cx={x - 3} cy="99" r="3" fill="#fff" /></g></g>
        </g>)}
        </g>
        <g className="expression">
        <path className="mouth" d={mouthPath ?? expression.path}
          style={{ fill: filled ? '#302b2b' : 'none', stroke: filled ? '#302b2b' : '#25272c',
            strokeWidth: filled ? 2 : mouthPath === undefined && expressionName === 'neutral' ? 2.8 : 4 }} />
        {mouthPath === undefined && filled && expressionName === 'grin' ? <g className="grin-details">
          <path d="M83 143Q100 149 117 143L113 149Q100 153 87 149Z" fill="#fff" />
          <path d="M91 156Q100 152 109 156Q100 160 91 156Z" fill="#ed94a0" />
        </g> : null}
        {mouthPath === undefined && filled && expressionName === 'surprised' ? <g className="surprised-details">
          <path d="M96 152Q100 149 104 152Q102 155 100 155Q98 155 96 152Z" fill="#ed94a0" />
        </g> : null}
        </g>
        {!isCat ? <>
        <ellipse cx="66" cy="133" rx="9" ry="5" fill="#ef9d30" opacity=".28" />
        <ellipse cx="134" cy="133" rx="9" ry="5" fill="#ef9d30" opacity=".28" />
        </> : null}
        {mood === 'working' ? <g className="workstation">
          <path d="M58 202H142L153 220V224Q153 227 148 227H52Q47 227 47 224V220Z" fill="#35495f" />
          <path d="M58 199H142L153 219Q154 222 149 222H51Q46 222 47 219Z" fill="#607c99" />
          <g fill="#e4edf6">{[0, 1, 2].flatMap(row => Array.from({ length: 10 }, (_, col) =>
            <rect key={`${row}-${col}`} x={60 - row * 3 + col * (8 + row * .6)} y={202 + row * 5}
              width={6 + row * .4} height="3" rx=".7" />))}
            <rect x="78" y="217" width="44" height="3" rx="1" /></g>
          <g className="typing-hand"><path className="arm-skin" d="M53 153Q31 187 78 201" /><g transform="translate(78 201) scale(-.48 .48)"><Glove cat={isCat} /></g></g>
          <g className="typing-hand typing-right"><path className="arm-skin" d="M147 153Q169 187 122 201" /><g transform="translate(122 201) scale(.48)"><Glove cat={isCat} /></g></g>
        </g> : null}
      </g>
      </g>
    </svg>
  </div>;
}
