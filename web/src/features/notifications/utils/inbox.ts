import { i18n } from "@/shared/i18n";
import type { IconName } from "@/shared/ui/GothamIcon.vue";

import type { NotificationItem } from "@/features/notifications/api/inbox";

const knownEventKeys = [
  "deploy_success",
  "deploy_failure",
  "backup_success",
  "backup_failure",
  "server_offline",
  "server_online",
  "certificate_expiring",
  "update_available",
  "redeploy_needed",
] as const;

export type NotificationLinkType = "application" | "database";

export type NotificationTone = "success" | "danger" | "neutral" | "warning";

export interface NotificationTextParams {
  name: string;
  host: string;
  error: string;
  detail?: string;
}

export function notificationText(
  eventKey: string,
  params: NotificationTextParams,
): string {
  const key = (knownEventKeys as readonly string[]).includes(eventKey)
    ? eventKey
    : "unknown";
  if (key === "redeploy_needed") {
    return String(
      i18n.global.t("notifications.inbox.events.redeploy_needed", {
        name: params.name,
        changes: pendingChangeLabels(params.detail ?? ""),
      }),
    );
  }
  return String(
    i18n.global.t(`notifications.inbox.events.${key}`, {
      name: params.name,
      host: params.host,
    }),
  );
}

function pendingChangeLabels(detail: string): string {
  return detail
    .split(",")
    .map((change) => change.trim())
    .filter((change) => change !== "")
    .map((change) =>
      String(i18n.global.t(`notifications.inbox.changes.${change}`)),
    )
    .join(", ");
}

export function notificationIcon(eventKey: string): IconName {
  if (eventKey.startsWith("deploy_") || eventKey === "redeploy_needed") {
    return "rocket";
  }
  if (eventKey.startsWith("backup_")) {
    return "db";
  }
  if (eventKey === "server_offline" || eventKey === "server_online") {
    return "server";
  }
  if (eventKey === "certificate_expiring") {
    return "shield";
  }
  if (eventKey === "update_available") {
    return "refresh";
  }
  return "bell";
}

export function notificationTone(eventKey: string): NotificationTone {
  if (eventKey.endsWith("_success") || eventKey === "server_online") {
    return "success";
  }
  if (
    eventKey.endsWith("_failure") ||
    eventKey === "server_offline" ||
    eventKey === "certificate_expiring"
  ) {
    return "danger";
  }
  if (eventKey === "redeploy_needed") {
    return "warning";
  }
  return "neutral";
}

export function notificationLinkType(
  item: NotificationItem,
): NotificationLinkType | null {
  if (!item.resource_id) {
    return null;
  }
  if (item.resource_type === "application") {
    return "application";
  }
  if (item.resource_type === "database") {
    return "database";
  }
  return null;
}
