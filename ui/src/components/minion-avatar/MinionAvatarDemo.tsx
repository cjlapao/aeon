'use client';

import { useState } from 'react';
import { MinionAvatar } from './MinionAvatar';
import type { AvatarType } from './catCharacters';
import { MINION_CHARACTERS, MINION_COLORS, MINION_MOUTHS, type MinionMood, type MinionSmile, type MinionView } from './characters';

/** Optional example. Render <MinionAvatarDemo /> on any development page. */
export function MinionAvatarDemo() {
  const [type, setType] = useState<AvatarType>('minion');
  const [colorVariant, setColorVariant] = useState(0);
  const [mood, setMood] = useState<MinionMood>('idle');
  const [view, setView] = useState<MinionView>('full');
  const [color, setColor] = useState('yellow');
  const [smile, setSmile] = useState<MinionSmile>('auto');
  const [paused, setPaused] = useState(false);
  return <section aria-label="Minion avatar playground">
    <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap' }}>
      <label>Type <select value={type} onChange={e => setType(e.target.value as AvatarType)}>
        <option value="minion">Minion</option><option value="cat">Cat</option>
      </select></label>
      <label>Mood <select value={mood} onChange={e => setMood(e.target.value as MinionMood)}>
        {['idle', 'hi', 'thinking', 'working', 'stuck', 'done'].map(v => <option key={v}>{v}</option>)}
      </select></label>
      <label>View <select value={view} onChange={e => setView(e.target.value as MinionView)}>
        <option value="full">Full character</option><option value="face">Face icon</option>
      </select></label>
      {type === 'cat' ? <button type="button" onClick={() => setColorVariant(v => (v + 1) % 4)}>Change coat colors</button> : <label>Color <select value={color} onChange={e => setColor(e.target.value)}>
        {Object.keys(MINION_COLORS).map(v => <option key={v}>{v}</option>)}
      </select></label>}
      <label>Smile <select value={smile} onChange={e => setSmile(e.target.value as MinionSmile)}>
        {['auto', ...Object.keys(MINION_MOUTHS)].map(v => <option key={v}>{v}</option>)}
      </select></label>
      <button type="button" aria-pressed={paused} onClick={() => setPaused(v => !v)}>{paused ? 'Resume' : 'Pause'}</button>
    </div>
    <div style={{ display: 'flex', gap: 24, flexWrap: 'wrap', marginTop: 24 }}>
      {Object.keys(MINION_CHARACTERS).map(name => <figure key={name} style={{ margin: 0, textAlign: 'center' }}>
        <MinionAvatar {...{ name, type, color, colorVariant, mood, view, smile, paused }} size={view === 'face' ? 64 : 128} />
        <figcaption>{name}</figcaption>
      </figure>)}
    </div>
  </section>;
}
