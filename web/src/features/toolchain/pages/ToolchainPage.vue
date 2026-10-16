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
import { useToolchainStore } from "@/features/toolchain/stores/toolchain";

const { t } = useI18n();
const message = useMessage();
const serversStore = useServersStore();
const store = useToolchainStore();

const searchQuery = ref("");
const checkedKeys = ref<string[]>([]);
const pendingTools = ref<Record<string, boolean>>({});
const busyBulk = ref(false);

const nodeOptions = computed<SelectOption[]>(() =>
  serversStore.servers.map((server) => ({
    label: server.name,
    value: server.id,
  })),
);

const errorText = computed<string | null>(() =>
  store.failure ? describeToolchainError(store.failure) : null,
);

/** filteredTools applies the search query client-side (name, id, category). */
const filteredTools = computed<ToolchainTool[]>(() => {
  const query = searchQuery.value.trim().toLowerCase();
  if (query === "") {
    return store.tools;
  }
  return store.tools.filter((tool) =>
    tool.name.toLowerCase().includes(query) ||
    tool.id.toLowerCase().includes(query) ||
    (tool.category ?? "").toLowerCase().includes(query),
  );
});

interface ToolGroup {
  category: string;
  tools: ToolchainTool[];
}

/** groupedTools sorts tools into category sections (uncategorized last). */
const groupedTools = computed<ToolGroup[]>(() => {
  const groups = new Map<string, ToolchainTool[]>();
  for (const tool of filteredTools.value) {
    const category = tool.category?.trim() || t("toolchain.page.uncategorized");
    const list = groups.get(category);
    if (list) {
      list.push(tool);
    } else {
      groups.set(category, [tool]);
    }
  }
  return [...groups.entries()]
    .map(([category, tools]) => ({
      category,
      tools: [...tools].sort((a, b) => a.name.localeCompare(b.name)),
    }))
    .sort((a, b) => a.category.localeCompare(b.category));
});

/** selectableIds are the checked rows that can actually be installed. */
const selectableIds = computed<string[]>(() =>
  checkedKeys.value.filter((id) => {
    const tool = store.tools.find((item) => item.id === id);
    return tool !== undefined && isToolSelectable(tool);
  }),
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
      if (!row.installed && row.installable) {
        buttons.push(
          h(NButton, {
            size: "small",
            type: "primary",
            ghost: true,
            loading: pendingTools.value[row.id] === true,
            onClick: () => void runToolAction(row, "install"),
          }, { default: () => t("toolchain.actions.install") }),
        );
      }
      if (row.installed && row.uninstallable) {
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
      <div>
        <p class="eyebrow">{{ $t("toolchain.page.eyebrow") }}</p>
        <h1>{{ $t("toolchain.page.title") }}</h1>
        <p class="page-desc">{{ $t("toolchain.page.description") }}</p>
      </div>
      <div class="page-actions">
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

    <NCard>
      <NSpace vertical :size="12">
        <div class="toolbar-row">
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
            clearable
            class="search-input"
          />
        </div>

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
        <NEmpty
          v-else-if="groupedTools.length === 0"
          :description="$t('toolchain.page.emptySearch')"
        >
          <template #extra>{{ $t("toolchain.page.emptySearchHint") }}</template>
        </NEmpty>

        <template v-else>
          <section v-for="group in groupedTools" :key="group.category">
            <h2 class="group-title">{{ group.category }}</h2>
            <NDataTable
              :columns="toolColumns"
              :data="group.tools"
              :row-key="(row: ToolchainTool) => row.id"
              v-model:checked-row-keys="checkedKeys"
              :bordered="false"
              :pagination="false"
            />
          </section>
        </template>
      </NSpace>
    </NCard>
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
  align-items: flex-start;
  gap: var(--space-4);
  flex-wrap: wrap;
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
.page-actions {
  display: flex;
  gap: var(--space-2);
  align-items: center;
}
.toolbar-row {
  display: flex;
  gap: var(--space-3);
  align-items: center;
  flex-wrap: wrap;
}
.node-select {
  min-width: 240px;
}
.search-input {
  max-width: 280px;
}
.tool-cell {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
}
.tool-name {
  color: var(--fg-2);
  font-weight: 600;
}
.tool-meta {
  font-size: var(--text-xs);
}
.group-title {
  font-size: var(--text-sm);
  font-weight: 600;
  color: var(--fg-2);
  text-transform: uppercase;
  letter-spacing: 0.06em;
  margin: var(--space-2) 0 var(--space-1);
}
.count-pill {
  margin-left: var(--space-2);
}
</style>
