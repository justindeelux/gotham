import { computed, inject, provide, ref } from "vue";
import type { InjectionKey } from "vue";
import { useMessage } from "naive-ui";

import type { TeamRole } from "@/features/teams";
import { canManageMembers } from "@/features/teams";
import { useTeamsStore } from "@/features/teams";
import { listApplications } from "@/features/applications";
import { listDatabases } from "@/features/databases";
import type { NotificationChannel } from "@/features/notifications/api/notifications";
import type { NotificationEventKey } from "@/features/notifications/api/notifications";
import {
  describeChannelError,
  eventLabel,
} from "@/features/notifications/api/notifications";
import { channelText } from "@/features/notifications/api/notifications";
import { useNotificationsStore } from "@/features/notifications/stores/notifications";
import { canSubmitChannel } from "@/features/notifications/schemas/notifications";
import {
  allowedEvents,
  buildConfig,
  emptyChannelForm,
  keepDeliverableEvents,
  resourceScopeInput,
} from "@/features/notifications/utils/channelHelpers";
import type {
  ChannelForm,
  ChannelScope,
  ResourceOption,
} from "@/features/notifications/utils/channelHelpers";

function createChannelDialogState() {
  const message = useMessage();
  const channelsStore = useNotificationsStore();
  const teamsStore = useTeamsStore();

  const formOpen = ref(false);
  const editingId = ref("");
  const saving = ref(false);
  const formErrorRaw = ref<unknown>(null);
  const formError = computed<string | null>(() =>
    formErrorRaw.value === null
      ? null
      : describeChannelError(formErrorRaw.value),
  );
  const secretOriginals = ref({ webhook_url: "", bot_token: "", password: "" });

  const form = ref<ChannelForm>(emptyChannelForm());

  const resourceApplications = ref<ResourceOption[]>([]);
  const resourceDatabases = ref<ResourceOption[]>([]);
  const loadedResourcesTeamId = ref("");
  let resourcesReadToken = 0;

  const applicationsUnavailable = ref(false);
  const databasesUnavailable = ref(false);

  const outOfScopeEvents = computed<NotificationEventKey[]>(() => {
    const deliverable = allowedEvents(form.value.resourceType);
    return form.value.events.filter((event) => !deliverable.includes(event));
  });

  const eventOptions = computed<
    Array<{ label: string; value: NotificationEventKey }>
  >(() => {
    const deliverable = allowedEvents(form.value.resourceType);
    const options = deliverable.map((event) => ({
      label: eventLabel(event),
      value: event,
    }));
    for (const event of outOfScopeEvents.value) {
      options.push({ label: eventLabel(event), value: event });
    }
    return options;
  });

  const outOfScopeWarning = computed<string>(() => {
    const events = outOfScopeEvents.value;
    if (events.length === 0) {
      return "";
    }
    const names = events.map((event) => eventLabel(event)).join(", ");
    return channelText(
      "notifications.dialog.outOfScope",
      "This channel is subscribed to {names}, which its resource scope " +
        "cannot deliver. The stored subscription is kept unchanged unless " +
        "you edit the events or the scope.",
      { names },
    );
  });

  const scopeOptions = computed<
    Array<{ label: string; value: ChannelScope; disabled?: boolean }>
  >(() => [
    {
      label: channelText(
        "notifications.dialog.scopeTeamWide",
        "Team-wide (all resources)",
      ),
      value: "",
    },
    {
      label: applicationsUnavailable.value
        ? channelText(
            "notifications.dialog.scopeAppUnavailable",
            "Application (unavailable)",
          )
        : channelText("notifications.dialog.scopeApp", "Application"),
      value: "application",
      disabled: applicationsUnavailable.value,
    },
    {
      label: databasesUnavailable.value
        ? channelText(
            "notifications.dialog.scopeDbUnavailable",
            "Database (unavailable)",
          )
        : channelText("notifications.dialog.scopeDb", "Database"),
      value: "database",
      disabled: databasesUnavailable.value,
    },
  ]);

  const unavailableScopeHint = computed<string>(() => {
    if (applicationsUnavailable.value && databasesUnavailable.value) {
      return channelText(
        "notifications.dialog.unavailableBoth",
        "Applications and databases are unavailable on this control plane; " +
          "the channel can stay team-wide.",
      );
    }
    if (applicationsUnavailable.value) {
      return channelText(
        "notifications.dialog.unavailableApps",
        "Applications are turned off on this control plane; " +
          "a database scope is still available. The platform administrator " +
          "must enable them.",
      );
    }
    if (databasesUnavailable.value) {
      return channelText(
        "notifications.dialog.unavailableDbs",
        "Databases are turned off on this control plane; " +
          "an application scope is still available. The platform administrator " +
          "must enable them.",
      );
    }
    return "";
  });

  const eventsHint = computed<string>(() => {
    switch (form.value.resourceType) {
      case "application":
        return channelText(
          "notifications.dialog.eventsHintApp",
          "Applications deliver deploy events; backup events cannot reach " +
            "this channel.",
        );
      case "database":
        return channelText(
          "notifications.dialog.eventsHintDb",
          "Databases deliver backup events; deploy events cannot reach " +
            "this channel.",
        );
      default:
        return channelText(
          "notifications.dialog.eventsHintTeam",
          "A team-wide channel receives every selected event.",
        );
    }
  });

  const teamOptions = computed<Array<{ label: string; value: string }>>(() => {
    const personal = channelText("teams.page.personal", "personal");
    return teamsStore.teams.map((team) => ({
      label: team.is_personal ? `${team.name} (${personal})` : team.name,
      value: team.id,
    }));
  });

  const activeRole = computed<TeamRole | null>(() => teamsStore.activeTeam?.role ?? null);
  const canMutate = computed<boolean>(
    () => teamsStore.featureDisabled || canManageMembers(activeRole.value),
  );

  const editing = computed<boolean>(() => editingId.value !== "");

  function selectScope(scope: string): void {
    const next: ChannelScope =
      scope === "application" || scope === "database" ? scope : "";
    form.value.resourceType = next;
    form.value.resourceId = "";
    form.value.events = keepDeliverableEvents(next, form.value.events);
  }

  const resourcePickerOptions = computed<Array<{ label: string; value: string }>>(
    () => {
      const options = (
        form.value.resourceType === "application"
          ? resourceApplications.value
          : resourceDatabases.value
      ).map((resource) => ({ label: resource.name, value: resource.id }));
      const current = form.value.resourceId;
      if (current !== "" && !options.some((option) => option.value === current)) {
        options.unshift({
          label: channelText(
            "notifications.dialog.missingResource",
            "{id} (missing)",
            { id: current },
          ),
          value: current,
        });
      }
      return options;
    },
  );

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

  const canSubmit = computed<boolean>(() => canSubmitChannel(form.value));

  function openCreate(): void {
    editingId.value = "";
    form.value = emptyChannelForm();
    secretOriginals.value = { webhook_url: "", bot_token: "", password: "" };
    formErrorRaw.value = null;
    formOpen.value = true;
  }

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
      events: [...channel.events],
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
    formErrorRaw.value = null;
    formOpen.value = true;
  }

  async function handleSave(): Promise<void> {
    saving.value = true;
    formErrorRaw.value = null;
    try {
      if (editing.value) {
        await channelsStore.update(editingId.value, {
          name: form.value.name.trim(),
          enabled: form.value.enabled,
          events: [...form.value.events],
          ...resourceScopeInput(form.value),
          config: buildConfig(form.value, secretOriginals.value),
        });
        message.success(channelText("notifications.toast.updated", "Channel updated"));
      } else {
        await channelsStore.create({
          name: form.value.name.trim(),
          kind: form.value.kind,
          enabled: form.value.enabled,
          events: [...form.value.events],
          ...resourceScopeInput(form.value),
          config: buildConfig(form.value, secretOriginals.value),
        });
        message.success(channelText("notifications.toast.created", "Channel created"));
      }
      formOpen.value = false;
    } catch (error) {
      formErrorRaw.value = error;
    } finally {
      saving.value = false;
    }
  }

  function resolveResourceName(resourceType: string, resourceId: string): string | undefined {
    const resources =
      resourceType === "application" ? resourceApplications.value : resourceDatabases.value;
    return resources.find((resource) => resource.id === resourceId)?.name;
  }

  return {
    formOpen,
    editing,
    saving,
    formError,
    form,
    eventOptions,
    outOfScopeWarning,
    eventsHint,
    scopeOptions,
    unavailableScopeHint,
    resourcePickerOptions,
    canSubmit,
    teamOptions,
    activeRole,
    canMutate,
    resourceApplications,
    resourceDatabases,
    loadResources,
    selectScope,
    openCreate,
    openEdit,
    handleSave,
    resolveResourceName,
  };
}

export type ChannelDialogState = ReturnType<typeof createChannelDialogState>;

const channelDialogKey: InjectionKey<ChannelDialogState> =
  Symbol("notifications.channel-dialog");

export function provideChannelDialog(): ChannelDialogState {
  const state = createChannelDialogState();
  provide(channelDialogKey, state);
  return state;
}

export function useChannelDialog(): ChannelDialogState {
  const state = inject(channelDialogKey);
  if (!state) {
    throw new Error("useChannelDialog must be used inside a page providing it.");
  }
  return state;
}
