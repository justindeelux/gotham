<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NDataTable,
  NEmpty,
  NInput,
  NSelect,
  NSpace,
  NSpin,
  NTabPane,
  NTabs,
  NTag,
} from "naive-ui";
import type { DataTableColumns, SelectOption } from "naive-ui";
import { useMessage } from "naive-ui";
import { computed, h, onBeforeUnmount, onMounted, ref, watch } from "vue";
import type { VNode } from "vue";
import { useI18n } from "vue-i18n";

import { useServersStore } from "@/features/servers/stores/servers";
import {
  describeToolchainError,
  isTerminalJobStatus,
  isToolSelectable,
} from "@/features/toolchain/api/toolchain";
import type {
  ToolchainAction,
  ToolchainJob,
  ToolchainTool,
} from "@/features/toolchain/api/toolchain";
import ToolStatusBadge from "@/features/toolchain/components/ToolStatusBadge.vue";
import InstallQueueModal from "@/features/toolchain/components/InstallQueueModal.vue";
import { useToolchainStore } from "@/features/toolchain/stores/toolchain";

const { t } = useI18n();
const message = useMessage();
const serversStore = useServersStore();
const store = useToolchainStore();

const searchQuery = ref("");
const checkedKeys = ref<string[]>([]);
const pendingTools = ref<Record<string, boolean>>({});
const busyBulk = ref(false);
// activeCategory is the selected tab; checkedKeys lives outside the tabs so
// multi-select survives tab switches and one bulk install covers every tab.
const activeCategory = ref("");

const nodeOptions = computed<SelectOption[]>(() =>
  serversStore.servers.map((server) => ({
    label: server.name,
    value: server.id,
  })),
);

const errorText = computed<string | null>(() =>
  store.failure ? describeToolchainError(store.failure) : null,
);

/** matchesQuery is the client-side search predicate (name, id, category). */
function matchesQuery(tool: ToolchainTool, query: string): boolean {
  return tool.name.toLowerCase().includes(query) ||
    tool.id.toLowerCase().includes(query) ||
    (tool.category ?? "").toLowerCase().includes(query);
}

/** categories lists every tab from the full catalog (not the search filter). */
const categories = computed<string[]>(() => {
  const names = new Set<string>();
  for (const tool of store.tools) {
    names.add(tool.category?.trim() || t("toolchain.page.uncategorized"));
  }
  return [...names].sort((a, b) => a.localeCompare(b));
});

// The tab list follows the catalog: an unknown active tab (node switch,
// locale change) falls back to the first category.
watch(
  categories,
  (names) => {
    if (!names.includes(activeCategory.value)) {
      activeCategory.value = names[0] ?? "";
    }
  },
  { immediate: true },
);

/** searching is true while the header box filters every tab's badge. */
const searching = computed<boolean>(() => searchQuery.value.trim() !== "");

/** matchCount reports how many tools of one tab pass the current search. */
function matchCount(category: string): number {
  const query = searchQuery.value.trim().toLowerCase();
  if (query === "") {
    return store.tools.filter((tool) =>
      (tool.category?.trim() || t("toolchain.page.uncategorized")) === category,
    ).length;
  }
  return store.tools.filter((tool) =>
    (tool.category?.trim() || t("toolchain.page.uncategorized")) === category &&
    matchesQuery(tool, query),
  ).length;
}

/** selectedInTab counts the checked rows of one tab for its badge. */
function selectedInTab(category: string): number {
  const ids = new Set(checkedKeys.value);
  return store.tools.filter((tool) =>
    ids.has(tool.id) &&
    (tool.category?.trim() || t("toolchain.page.uncategorized")) === category,
  ).length;
}

/** tabLabel names a tab with its counts: total, matches while searching, selected. */
function tabLabel(category: string): string {
  const total = store.tools.filter((tool) =>
    (tool.category?.trim() || t("toolchain.page.uncategorized")) === category,
  ).length;
  const matched = matchCount(category);
  const count = searching.value
    ? String(t("toolchain.page.tabMatches", { matched, total }))
    : String(total);
  const selected = selectedInTab(category);
  const suffix = selected > 0
    ? ` · ${String(t("toolchain.page.tabSelected", { count: selected }))}`
    : "";
  return `${category} (${count})${suffix}`;
}

/** toolsIn lists one tab's tools, filtered by the header search, sorted by name. */
function toolsIn(category: string): ToolchainTool[] {
  const query = searchQuery.value.trim().toLowerCase();
  return store.tools
    .filter((tool) =>
      (tool.category?.trim() || t("toolchain.page.uncategorized")) === category &&
      (query === "" || matchesQuery(tool, query)),
    )
    .sort((a, b) => a.name.localeCompare(b.name));
}

