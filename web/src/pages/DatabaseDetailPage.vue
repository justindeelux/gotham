<script setup lang="ts">
import {
  NAlert,
  NAvatar,
  NButton,
  NCard,
  NDescriptions,
  NDescriptionsItem,
  NEmpty,
  NIcon,
  NInput,
  NModal,
  NPopconfirm,
  NSpace,
  NSpin,
  NTabPane,
  NTabs,
  NText,
  useMessage,
} from "naive-ui";
import { computed, onMounted, onUnmounted, ref, watch } from "vue";
import { RouterLink, useRoute, useRouter } from "vue-router";

import { describeDatabaseError } from "../api/databases";
import type { Database } from "../api/databases";
import DatabaseStatusTag from "../components/DatabaseStatusTag.vue";
import GothamIcon from "../components/GothamIcon.vue";
import { useMediaQuery } from "../composables/useMediaQuery";
import { useDatabasesStore } from "../stores/databases";
import { useServersStore } from "../stores/servers";
import { relativeTime } from "../utils/format";

const route = useRoute();
const router = useRouter();
const message = useMessage();
const databasesStore = useDatabasesStore();
const serversStore = useServersStore();

const dbId = computed<string>(() => String(route.params.id ?? ""));
const activeTab = ref("overview");
const renameOpen = ref(false);
const renameValue = ref("");
const renaming = ref(false);
const revealed = ref(false);

const NAME_PATTERN = /^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,62}$/;

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

/** copyText copies a value to the clipboard and confirms with a toast. */
async function copyText(value: string, label: string): Promise<void> {
  try {
    await navigator.clipboard.writeText(value);
    message.success(`${label} copied to clipboard`);
  } catch {
    message.error(`Could not copy ${label.toLowerCase()}`);
  }
}

type CredentialField = "username" | "password" | "database" | "root_password";

/** copyCredential copies one cached credential field without narrowing issues. */
function copyCredential(field: CredentialField, label: string): void {
  const value = credentials.value?.[field] ?? "";
  if (value === "") {
    message.error(`No ${label.toLowerCase()} cached yet`);
    return;
  }
  void copyText(value, label);
}

