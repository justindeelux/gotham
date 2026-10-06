import type { Server, ServerStatus } from "@/features/servers";
import { activeLocale, i18n } from "@/shared/i18n";

/**
 * Pure option builders behind the required server picker (PE-5, Linear
 * JUS-34; relaxed per fix round 1 H4): every create flow and the resource
 * move card share one definition of which nodes are selectable and why the
 * rest are not.
 *
 * The contract only refuses offline nodes ("offline disabled with a
 * reason"); every other state stays selectable, matching the pre-PE-5
 * wizards and what the backend accepts on create.
 *
 * Display strings resolve in the active locale at invocation time
 * (English output unchanged); server names, ids and wire statuses are
 * never translated, only the curated reason labels around them.
 */

/** isUsableServer reports whether a node may host a new or moved resource. */
export function isUsableServer(server: Server): boolean {
  return server.status !== "offline";
}

/** unusableReason names why a node cannot host resources, for the picker hint. */
export function unusableReason(status: ServerStatus): string {
  // Tracks the locale when called during render or inside a computed.
  void activeLocale.value;
  switch (status) {
    case "offline":
      return String(i18n.global.t("projects.server.reason.offline"));
    case "error":
      return String(i18n.global.t("projects.server.reason.error"));
    case "pending":
      return String(i18n.global.t("projects.server.reason.pending"));
    case "validating":
      return String(i18n.global.t("projects.server.reason.validating"));
    default:
      return String(
        i18n.global.t("projects.server.reason.other", { status }),
      );
  }
}

/**
 * buildServerOptions maps nodes to select options. Offline nodes are
 * disabled: the picker refuses them up front with the reason below instead
 * of failing server-side.
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
 * unusableServerHint summarizes the nodes the picker disabled, with the
 * reason each one cannot host resources. Long fleets collapse to the first
 * few names plus a remainder ("and 23 more"). Empty when every node is
 * usable.
 */
export function unusableServerHint(servers: Server[]): string {
  void activeLocale.value;
  const blocked = servers.filter((server) => !isUsableServer(server));
  if (blocked.length === 0) {
    return "";
  }
  const shown = blocked
    .slice(0, 3)
    .map((server) =>
      String(
        i18n.global.t("projects.server.unusableEntry", {
          name: server.name,
          reason: unusableReason(server.status),
        }),
      ),
    );
  if (blocked.length > shown.length) {
    shown.push(
      String(
        i18n.global.t("projects.server.andMore", {
          count: blocked.length - shown.length,
        }),
      ),
    );
  }
  return shown.join("; ");
}

/**
 * singleUsableServerId preselects the node when exactly one server exists
 * and it is selectable. A lone offline node is never preselected: it is
 * disabled, so selecting it would leave the form valid-looking but
 * unsubmittable.
 */
export function singleUsableServerId(servers: Server[]): string {
  if (servers.length === 1 && isUsableServer(servers[0])) {
    return servers[0].id;
  }
  return "";
}
