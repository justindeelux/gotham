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

/**
 * Channel dialog state is per page instance (created by provideChannelDialog
 * in the page, shared via inject): typed secrets, the draft, the resource
 * lists and the read token never outlive the page.
 */
function createChannelDialogState() {
  const message = useMessage();
  const channelsStore = useNotificationsStore();
  const teamsStore = useTeamsStore();

  const formOpen = ref(false);
  const editingId = ref("");
  const saving = ref(false);
  /**
   * Raw failure behind the dialog alert. The display string derives from it
   * plus the current locale, so a language switch refreshes a retained
   * alert without losing the typed draft.
   */
  const formErrorRaw = ref<unknown>(null);
  const formError = computed<string | null>(() =>
    formErrorRaw.value === null
      ? null
      : describeChannelError(formErrorRaw.value),
  );
  /** Masked secrets as read, so an untouched field is never sent back. */
  const secretOriginals = ref({ webhook_url: "", bot_token: "", password: "" });

  const form = ref<ChannelForm>(emptyChannelForm());

  /** Applications of the active team, offered when the scope is one app. */
  const resourceApplications = ref<ResourceOption[]>([]);
  /** Databases of the active team, offered when the scope is one database. */
  const resourceDatabases = ref<ResourceOption[]>([]);
  /** Team the resource lists belong to; a switch drops them. */
  const loadedResourcesTeamId = ref("");
  /** Token of the newest resource read; a stale response never writes state. */
  let resourcesReadToken = 0;

  /** applicationsUnavailable is true when the picker's app list cannot load. */
  const applicationsUnavailable = ref(false);
  /** databasesUnavailable is true when the picker's database list cannot load. */
  const databasesUnavailable = ref(false);

  /** outOfScopeEvents lists stored events the current scope cannot deliver. */
  const outOfScopeEvents = computed<NotificationEventKey[]>(() => {
    const deliverable = allowedEvents(form.value.resourceType);
    return form.value.events.filter((event) => !deliverable.includes(event));
  });

  /**
   * Event options the current scope can deliver, plus any stored event outside
   * those rules. The extra keys keep their label and stay removable, but once
   * removed they disappear from the list and cannot be re-selected — a dead
   * combination cannot be built here, only preserved from older data.
   */
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

  /** outOfScopeWarning explains a stored subscription its scope cannot deliver. */
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

  /**
   * Scope choices: a feature whose list is unavailable is disabled and labelled,
   * so the other scope (or team-wide) stays selectable.
   */
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

  /** unavailableScopeHint names the resource features that cannot be offered. */
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
        "Applications are unavailable on this control plane " +
          "(FEATURE_APPLICATIONS=false); a database scope is still available.",
      );
    }
    if (databasesUnavailable.value) {
      return channelText(
        "notifications.dialog.unavailableDbs",
        "Databases are unavailable on this control plane " +
          "(FEATURE_DATABASES=false); an application scope is still available.",
      );
    }
    return "";
  });

  /** eventsHint explains the event restriction of the selected scope. */
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
    // The personal marker reuses the teams catalog so it refreshes on a
    // language switch; the team name itself is user data and stays raw.
    const personal = channelText("teams.page.personal", "personal");
    return teamsStore.teams.map((team) => ({
      label: team.is_personal ? `${team.name} (${personal})` : team.name,
      value: team.id,
    }));
  });

  const activeRole = computed<TeamRole | null>(() => teamsStore.activeTeam?.role ?? null);
  /** Without a teams surface the backend still enforces the role. */
  const canMutate = computed<boolean>(
    () => teamsStore.featureDisabled || canManageMembers(activeRole.value),
  );

  const editing = computed<boolean>(() => editingId.value !== "");

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
  const canSubmit = computed<boolean>(() => canSubmitChannel(form.value));

  /** openCreate resets the modal for a new channel. */
  function openCreate(): void {
    editingId.value = "";
    form.value = emptyChannelForm();
    secretOriginals.value = { webhook_url: "", bot_token: "", password: "" };
    formErrorRaw.value = null;
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
      // The stored subscription is preserved exactly: an unrelated save must
      // never expand or normalize events the operator did not touch. The form
      // warns when the scope cannot deliver a stored key; changing the scope or
      // the events is the deliberate path that re-constrains them.
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

  /** handleSave creates or updates the channel. */
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

  /** resolveResourceName maps a stored scope pair onto a display name. */
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

/**
 * provideChannelDialog creates the dialog state for one page mount.
 * Call once in the page; descendants share it through useChannelDialog.
 */
export function provideChannelDialog(): ChannelDialogState {
  const state = createChannelDialogState();
  provide(channelDialogKey, state);
  return state;
}

/** useChannelDialog shares the page instance; call in descendant components. */
export function useChannelDialog(): ChannelDialogState {
  const state = inject(channelDialogKey);
  if (!state) {
    throw new Error("useChannelDialog must be used inside a page providing it.");
  }
  return state;
}
