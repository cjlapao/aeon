import { toRpcError } from "./bridge";

/** Explain the additive file API when a window reaches an older daemon build. */
export function fileReaderError(cause: unknown, method: string): string {
  const error = toRpcError(cause);
  if (method !== "loop.read" && (error.code === -32601 || /method "loop\.files?\.?(?:read)?" is not registered/.test(error.message))) {
    return "The connected daemon does not support workspace file reading yet. Rebuild and restart the daemon on this same socket and state database, then reopen this view. Existing loops and files will remain available.";
  }
  return error.message;
}
