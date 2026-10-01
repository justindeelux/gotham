<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NEmpty,
  NForm,
  NFormItem,
  NInput,
  NModal,
  NPopconfirm,
  NSelect,
  NSpace,
  NSpin,
  NSwitch,
  NTag,
  NText,
  useMessage,
} from "naive-ui";
import { computed, onMounted, ref, watch } from "vue";

import type {
  ChannelConfig,
  NotificationChannel,
  NotificationEventKey,
  NotificationKind,
  TestResult,
} from "../api/notifications";
import {
  allNotificationEvents,
  describeChannelError,
  eventLabel,
  kindLabel,
} from "../api/notifications";
import { listApplications } from "../api/applications";
import { listDatabases } from "../api/databases";
import type { TeamRole } from "../api/teams";
import { canManageMembers, roleLabel, roleTagType } from "../api/teams";
import { useNotificationsStore } from "../stores/notifications";
import { useTeamsStore } from "../stores/teams";
import { relativeTime } from "../utils/format";

/**
 * FE-8.1 (3) — notification channels.
 *
 * There is no mockup for this page; the layout follows the established
 * settings/form pattern and the channel cards of
 * docs/design/team-settings.html (which is the notifications reference).
 * Secrets are write-only: reads show the masked value the API returns, and an
 * update only carries a value the operator actually typed.
 */
const message = useMessage();
const channelsStore = useNotificationsStore();
const teamsStore = useTeamsStore();

