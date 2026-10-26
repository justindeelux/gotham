import { z } from "zod";

export const notificationItemSchema = z.object({
  id: z.string(),
  event_key: z.string(),
  resource_type: z.string().optional(),
  resource_id: z.string().optional(),
  project_id: z.string().optional(),
  name: z.string(),
  host: z.string(),
  error: z.string(),
  created_at: z.string(),
});

export const notificationInboxSchema = z.object({
  notifications: z.array(notificationItemSchema),
  unread_count: z.number(),
});
