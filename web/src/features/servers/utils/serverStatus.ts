import type { ServerStatus } from "../api/servers";

/**
 * serverStatusKey maps a server lifecycle state onto its shared catalog key
 * (`servers.status.*`). It is the one place the terminal entry points resolve
 * that copy, so the label cannot drift between the picker, the stage and the
 * server detail tab.
 */
export function serverStatusKey(status: ServerStatus): string {
  return `servers.status.${status}`;
}
