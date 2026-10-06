import { useCopyText } from "@/shared/composables/useCopyText";
import { useMessage } from "naive-ui";
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import type { InjectionKey } from "vue";
import { i18n } from "@/shared/i18n";

/** t resolves a databases/common message in the current locale. */
function t(key: string, params?: Record<string, string | number>): string {
  return String(i18n.global.t(key, params ?? {}));
}
import { useRoute, useRouter } from "vue-router";

import { describeDatabaseError } from "@/features/databases/api/databases";
import type { Database, UpdateDatabaseInput } from "@/features/databases/api/databases";
import {
  databaseMessages,
  isDatabaseNameValid,
} from "@/features/databases/schemas/databases";
import {
  connectionScheme,
  dbContainerName,
  enginePorts,
  maskConnectionPassword,
} from "@/features/databases/utils/databaseConnection";
import { useMediaQuery } from "@/shared/composables/useMediaQuery";
import { resolveValidationMessage } from "@/shared/i18n";
import { useDatabasesStore } from "@/features/databases/stores/databases";
import { resolveEnvironmentScope } from "@/features/projects/utils/canonicalRoutes";
import { useProjectsStore } from "@/features/projects/stores/projects";
import { useServersStore } from "@/features/servers";

export type CredentialField = "username" | "password" | "database" | "root_password";

/**
 * useDatabaseDetail owns the detail page's overview state: the row, its
 * credentials, the connection string, lifecycle actions and the rename
 * dialog. Backup-tab state lives in useDatabaseBackups.
 */
