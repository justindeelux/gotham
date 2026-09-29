import { http } from "./http";
import { isApiError } from "./servers";

/**
 * Typed client for the notification-channel routes served by
 * `internal/notifications` (see Mount in routes.go for the contract):
 *
 *   GET    /notification-channels          POST   /notification-channels
 *   GET    /notification-channels/{id}     PATCH  /notification-channels/{id}
 *   DELETE /notification-channels/{id}     POST   /notification-channels/{id}/test
 *
 * Channels are team-scoped: the control plane resolves the active team from
 * the optional `X-Team-Id` header (the caller's personal team when absent), so
 * every call here carries the team selected in the UI.
 *
 * Secrets are write-only: reads return them masked (at most the last four
 * characters) with `secrets_configured` telling a client one is stored. An
 * update replaces a secret only when the request carries a new value.
 *
 * Disabled (FEATURE_NOTIFICATIONS=false) every route is unmounted: reads
 * answer 404 and the surface is hidden rather than rendered as an error.
 */

/** Transport of one channel (Kind in internal/notifications/model.go). */
export type NotificationKind = "discord" | "slack" | "telegram" | "email";

/** Subscribable event keys (EventKey in internal/notifications/model.go). */
export type NotificationEventKey =
  | "deploy_success"
  | "deploy_failure"
  | "backup_success"
  | "backup_failure";

/** Every event key in subscription order (AllEvents). */
export const allNotificationEvents: NotificationEventKey[] = [
  "deploy_success",
  "deploy_failure",
  "backup_success",
  "backup_failure",
];

/**
 * Transport configuration. Which fields are required depends on the channel
 * kind; secret fields (webhook_url, bot_token, password) are masked on read.
 */
export interface ChannelConfig {
  webhook_url?: string;
  bot_token?: string;
  chat_id?: string;
  host?: string;
  port?: number;
  username?: string;
  password?: string;
  from?: string;
  to?: string[];
}

/** One channel as the API returns it (ChannelView in model.go). */
export interface NotificationChannel {
  id: string;
  team_id: string;
  name: string;
  kind: NotificationKind;
  enabled: boolean;
  events: NotificationEventKey[];
  resource_type?: string;
  resource_id?: string;
  config: ChannelConfig;
  /** True when a secret is stored; the value itself is never returned. */
  secrets_configured: boolean;
  created_at: string;
  updated_at: string;
}

/**
 * Body of the create/update endpoints. On update a zero field leaves the
 * stored value unchanged and a secret is replaced only when a new value is
 * named; the kind is immutable after creation.
 */
export interface ChannelInput {
  name?: string;
  kind?: NotificationKind;
  enabled?: boolean;
  events?: NotificationEventKey[];
  config?: ChannelConfig;
}

/** Answer of the send-test action. It never carries the channel's secrets. */
export interface TestResult {
  ok: boolean;
  message: string;
}

/** Wire envelopes. */
interface ChannelEnvelope {
  channel: NotificationChannel;
}
interface ChannelListEnvelope {
  channels: NotificationChannel[];
}
interface TestEnvelope {
  check: TestResult;
}

/** teamHeaders scopes a call to one team; empty means the personal team. */
function teamHeaders(teamId: string): Record<string, string> {
  return teamId ? { "X-Team-Id": teamId } : {};
}

/** listChannels returns one team's channels, oldest first. */
export async function listChannels(
  teamId: string,
): Promise<NotificationChannel[]> {
  const response = await http.get<ChannelListEnvelope>("/notification-channels", {
    headers: teamHeaders(teamId),
  });
  return response.data.channels ?? [];
}

/** createChannel stores a channel in one team. */
export async function createChannel(
  teamId: string,
  input: ChannelInput,
): Promise<NotificationChannel> {
  const response = await http.post<ChannelEnvelope>(
    "/notification-channels",
    input,
    { headers: teamHeaders(teamId) },
  );
  return response.data.channel;
}

/** updateChannel patches one channel; omitted fields stay unchanged. */
export async function updateChannel(
  teamId: string,
  id: string,
  input: ChannelInput,
): Promise<NotificationChannel> {
  const response = await http.patch<ChannelEnvelope>(
    `/notification-channels/${id}`,
    input,
    { headers: teamHeaders(teamId) },
  );
  return response.data.channel;
}

/** deleteChannel removes one channel. */
export async function deleteChannel(teamId: string, id: string): Promise<void> {
  await http.delete(`/notification-channels/${id}`, {
    headers: teamHeaders(teamId),
  });
}

/** testChannel delivers a synthetic event and reports the outcome. */
export async function testChannel(
  teamId: string,
  id: string,
): Promise<TestResult> {
  const response = await http.post<TestEnvelope>(
    `/notification-channels/${id}/test`,
    {},
    { headers: teamHeaders(teamId) },
  );
  return response.data.check;
}

/** kindLabel renders a channel kind as display text. */
export function kindLabel(kind: NotificationKind): string {
  switch (kind) {
    case "discord":
      return "Discord webhook";
    case "slack":
      return "Slack incoming webhook";
    case "telegram":
      return "Telegram bot";
    case "email":
      return "Email (SMTP)";
    default:
      return kind;
  }
}

/** eventLabel renders an event key as display text. */
export function eventLabel(event: NotificationEventKey): string {
  switch (event) {
    case "deploy_success":
      return "Deploy succeeded";
    case "deploy_failure":
      return "Deploy failed";
    case "backup_success":
      return "Backup succeeded";
    case "backup_failure":
      return "Backup failed";
    default:
      return event;
  }
}

/** isFeatureDisabled reports whether an error is the notifications 404. */
export function isFeatureDisabled(error: unknown): boolean {
  return isApiError(error) && error.status === 404;
}

/** describeChannelError maps a thrown error to a user-facing message. */
export function describeChannelError(error: unknown): string {
  if (isApiError(error)) {
    if (error.status === 401) {
      return "Your session expired. Please sign in again.";
    }
    if (error.status === 403) {
      return "Your team role does not allow this action.";
    }
    if (error.status === 404) {
      return (
        "Notification channels are not enabled on this control plane " +
        "(FEATURE_NOTIFICATIONS=false)."
      );
    }
    if (error.status === 400) {
      return stripPrefix(error.message) || "Invalid channel configuration.";
    }
    return error.message || "Request failed";
  }
  if (error instanceof Error) {
    return error.message;
  }
  return "Something went wrong. Please try again.";
}

/** stripPrefix drops the domain package prefix from a backend message. */
function stripPrefix(message: string): string {
  return (message ?? "").replace(/^notifications:\s*/i, "").trim();
}
