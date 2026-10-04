/**
 * Polling cadences for an application's deployment history. An in-flight
 * deployment polls fast so the pipeline advances promptly; an idle
 * application polls slowly so a deployment triggered elsewhere (a provider
 * webhook, another session) is discovered without a manual refresh.
 */
export const ACTIVE_POLL_INTERVAL_MS = 3_000;
export const IDLE_POLL_INTERVAL_MS = 15_000;

/** desiredPollIntervalMs returns the cadence for the current deployment state. */
export function desiredPollIntervalMs(isActive: boolean): number {
  return isActive ? ACTIVE_POLL_INTERVAL_MS : IDLE_POLL_INTERVAL_MS;
}