/** selectableIds are the checked rows that can actually be installed. */
const selectableIds = computed<string[]>(() =>
  checkedKeys.value.filter((id) => {
    const tool = store.tools.find((item) => item.id === id);
    return tool !== undefined && isToolSelectable(tool);
  }),
);

/** footerText announces the bulk selection for the sticky footer status. */
const footerText = computed<string>(() =>
  selectableIds.value.length > 0
    ? String(t("toolchain.page.footerSelected", { count: selectableIds.value.length }))
    : String(t("toolchain.page.footerEmpty")),
);

/** queueType maps a job status to one Naive tag. */
function queueType(status: ToolchainJob["status"]): "default" | "info" | "success" | "error" {
  switch (status) {
    case "running":
      return "info";
    case "succeeded":
      return "success";
    case "failed":
      return "error";
    default:
      return "default";
  }
}

/** runToolAction enqueues one tool action with a per-row spinner. */
async function runToolAction(tool: ToolchainTool, action: ToolchainAction): Promise<void> {
  pendingTools.value = { ...pendingTools.value, [tool.id]: true };
  try {
    const skipped = await store.enqueue([tool.id], action);
    if (skipped.length > 0) {
      message.warning(t("toolchain.page.skipped", { tools: skipped.join(", ") }));
    } else {
      message.success(t("toolchain.page.enqueued", { count: 1 }));
      // The progress modal owns its markup and subscribes to the same
      // store: opening it shows the new job's live output immediately.
      store.openQueueModal();
    }
  } catch (err) {
    message.error(describeToolchainError(err));
  } finally {
    const next = { ...pendingTools.value };
    delete next[tool.id];
    pendingTools.value = next;
  }
}

async function handleInstallSelected(): Promise<void> {
  const ids = selectableIds.value;
  if (ids.length === 0) {
    return;
  }
  busyBulk.value = true;
  try {
    const skipped = await store.enqueue(ids, "install");
    checkedKeys.value = [];
    if (skipped.length > 0) {
      message.warning(t("toolchain.page.skipped", { tools: skipped.join(", ") }));
    }
    const created = ids.length - skipped.length;
    if (created > 0) {
      message.success(t("toolchain.page.enqueued", { count: created }));
      store.openQueueModal();
    }
  } catch (err) {
    message.error(describeToolchainError(err));
  } finally {
    busyBulk.value = false;
  }
}

async function handleCancel(job: ToolchainJob): Promise<void> {
  pendingTools.value = { ...pendingTools.value, [job.tool_id]: true };
  try {
    await store.cancel(job.id);
  } catch (err) {
    message.error(describeToolchainError(err));
  } finally {
    const next = { ...pendingTools.value };
    delete next[job.tool_id];
    pendingTools.value = next;
  }
}

async function selectNode(serverId: string): Promise<void> {
  checkedKeys.value = [];
  searchQuery.value = "";
  try {
    await store.subscribe(serverId);
  } catch (err) {
    message.error(describeToolchainError(err));
  }
}

async function handleRetry(): Promise<void> {
  if (store.serverId === "") {
    return;
  }
  try {
    await store.refresh();
  } catch (err) {
    message.error(describeToolchainError(err));
  }
}

