// S44 — the folding rule on the component tree, three states, tested as pure
// data (the promise being tested: a folded layer never means collapsed rows).
import { describe, expect, it } from 'vitest';
import { visibleLayers } from '../src/features/projects/layers';

describe('visibleLayers (S44)', () => {
  it('folds the root layer when there is exactly one root', () => {
    const layers = visibleLayers([{ id: 'root-solo', name: 'Solo' }]);
    expect(layers.showProjects).toBe(false);
    expect(layers.reason).toMatch(/folds/);
  });

  it('shows the root layer when a second root was built on purpose', () => {
    expect(visibleLayers([
      { id: 'root-platform', name: 'Platform' },
      { id: 'root-core', name: 'Core' },
    ]).showProjects).toBe(true);
  });

  it('shows it too with two roots, whether or not one was auto-made', () => {
    expect(visibleLayers([
      { id: 'root-platform', name: 'Platform' },
      { id: 'root-solo', name: 'Solo' },
    ]).showProjects).toBe(true);
  });

  it('says so plainly when the tier is empty — and the empty state names the door', () => {
    const layers = visibleLayers([]);
    expect(layers.showProjects).toBe(false);
    expect(layers.reason).toMatch(/create/);
  });
});
