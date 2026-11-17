# MinionAvatar

Portable React + TypeScript avatar from the approved animated preview. No runtime dependencies beyond React. Copy this entire folder into your React application's `src/components/minion-avatar/` directory, then import it. The component imports its own scoped CSS. It does not depend on the preview host, app theme, a canvas, or an animation library.

## Basic usage

```tsx
import { MinionAvatar } from './components/minion-avatar';

<MinionAvatar name="pip" color="yellow" mood="working" />
<MinionAvatar name="bop" color="purple" mood="thinking" view="face" size={48} />
<MinionAvatar name="noodle" color="#76c8b4" mood="stuck" smile="smirk" />
<MinionAvatar type="cat" name="pip" mood="working" colorVariant={2} />
<MinionAvatar type="cat" name="bop" mood="hi" view="face" size={48} />
```

`size` is the width in pixels. Full characters have a 200:260 aspect ratio. Face icons are square, cropped from the same animated character. Body, keyboard and most hand movement are outside the face crop; expressions, eyes and head movement remain visible. The thought cloud overlays the avatar without changing its layout size.

## Props

| Prop | Default | Choices / behavior |
| --- | --- | --- |
| `type` | `"minion"` | `minion` or `cat`. Both share moods, smiles, wave joints, typing, eye tracking, pause, speed and accessibility behavior. |
| `catCoat` | From name | Optional override: `tabby`, `tuxedo`, or `siamese`. Pip defaults to tabby, Bop to tuxedo, Noodle to Siamese-style. Unknown names default to tabby. |
| `colorVariant` | `0` | Cat coat palette index, 0–3. Other finite integers wrap, including negatives. Non-finite values fall back to 0. Keeps the named cat's coat identity. |
| `name` | `"pip"` | `pip`, `bop`, `noodle`, case insensitive. Other names get stable eyes/hair derived from their name. Names identify appearance, not unique DOM IDs. |
| `color` | `"yellow"` | Minions: `yellow`, `green`, `purple`, `orange`, `blue`, `pink`, or any CSS color. Cats: use `colorVariant` for coat-aware palettes; a custom CSS color such as `#ccaa88` or `var(--agent-color)` overrides the base fur, keeping coat markings. Named minion colors leave cat fur on its coat palette. |
| `mood` | `"idle"` | `idle`, `hi`, `thinking`, `working`, `stuck`, `done`. |
| `view` | `"full"` | `full` or square `face`. |
| `smile` | `"auto"` | `auto`, `smile`, `grin`, `neutral`, `smirk`, `worried`, `surprised`. Independent of mood animation. |
| `size` | `128` | Width in pixels; minimum 16. Face icons commonly use 32, 48, 64 or 96. |
| `paused` | `false` | Pauses animation and eye tracking. Transient blinks/waves reset when paused. |
| `speed` | `1` | Animation multiplier, clamped to 0.25–4. Does not change the quiet interval between attention waves. |
| `eyeTracking` | `true` | Tracks pointer movement within the avatar in every mood, including working. Random glances continue between pointer movements. |
| `attentionWaves` | `true` | Occasional help waves only in `stuck`. Does not disable the explicit `hi` mood. |
| `attentionInterval` | `[9000, 16000]` | Random quiet time between help waves, in milliseconds. First wave starts after 2.5–6 seconds. Endpoints are sorted; minimum quiet time is 4 seconds. Use finite positive values. |
| `character` | From name | Override `eyes: 1 \| 2`, `hair: "sprout" \| "tuft" \| "parted" \| "bald"`, and/or `body: "tall" \| "medium" \| "short"`. |
| `mouthPath` | Built-in expression | Custom SVG mouth path. See coordinates below. |
| `mouthFilled` | From smile | Whether to fill the mouth. Set explicitly with custom paths. |
| `overallsColor` | `"#3984f6"` | CSS color. |
| `gloveColor` | Theme-aware | Dark charcoal in light mode, pale gray in dark mode. Optional CSS color override. |
| `feetColor` | Theme-aware | Same automatic palette as gloves. Optional CSS color override. |
| `shadowColor` | Theme-aware | Subtle dark shadow in light mode, light shadow in dark mode. Optional CSS color override. |
| `gogglesColor` | `"#b8bec8"` | CSS color. |
| `showThoughtBubble` | `true` | Show solid white cloud with animated dots during `thinking`. |
| `decorative` | `false` | Hides avatar from assistive technology when nearby text supplies the same information. |
| `aria-label` | `"{name}, {mood}"` | Override accessible description. |
| `className`, `style`, other div attributes | — | Applied to wrapper. Use a surrounding button for actions. |

The component respects the system's reduced-motion preference. It also suspends timers and animation while offscreen or when the document is hidden, and cleans up timers/listeners on unmount. Each instance has independent timing. No timers or random values run during server rendering.

## Cats

Cats all use the same size and body shape, two eyes, paws, whiskers, ears and a swishing tail. `character` eye/hair/body overrides apply only to minions. Coat identity follows the name, while `colorVariant` changes colors within that coat. Tabby includes brown, gray and ginger variations; tuxedo keeps its white blaze and chest; Siamese-style keeps the face mask and darker paws. Mouths, teeth and tongue scale down together to fit the muzzle. Shoulder paths blend into the body in resting, waving and typing poses. Face icons include the ears.

```tsx
<MinionAvatar type="cat" name="pip" colorVariant={0} />
<MinionAvatar type="cat" name="bop" colorVariant={1} />
<MinionAvatar type="cat" name="noodle" colorVariant={2} smile="grin" />
```