/** The mutable config draft of the create/edit modal. */
interface ChannelForm {
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
type ChannelScope = "" | "application" | "database";

/** One selectable resource of the picker. */
interface ResourceOption {
  id: string;
  name: string;
}

const formOpen = ref(false);
const editingId = ref("");
const saving = ref(false);
const formError = ref<string | null>(null);
/** Masked secrets as read, so an untouched field is never sent back. */
const secretOriginals = ref({ webhook_url: "", bot_token: "", password: "" });

const form = ref<ChannelForm>(emptyForm());

const testingId = ref("");
const testResults = ref<Record<string, TestResult | null>>({});

const kindOptions: Array<{ label: string; value: NotificationKind }> = [
  { label: "Discord webhook", value: "discord" },
  { label: "Slack incoming webhook", value: "slack" },
  { label: "Telegram bot", value: "telegram" },
  { label: "Email (SMTP)", value: "email" },
];

/**
 * allowedEvents lists the event keys a scope can actually deliver:
 * applications emit deploys, databases emit backups, team-wide gets all.
 */
function allowedEvents(scope: ChannelScope): NotificationEventKey[] {
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
function keepDeliverableEvents(
  scope: ChannelScope,
  events: NotificationEventKey[],
): NotificationEventKey[] {
  const deliverable = allowedEvents(scope);
  const kept = deliverable.filter((event) => events.includes(event));
  return kept.length > 0 ? kept : deliverable;
}

/** Event options the current scope can deliver; a dead combination is absent. */
const eventOptions = computed<
  Array<{ label: string; value: NotificationEventKey }>
>(() =>
  allowedEvents(form.value.resourceType).map((event) => ({
    label: eventLabel(event),
    value: event,
  })),
);

/** applicationsUnavailable is true when the picker's app list cannot load. */
const applicationsUnavailable = ref(false);
/** databasesUnavailable is true when the picker's database list cannot load. */
const databasesUnavailable = ref(false);

/**
 * Scope choices: a feature whose list is unavailable is disabled and labelled,
 * so the other scope (or team-wide) stays selectable.
 */
const scopeOptions = computed<
  Array<{ label: string; value: ChannelScope; disabled?: boolean }>
>(() => [
  { label: "Team-wide (all resources)", value: "" },
  {
    label: applicationsUnavailable.value
      ? "Application (unavailable)"
      : "Application",
    value: "application",
    disabled: applicationsUnavailable.value,
  },
  {
    label: databasesUnavailable.value
      ? "Database (unavailable)"
      : "Database",
    value: "database",
    disabled: databasesUnavailable.value,
  },
]);

/** unavailableScopeHint names the resource features that cannot be offered. */
const unavailableScopeHint = computed<string>(() => {
  if (applicationsUnavailable.value && databasesUnavailable.value) {
    return "Applications and databases are unavailable on this control plane; the channel can stay team-wide.";
  }
  if (applicationsUnavailable.value) {
    return "Applications are unavailable on this control plane (FEATURE_APPLICATIONS=false); a database scope is still available.";
  }
  if (databasesUnavailable.value) {
    return "Databases are unavailable on this control plane (FEATURE_DATABASES=false); an application scope is still available.";
  }
  return "";
});

/** eventsHint explains the event restriction of the selected scope. */
const eventsHint = computed<string>(() => {
  switch (form.value.resourceType) {
    case "application":
      return "Applications deliver deploy events; backup events cannot reach this channel.";
    case "database":
      return "Databases deliver backup events; deploy events cannot reach this channel.";
    default:
      return "A team-wide channel receives every selected event.";
  }
});

/** Applications of the active team, offered when the scope is one app. */
const resourceApplications = ref<ResourceOption[]>([]);
/** Databases of the active team, offered when the scope is one database. */
const resourceDatabases = ref<ResourceOption[]>([]);
/** Team the resource lists belong to; a switch drops them. */
const loadedResourcesTeamId = ref("");
/** Token of the newest resource read; a stale response never writes state. */
let resourcesReadToken = 0;

const teamOptions = computed<Array<{ label: string; value: string }>>(() =>
  teamsStore.teams.map((team) => ({
    label: `${team.name}${team.is_personal ? " (personal)" : ""}`,
    value: team.id,
  })),
);

const activeRole = computed<TeamRole | null>(() => teamsStore.activeTeam?.role ?? null);
/** Without a teams surface the backend still enforces the role. */
const canMutate = computed<boolean>(
  () => teamsStore.featureDisabled || canManageMembers(activeRole.value),
);

const editing = computed<boolean>(() => editingId.value !== "");

/** emptyForm is the create draft: a Discord webhook with every event. */
function emptyForm(): ChannelForm {
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

/** selectScope switches the scope kind, drops the stale pick and re-constrains
 * the subscription so a channel that can never fire cannot be saved. */
function selectScope(scope: string): void {
  const next: ChannelScope =
    scope === "application" || scope === "database" ? scope : "";
  form.value.resourceType = next;
  form.value.resourceId = "";
  form.value.events = keepDeliverableEvents(next, form.value.events);
}

/**
 * resourcePickerOptions offers the resources of the selected scope. While
 * editing, a stored resource the list no longer carries is kept as a
 * "(missing)" option so an untouched scope survives the save.
 */
const resourcePickerOptions = computed<Array<{ label: string; value: string }>>(
  () => {
    const options = (
      form.value.resourceType === "application"
        ? resourceApplications.value
        : resourceDatabases.value
    ).map((resource) => ({ label: resource.name, value: resource.id }));
    const current = form.value.resourceId;
    if (current !== "" && !options.some((option) => option.value === current)) {
      options.unshift({ label: `${current} (missing)`, value: current });
    }
    return options;
  },
);

/**
 * loadResources reads the active team's applications and databases. A team
 * switch drops the previous team's lists first, and only the newest read may
 * write, so a late response cannot offer another team's resources. The two
 * lists load independently: a disabled feature on one endpoint (404) must not
 * hide the other scope's picker.
 */
async function loadResources(teamId: string): Promise<void> {
  const token = ++resourcesReadToken;
  const isCurrent = (): boolean => token === resourcesReadToken;
  if (loadedResourcesTeamId.value !== teamId) {
    resourceApplications.value = [];
    resourceDatabases.value = [];
    loadedResourcesTeamId.value = teamId;
  }
  const [applications, databases] = await Promise.allSettled([
    listApplications(teamId),
    listDatabases(teamId),
  ]);
  if (!isCurrent()) {
    return;
  }
  applicationsUnavailable.value = applications.status === "rejected";
  resourceApplications.value =
    applications.status === "fulfilled"
      ? applications.value.map(({ id, name }) => ({ id, name }))
      : [];
  databasesUnavailable.value = databases.status === "rejected";
  resourceDatabases.value =
    databases.status === "fulfilled"
      ? databases.value.map(({ id, name }) => ({ id, name }))
      : [];
}

/** canSubmit mirrors the backend rules: name and at least one event. */
const canSubmit = computed<boolean>(
  () =>
    form.value.name.trim() !== "" &&
    form.value.events.length > 0 &&
    (form.value.resourceType === "" || form.value.resourceId !== ""),
);

/** parseRecipients splits a comma/newline separated address list. */
function parseRecipients(raw: string): string[] {
  return raw
    .split(/[\s,]+/)
    .map((value) => value.trim())
    .filter((value) => value !== "");
}

/** openCreate resets the modal for a new channel. */
function openCreate(): void {
  editingId.value = "";
  form.value = emptyForm();
  secretOriginals.value = { webhook_url: "", bot_token: "", password: "" };
  formError.value = null;
  formOpen.value = true;
}

/** openEdit prefills the modal from a stored channel (secrets stay masked). */
function openEdit(channel: NotificationChannel): void {
  editingId.value = channel.id;
  secretOriginals.value = {
    webhook_url: channel.config.webhook_url ?? "",
    bot_token: channel.config.bot_token ?? "",
    password: channel.config.password ?? "",
  };
  const resourceType: ChannelScope =
    channel.resource_type === "application" ||
    channel.resource_type === "database"
      ? channel.resource_type
      : "";
  form.value = {
    name: channel.name,
    kind: channel.kind,
    enabled: channel.enabled,
    // A stored channel may predate the scope/event constraint; drop events
    // its scope can never deliver instead of resubmitting a dead combination.
    events: keepDeliverableEvents(resourceType, channel.events),
    resourceType,
    resourceId: channel.resource_id ?? "",
    webhook_url: channel.config.webhook_url ?? "",
    bot_token: channel.config.bot_token ?? "",
    chat_id: channel.config.chat_id ?? "",
    host: channel.config.host ?? "",
    port: String(channel.config.port ?? 587),
    username: channel.config.username ?? "",
    password: channel.config.password ?? "",
    from: channel.config.from ?? "",
    to: channel.config.to?.join(", ") ?? "",
  };
  formError.value = null;
  formOpen.value = true;
}

/**
 * buildConfig maps the draft onto the request config. A secret is only sent
 * when it differs from the masked value read back (an untouched masked field
 * must never overwrite the stored secret).
 */
function buildConfig(): ChannelConfig {
  const draft = form.value;
  const config: ChannelConfig = {};
  switch (draft.kind) {
    case "discord":
    case "slack":
      if (
        draft.webhook_url !== "" &&
        draft.webhook_url !== secretOriginals.value.webhook_url
      ) {
        config.webhook_url = draft.webhook_url;
      }
      break;
    case "telegram":
      if (
        draft.bot_token !== "" &&
        draft.bot_token !== secretOriginals.value.bot_token
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
        draft.password !== secretOriginals.value.password
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
function resourceScopeInput(): { resource_type: string; resource_id: string } {
  if (form.value.resourceType === "") {
    return { resource_type: "", resource_id: "" };
  }
  return {
    resource_type: form.value.resourceType,
    resource_id: form.value.resourceId,
  };
}

/** handleSave creates or updates the channel. */
async function handleSave(): Promise<void> {
  saving.value = true;
  formError.value = null;
  try {
    if (editing.value) {
      await channelsStore.update(editingId.value, {
        name: form.value.name.trim(),
        enabled: form.value.enabled,
        events: [...form.value.events],
        ...resourceScopeInput(),
        config: buildConfig(),
      });
      message.success("Channel updated");
    } else {
      await channelsStore.create({
        name: form.value.name.trim(),
        kind: form.value.kind,
        enabled: form.value.enabled,
        events: [...form.value.events],
        ...resourceScopeInput(),
        config: buildConfig(),
      });
      message.success("Channel created");
    }
    formOpen.value = false;
  } catch (error) {
    formError.value = describeChannelError(error);
  } finally {
    saving.value = false;
  }
}

/** handleToggle flips a channel's enabled flag in place. */
async function handleToggle(channel: NotificationChannel, enabled: boolean): Promise<void> {
  try {
    await channelsStore.update(channel.id, { enabled });
  } catch (error) {
    message.error(describeChannelError(error));
  }
}

/** handleDelete removes a channel after confirmation. */
async function handleDelete(channel: NotificationChannel): Promise<void> {
  try {
    await channelsStore.remove(channel.id);
    message.success(`Deleted ${channel.name}`);
  } catch (error) {
    message.error(describeChannelError(error));
  }
}

/** handleTest delivers a synthetic event and records the outcome. */
async function handleTest(channel: NotificationChannel): Promise<void> {
  testingId.value = channel.id;
  try {
    const result = await channelsStore.test(channel.id);
    testResults.value = { ...testResults.value, [channel.id]: result };
  } catch (error) {
    testResults.value = {
      ...testResults.value,
      [channel.id]: { ok: false, message: describeChannelError(error) },
    };
  } finally {
    testingId.value = "";
  }
}

/** configSummary renders the non-secret routing facts of a channel. */
function configSummary(channel: NotificationChannel): string {
  const config = channel.config;
  switch (channel.kind) {
    case "discord":
    case "slack":
      return config.webhook_url ? `webhook ${config.webhook_url}` : "webhook not configured";
    case "telegram":
      return `chat ${config.chat_id || "—"}`;
    case "email":
      return `${config.host || "—"}:${config.port || 587}${
        config.to && config.to.length > 0 ? ` → ${config.to.join(", ")}` : ""
      }`;
    default:
      return "";
  }
}

/** scopeLabel renders the channel's resource scope for the card header. */
function scopeLabel(channel: NotificationChannel): string {
  if (!channel.resource_type || !channel.resource_id) {
    return "Team-wide";
  }
  const resources =
    channel.resource_type === "application"
      ? resourceApplications.value
      : resourceDatabases.value;
  const name =
    resources.find((resource) => resource.id === channel.resource_id)?.name ??
    channel.resource_id;
  return channel.resource_type === "application" ? `App: ${name}` : `Database: ${name}`;
}

watch(
  () => teamsStore.activeTeamId,
  () => {
    void channelsStore.fetchChannels().catch(() => undefined);
    void loadResources(teamsStore.activeTeamId);
  },
);

onMounted(async () => {
  const selectionBeforeLoad = teamsStore.activeTeamId;
  await teamsStore.ensureTeams();
  // The selection watcher already reloaded the list when ensureTeams changed
  // the active team; only an unchanged selection needs an explicit first read.
  if (teamsStore.activeTeamId === selectionBeforeLoad) {
    await Promise.all([
      channelsStore.fetchChannels().catch(() => undefined),
      loadResources(teamsStore.activeTeamId),
    ]);
  }
});
</script>

<template>
  <div class="notifications-page">
    <div class="page-head">
      <div>
        <p class="eyebrow">Team · Notifications</p>
        <h1>Notification channels</h1>
        <p class="page-desc">
          Deploy and backup events are delivered to the team's channels:
          Discord and Slack webhooks, a Telegram bot, or SMTP email. Channels
          are team-scoped, and secrets are write-only — reads only ever show a
          masked value.
        </p>
      </div>
      <div class="page-actions">
        <NButton
          v-if="canMutate"
          type="primary"
          :disabled="channelsStore.featureDisabled"
          @click="openCreate"
        >
          New channel
        </NButton>
      </div>
    </div>

    <NCard v-if="channelsStore.featureDisabled" title="Notifications unavailable">
      <NEmpty
        description="Notification channels are not enabled on this control plane (FEATURE_NOTIFICATIONS=false)."
      />
    </NCard>

    <template v-else>
      <NCard v-if="!teamsStore.featureDisabled" title="Team scope">
        <NSpace align="center" :size="12" wrap>
          <NSelect
            :value="teamsStore.activeTeamId"
            :options="teamOptions"
            filterable
            style="width: 260px"
            aria-label="Notification team"
            @update:value="(value: string) => teamsStore.selectTeam(value)"
          />
          <NTag
            v-if="activeRole"
            :type="roleTagType(activeRole)"
            size="small"
            round
          >
            your role: {{ roleLabel(activeRole) }}
          </NTag>
          <NText depth="3">
            Channels below belong to this team. A read-only role can look but
            not change anything.
          </NText>
        </NSpace>
      </NCard>

      <NAlert
        v-if="channelsStore.error"
        type="error"
        :show-icon="true"
        data-testid="channels-error"
      >
        {{ channelsStore.error }}
      </NAlert>

      <NSpin :show="channelsStore.loading">
        <NSpace vertical :size="16">
          <NCard
            v-for="channel in channelsStore.channels"
            :key="channel.id"
          >
            <template #header>
              <NSpace align="center" :size="10">
                <NText strong>{{ channel.name }}</NText>
                <NTag size="small" round>{{ kindLabel(channel.kind) }}</NTag>
                <NTag size="small" round>{{ scopeLabel(channel) }}</NTag>
                <NTag
                  :type="channel.enabled ? 'success' : 'default'"
                  size="small"
                  round
                >
                  {{ channel.enabled ? "enabled" : "disabled" }}
                </NTag>
                <NTag
                  v-if="channel.secrets_configured"
                  size="small"
                  round
                  type="info"
                >
                  secret configured
                </NTag>
              </NSpace>
            </template>
            <template #header-extra>
              <NSpace align="center" :size="8">
                <NText depth="3" class="small">Enabled</NText>
                <NSwitch
                  :value="channel.enabled"
                  :disabled="!canMutate"
                  :aria-label="`Enable ${channel.name}`"
                  @update:value="(value: boolean) => void handleToggle(channel, value)"
                />
              </NSpace>
            </template>

            <div class="channel-body" :data-channel="channel.name">
              <NSpace vertical :size="12">
                <NText depth="3" class="mono">{{ configSummary(channel) }}</NText>
                <NSpace :size="8" wrap>
                  <NTag
                    v-for="event in channel.events"
                    :key="event"
                    size="small"
                    :bordered="false"
                  >
                    {{ eventLabel(event) }}
                  </NTag>
                </NSpace>
                <NSpace align="center" :size="12" wrap>
                  <NButton
                    size="small"
                    secondary
                    :disabled="!canMutate"
                    :loading="testingId === channel.id"
                    @click="void handleTest(channel)"
                  >
                    Send test
                  </NButton>
                  <NText
                    v-if="testResults[channel.id]"
                    :type="testResults[channel.id]?.ok ? 'success' : 'error'"
                    depth="1"
                    class="small"
                    data-test-channel-result
                  >
                    {{ testResults[channel.id]?.ok ? "Test delivered: " : "Test failed: " }}{{
                      testResults[channel.id]?.message
                    }}
                  </NText>
                  <NText v-else depth="3" class="small">
                    No test sent yet.
                  </NText>
                </NSpace>
                <NSpace align="center" :size="12">
                  <NButton
                    size="small"
                    :disabled="!canMutate"
                    @click="openEdit(channel)"
                  >
                    Edit
                  </NButton>
                  <NPopconfirm
                    :positive-button-props="{ type: 'error' }"
                    @positive-click="handleDelete(channel)"
                  >
                    <template #trigger>
                      <NButton
                        size="small"
                        ghost
                        type="error"
                        :disabled="!canMutate"
                      >
                        Delete
                      </NButton>
                    </template>
                    Delete channel "{{ channel.name }}"? Deploy and backup
                    notifications stop immediately.
                  </NPopconfirm>
                  <NText depth="3" class="small">
                    Updated {{ relativeTime(channel.updated_at) }}
                  </NText>
                </NSpace>
              </NSpace>
            </div>
          </NCard>

          <NCard
            v-if="
              !channelsStore.loading &&
              channelsStore.loaded &&
              channelsStore.channels.length === 0 &&
              !channelsStore.error
            "
          >
            <NEmpty description="No channels for this team yet.">
              <template v-if="canMutate" #extra>
                <NButton type="primary" @click="openCreate">
                  Create the first channel
                </NButton>
              </template>
            </NEmpty>
          </NCard>
        </NSpace>
      </NSpin>
    </template>

    <NModal
      v-model:show="formOpen"
      preset="card"
      :title="editing ? 'Edit notification channel' : 'New notification channel'"
      style="width: 560px; max-width: 94vw"
    >
      <NSpace vertical :size="12">
        <NAlert v-if="formError" type="error" :show-icon="true">
          {{ formError }}
        </NAlert>
        <NForm label-placement="top" :show-feedback="false">
          <NFormItem label="Name">
            <NInput
              v-model:value="form.name"
              placeholder="e.g. Deploy alerts"
              aria-label="Channel name"
            />
          </NFormItem>
          <NFormItem label="Kind">
            <NSelect
              v-model:value="form.kind"
              :options="kindOptions"
              :disabled="editing"
              aria-label="Channel kind"
            />
          </NFormItem>

          <template v-if="form.kind === 'discord' || form.kind === 'slack'">
            <NFormItem label="Webhook URL">
              <NInput
                v-model:value="form.webhook_url"
                type="password"
                show-password-on="click"
                autocomplete="new-password"
                :placeholder="
                  editing
                    ? 'Leave the masked value to keep the stored URL'
                    : 'https://discord.com/api/webhooks/…'
                "
                aria-label="Webhook URL"
              />
            </NFormItem>
          </template>

          <template v-else-if="form.kind === 'telegram'">
            <NFormItem label="Bot token">
              <NInput
                v-model:value="form.bot_token"
                type="password"
                show-password-on="click"
                autocomplete="new-password"
                :placeholder="
                  editing ? 'Leave the masked value to keep the stored token' : '123456:ABC-…'
                "
                aria-label="Bot token"
              />
            </NFormItem>
            <NFormItem label="Chat ID">
              <NInput
                v-model:value="form.chat_id"
                placeholder="-100…"
                aria-label="Chat ID"
              />
            </NFormItem>
          </template>

          <template v-else>
            <NFormItem label="SMTP host">
              <NInput
                v-model:value="form.host"
                placeholder="smtp.example.com"
                aria-label="SMTP host"
              />
            </NFormItem>
            <NFormItem label="SMTP port">
              <NInput
                v-model:value="form.port"
                type="text"
                inputmode="numeric"
                placeholder="587"
                aria-label="SMTP port"
              />
            </NFormItem>
            <NFormItem label="SMTP username">
              <NInput
                v-model:value="form.username"
                placeholder="ops@example.com"
                aria-label="SMTP username"
              />
            </NFormItem>
            <NFormItem label="SMTP password">
              <NInput
                v-model:value="form.password"
                type="password"
                show-password-on="click"
                autocomplete="new-password"
                :placeholder="
                  editing
                    ? 'Leave the masked value to keep the stored password'
                    : 'Leave blank for an open relay'
                "
                aria-label="SMTP password"
              />
            </NFormItem>
            <NFormItem label="From address">
              <NInput
                v-model:value="form.from"
                placeholder="Gotham <ops@example.com>"
                aria-label="From address"
              />
            </NFormItem>
            <NFormItem label="Recipients">
              <NInput
                v-model:value="form.to"
                type="textarea"
                :autosize="{ minRows: 2, maxRows: 4 }"
                placeholder="ops@example.com, oncall@example.com"
                aria-label="Recipients"
              />
            </NFormItem>
          </template>

          <NFormItem label="Events">
            <div class="field-stack">
              <NSelect
                v-model:value="form.events"
                multiple
                :options="eventOptions"
                placeholder="Select at least one event"
                aria-label="Events"
              />
              <NText v-if="form.events.length === 0" type="error" class="small">
                Select at least one event.
              </NText>
              <NText v-else depth="3" class="small">
                {{ eventsHint }}
              </NText>
            </div>
          </NFormItem>

          <NFormItem label="Resource scope">
            <div class="field-stack">
              <NSelect
                :value="form.resourceType"
                :options="scopeOptions"
                aria-label="Resource scope"
                @update:value="(value: string) => selectScope(value)"
              />
              <NSelect
                v-if="form.resourceType !== ''"
                v-model:value="form.resourceId"
                :options="resourcePickerOptions"
                filterable
                :placeholder="
                  form.resourceType === 'application'
                    ? 'Select an application'
                    : 'Select a database'
                "
                aria-label="Resource"
              />
              <NText v-if="unavailableScopeHint !== ''" depth="3" class="small">
                {{ unavailableScopeHint }}
              </NText>
            </div>
          </NFormItem>

          <NFormItem label="Enabled">
            <NSwitch v-model:value="form.enabled" aria-label="Channel enabled" />
          </NFormItem>
        </NForm>
        <NText depth="3" class="small">
          Secrets travel once, are sealed server-side and are never displayed
          again — a read only returns the mask. A team-wide channel receives
          every selected event; a scoped channel only receives events of the
          selected resource.
        </NText>
      </NSpace>
      <template #footer>
        <NSpace justify="end" :size="8">
          <NButton @click="formOpen = false">Cancel</NButton>
          <NButton
            type="primary"
            :loading="saving"
            :disabled="!canSubmit"
            @click="void handleSave()"
          >
            {{ editing ? "Save" : "Create channel" }}
          </NButton>
        </NSpace>
      </template>
    </NModal>
  </div>
</template>

<style scoped>
.notifications-page {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}

.page-head {
  display: flex;
  align-items: flex-start;
  gap: var(--space-4);
  flex-wrap: wrap;
}

.eyebrow {
  font-family: var(--font-mono);
  font-size: 11px;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--muted);
  margin: 0 0 var(--space-2);
}

.page-head h1 {
  font-size: var(--text-2xl);
  line-height: 1.25;
  color: var(--fg-2);
  margin: 0 0 var(--space-2);
}

.page-desc {
  color: var(--muted);
  margin: 0;
  max-width: 72ch;
}

.page-actions {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex-wrap: wrap;
}

.mono {
  font-family: var(--font-mono);
}

.small {
  font-size: var(--text-xs);
}

.field-stack {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  width: 100%;
}

@media (max-width: 860px) {
  .page-actions {
    margin-left: 0;
  }
}
</style>
