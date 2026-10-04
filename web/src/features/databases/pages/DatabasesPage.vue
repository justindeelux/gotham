<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NDataTable,
  NEmpty,
  NIcon,
  NInput,
  NPopconfirm,
  NSpace,
  NTag,
  NText,
  useMessage,
} from "naive-ui";
import type { DataTableColumns } from "naive-ui";
import { computed, h, onMounted, onUnmounted, ref } from "vue";
import type { VNode } from "vue";
import { useRouter } from "vue-router";

import { describeDatabaseError } from "@/features/databases/api/databases";
import type { Database } from "@/features/databases/api/databases";
import { databaseEmptyDescription, databaseEmptyHint } from "@/features/databases/api/databases";
import CreateDatabaseWizard from "@/features/databases/components/CreateDatabaseWizard.vue";
import DatabaseStatusTag from "@/features/databases/components/DatabaseStatusTag.vue";
import GothamIcon from "@/shared/ui/GothamIcon.vue";
import { useDatabasesStore } from "@/features/databases/stores/databases";
import { useServersStore } from "@/features/servers";
import { relativeTime } from "@/shared/utils/format";

const router = useRouter();
const message = useMessage();
const databasesStore = useDatabasesStore();
const serversStore = useServersStore();

const wizardOpen = ref(false);
const restartingId = ref<string | null>(null);

/** Filter chip keys mirroring the databases.html toolbar. */
type DatabaseFilter = "all" | "running" | "stopped" | "public";

const activeFilter = ref<DatabaseFilter>("all");
const searchQuery = ref("");

/** matchesFilter applies the active status chip to one database. */
function matchesFilter(database: Database, filter: DatabaseFilter): boolean {
  switch (filter) {
    case "running":
      return database.status === "running";
    case "stopped":
      return database.status === "stopped";
    case "public":
      return database.public_port > 0;
    case "all":
    default:
      return true;
  }
}

/** matchesSearch applies the name/engine/node query to one database. */
function matchesSearch(database: Database, query: string): boolean {
  const needle = query.trim().toLowerCase();
  if (needle === "") {
    return true;
  }
  const haystacks = [
    database.name,
    database.engine,
    serverName(database.server_id),
  ];
  return haystacks.some((field) => field.toLowerCase().includes(needle));
}

/** serverName resolves a node id to its display name. */
function serverName(serverId: string): string {
  return (
    serversStore.servers.find((server) => server.id === serverId)?.name ??
    serverId.slice(0, 8)
  );
}

/** engineLabel renders engine + version + image hint. */
function engineLabel(database: Database): string {
  return database.version
    ? `${database.engine}:${database.version}`
    : database.engine;
}

/** filteredDatabases applies the chip filter and the search query. */
const filteredDatabases = computed<Database[]>(() =>
  databasesStore.databases.filter(
    (item) => matchesFilter(item, activeFilter.value) && matchesSearch(item, searchQuery.value),
  ),
);

/** filterCounts renders live counts on the chips; never invented. */
const filterCounts = computed<Record<DatabaseFilter, number>>(() => ({
  all: databasesStore.databases.length,
  running: databasesStore.databases.filter((item) => item.status === "running").length,
  stopped: databasesStore.databases.filter((item) => item.status === "stopped").length,
  public: databasesStore.databases.filter((item) => item.public_port > 0).length,
}));

/** engineBreakdown summarizes engines for the KPI subtitle. */
const engineBreakdown = computed<string>(() => {
  const counts = new Map<string, number>();
  for (const item of databasesStore.databases) {
    counts.set(item.engine, (counts.get(item.engine) ?? 0) + 1);
  }
  if (counts.size === 0) {
    return "None yet";
  }
  return [...counts.entries()]
    .map(([engine, count]) => `${count} ${engine}`)
    .join(" · ");
});

/** nameCell renders the database name with its engine image. */
function nameCell(database: Database): VNode {
  return h("div", { class: "cell-main" }, [
    h("span", { class: "mono cell-name" }, database.name),
    h("span", { class: "cell-sub mono" }, engineLabel(database)),
  ]);
}

/** portCell renders the public port tag or an em dash. */
function portCell(database: Database): VNode {
  if (database.public_port > 0) {
    return h(NTag, { size: "small", round: true }, {
      default: () => String(database.public_port),
    });
  }
  return h(NText, { depth: 3 }, { default: () => "—" });
}

