import { useEffect, useRef, useState } from 'react';
import type { MinionMood } from './characters';

export function useMinionAnimation({ mood, paused, speed, attention, interval }: {
  mood: MinionMood; paused: boolean; speed: number; attention: boolean;
  interval: readonly [number, number];
}) {
  const ref = useRef<HTMLDivElement>(null);
  const [visible, setVisible] = useState(true);
  const [hidden, setHidden] = useState(false);
  const [reduced, setReduced] = useState(false);
  const min = Math.max(4000, Math.min(interval[0], interval[1]));
  const max = Math.max(min, interval[0], interval[1]);

  useEffect(() => {
    const media = window.matchMedia('(prefers-reduced-motion: reduce)');
    const motion = () => setReduced(media.matches);
    const visibility = () => setHidden(document.hidden);
    motion(); visibility();
    media.addEventListener('change', motion);
    document.addEventListener('visibilitychange', visibility);
    const observer = typeof IntersectionObserver === 'undefined' ? null : new IntersectionObserver(
      ([entry]) => setVisible(Boolean(entry?.isIntersecting)),
    );
    if (ref.current) observer?.observe(ref.current);
    return () => {
      observer?.disconnect();
      media.removeEventListener('change', motion);
      document.removeEventListener('visibilitychange', visibility);
    };
  }, []);

  const stopped = paused || !visible || hidden || reduced;
  useEffect(() => {
    const root = ref.current;
    if (!root || stopped) return;
    const timers = new Set<ReturnType<typeof setTimeout>>();
    const later = (fn: () => void, ms: number) => {
      const timer = setTimeout(() => { timers.delete(timer); fn(); }, ms);
      timers.add(timer);
    };
    const look = () => {
      root.classList.add('blink');
      later(() => root.classList.remove('blink'), 140 / speed);
      root.style.setProperty('--look-x', `${(Math.random() - .5) * 10}px`);
      root.style.setProperty('--look-y', `${mood === 'thinking' ? -6 : (Math.random() - .5) * 6}px`);
      later(look, (1900 + Math.random() * 3300) / speed);
    };
    const wave = () => {
      root.classList.add('attention-wave');
      later(() => {
        root.classList.remove('attention-wave');
        later(wave, min + Math.random() * (max - min));
      }, 4000 / speed);
    };
    later(look, (700 + Math.random() * 1400) / speed);
    if (mood === 'stuck' && attention) later(wave, 2500 + Math.random() * 3500);
    return () => {
      timers.forEach(clearTimeout);
      root.classList.remove('blink', 'attention-wave');
    };
  }, [mood, stopped, speed, attention, min, max]);

  return { ref, stopped };
}
