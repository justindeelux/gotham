import { http, teamHeaders } from "@/shared/api/http";
import { isApiError, stripErrorPrefix } from "@/features/servers";
import { i18n } from "@/shared/i18n";

export type NotificationKind = "discord" | "slack" | "telegram" | "email";

export type NotificationEventKey =
  | "deploy_success"
  | "deploy_failure"
  | "backup_success"
  | "backup_failure";

export const allNotificationEvents: NotificationEventKey[] = [
  "deploy_success",
  "deploy_failure",
  "backup_success",
  "backup_failure",
];

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
  secrets_configured: boolean;
  created_at: string;
  updated_at: string;
}

export interface ChannelInput {
  name?: string;
  kind?: NotificationKind;
  enabled?: boolean;
  events?: NotificationEventKey[];
  resource_type?: string;
  resource_id?: string;
  config?: ChannelConfig;
}

export interface TestResult {
  ok: boolean;
  message: string;
}

interface ChannelEnvelope {
  channel: NotificationChannel;
}
interface ChannelListEnvelope {
  channels: NotificationChannel[];
}
interface TestEnvelope {
  check: TestResult;
}

export async function listChannels(
  teamId: string,
): Promise<NotificationChannel[]> {
  const response = await http.get<ChannelListEnvelope>("/notification-channels", {
    headers: teamHeaders(teamId),
  });
  return response.data.channels ?? [];
}

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

export async function deleteChannel(teamId: string, id: string): Promise<void> {
  await http.delete(`/notification-channels/${id}`, {
    headers: teamHeaders(teamId),
  });
}

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

export type DeliveryStatus = "success" | "failure";

export interface NotificationDelivery {
  id: string;
  channel_id: string;
  event_type: string;
  status: DeliveryStatus;
  error_summary?: string;
  created_at: string;
}

interface DeliveryListEnvelope {
  deliveries: NotificationDelivery[];
}

export async function listDeliveries(
  teamId: string,
  id: string,
): Promise<NotificationDelivery[]> {
  const response = await http.get<DeliveryListEnvelope>(
    `/notification-channels/${id}/deliveries`,
    { headers: teamHeaders(teamId) },
  );
  return response.data.deliveries ?? [];
}

export type TextParams = Record<string, string | number>;

export function channelText(
  key: string,
  fallback: string,
  params?: TextParams,
): string {
  const composer = i18n.global;
  if (composer.te(key)) {
    return String(composer.t(key, params ?? {}));
  }
  let out = fallback;
  for (const [name, value] of Object.entries(params ?? {})) {
    out = out.replaceAll(`{${name}}`, String(value));
  }
  return out;
}

export function kindLabel(kind: NotificationKind): string {
  switch (kind) {
    case "discord":
      return channelText("notifications.kinds.discord", "Discord webhook");
    case "slack":
      return channelText(
        "notifications.kinds.slack",
        "Slack incoming webhook",
      );
    case "telegram":
      return channelText("notifications.kinds.telegram", "Telegram bot");
    case "email":
      return channelText("notifications.kinds.email", "Email (SMTP)");
    default:
      return kind;
  }
}

export function eventLabel(event: NotificationEventKey): string {
  switch (event) {
    case "deploy_success":
      return channelText(
        "notifications.events.deploy_success",
        "Deploy succeeded",
      );
    case "deploy_failure":
      return channelText(
        "notifications.events.deploy_failure",
        "Deploy failed",
      );
    case "backup_success":
      return channelText(
        "notifications.events.backup_success",
        "Backup succeeded",
      );
    case "backup_failure":
      return channelText(
        "notifications.events.backup_failure",
        "Backup failed",
      );
    default:
      return event;
  }
}

export function deliveryEventLabel(eventType: string): string {
  if (eventType === "test") {
    return channelText("notifications.history.testEvent", "Test message");
  }
  if ((allNotificationEvents as string[]).includes(eventType)) {
    return eventLabel(eventType as NotificationEventKey);
  }
  return eventType;
}

export function deliveryStatusLabel(status: DeliveryStatus): string {
  return status === "success"
    ? channelText("notifications.history.delivered", "Delivered")
    : channelText("notifications.history.failed", "Failed");
}

export function isFeatureDisabled(error: unknown): boolean {
  return isApiError(error) && error.status === 404;
}

export function describeChannelError(error: unknown): string {
  if (isApiError(error)) {
    if (error.status === 401) {
      return channelText(
        "notifications.errors.sessionExpired",
        "Your session expired. Please sign in again.",
      );
    }
    if (error.status === 403) {
      return channelText(
        "notifications.errors.forbiddenRole",
        "Your team role does not allow this action.",
      );
    }
    if (error.status === 404) {
      return channelText(
        "notifications.errors.featureDisabled",
        "Notification channels are turned off on this control plane. " +
          "The platform administrator must enable them.",
      );
    }
    if (error.status === 400) {
      return (
        stripErrorPrefix(error.message) ||
        channelText(
          "notifications.errors.invalidConfig",
          "Invalid channel configuration.",
        )
      );
    }
    return withChannelStatusDiagnostic(stripErrorPrefix(error.message));
  }
  if (error instanceof Error) {
    return withChannelDiagnostic(stripErrorPrefix(error.message));
  }
  return channelText(
    "notifications.errors.unexpected",
    "Something went wrong. Please try again.",
  );
}

function withChannelStatusDiagnostic(raw: string): string {
  const summary = channelText(
    "notifications.errors.requestFailed",
    "Request failed",
  );
  if (raw === "" || raw === summary) {
    return summary;
  }
  const lead = summary.endsWith(".") ? summary.slice(0, -1) : summary;
  return `${lead}: ${raw}`;
}

function withChannelDiagnostic(raw: string): string {
  const summary = channelText(
    "notifications.errors.unexpected",
    "Something went wrong. Please try again.",
  );
  if (raw === "" || raw === summary) {
    return summary;
  }
  const lead = summary.endsWith(".") ? summary.slice(0, -1) : summary;
  return `${lead}: ${raw}`;
}

