import { allNotificationEvents } from "@/features/notifications/api/notifications";
import { channelText } from "@/features/notifications/api/notifications";
import type {
  ChannelConfig,
  NotificationChannel,
  NotificationEventKey,
  NotificationKind,
} from "@/features/notifications/api/notifications";

/** The mutable config draft of the create/edit modal. */
export interface ChannelForm {
  name: string;
  kind: NotificationKind;
  enabled: boolean;
  /** Subscribed event keys; the backend requires at least one. */
  events: NotificationEventKey[];
  /** Empty for a team-wide channel. */
  resourceType: ChannelScope;
  resourceId: string;
  webhook_url: string;
  bot_token: string;
  chat_id: string;
  host: string;
  port: string;
  username: string;
  password: string;
  from: string;
  /** Comma/space separated recipient list. */
  to: string;
}

/** Resource scope of a channel: team-wide or one resource. */
export type ChannelScope = "" | "application" | "database";

/** One selectable resource of the picker. */
export interface ResourceOption {
  id: string;
  name: string;
}

/** Masked secrets as read, so an untouched field is never sent back. */
export interface ChannelSecrets {
  webhook_url: string;
  bot_token: string;
  password: string;
}

/** emptyChannelForm is the create draft: a Discord webhook with every event. */
export function emptyChannelForm(): ChannelForm {
  return {
    name: "",
    kind: "discord",
    enabled: true,
    events: [...allNotificationEvents],
    resourceType: "",
    resourceId: "",
    webhook_url: "",
    bot_token: "",
    chat_id: "",
    host: "",
    port: "587",
    username: "",
    password: "",
    from: "",
    to: "",
  };
}

/**
 * allowedEvents lists the event keys a scope can actually deliver:
 * applications emit deploys, databases emit backups, team-wide gets all.
 */
export function allowedEvents(scope: ChannelScope): NotificationEventKey[] {
  switch (scope) {
    case "application":
      return allNotificationEvents.filter((event) => event.startsWith("deploy_"));
    case "database":
      return allNotificationEvents.filter((event) => event.startsWith("backup_"));
    default:
      return [...allNotificationEvents];
  }
}

/** keepDeliverableEvents narrows a selection to what the scope can deliver. */
export function keepDeliverableEvents(
  scope: ChannelScope,
  events: NotificationEventKey[],
): NotificationEventKey[] {
  const deliverable = allowedEvents(scope);
  const kept = deliverable.filter((event) => events.includes(event));
  return kept.length > 0 ? kept : deliverable;
}

/** parseRecipients splits a comma/newline separated address list. */
export function parseRecipients(raw: string): string[] {
  return raw
    .split(/[\s,]+/)
    .map((value) => value.trim())
    .filter((value) => value !== "");
}

/**
 * buildConfig maps the draft onto the request config. A secret is only sent
 * when it differs from the masked value read back (an untouched masked field
 * must never overwrite the stored secret).
 */
export function buildConfig(draft: ChannelForm, originals: ChannelSecrets): ChannelConfig {
  const config: ChannelConfig = {};
  switch (draft.kind) {
    case "discord":
    case "slack":
      if (
        draft.webhook_url !== "" &&
        draft.webhook_url !== originals.webhook_url
      ) {
        config.webhook_url = draft.webhook_url;
      }
      break;
    case "telegram":
      if (
        draft.bot_token !== "" &&
        draft.bot_token !== originals.bot_token
      ) {
        config.bot_token = draft.bot_token;
      }
      if (draft.chat_id !== "") {
        config.chat_id = draft.chat_id;
      }
      break;
    case "email": {
      if (draft.host !== "") {
        config.host = draft.host;
      }
      const port = Number(draft.port.trim());
      if (draft.port.trim() !== "" && Number.isFinite(port)) {
        config.port = port;
      }
      if (draft.username !== "") {
        config.username = draft.username;
      }
      if (
        draft.password !== "" &&
        draft.password !== originals.password
      ) {
        config.password = draft.password;
      }
      if (draft.from !== "") {
        config.from = draft.from;
      }
      const to = parseRecipients(draft.to);
      if (to.length > 0) {
        config.to = to;
      }
      break;
    }
  }
  return config;
}

/**
 * resourceScopeInput maps the scope draft onto the request pair. The empty
 * pair clears a stored override (both halves are always sent together).
 */
export function resourceScopeInput(
  draft: Pick<ChannelForm, "resourceType" | "resourceId">,
): { resource_type: string; resource_id: string } {
  if (draft.resourceType === "") {
    return { resource_type: "", resource_id: "" };
  }
  return {
    resource_type: draft.resourceType,
    resource_id: draft.resourceId,
  };
}

/**
 * configSummary renders the non-secret routing facts of a channel. Secret
 * material aside, every technical value (URLs, hosts, ports, addresses)
 * renders verbatim; only the framing words resolve in the current locale.
 */
export function configSummary(channel: NotificationChannel): string {
  const config = channel.config;
  switch (channel.kind) {
    case "discord":
    case "slack":
      return config.webhook_url
        ? channelText("notifications.configSummary.webhook", "webhook {value}", {
            value: config.webhook_url,
          })
        : channelText(
            "notifications.configSummary.webhookMissing",
            "webhook not configured",
          );
    case "telegram":
      return channelText("notifications.configSummary.chat", "chat {id}", {
        id: config.chat_id || channelText("notifications.configSummary.chatMissing", "—"),
      });
    case "email":
      return `${config.host || "—"}:${config.port || 587}${
        config.to && config.to.length > 0 ? ` → ${config.to.join(", ")}` : ""
      }`;
    default:
      return "";
  }
}

/**
 * scopeLabel renders the channel's resource scope for the card header.
 * The resource name itself is user data and stays verbatim.
 */
export function scopeLabel(
  channel: NotificationChannel,
  resolveName: (_resourceType: string, _resourceId: string) => string | undefined,
): string {
  if (!channel.resource_type || !channel.resource_id) {
    return channelText("notifications.scopeLabel.teamWide", "Team-wide");
  }
  const name =
    resolveName(channel.resource_type, channel.resource_id) ?? channel.resource_id;
  return channel.resource_type === "application"
    ? channelText("notifications.scopeLabel.app", "App: {name}", { name })
    : channelText("notifications.scopeLabel.database", "Database: {name}", {
        name,
      });
}