const toolColumns = computed<DataTableColumns<ToolchainTool>>(() => [
  {
    type: "selection",
    // Detect-only and already-installed rows cannot be installed: their
    // checkbox is disabled, and the row itself shows why (the detect-only
    // note or the installed badge).
    disabled: (row) => !isToolSelectable(row),
  },
  {
    title: t("toolchain.columns.tool"),
    key: "name",
    minWidth: 220,
    render: (row) =>
      h("div", { class: "tool-cell" }, [
        h("span", { class: "tool-name" }, row.name),
        row.description
          ? h("span", { class: "muted tool-meta" }, row.description)
          : null,
        row.id === "git" || row.id === "docker"
          ? h("span", { class: "muted tool-meta" }, t(`toolchain.page.required.${row.id}`))
          : null,
        !row.installable && !row.uninstallable
          ? h("span", { class: "muted tool-meta" }, t("toolchain.page.detectOnlyNote"))
          : null,
      ]),
  },
  {
    title: t("toolchain.columns.status"),
    key: "status",
    width: 170,
    render: (row) => h(ToolStatusBadge, { tool: row }),
  },
  {
    title: t("toolchain.columns.queue"),
    key: "queue",
    width: 150,
    render: (row) => {
      const job = store.jobForTool(row.id);
      if (!job || isTerminalJobStatus(job.status)) {
        return h("span", { class: "muted" }, "—");
      }
      return h(
        NSpace,
        { align: "center", size: 6, wrap: false },
        {
          default: () => [
            h(NTag, { type: queueType(job.status), size: "small", round: true }, {
              default: () => t(`toolchain.queue.${job.status}`),
            }),
            job.status === "queued"
              ? h(NButton, {
                  size: "tiny",
                  ghost: true,
                  loading: pendingTools.value[row.id] === true,
                  onClick: () => void handleCancel(job),
                }, { default: () => t("toolchain.actions.cancel") })
              : null,
          ],
        },
      );
    },
  },
  {
    title: t("toolchain.columns.actions"),
    key: "actions",
    width: 200,
    render: (row) => {
      const buttons: VNode[] = [];
      const updateGit = row.id === "git" && row.installed && !row.version_ok;
      if ((!row.installed || updateGit) && row.installable) {
        buttons.push(
          h(NButton, {
            size: "small",
            type: "primary",
            ghost: true,
            loading: pendingTools.value[row.id] === true,
            onClick: () => void runToolAction(row, "install"),
          }, { default: () => t(updateGit ? "toolchain.actions.installUpdate" : "toolchain.actions.install") }),
        );
      }
      if (row.installed && row.uninstallable && row.id !== "git" && row.id !== "docker") {
        buttons.push(
          h(NButton, {
            size: "small",
            ghost: true,
            loading: pendingTools.value[row.id] === true,
            onClick: () => void runToolAction(row, "uninstall"),
          }, { default: () => t("toolchain.actions.uninstall") }),
        );
      }
      if (buttons.length === 0) {
        return h("span", { class: "muted" }, "—");
      }
      return h(NSpace, { size: 8, wrap: false }, { default: () => buttons });
    },
  },
]);

onMounted(() => {
  void serversStore
    .fetchServers()
    .catch((err: unknown) => message.error(describeToolchainError(err)));
  if (serversStore.servers.length > 0 && store.serverId === "") {
    void selectNode(serversStore.servers[0].id);
  } else if (store.serverId !== "") {
    // Remount with a kept node (route leave + return): disconnect() released
    // the socket on unmount, so a bare refresh() would leave realtime dead —
    // resubscribe to re-establish the socket and channel, then reload.
    void store
      .subscribe(store.serverId)
      .catch((err: unknown) => message.error(describeToolchainError(err)));
  }
});

onBeforeUnmount(() => {
  // Release the socket and the channel: without this the realtime client
  // (created outside a component scope) would keep buffering log lines in
  // the background after the route is left.
  store.disconnect();
});

watch(
  () => serversStore.servers.length,
  () => {
    if (store.serverId === "" && serversStore.servers.length > 0) {
      void selectNode(serversStore.servers[0].id);
    }
  },
);

// A tool that becomes installed (or detect-only) while checked must not
// stay selected: drop rows that are no longer installable.
watch(
  () => store.tools,
  (tools) => {
    const allowed = new Set(
      tools.filter((tool) => isToolSelectable(tool)).map((tool) => tool.id),
    );
    const next = checkedKeys.value.filter((id) => allowed.has(id));
    if (next.length !== checkedKeys.value.length) {
      checkedKeys.value = next;
    }
  },
);
</script>

