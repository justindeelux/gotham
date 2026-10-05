import { useCopyText } from "@/shared/composables/useCopyText";
import { useMessage } from "naive-ui";
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import type { InjectionKey } from "vue";
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
import { useDatabasesStore } from "@/features/databases/stores/databases";
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
  function copyCredential(field: CredentialField, label: string): void {
    const value = credentials.value?.[field] ?? "";
    if (value === "") {
      message.error(`No ${label.toLowerCase()} cached yet`);
      return;
    }
    void copyText(value, label);
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
      message.error("Node address unknown — cannot build the public DSN yet");
      return;
    }
    if (connectionString.value === "") {
      message.error("Credentials are not loaded yet");
      return;
    }
    void copyText(connectionString.value, "Connection string");
  }

  /** fetchAll loads the row, its credentials and the node list. */
  async function fetchAll(): Promise<void> {
    if (!dbId.value) {
      pageError.value = "No database selected.";
      return;
    }
    pageLoading.value = true;
    pageError.value = null;
    try {
      await databasesStore.fetchDatabase(dbId.value);
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
        action === "start" ? "Database started"
          : action === "stop" ? "Database stopped"
          : "Database restarted",
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
      message.error(databaseMessages.nameRule);
      return;
    }
    renaming.value = true;
    try {
      await databasesStore.rename(dbId.value, name);
      message.success(`Database renamed to "${name}"`);
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
      message.success(`Database "${name}" deleted · volume kept for 7 days`);
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
    moveSaving.value = true;
    moveError.value = null;
    try {
      const updated = await databasesStore.update(dbId.value, input);
      message.success("Location saved");
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
      moveError.value = describeDatabaseError(error);
    } finally {
      moveSaving.value = false;
    }
  }

  watch(dbId, () => {
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