/** actionsCell renders per-row Details / restart / delete controls. */
function actionsCell(database: Database): VNode {
  return h(NSpace, { size: 8, align: "center", wrap: false }, {
    default: () => [
      h(
        NButton,
        {
          size: "small",
          secondary: true,
          onClick: () => {
            void router.push({
              name: "database-detail",
              params: { id: database.id },
            });
          },
        },
        { default: () => "Details" },
      ),
      h(
        NButton,
        {
          size: "small",
          secondary: true,
          loading: restartingId.value === database.id,
          disabled: databasesStore.acting,
          "aria-label": `Restart ${database.name}`,
          onClick: () => void handleRestart(database),
        },
        { default: () => "Restart" },
      ),
      h(
        NPopconfirm,
        {
          onPositiveClick: () => void handleDelete(database),
        },
        {
          trigger: () =>
            h(
              NButton,
              { size: "small", type: "error", ghost: true, "aria-label": `Delete ${database.name}` },
              { default: () => "Delete" },
            ),
          default: () =>
            `Delete database "${database.name}"? The container is removed from the node, the volume ${database.volume} is kept for 7 days.`,
        },
      ),
    ],
  });
}

const columns: DataTableColumns<Database> = [
  {
    title: "Database",
    key: "name",
    minWidth: 200,
    render: (row) => nameCell(row),
  },
  {
    title: "Node",
    key: "server_id",
    width: 160,
    render: (row) => h("span", { class: "mono" }, serverName(row.server_id)),
  },
  {
    title: "Public port",
    key: "public_port",
    width: 120,
    render: (row) => portCell(row),
  },
  {
    title: "Status",
    key: "status",
    width: 130,
    render: (row) => h(DatabaseStatusTag, { status: row.status }),
  },
  {
    title: "Created",
    key: "created_at",
    width: 110,
    render: (row) => relativeTime(row.created_at),
  },
  {
    title: "Actions",
    key: "actions",
    width: 280,
    render: (row) => actionsCell(row),
  },
];

/** rowKey identifies a row by its database id. */
function rowKey(row: Database): string {
  return row.id;
}

/** fetchAll loads the databases and the node names. */
async function fetchAll(): Promise<void> {
  try {
    await databasesStore.fetchDatabases();
  } catch {
    // The store already exposes the error; the alert renders it.
  }
  void serversStore.fetchServers().catch(() => undefined);
}

/** handleDelete removes one database, surfacing backend errors honestly. */
async function handleDelete(database: Database): Promise<void> {
  try {
    await databasesStore.remove(database.id);
    message.success(
      `Database "${database.name}" deleted · volume kept for 7 days`,
    );
  } catch (error) {
    message.error(describeDatabaseError(error));
  }
}

/** handleRestart re-runs one database container in place. */
async function handleRestart(database: Database): Promise<void> {
  restartingId.value = database.id;
  try {
    await databasesStore.restart(database.id);
    message.success(`Database "${database.name}" restarting`);
  } catch (error) {
    message.error(describeDatabaseError(error));
  } finally {
    restartingId.value = null;
  }
}

onMounted(() => {
  void fetchAll();
  databasesStore.pollDatabases();
});

onUnmounted(() => {
  databasesStore.stopPolling();
});
</script>

