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
import { computed, h, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
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

function matchesQuery(tool: ToolchainTool, query: string): boolean {
  return tool.name.toLowerCase().includes(query) ||
    tool.id.toLowerCase().includes(query) ||
    (tool.category ?? "").toLowerCase().includes(query);
}

function categoryOf(tool: ToolchainTool): string {
  return tool.category?.trim() || t("toolchain.page.uncategorized");
}

const groupedTools = computed<Map<string, ToolchainTool[]>>(() => {
  const groups = new Map<string, ToolchainTool[]>();
  for (const tool of store.tools) {
    const key = categoryOf(tool);
    const list = groups.get(key);
    if (list) {
      list.push(tool);
    } else {
      groups.set(key, [tool]);
    }
  }
  return groups;
});

const categories = computed<string[]>(() =>
  [...groupedTools.value.keys()].sort((a, b) => a.localeCompare(b)),
);

watch(
  categories,
  (names) => {
    if (!names.includes(activeCategory.value)) {
      activeCategory.value = names[0] ?? "";
    }
  },
  { immediate: true },
);

const searching = computed<boolean>(() => searchQuery.value.trim() !== "");

const selectableIds = computed<string[]>(() =>
  checkedKeys.value.filter((id) => {
    const tool = store.tools.find((item) => item.id === id);
    return tool !== undefined && isToolSelectable(tool);
  }),
);

function matchesIn(category: string): number {
  const query = searchQuery.value.trim().toLowerCase();
  const tools = groupedTools.value.get(category) ?? [];
  if (query === "") {
    return tools.length;
  }
  return tools.filter((tool) => matchesQuery(tool, query)).length;
}

function selectableInTab(category: string): number {
  const ids = new Set(selectableIds.value);
  return (groupedTools.value.get(category) ?? []).filter((tool) => ids.has(tool.id)).length;
}

function tabLabel(category: string): string {
  const total = groupedTools.value.get(category)?.length ?? 0;
  const matched = matchesIn(category);
  const count = searching.value
    ? String(t("toolchain.page.tabMatches", { matched, total }))
    : String(total);
  const selected = selectableInTab(category);
  const suffix = selected > 0
    ? ` · ${String(t("toolchain.page.tabSelected", { count: selected }))}`
    : "";
  return `${category.toUpperCase()} (${count})${suffix}`;
}

const activeTools = computed<ToolchainTool[]>(() => {
  const query = searchQuery.value.trim().toLowerCase();
  return (groupedTools.value.get(activeCategory.value) ?? [])
    .filter((tool) => query === "" || matchesQuery(tool, query))
    .sort((a, b) => a.name.localeCompare(b.name));
});

function tabPropsFor(category: string): Record<string, string | number | boolean> {
  const selected = category === activeCategory.value;
  return {
    role: "tab",
    "aria-selected": selected,
    tabindex: selected ? 0 : -1,
  };
}

function handleTabKeydown(event: KeyboardEvent): void {
  const names = categories.value;
  if (names.length === 0) {
    return;
  }
  const current = Math.max(0, names.indexOf(activeCategory.value));
  let next = -1;
  switch (event.key) {
    case "ArrowRight":
      next = (current + 1) % names.length;
      break;
    case "ArrowLeft":
      next = (current - 1 + names.length) % names.length;
      break;
    case "Home":
      next = 0;
      break;
    case "End":
      next = names.length - 1;
      break;
    default:
      return;
  }
  event.preventDefault();
  if (next === current) {
    return;
  }
  activeCategory.value = names[next];
  const tablist = event.currentTarget as HTMLElement | null;
  nextTick(() => {
    tablist?.querySelectorAll<HTMLElement>(".n-tabs-tab")[next]?.focus();
  });
}

const footerText = computed<string>(() =>
  selectableIds.value.length > 0
    ? String(t("toolchain.page.footerSelected", { count: selectableIds.value.length }))
    : String(t("toolchain.page.footerEmpty")),
);

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

async function runToolAction(tool: ToolchainTool, action: ToolchainAction): Promise<void> {
  pendingTools.value = { ...pendingTools.value, [tool.id]: true };
  try {
    const skipped = await store.enqueue([tool.id], action);
    if (skipped.length > 0) {
      message.warning(t("toolchain.page.skipped", { tools: skipped.join(", ") }));
    } else {
      message.success(t("toolchain.page.enqueued", { count: 1 }));
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
    void store
      .subscribe(store.serverId)
      .catch((err: unknown) => message.error(describeToolchainError(err)));
  }
});

onBeforeUnmount(() => {
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

        <template v-else>
          <NTabs
            v-model:value="activeCategory"
            type="line"
            animated
            role="tablist"
            :aria-label="t('toolchain.page.tabsLabel')"
            @keydown="handleTabKeydown"
          >
            <NTabPane
              v-for="category in categories"
              :key="category"
              :name="category"
              :tab-props="tabPropsFor(category)"
            >
              <template #tab>{{ tabLabel(category) }}</template>
            </NTabPane>
          </NTabs>

          <div role="tabpanel" :aria-label="tabLabel(activeCategory)">
            <NEmpty
              v-if="activeTools.length === 0"
              :description="$t('toolchain.page.emptySearch')"
            >
              <template #extra>{{ $t("toolchain.page.emptySearchHint") }}</template>
            </NEmpty>
            <NDataTable
              v-else
              :columns="toolColumns"
              :data="activeTools"
              :row-key="(row: ToolchainTool) => row.id"
              v-model:checked-row-keys="checkedKeys"
              :bordered="false"
              :pagination="false"
            />
          </div>
        </template>

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
      </NSpace>
    </NCard>

    <InstallQueueModal />
  </div>
</template>

<style scoped>
.toolchain-page {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}
.page-title {
  flex: 1 1 16rem;
  min-width: 0;
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
  padding-bottom: calc(var(--space-3) + env(safe-area-inset-bottom));
  border-top: 1px solid var(--border);
  background: var(--surface);
}
.footer-status {
  margin: 0;
  margin-right: auto;
  color: var(--muted);
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
