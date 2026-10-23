import type { Server, ServerStatus } from "@/features/servers";
import { activeLocale, i18n } from "@/shared/i18n";

export function isUsableServer(server: Server): boolean {
  return server.status !== "offline";
}

export function unusableReason(status: ServerStatus): string {
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

export function buildServerOptions(
  servers: Server[],
): Array<{ label: string; value: string; disabled: boolean }> {
  return servers.map((server) => ({
    label: `${server.name} · ${server.ip}`,
    value: server.id,
    disabled: !isUsableServer(server),
  }));
}

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

export function singleUsableServerId(servers: Server[]): string {
  if (servers.length === 1 && isUsableServer(servers[0])) {
    return servers[0].id;
  }
  return "";
}