<template>
  <div class="databases-page">
    <div class="page-head">
      <div>
        <p class="eyebrow">Operations · Managed database</p>
        <h1>Databases</h1>
        <p class="page-desc">
          Each database is a container with its own volume on an agent-managed
          node. Credentials are stored encrypted and never appear on the rows
          below — open a database to reveal them.
        </p>
      </div>
      <div class="page-actions">
        <NButton type="primary" @click="wizardOpen = true">
          Create database
        </NButton>
      </div>
    </div>

    <!-- KPI row ported from databases.html. Size/backup/schedule tiles need
         backend aggregates the API does not expose yet, so this row reports
         the same population the list does. -->
    <div class="kpi-row">
      <div class="stat">
        <p class="stat-label">Databases</p>
        <p class="stat-value num">{{ filterCounts.all }}</p>
        <p class="stat-sub muted">{{ engineBreakdown }}</p>
      </div>
      <div class="stat">
        <p class="stat-label">Running</p>
        <p class="stat-value num">{{ filterCounts.running }}</p>
        <p class="stat-sub muted">of {{ filterCounts.all }}</p>
      </div>
      <div class="stat">
        <p class="stat-label">Public port</p>
        <p class="stat-value num">{{ filterCounts.public }}</p>
        <p class="stat-sub muted">reachable outside the node</p>
      </div>
      <div class="stat">
        <p class="stat-label">Stopped</p>
        <p class="stat-value num">{{ filterCounts.stopped }}</p>
        <p class="stat-sub muted">container stopped, volume intact</p>
      </div>
    </div>

    <NAlert
      v-if="databasesStore.error"
      type="error"
      :show-icon="true"
      style="margin-bottom: 12px"
    >
      {{ databasesStore.error }}
    </NAlert>

    <NCard title="Managed databases">
      <template #header-extra>
        <NText depth="3">{{ databasesStore.databases.length }} databases</NText>
      </template>
      <NSpace vertical :size="12">
        <NSpace :size="8" align="center">
          <NButton
            size="small"
            :secondary="activeFilter !== 'all'"
            :type="activeFilter === 'all' ? 'primary' : undefined"
            round
            @click="activeFilter = 'all'"
          >
            All · {{ filterCounts.all }}
          </NButton>
          <NButton
            size="small"
            :secondary="activeFilter !== 'running'"
            :type="activeFilter === 'running' ? 'primary' : undefined"
            round
            @click="activeFilter = 'running'"
          >
            Running · {{ filterCounts.running }}
          </NButton>
          <NButton
            size="small"
            :secondary="activeFilter !== 'stopped'"
            :type="activeFilter === 'stopped' ? 'primary' : undefined"
            round
            @click="activeFilter = 'stopped'"
          >
            Stopped · {{ filterCounts.stopped }}
          </NButton>
          <NButton
            size="small"
            :secondary="activeFilter !== 'public'"
            :type="activeFilter === 'public' ? 'primary' : undefined"
            round
            @click="activeFilter = 'public'"
          >
            Public port · {{ filterCounts.public }}
          </NButton>
          <NInput
            v-model:value="searchQuery"
            class="search-input"
            placeholder="Search databases…"
            aria-label="Search databases"
            clearable
          />
        </NSpace>

        <NDataTable
          v-if="filteredDatabases.length > 0 || databasesStore.loading"
          :columns="columns"
          :data="filteredDatabases"
          :loading="databasesStore.loading"
          :row-key="rowKey"
          :bordered="false"
          :scroll-x="900"
          :pagination="{ pageSize: 10 }"
        />
        <NEmpty
          v-else
          :description="databaseEmptyDescription(databasesStore.databases.length)"
        >
          <template #icon>
            <NIcon>
              <GothamIcon name="db" />
            </NIcon>
          </template>
          <template #extra>
            <p class="empty-hint">
              {{ databaseEmptyHint(databasesStore.databases.length) }}
            </p>
            <NButton type="primary" @click="wizardOpen = true">
              Create database
            </NButton>
          </template>
        </NEmpty>
      </NSpace>
      <template #footer>
        <NText depth="3">
          Deleting a database removes only the container — the volume
          <span class="mono">gotham-db-{id}</span> is kept for 7 days.
        </NText>
      </template>
    </NCard>

    <CreateDatabaseWizard
      v-model:show="wizardOpen"
      @created="() => void databasesStore.refreshDatabases()"
    />
  </div>
</template>

<style scoped>
.databases-page {
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

.kpi-row {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: var(--space-4);
}

.stat {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  padding: var(--space-4);
}

.stat-label {
  margin: 0 0 var(--space-2);
  font-size: var(--text-xs);
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--muted);
}

.stat-value {
  margin: 0 0 var(--space-2);
  font-size: var(--text-3xl);
  color: var(--fg-2);
}

.stat-sub {
  margin: 0;
  font-size: var(--text-xs);
}

@media (max-width: 1180px) {
  .kpi-row {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 640px) {
  .kpi-row {
    grid-template-columns: minmax(0, 1fr);
  }
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

.search-input {
  margin-left: auto;
  max-width: 280px;
}

.cell-main {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.cell-name {
  color: var(--fg-2);
  font-weight: 600;
}

.cell-sub {
  font-size: var(--text-xs);
  color: var(--muted);
}

.empty-hint {
  color: var(--muted);
  font-size: var(--text-sm);
  margin: 0 0 var(--space-3);
}

@media (max-width: 860px) {
  .page-actions {
    margin-left: 0;
    width: 100%;
  }

  .search-input {
    margin-left: 0;
    max-width: none;
    width: 100%;
  }
}
</style>
