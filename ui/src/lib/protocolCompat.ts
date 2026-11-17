import { MIN_COMPATIBLE_APP_PROTOCOL_VERSION } from './protocol';
import type { LiveState } from './live';

/**
 * Whether this app and the connected daemon can safely exchange writes, and
 * why not when they can't — computed in one place.
 *
 * Before this, `App.tsx`'s `canWrite` and `daemonFooter.tsx`'s mismatch
 * indicator each ran their own `protocolVersion > MIN_COMPATIBLE_APP_PROTOCOL_VERSION`
 * comparison. Two copies of the same rule is how S38's protocol bump went
 * unnoticed for as long as it did: nothing forced them to agree, and nothing
 * caught it when `protocol.ts`'s constant fell behind. There is now one
 * verdict, read by both.
 */
export interface ProtocolCompat {
  /** The daemon's negotiated protocol number, when one is known. */
  protocolVersion?: number;
  /** True once a protocol number is known, the connection is live, and it isn't newer than this app understands. */
  canWrite: boolean;
  /** True once a protocol number is known and it is newer than this app was built to speak. */
  mismatched: boolean;
  /** The full explanation for a mismatch, for a tooltip or detail panel. Present only when `mismatched`. */
  explanation?: string;
}

export function checkProtocolCompat(live: LiveState): ProtocolCompat {
  const protocolVersion = live.state === 'current' || live.state === 'stale' ? live.protocolVersion : undefined;
  if (protocolVersion === undefined) return { canWrite: false, mismatched: false };
  const mismatched = protocolVersion > MIN_COMPATIBLE_APP_PROTOCOL_VERSION;
  return {
    protocolVersion,
    canWrite: live.state === 'current' && !mismatched,
    mismatched,
    explanation: mismatched
      ? `This app understands protocol ${MIN_COMPATIBLE_APP_PROTOCOL_VERSION}; the daemon negotiated ${protocolVersion}. Read-only views remain available — update Aeon before making changes.`
      : undefined,
  };
}
