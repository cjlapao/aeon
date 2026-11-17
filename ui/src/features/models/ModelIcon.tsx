import { useState } from 'react';
import { CustomIcon } from '../../components/kit';
export function ModelIcon({ icon, large = false }: { icon?: string; large?: boolean }) {
  const [failedIcon, setFailedIcon] = useState<string>();
  return <span aria-hidden="true" className={`aeon-accent-wash aeon-accent-text flex shrink-0 items-center justify-center overflow-hidden rounded-2xl border border-current/10 ${large ? 'h-20 w-20' : 'h-12 w-12'}`}>
    {icon?.startsWith('data:image/') && failedIcon !== icon ? <img src={icon} alt="" onError={() => setFailedIcon(icon)} className="h-full w-full object-cover" /> : <CustomIcon icon="Idea" className={large ? 'h-9 w-9' : 'h-6 w-6'} />}
  </span>;
}