<template>
  <div class="toolchain-page">
    <div class="page-head">
      <div class="page-title">
        <p class="eyebrow">{{ $t("toolchain.page.eyebrow") }}</p>
        <h1>{{ $t("toolchain.page.title") }}</h1>
        <p class="page-desc">{{ $t("toolchain.page.description") }}</p>
      </div>
      <div class="header-controls">
        <NSelect
          :value="store.serverId === '' ? null : store.serverId"
          :options="nodeOptions"
          :placeholder="$t('toolchain.page.selectNodePlaceholder')"
          :aria-label="$t('toolchain.page.selectNode')"
          :loading="serversStore.loading"
          :disabled="serversStore.loading"
          class="node-select"
          @update:value="(id: string) => void selectNode(id)"
        />
        <NInput
          v-model:value="searchQuery"
          :placeholder="$t('toolchain.page.searchPlaceholder')"
          :aria-label="$t('toolchain.page.searchPlaceholder')"
          clearable
          class="search-input"
        />
      </div>
    </div>

    <NCard>
      <NSpace vertical :size="12">
        <template v-if="errorText">
          <NAlert type="error" :show-icon="true">
            {{ errorText }}
          </NAlert>
          <div>
            <NButton size="small" ghost @click="() => void handleRetry()">
              {{ $t("toolchain.page.retry") }}
            </NButton>
          </div>
        </template>

        <NSpin v-else-if="store.loading" :show="true" />

        <NEmpty
          v-else-if="store.serverId === ''"
          :description="$t('toolchain.page.noNodes')"
        />
        <NEmpty
          v-else-if="store.tools.length === 0"
          :description="$t('toolchain.page.emptyTools')"
        >
          <template #extra>{{ $t("toolchain.page.emptyToolsHint") }}</template>
        </NEmpty>

        <!-- Category tabs (Naive UI owns tablist roles and arrow-key moves);
             one shared checked-keys model keeps the bulk selection across tabs. -->
        <template v-else>
          <NTabs
            v-model:value="activeCategory"
            type="line"
            :aria-label="t('toolchain.page.tabsLabel')"
          >
            <NTabPane
              v-for="category in categories"
              :key="category"
              :name="category"
              :tab="tabLabel(category)"
            >
              <NEmpty
                v-if="toolsIn(category).length === 0"
                :description="$t('toolchain.page.emptySearch')"
              >
                <template #extra>{{ $t("toolchain.page.emptySearchHint") }}</template>
              </NEmpty>
              <NDataTable
                v-else
                :columns="toolColumns"
                :data="toolsIn(category)"
                :row-key="(row: ToolchainTool) => row.id"
                v-model:checked-row-keys="checkedKeys"
                :bordered="false"
                :pagination="false"
              />
            </NTabPane>
          </NTabs>

          <div class="table-footer">
            <p class="footer-status" role="status" aria-live="polite">
              {{ footerText }}
            </p>
            <div class="footer-actions">
              <NButton
                secondary
                :disabled="store.activeJobs.length === 0"
                @click="store.openQueueModal()"
              >
                {{ $t("toolchain.page.queue") }}
                <span v-if="store.activeJobs.length > 0" class="count-pill">
                  {{ store.activeJobs.length }}
                </span>
              </NButton>
              <NButton
                type="primary"
                :disabled="selectableIds.length === 0"
                :loading="busyBulk"
                @click="() => void handleInstallSelected()"
              >
                {{ $t("toolchain.page.installSelected") }}
                <span v-if="selectableIds.length > 0" class="count-pill">
                  {{ selectableIds.length }}
                </span>
              </NButton>
            </div>
          </div>
        </template>
      </NSpace>
    </NCard>

    <!-- Install progress modal (JUS-178): renders from the same store and
         opens via the queueModalOpen hook (Queue button, post-enqueue). -->
    <InstallQueueModal />
  </div>
</template>

<style scoped>
.toolchain-page {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}
.page-head {
  display: flex;
  justify-content: space-between;
  align-items: flex-end;
  gap: var(--space-4);
  flex-wrap: wrap;
}
.page-title {
  flex: 1 1 16rem;
  min-width: 0;
}
.eyebrow {
  color: var(--fg-3);
  font-size: var(--text-xs);
  text-transform: uppercase;
  letter-spacing: 0.08em;
  margin: 0 0 var(--space-1);
}
.page-head h1 {
  margin: 0;
  font-size: var(--text-2xl);
  color: var(--fg-1);
}
.page-desc {
  color: var(--fg-3);
  margin: var(--space-2) 0 0;
  max-width: 640px;
}
.header-controls {
  display: flex;
  gap: var(--space-3);
  align-items: center;
  flex-wrap: wrap;
  flex: 1 1 20rem;
}
.node-select {
  flex: 1 1 12rem;
  max-width: 100%;
}
.search-input {
  flex: 1 1 10rem;
  max-width: 100%;
}
.table-footer {
  position: sticky;
  bottom: 0;
  z-index: 1;
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex-wrap: wrap;
  padding-block: var(--space-3);
  border-top: 1px solid var(--border);
  background: var(--n-color);
}
.footer-status {
  margin: 0;
  margin-right: auto;
  color: var(--fg-3);
  font-size: var(--text-sm);
}
.footer-actions {
  display: flex;
  gap: var(--space-2);
  align-items: center;
  flex-wrap: wrap;
}
:deep(.tool-cell) {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
}
:deep(.tool-name) {
  color: var(--fg-2);
  font-weight: 600;
}
:deep(.tool-meta) {
  font-size: var(--text-xs);
}
.count-pill {
  margin-left: var(--space-2);
}
</style>
