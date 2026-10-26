import { http } from "@/shared/api/http";
import { parseWith } from "@/shared/validation/parse";

import {
  notificationInboxSchema,
  notificationItemSchema,
} from "@/features/notifications/schemas/inbox";

export interface NotificationItem {
  id: string;
  event_key: string;
  resource_type?: string;
  resource_id?: string;
  project_id?: string;
  name: string;
  host: string;
  error: string;
  created_at: string;
}

export interface NotificationPage {
  notifications: NotificationItem[];
  unreadCount: number;
}

export const notificationPageLimit = 30;

export async function listNotifications(
  limit = notificationPageLimit,
): Promise<NotificationPage> {
  const { data } = await http.get("/notifications", { params: { limit } });
  const page = parseWith(notificationInboxSchema, data, {
    context: "NotificationInbox",
  });
  return {
    notifications: page.notifications ?? [],
    unreadCount: page.unread_count ?? 0,
  };
}

export async function markNotificationsSeen(seenAt: string): Promise<void> {
  await http.post("/notifications/seen", { seen_at: seenAt });
}

export function notificationsChannel(userId: string): string {
  return `notifications:${userId}`;
}

interface NotificationFrame {
  type?: unknown;
  data?: unknown;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

export function parseNotificationFrame(raw: string): NotificationItem | null {
  let frame: NotificationFrame;
  try {
    const parsed: unknown = JSON.parse(raw);
    if (!isRecord(parsed)) {
      return null;
    }
    frame = parsed;
  } catch {
    return null;
  }
  if (frame.type !== "notification" || typeof frame.data !== "string") {
    return null;
  }
  let item: unknown;
  try {
    item = JSON.parse(frame.data);
  } catch {
    return null;
  }
  const parsed = notificationItemSchema.safeParse(item);
  return parsed.success ? (parsed.data as NotificationItem) : null;
}
