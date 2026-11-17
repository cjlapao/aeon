import { useRef } from 'react';
import { Menu, SideMenuIconButton, UserAvatar } from '../components/kit';
import { useAppSettings } from '../lib/appSettings';

/** The local user's navigation, ready for an identity when login arrives. */
export function UserMenu() {
  const { controlTone } = useAppSettings();
  const trigger = useRef<HTMLButtonElement>(null);

  return (
    <Menu
      aria-label="User menu"
      placement="right"
      tone={controlTone}
      variant="elevated"
      padding="xs"
      corner="rounded"
      width={224}
      items={[{ id: 'models', label: 'Models', icon: 'Idea', to: '/models' }, { id: 'settings', label: 'Settings', icon: 'Cog', to: '/settings' }]}
      onSelect={() => {
        // The joined sidebar owns the mobile drawer. Notify it from the
        // trigger inside the sidebar, since the popup lives in a portal.
        trigger.current?.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }));
      }}
      trigger={<SideMenuIconButton ref={trigger} label="User menu" color={controlTone}><UserAvatar icon="User" tone={controlTone} size="md" aria-hidden="true" /></SideMenuIconButton>}
    />
  );
}
