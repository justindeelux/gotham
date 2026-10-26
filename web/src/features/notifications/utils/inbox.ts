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
] as const;

export type NotificationLinkType = "application" | "database";

export type NotificationTone = "success" | "danger" | "neutral";

export interface NotificationTextParams {
  name: string;
  host: string;
  error: string;
}

export function notificationText(
  eventKey: string,
  params: NotificationTextParams,
): string {
  const key = (knownEventKeys as readonly string[]).includes(eventKey)
    ? eventKey
    : "unknown";
  return String(
    i18n.global.t(`notifications.inbox.events.${key}`, {
      name: params.name,
      host: params.host,
    }),
  );
}

export function notificationIcon(eventKey: string): IconName {
  if (eventKey.startsWith("deploy_")) {
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
