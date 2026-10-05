import type { Server, ServerStatus } from "@/features/servers";

/**
 * Pure option builders behind the required server picker (PE-5, Linear
 * JUS-34): every create flow and the resource move card share one
 * definition of which nodes are selectable and why the rest are not.
 */

/** isUsableServer reports whether a node may host a new or moved resource. */
export function isUsableServer(server: Server): boolean {
  return server.status === "ready";
}

/** unusableReason names why a node cannot host resources, for the picker hint. */
export function unusableReason(status: ServerStatus): string {
  switch (status) {
    case "offline":
      return "offline";
    case "error":
      return "in error";
    case "pending":
      return "still pending";
    case "validating":
      return "still validating";
    default:
      return `status ${status}`;
  }
}

/**
 * buildServerOptions maps nodes to select options. Anything but `ready` is
 * disabled: picking it would fail server-side, so the picker refuses it
 * up front instead.
 */
export function buildServerOptions(
  servers: Server[],
): Array<{ label: string; value: string; disabled: boolean }> {
  return servers.map((server) => ({
    label: `${server.name} · ${server.ip}`,
    value: server.id,
    disabled: !isUsableServer(server),
  }));
}

/**
 * unusableServerHint lists the nodes the picker disabled, with the reason
 * each one cannot host resources. Empty when every node is usable.
 */
export function unusableServerHint(servers: Server[]): string {
  const blocked = servers.filter((server) => !isUsableServer(server));
  if (blocked.length === 0) {
    return "";
  }
  return blocked
    .map((server) => `${server.name} is ${unusableReason(server.status)}`)
    .join("; ");
}

/**
 * singleUsableServerId preselects the node when exactly one usable server
 * exists. A lone offline node is never preselected: it is disabled, so
 * selecting it would leave the form valid-looking but unsubmittable.
 */
export function singleUsableServerId(servers: Server[]): string {
  const usable = servers.filter(isUsableServer);
  return usable.length === 1 ? usable[0].id : "";
}
