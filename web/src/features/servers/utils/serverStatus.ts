import type { ServerStatus } from "../api/servers";

export function serverStatusKey(status: ServerStatus): string {
  return `servers.status.${status}`;
}
