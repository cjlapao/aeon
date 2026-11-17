import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, act } from '@testing-library/react';
import { renderToString } from 'react-dom/server';
import { MinionAvatar } from './MinionAvatar';
import { characterForName, characterGeometry, MINION_MOUTHS } from './characters';
import { CAT_COAT_COLORS, catCoatForName } from './catCharacters';

let reduced = false;
let motionListener: (() => void) | undefined;
beforeEach(() => {
  vi.useFakeTimers();
  reduced = false;
  motionListener = undefined;
  vi.stubGlobal('matchMedia', () => ({
    get matches() { return reduced; },
    addEventListener: (_event: string, listener: () => void) => { motionListener = listener; },
    removeEventListener: vi.fn(),
  }));
});
afterEach(() => { cleanup(); vi.useRealTimers(); vi.unstubAllGlobals(); vi.restoreAllMocks(); });

describe('MinionAvatar', () => {
  it('gives named cats their own coats while keeping identical bodies and two eyes', () => {
    const { container } = render(<>{['pip', 'bop', 'noodle'].map(name =>
      <MinionAvatar key={name} name={name} type="cat" character={{ body: 'tall', eyes: 1 }} />)}</>);
    const cats = Array.from(container.querySelectorAll('.minion-avatar'));
    expect(cats.map(cat => cat.getAttribute('data-coat'))).toEqual(['tabby', 'tuxedo', 'siamese']);
    expect(new Set(cats.map(cat => cat.querySelector('.character-build')?.getAttribute('transform'))).size).toBe(1);
    for (const cat of cats) {
      expect(cat.querySelectorAll('.eye')).toHaveLength(2);
      expect(cat.querySelector('.cat-body')).not.toBeNull();
      expect(cat.querySelector('.cat-paw')).not.toBeNull();
    }
    expect(catCoatForName('__proto__')).toBe('tabby');
    expect(catCoatForName('BOP')).toBe('tuxedo');
  });

  it('varies colors within the named coat and supports explicit coat and fur overrides', () => {
    const { rerender } = render(<MinionAvatar type="cat" name="bop" colorVariant={2} />);
    let cat = screen.getByRole('img');
    expect(cat.style.getPropertyValue('--fur')).toBe(CAT_COAT_COLORS.tuxedo[2][0]);
    expect(cat.getAttribute('data-coat')).toBe('tuxedo');
    rerender(<MinionAvatar type="cat" name="bop" colorVariant={-1} catCoat="siamese" color="#ccaa88" />);
    cat = screen.getByRole('img');
    expect(cat.getAttribute('data-coat')).toBe('siamese');
    expect(cat.style.getPropertyValue('--fur')).toBe('#ccaa88');
    expect(cat.style.getPropertyValue('--stripe')).toBe(CAT_COAT_COLORS.siamese[3][1]);
  });

  it('keeps cat mouths and details together, crops ears into face icons, and joins typing paws to arms', () => {
    const { container, rerender } = render(<MinionAvatar type="cat" name="noodle" smile="grin" view="face" />);
    expect(container.querySelector('.expression > .mouth')).not.toBeNull();
    expect(container.querySelector('.expression > .grin-details')).not.toBeNull();
    expect(container.querySelector('svg')?.getAttribute('viewBox')).toBe('20 12 160 160');
    expect(container.querySelector('.point-mask')).not.toBeNull();
    rerender(<MinionAvatar type="cat" mood="working" />);
    for (const hand of Array.from(container.querySelectorAll('.typing-hand'))) {
      expect(hand.querySelector('.arm-skin')).not.toBeNull();
      expect(hand.querySelector('.cat-paw')).not.toBeNull();
    }
    expect(container.querySelector('.glove')).toBeNull();
  });

  it('shares stuck waves and timer cleanup with cats', () => {
    vi.spyOn(Math, 'random').mockReturnValue(0);
    const { container, unmount } = render(<MinionAvatar type="cat" mood="stuck" />);
    act(() => vi.advanceTimersByTime(2600));
    expect(container.firstElementChild?.classList.contains('attention-wave')).toBe(true);
    unmount();
    expect(vi.getTimerCount()).toBe(0);
  });

  it('keeps round eyes and a shared foot baseline across the three builds', () => {
    for (const name of ['pip', 'bop', 'noodle']) {
      const geometry = characterGeometry(characterForName(name).body);
      const correction = /scale\(([^ ]+) ([^)]+)\)/.exec(geometry.eyeTransform)!;
      expect(geometry.sx * Number(correction[1])).toBeCloseTo(geometry.sy * Number(correction[2]));
      expect(228 * geometry.sy + 228 * (1 - geometry.sy)).toBeCloseTo(228);
      const [, , width, height] = geometry.faceViewBox.split(' ').map(Number);
      expect(width).toBe(height);
    }
    expect(characterGeometry('tall').sx).toBeLessThan(characterGeometry('medium').sx);
    expect(characterGeometry('tall').sy).toBeGreaterThan(characterGeometry('medium').sy);
    expect(characterGeometry('short').sy).toBeLessThan(characterGeometry('medium').sy);
  });

  it('renders refined expressions without leaking details into custom mouths', () => {
    const { container, rerender } = render(<MinionAvatar mood="done" />);
    expect(container.querySelector('.grin-details')).not.toBeNull();
    rerender(<MinionAvatar smile="surprised" />);
    expect(container.querySelector('.surprised-details')).not.toBeNull();
    expect(container.querySelector('.grin-details')).toBeNull();
    rerender(<MinionAvatar smile="grin" mouthPath="M80 140H120" />);
    expect(container.querySelector('.grin-details')).toBeNull();
    rerender(<MinionAvatar smile="grin" mouthFilled={false} />);
    expect(container.querySelector('.grin-details')).toBeNull();
  });

  it('places both typing arms above the keyboard in the same moving groups as their hands', () => {
    const { container } = render(<MinionAvatar name="pip" mood="working" />);
    const workstation = container.querySelector('.workstation')!;
    const hands = workstation.querySelectorAll('.typing-hand');
    expect(hands).toHaveLength(2);
    for (const hand of Array.from(hands)) {
      expect(hand.querySelector('.arm-skin')).not.toBeNull();
      expect(hand.querySelector('.glove')).not.toBeNull();
      expect(workstation.lastElementChild === hand || workstation.lastElementChild?.previousElementSibling === hand).toBe(true);
    }
    expect(workstation.closest('.character-build')).not.toBeNull();
  });

  it('uses named features, custom colors and a square face viewport', () => {
    const { container } = render(<MinionAvatar name="Bop" color="#76c8b4" view="face" size={48} />);
    const root = screen.getByRole('img', { name: 'Bop, idle' });
    expect(root.style.width).toBe('48px');
    expect(root.style.getPropertyValue('--skin')).toBe('#76c8b4');
    expect(container.querySelector('svg')?.getAttribute('viewBox')).toBe('36 34 128 128');
    expect(container.querySelectorAll('.eye')).toHaveLength(1);
    expect(characterForName('new-agent')).toEqual(characterForName('new-agent'));
    expect([1, 2]).toContain(characterForName('__proto__').eyes);
    expect(['sprout', 'tuft', 'parted']).toContain(characterForName('constructor').hair);
  });

  it('changes smile without changing mood or creating new timers', () => {
    const { container, rerender } = render(<MinionAvatar mood="working" smile="neutral" />);
    const count = vi.getTimerCount();
    expect(container.querySelector('.mouth')?.getAttribute('d')).toBe(MINION_MOUTHS.neutral.path);
    rerender(<MinionAvatar mood="working" smile="grin" />);
    expect(container.firstElementChild?.getAttribute('data-mood')).toBe('working');
    expect(container.querySelector('.workstation')).not.toBeNull();
    expect(container.querySelector('.mouth')?.getAttribute('d')).toBe(MINION_MOUTHS.grin.path);
    expect(vi.getTimerCount()).toBe(count);
  });

  it('keeps two avatars independent and supports custom mouth geometry', () => {
    const { container } = render(<><MinionAvatar name="pip" mood="thinking" />
      <MinionAvatar name="noodle" mood="done" mouthPath="M80 140H120" mouthFilled={false} /></>);
    const avatars = container.querySelectorAll('.minion-avatar');
    expect(avatars).toHaveLength(2);
    expect(avatars[0]!.querySelector('.thought-dot')).not.toBeNull();
    expect(avatars[1]!.querySelector('.thought')).toBeNull();
    expect(avatars[1]!.querySelector('.mouth')?.getAttribute('d')).toBe('M80 140H120');
    expect(avatars[1]!.querySelector<SVGPathElement>('.mouth')?.style.fill).toBe('none');
  });

  it('waves occasionally while stuck, clears the wave on mood changes, and cleans up timers', () => {
    vi.spyOn(Math, 'random').mockReturnValue(0);
    const { container, rerender, unmount } = render(<MinionAvatar mood="stuck" />);
    act(() => vi.advanceTimersByTime(2600));
    expect(container.firstElementChild?.classList.contains('attention-wave')).toBe(true);
    rerender(<MinionAvatar mood="idle" />);
    expect(container.firstElementChild?.classList.contains('attention-wave')).toBe(false);
    act(() => vi.advanceTimersByTime(20000));
    expect(container.firstElementChild?.classList.contains('attention-wave')).toBe(false);
    unmount();
    expect(vi.getTimerCount()).toBe(0);
  });

  it('respects disabled attention waves and explicit pause', () => {
    const { container, rerender } = render(<MinionAvatar mood="stuck" attentionWaves={false} />);
    act(() => vi.advanceTimersByTime(30000));
    expect(container.firstElementChild?.classList.contains('attention-wave')).toBe(false);
    rerender(<MinionAvatar mood="stuck" paused />);
    expect(vi.getTimerCount()).toBe(0);
    expect(container.firstElementChild?.getAttribute('data-paused')).toBe('true');
  });

  it('responds to reduced-motion changes and releases document listeners', () => {
    const remove = vi.spyOn(document, 'removeEventListener');
    const { container, unmount } = render(<MinionAvatar mood="stuck" />);
    act(() => { reduced = true; motionListener?.(); });
    expect(container.firstElementChild?.getAttribute('data-paused')).toBe('true');
    expect(vi.getTimerCount()).toBe(0);
    unmount();
    expect(remove.mock.calls.some(([event]) => event === 'visibilitychange')).toBe(true);
  });

  it('suspends timers offscreen and restores them when visible', () => {
    let notify: ((entries: { isIntersecting: boolean }[]) => void) | undefined;
    const disconnect = vi.fn();
    vi.stubGlobal('IntersectionObserver', class {
      constructor(callback: typeof notify) { notify = callback; }
      observe() {} disconnect = disconnect;
    });
    const { unmount } = render(<MinionAvatar mood="working" />);
    act(() => notify?.([{ isIntersecting: false }]));
    expect(vi.getTimerCount()).toBe(0);
    act(() => notify?.([{ isIntersecting: true }]));
    expect(vi.getTimerCount()).toBeGreaterThan(0);
    unmount();
    expect(disconnect).toHaveBeenCalledOnce();
    expect(vi.getTimerCount()).toBe(0);
  });

  it('preserves caller pointer handlers while paused and supports decorative avatars', () => {
    const onPointerMove = vi.fn();
    const { container } = render(<MinionAvatar decorative paused onPointerMove={onPointerMove} />);
    fireEvent.pointerMove(container.firstElementChild!);
    expect(onPointerMove).toHaveBeenCalledOnce();
    expect(screen.queryByRole('img')).toBeNull();
    expect(container.firstElementChild?.getAttribute('aria-hidden')).toBe('true');
  });

  it('renders deterministic markup on the server without animation timers', () => {
    const first = renderToString(<MinionAvatar name="Ada" mood="thinking" />);
    expect(first).toBe(renderToString(<MinionAvatar name="Ada" mood="thinking" />));
    expect(vi.getTimerCount()).toBe(0);
  });
});