/** fetchAll loads the row, its credentials and the node list. */
async function fetchAll(): Promise<void> {
  if (!dbId.value) {
    return;
  }
  try {
    await databasesStore.fetchDatabase(dbId.value);
  } catch (error) {
    message.error(describeDatabaseError(error));
    return;
  }
  try {
    await databasesStore.fetchCredentials(dbId.value);
  } catch {
    // The store already exposes the error; the alert renders it.
  }
  void serversStore.fetchServers().catch(() => undefined);
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
  if (!NAME_PATTERN.test(name)) {
    message.error("Name must be 1-63 characters of letters, digits, ., _ or -.");
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

/** handleDelete soft-deletes the row and returns to the list. */
async function handleDelete(): Promise<void> {
  const name = database.value?.name ?? dbId.value;
  try {
    await databasesStore.remove(dbId.value);
    message.success(`Database "${name}" deleted · volume kept for 7 days`);
    await router.push({ name: "databases" });
  } catch (error) {
    message.error(describeDatabaseError(error));
  }
}

watch(dbId, () => {
  activeTab.value = "overview";
  revealed.value = false;
  void fetchAll();
});

onMounted(() => {
  void fetchAll();
  databasesStore.pollDatabases();
});

onUnmounted(() => {
  databasesStore.stopPolling();
});
</script>

<template>
  <NSpace vertical :size="16">
    <nav class="breadcrumb" aria-label="Breadcrumb">
      <RouterLink to="/databases">Databases</RouterLink>
      <span class="breadcrumb__sep">/</span>
      <span class="muted mono">{{ database?.name ?? shortId }}</span>
    </nav>

    <NSpin :show="databasesStore.loading">
      <NAlert
        v-if="databasesStore.error"
        type="error"
        :show-icon="true"
        style="margin-bottom: 12px"
      >
        {{ databasesStore.error }}
      </NAlert>

      <div class="page-head">
        <NAvatar round :size="48">{{ initials }}</NAvatar>
        <div class="page-head__title">
          <NSpace align="center" :size="10">
            <NText strong style="font-size: 20px" class="mono">
              {{ database?.name ?? shortId }}
            </NText>
            <DatabaseStatusTag
              v-if="database"
              :status="database.status"
              size="medium"
            />
          </NSpace>
          <NText depth="3" class="mono">{{ dbId }}</NText>
        </div>
        <NSpace class="page-head__actions" align="center" :size="8">
          <NButton
            :disabled="!canStart"
            :loading="databasesStore.acting"
            @click="() => void handleLifecycle('start')"
          >
            Start
          </NButton>
          <NButton
            :disabled="!canStop"
            :loading="databasesStore.acting"
            @click="() => void handleLifecycle('stop')"
          >
            Stop
          </NButton>
          <NButton
            :disabled="!canRestart"
            :loading="databasesStore.acting"
            @click="() => void handleLifecycle('restart')"
          >
            Restart
          </NButton>
          <NButton :disabled="!database" @click="openRename">
            Rename
          </NButton>
          <NPopconfirm @positive-click="() => void handleDelete()">
            <template #trigger>
              <NButton type="error" ghost :loading="databasesStore.acting">
                Delete
              </NButton>
            </template>
            Delete this database? The container is removed from the node, the
            volume {{ database?.volume ?? "" }} is kept for 7 days before
            permanent removal.
          </NPopconfirm>
        </NSpace>
      </div>

      <NTabs v-model:value="activeTab" type="line" animated>
        <NTabPane name="overview" tab="Overview">
          <NSpace vertical :size="16" style="margin-top: 16px">
            <NCard v-if="database" title="Details">
              <NDescriptions :column="descColumns" bordered label-placement="left">
                <NDescriptionsItem label="Engine">
                  <span class="mono">{{ engineLabel }}</span>
                </NDescriptionsItem>
                <NDescriptionsItem label="Node">
                  <span class="mono">{{ serverLabel }}</span>
                </NDescriptionsItem>
                <NDescriptionsItem label="Public port">
                  <span class="mono">
                    {{
                      database.public_port > 0
                        ? database.public_port
                        : "off · internal network only"
                    }}
                  </span>
                </NDescriptionsItem>
                <NDescriptionsItem label="Volume">
                  <span class="mono">{{ database.volume }}</span>
                </NDescriptionsItem>
                <NDescriptionsItem label="Container">
                  <span class="mono">{{ database.container_id || "—" }}</span>
                </NDescriptionsItem>
                <NDescriptionsItem label="Created">
                  {{ relativeTime(database.created_at) }}
                </NDescriptionsItem>
              </NDescriptions>
            </NCard>

            <NCard title="Credentials">
              <template #header-extra>
                <NText depth="3">Stored encrypted · owner only</NText>
              </template>
              <NAlert
                v-if="databasesStore.credentialsError"
                type="error"
                :show-icon="true"
                style="margin-bottom: 12px"
              >
                {{ databasesStore.credentialsError }}
              </NAlert>
              <NSpin :show="databasesStore.credentialsLoading">
                <NSpace v-if="credentials" vertical :size="12">
                  <div class="credential-row">
                    <NText depth="3">Username</NText>
                    <NText class="mono grow">{{ credentials.username }}</NText>
                    <NButton
                      size="small"
                      secondary
                      @click="() => copyCredential('username', 'Username')"
                    >
                      Copy
                    </NButton>
                  </div>
                  <div class="credential-row">
                    <NText depth="3">Password</NText>
                    <NText class="mono grow">
                      {{ revealed ? credentials.password : "••••••••••••" }}
                    </NText>
                    <NButton
                      size="small"
                      secondary
                      @click="revealed = !revealed"
                    >
                      {{ revealed ? "Hide" : "Reveal" }}
                    </NButton>
                    <NButton
                      size="small"
                      secondary
                      @click="() => copyCredential('password', 'Password')"
                    >
                      Copy
                    </NButton>
                  </div>
                  <div class="credential-row">
                    <NText depth="3">Database</NText>
                    <NText class="mono grow">{{ credentials.database }}</NText>
                    <NButton
                      size="small"
                      secondary
                      @click="() => copyCredential('database', 'Database')"
                    >
                      Copy
                    </NButton>
                  </div>
                  <div v-if="credentials.root_password" class="credential-row">
                    <NText depth="3">Root password</NText>
                    <NText class="mono grow">
                      {{ revealed ? credentials.root_password : "••••••••••••" }}
                    </NText>
                    <NButton
                      size="small"
                      secondary
                      @click="() => copyCredential('root_password', 'Root password')"
                    >
                      Copy
                    </NButton>
                  </div>
                </NSpace>
                <NEmpty
                  v-else-if="!databasesStore.credentialsLoading"
                  description="No credentials cached — they load automatically with the page."
                />
              </NSpin>
            </NCard>
          </NSpace>
        </NTabPane>

        <NTabPane name="backups" tab="Backups">
          <NCard style="margin-top: 16px">
            <NEmpty description="Backups arrive with BE-5.2">
              <template #icon>
                <NIcon>
                  <GothamIcon name="db" />
                </NIcon>
              </template>
              <template #extra>
                <p class="empty-hint">
                  Scheduled dumps, retention and restore ship with the backups
                  backend. Nothing is scheduled for this database yet.
                </p>
              </template>
            </NEmpty>
          </NCard>
        </NTabPane>
      </NTabs>
    </NSpin>

    <NModal
      v-model:show="renameOpen"
      preset="card"
      title="Rename database"
      style="width: 480px; max-width: 94vw"
    >
      <NSpace vertical :size="12">
        <NText depth="3">
          Only the display name changes — the container, volume and credentials
          stay untouched.
        </NText>
        <NInput
          v-model:value="renameValue"
          class="mono"
          placeholder="New database name"
          @keyup.enter="() => void handleRename()"
        />
        <NSpace justify="end" :size="8">
          <NButton @click="renameOpen = false">Cancel</NButton>
          <NButton
            type="primary"
            :loading="renaming"
            :disabled="!NAME_PATTERN.test(renameValue.trim())"
            @click="() => void handleRename()"
          >
            Rename
          </NButton>
        </NSpace>
      </NSpace>
    </NModal>
  </NSpace>
</template>

<style scoped>
.breadcrumb {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--text-xs);
}

.breadcrumb__sep {
  color: var(--meta);
}

.muted {
  color: var(--muted);
}

.mono {
  font-family: var(--font-mono);
}

.grow {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
}

.page-head {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  flex-wrap: wrap;
}

.page-head__title {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  flex: 1;
  min-width: 0;
}

.page-head__actions {
  margin-left: auto;
  flex-shrink: 0;
}

.credential-row {
  display: flex;
  align-items: center;
  gap: var(--space-3);
}

.empty-hint {
  color: var(--muted);
  font-size: var(--text-sm);
  margin: 0;
  max-width: 62ch;
}
</style>
