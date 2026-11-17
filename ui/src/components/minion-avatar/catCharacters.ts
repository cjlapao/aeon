export type AvatarType = 'minion' | 'cat';
export type CatCoat = 'tabby' | 'tuxedo' | 'siamese';

export const CAT_CHARACTERS = { pip: 'tabby', bop: 'tuxedo', noodle: 'siamese' } as const;
export const CAT_COAT_COLORS = {
  tabby: [['#b39873', '#594535'], ['#a7aaa9', '#535b5b'], ['#dba56d', '#895933'], ['#c4b7a5', '#6c6257']],
  tuxedo: [['#45474d', '#25272c'], ['#53565c', '#30343a'], ['#373b40', '#24272b'], ['#5d6269', '#393e45']],
  siamese: [['#d5c3a7', '#6d594b'], ['#cdbda9', '#646166'], ['#e1cfb1', '#765e4c'], ['#d7cebf', '#7e7772']],
} as const;

export function catCoatForName(name: string): CatCoat {
  const key = name.toLowerCase();
  return Object.hasOwn(CAT_CHARACTERS, key) ? CAT_CHARACTERS[key as keyof typeof CAT_CHARACTERS] : 'tabby';
}