export function useDatabaseDetail() {
  const route = useRoute();
  const router = useRouter();
  const message = useMessage();
  const databasesStore = useDatabasesStore();
  const projectsStore = useProjectsStore();
  const serversStore = useServersStore();
  const { copyText } = useCopyText();

  const dbId = computed<string>(() => String(route.params.id ?? ""));
  const renameOpen = ref(false);
  const renameValue = ref("");
  const renaming = ref(false);
  const revealed = ref(false);

  // The detail page owns its own load state: the list store's flags describe the
  // polling list, not this row's fetch, so a failed detail load would otherwise
  // render an empty shell with no explanation.
  const pageLoading = ref(false);
  const pageError = ref<string | null>(null);

  /** isNarrow stacks the two-column descriptions on small screens. */
  const isNarrow = useMediaQuery("(max-width: 640px)");

  /** descColumns renders descriptions in one column below 640px. */
  const descColumns = computed<number>(() => (isNarrow.value ? 1 : 2));

  const database = computed<Database | null>(
    () => databasesStore.databases.find((item) => item.id === dbId.value) ?? null,
  );

  /** shortId renders the head of the database UUID for the header. */
  const shortId = computed<string>(() => dbId.value.slice(0, 8));

  /** initials derives a two-letter avatar from the database name. */
  const initials = computed<string>(() =>
    (database.value?.name.slice(0, 2) ?? "DB").toUpperCase(),
  );

  /** engineLabel renders engine + version for the header. */
  const engineLabel = computed<string>(() => {
    if (!database.value) {
      return "";
    }
    return database.value.version
      ? `${database.value.engine}:${database.value.version}`
      : database.value.engine;
  });

  /** serverLabel resolves the node name for the overview. */
  const serverLabel = computed<string>(() => {
    if (!database.value) {
      return "—";
    }
    const server = serversStore.servers.find(
      (item) => item.id === database.value?.server_id,
    );
    return server ? `${server.name} · ${server.ip}` : database.value.server_id;
  });

  /** canStart/canStop/canRestart gate the lifecycle buttons by status. */
  const canStart = computed<boolean>(
    () => database.value?.status === "stopped" || database.value?.status === "error",
  );

  const canStop = computed<boolean>(
    () => database.value?.status === "running",
  );

  const canRestart = computed<boolean>(
    () => database.value?.status === "running" || database.value?.status === "stopped",
  );

  const credentials = computed(() =>
    databasesStore.credentialsOf(dbId.value),
  );

  /** copyCredential copies one cached credential field without narrowing issues. */
  function copyCredential(field: CredentialField, labelKey: string): void {
    const value = credentials.value?.[field] ?? "";
    if (value === "") {
      message.error(
        t("databases.detail.credentials.noCached", {
          label: String(t(labelKey)).toLowerCase(),
        }),
      );
      return;
    }
    void copyText(value, String(t(labelKey)));
  }

  /**
   * connectionString builds the full DSN (with the real password) for the Copy
   * button. The public endpoint is preferred when the database exposes a public
   * port, but only when the node address is actually resolved — substituting the
   * raw server UUID would copy an unroutable host. The internal fallback uses
   * the exact backend container-name rule.
   */
  const connectionString = computed<string>(() => {
    const db = database.value;
    const creds = credentials.value;
    if (!db || !creds) {
      return "";
    }
    const scheme = connectionScheme(db.engine);
    const server = serversStore.servers.find((item) => item.id === db.server_id);
    if (db.public_port > 0) {
      if (!server?.ip) {
        return "";
      }
      return `${scheme}://${creds.username}:${creds.password}@${server.ip}:${db.public_port}/${creds.database}`;
    }
    const internalPort = enginePorts[db.engine] ?? 0;
    return `${scheme}://${creds.username}:${creds.password}@${dbContainerName(db.name, db.id)}:${internalPort}/${creds.database}`;
  });

  /** nodeAddressUnknown renders instead of a DSN with an unroutable host. */
  const nodeAddressUnknown = computed<boolean>(() => {
    const db = database.value;
    if (!db || !credentials.value || db.public_port <= 0) {
      return false;
    }
    return !serversStore.servers.some((item) => item.id === db.server_id && item.ip);
  });

  /** connectionDisplay masks the password for on-screen rendering. */
  const connectionDisplay = computed<string>(() =>
    maskConnectionPassword(connectionString.value),
  );

  /** copyConnectionString copies the unmasked DSN. */
  function copyConnectionString(): void {
    if (nodeAddressUnknown.value) {
      message.error(t("databases.detail.credentials.nodeDsnError"));
      return;
    }
    if (connectionString.value === "") {
      message.error(t("databases.detail.credentials.notLoaded"));
      return;
    }
    void copyText(
      connectionString.value,
      String(t("databases.detail.credentials.connectionString")),
    );
  }

  /** fetchAll loads the row, its credentials and the node list. */
  async function fetchAll(): Promise<void> {
    if (!dbId.value) {
      pageError.value = t("databases.detail.noSelection");
      return;
    }
    pageLoading.value = true;
    pageError.value = null;
    try {
      const database = await databasesStore.fetchDatabase(dbId.value);
      // The row ids are the authority: a wrong project/environment in the
      // URL replaces it with the canonical nested URL instead of rendering
      // silently. The route watcher reloads from there.
      const canonical = resolveEnvironmentScope(
        { projectId: database.project_id, environmentId: database.environment_id },
        {
          projectId: String(route.params.projectId ?? ""),
          environmentId: String(route.params.environmentId ?? ""),
        },
      );
      if (canonical !== null) {
        await router.replace({
          name: "database-detail",
          params: { projectId: canonical.projectId, environmentId: canonical.environmentId, id: dbId.value },
        });
        pageLoading.value = false;
        return;
      }
    } catch (error) {
      pageError.value = describeDatabaseError(error);
      pageLoading.value = false;
      return;
    }
    pageLoading.value = false;
    try {
      await databasesStore.fetchCredentials(dbId.value);
    } catch {
      // The store already exposes the error; the alert renders it.
    }
    void serversStore.fetchServers().catch(() => undefined);
  }

  /** resetView clears the per-database UI when the route id changes. */
  function resetView(): void {
    revealed.value = false;
    renameOpen.value = false;
    renameValue.value = "";
    moveSaving.value = false;
    moveError.value = null;
  }

  /** handleLifecycle runs one start/stop/restart action. */
  async function handleLifecycle(
    action: "start" | "stop" | "restart",
  ): Promise<void> {
    try {
      if (action === "start") {
        await databasesStore.start(dbId.value);
      } else if (action === "stop") {
        await databasesStore.stop(dbId.value);
      } else {
        await databasesStore.restart(dbId.value);
      }
      message.success(
        action === "start"
          ? t("databases.detail.lifecycle.started")
          : action === "stop"
            ? t("databases.detail.lifecycle.stopped")
            : t("databases.detail.lifecycle.restarted"),
      );
    } catch (error) {
      message.error(describeDatabaseError(error));
    }
  }

  /** openRename prefills the current name and opens the dialog. */
  function openRename(): void {
    renameValue.value = database.value?.name ?? "";
    renameOpen.value = true;
  }

  /** handleRename submits the PATCH rename. */
  async function handleRename(): Promise<void> {
    const name = renameValue.value.trim();
    if (!isDatabaseNameValid(name)) {
      message.error(resolveValidationMessage(databaseMessages.nameRule));
      return;
    }
    renaming.value = true;
    try {
      await databasesStore.rename(dbId.value, name);
      message.success(t("databases.detail.lifecycle.renamed", { name }));
      renameOpen.value = false;
    } catch (error) {
      message.error(describeDatabaseError(error));
    } finally {
      renaming.value = false;
    }
  }

  /** handleDelete soft-deletes the row and returns to the environment. */
  async function handleDelete(): Promise<void> {
    const current = database.value;
    const name = current?.name ?? dbId.value;
    try {
      await databasesStore.remove(dbId.value);
      message.success(t("databases.detail.lifecycle.deleted", { name }));
      if (current) {
        await router.push({
          name: "environment-detail",
          params: { projectId: current.project_id, environmentId: current.environment_id },
        });
      } else {
        await router.push({ name: "projects" });
      }
    } catch (error) {
      message.error(describeDatabaseError(error));
    }
  }

  /** canWrite follows the contract's roles: viewers read, members write. */
  const canWrite = computed<boolean>(() => projectsStore.canWrite);

  const moveSaving = ref(false);
  const moveError = ref<string | null>(null);

  /**
   * handleMove applies the location settings (move environment, change
   * node). Only changed fields ride the PATCH; a move retargets the nested
   * route to the new environment. The contract's 409 refusals (in-flight
   * deploy, pinned node, name collision) render inline through moveError.
   * The navigation is identity-guarded: leaving the database mid-request
   * never yanks the user back to it.
   */
  async function handleMove(scope: {
    projectId: string;
    environmentId: string;
    serverId: string;
  }): Promise<void> {
    const current = database.value;
    if (!current) {
      return;
    }
    const input: UpdateDatabaseInput = {};
    if (scope.environmentId !== current.environment_id) {
      input.environment_id = scope.environmentId;
    }
    if (scope.serverId !== current.server_id) {
      input.server_id = scope.serverId;
    }
    if (Object.keys(input).length === 0) {
      return;
    }
    const targetId = dbId.value;
    moveSaving.value = true;
    moveError.value = null;
    try {
      const updated = await databasesStore.update(targetId, input);
      if (targetId !== dbId.value) {
        return; // the route moved on while the write was in flight
      }
      message.success(t("databases.detail.lifecycle.locationSaved"));
      await refreshProjectCounts([current.project_id, updated.project_id]);
      if (targetId !== dbId.value) {
        return;
      }
      if (input.environment_id) {
        await router.push({
          name: "database-detail",
          params: {
            projectId: updated.project_id,
            environmentId: updated.environment_id,
            id: updated.id,
          },
        });
      }
    } catch (error) {
      if (targetId === dbId.value) {
        moveError.value = describeDatabaseError(error);
      }
    } finally {
      if (targetId === dbId.value) {
        moveSaving.value = false;
      }
    }
  }

  /**
   * refreshProjectCounts invalidates the projects store after a move, so
   * project/environment counts converge without relying on a remount.
   */
  async function refreshProjectCounts(projectIds: string[]): Promise<void> {
    try {
      await projectsStore.fetchProjects();
    } catch {
      // The store already exposes the error; counts converge on next load.
    }
    const detail = projectsStore.detail;
    if (detail && projectIds.includes(detail.id)) {
      try {
        await projectsStore.fetchDetail(detail.id);
      } catch {
        // Same as above; the detail alert renders it.
      }
    }
  }

  // The detail route is reused when navigating between databases, and a
  // move keeps the id while the environment changes: reload on either.
  watch([dbId, () => String(route.params.environmentId ?? "")], () => {
    resetView();
    void fetchAll();
  });

  onMounted(() => {
    void fetchAll();
    databasesStore.pollDatabases();
  });

  onUnmounted(() => {
    databasesStore.stopPolling();
  });

  return {
    databasesStore,
    serversStore,
    dbId,
    database,
    pageLoading,
    pageError,
    shortId,
    initials,
    engineLabel,
    serverLabel,
    canStart,
    canStop,
    canRestart,
    credentials,
    revealed,
    renameOpen,
    renameValue,
    renaming,
    descColumns,
    connectionString,
    connectionDisplay,
    nodeAddressUnknown,
    copyText,
    copyCredential,
    copyConnectionString,
    fetchAll,
    resetView,
    handleLifecycle,
    openRename,
    handleRename,
    handleDelete,
    canWrite,
    moveSaving,
    moveError,
    handleMove,
  };
}

export type DatabaseDetail = ReturnType<typeof useDatabaseDetail>;

/** Injection key for the page-owned detail state shared with tab cards. */
export const databaseDetailKey: InjectionKey<DatabaseDetail> =
  Symbol("database-detail");
