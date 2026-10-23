export const ACTIVE_POLL_INTERVAL_MS = 3_000;
export const IDLE_POLL_INTERVAL_MS = 15_000;

export function desiredPollIntervalMs(isActive: boolean): number {
  return isActive ? ACTIVE_POLL_INTERVAL_MS : IDLE_POLL_INTERVAL_MS;
}