The existing minion color overrides stay available. `gloveColor`, `feetColor`, `gogglesColor`, and `overallsColor` affect minions; cats use fur and coat markings instead. `shadowColor` applies to both types. Edit `catCharacters.ts` for named coats and four-color palettes; edit `CatFeatures.tsx` for cat drawing details and `MinionAvatar.css` for cat shoulder paths and mouth scaling. The optional React demo includes a type selector and a cat color-variation button.

## Same mood, different smiles

```tsx
<MinionAvatar mood="working" smile="grin" />
<MinionAvatar mood="working" smile="neutral" />
<MinionAvatar mood="stuck" smile="smirk" />
<MinionAvatar mood="thinking" smile="smile" />
```

`smile="auto"` uses `MOOD_SMILES` in `characters.ts`: stuck looks worried, done grins, and the other moods smile. Changing a smile does not change mood or restart the animation timers.

## Controlled by your agent state

```tsx
import { MinionAvatar, type MinionMood } from './components/minion-avatar';

const moodByStatus: Record<string, MinionMood> = {
  idle: 'idle', planning: 'thinking', running: 'working',
  needs_input: 'stuck', complete: 'done',
};

function AgentIcon({ agent }: {
  agent: { name: string; status: string; color: string };
}) {
  return <MinionAvatar
    name={agent.name}
    mood={moodByStatus[agent.status] ?? 'idle'}
    color={agent.color}
    view="face"
    size={48}
    aria-label={`${agent.name}: ${agent.status}`}
  />;
}
```

For a clickable avatar, wrap it in a labeled native button and set `decorative` on the avatar. The component itself is a non-interactive image, not a button.

## Customize appearance

Override one instance without editing the registry:

```tsx
<MinionAvatar
  name="Ada"
  color="pink"
  character={{ eyes: 1, hair: 'parted' }}
  overallsColor="#324873"
  gloveColor="#343039"
  gogglesColor="#d7dce5"
  mood="hi"
  smile="grin"
  speed={0.8}
/>
```

Add permanent named characters in `MINION_CHARACTERS`, colors in `MINION_COLORS`, and expressions in `MINION_MOUTHS` inside `characters.ts`. Their TypeScript choices update automatically. Pip is tall and slim with upright hair; Bop has one eye, medium proportions, and parted hair; Noodle is shorter, rounder, and bald. Unknown names use medium proportions unless overridden. Eyes and goggles keep circular proportions across body shapes, and face crops adjust to the chosen build. Hands and keyboard scale with the body and stay aligned.

The working keyboard has a blue-gray case and light keys on both themes. Each typing arm renders above the keyboard and shares its hand's shoulder rotation. Grin includes teeth and a small tongue; surprised uses a softer oval mouth; neutral is a thin, gently curved closed mouth. Custom `mouthPath` suppresses built-in teeth and tongue details, so custom geometry stays independent. Setting `mouthFilled={false}` also hides those details. When editing the built-in grin or surprised paths, update their detail paths in `MinionAvatar.tsx` to match.

Mouth paths use the full SVG's 200 × 260 coordinates. Keep mouths roughly within x=78–122, y=138–158. For example:

```tsx
<MinionAvatar mood="working" mouthPath="M84 143Q100 160 116 143" mouthFilled={false} />
```

To add a mood, update `MinionMood`, `MOOD_SMILES`, the `data-mood` rules in `MinionAvatar.css`, and conditional props/markup where needed. Use `useMinionAnimation.ts` for timed gestures. Keep timer cleanup and reduced-motion behavior intact.

## Files and animation changes

Waves use nested shoulder, elbow, and wrist joints. At rest the right arm mirrors the left, including hand height. During a wave the forearm's SVG path gradually changes curve while the wrist follows the gesture. This applies to `hi` and occasional `stuck` waves, with the same `speed`, pause, and reduced-motion controls as other animations. To adjust the wave, edit `minion-wave`, `minion-elbow`, `minion-forearm`, and `minion-wrist` together in the CSS; keep the elbow and wrist path endpoints aligned with their transform origins.

- `MinionAvatar.tsx`: public props and character drawing; keyboard, gloves and eyes.
- `MinionAvatar.css`: scoped appearance, face crop, thought cloud and keyframes.
- `characters.ts`: presets, palettes, mouth paths and default expressions.
- `catCharacters.ts`: named cat coats, coat-specific palettes and public `AvatarType` / `CatCoat` types.
- `CatFeatures.tsx`: cat body, face, eyes, tail, feet and paws.
- `useMinionAnimation.ts`: random blinks, gaze and stuck waves; visibility and lifecycle cleanup.
- `MinionAvatarDemo.tsx`: interactive React example for exploring props.
- `MinionAvatar.test.tsx`: behavior and lifecycle checks.

All selectors and keyframes are prefixed with `minion`. Gloves, feet, glove seams, and the ground shadow follow the inherited CSS `color-scheme` using `light-dark()`. Aeon's `:root` / `:root.dark` theme already supplies this, so changes apply immediately without React theme listeners. In another application, set `color-scheme: light` or `color-scheme: dark` on a parent. Explicit `gloveColor`, `feetColor`, and `shadowColor` props override the automatic colors. Shadow opacity can be adjusted through `--shadow-opacity` on the wrapper. The white thought cloud remains white in both themes.

For Next.js, use this client component from a client boundary when passing handlers. Its CSS import works in Vite and modern Next.js; if your framework requires global CSS imports at the application entry, move the CSS import there. For a component library, include the CSS in the published files and ensure the bundler preserves that import.

## Verify in this repository

From `aeon/ui`, using installed development dependencies:

```bash
node_modules/.bin/tsc --noEmit --strict --jsx react-jsx --moduleResolution bundler --module esnext --target es2022 --lib es2022,dom --skipLibCheck src/components/minion-avatar/index.ts src/components/minion-avatar/MinionAvatarDemo.tsx
node_modules/.bin/vitest run --config src/components/minion-avatar/vitest.config.ts
```
