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
import { computed, h, onMounted, ref, watch } from "vue";
import type { VNode } from "vue";
import { useI18n } from "vue-i18n";

import { useServersStore } from "@/features/servers/stores/servers";
import {
  describeToolchainError,
  isTerminalJobStatus,
} from "@/features/toolchain/api/toolchain";
import type {
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
const busyTool = ref<string | null>(null);
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
    return tool !== undefined && !tool.installed && tool.installable;
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

async function handleInstall(tool: ToolchainTool): Promise<void> {
  busyTool.value = tool.id;
  try {
    const skipped = await store.enqueue([tool.id], "install");
    if (skipped.length > 0) {
      message.warning(t("toolchain.page.skipped", { tools: skipped.join(", ") }));
    } else {
      message.success(t("toolchain.page.enqueued", { count: 1 }));
    }
  } catch (err) {
    message.error(describeToolchainError(err));
  } finally {
    busyTool.value = null;
  }
}

async function handleUninstall(tool: ToolchainTool): Promise<void> {
  busyTool.value = tool.id;
  try {
    const skipped = await store.enqueue([tool.id], "uninstall");
    if (skipped.length > 0) {
      message.warning(t("toolchain.page.skipped", { tools: skipped.join(", ") }));
    } else {
      message.success(t("toolchain.page.enqueued", { count: 1 }));
    }
  } catch (err) {
    message.error(describeToolchainError(err));
  } finally {
    busyTool.value = null;
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
    message.success(t("toolchain.page.enqueued", { count: ids.length - skipped.length }));
  } catch (err) {
    message.error(describeToolchainError(err));
  } finally {
    busyBulk.value = false;
  }
}

async function handleCancel(job: ToolchainJob): Promise<void> {
  busyTool.value = job.tool_id;
  try {
    await store.cancel(job.id);
  } catch (err) {
    message.error(describeToolchainError(err));
  } finally {
    busyTool.value = null;
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

const toolColumns = computed<DataTableColumns<ToolchainTool>>(() => [
  { type: "selection" },
  {
    title: "Tool",
    key: "name",
    minWidth: 220,
    render: (row) =>
      h("div", { style: "display:flex;flex-direction:column;gap:2px" }, [
        h("span", { style: "color:var(--fg-2);font-weight:600" }, row.name),
        row.description
          ? h("span", { class: "muted", style: "font-size:var(--text-xs)" }, row.description)
          : null,
        !row.installable && !row.uninstallable
          ? h("span", { class: "muted", style: "font-size:var(--text-xs)" }, t("toolchain.page.detectOnlyNote"))
          : null,
      ]),
  },
  {
    title: "Status",
    key: "status",
    width: 170,
    render: (row) => h(ToolStatusBadge, { tool: row }),
  },
  {
    title: "Queue",
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
                  loading: busyTool.value === row.id,
                  "aria-label": t("toolchain.actions.cancel"),
                  onClick: () => void handleCancel(job),
                }, { default: () => t("toolchain.actions.cancel") })
              : null,
          ],
        },
      );
    },
  },
  {
    title: "Actions",
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
            loading: busyTool.value === row.id,
            onClick: () => void handleInstall(row),
          }, { default: () => t("toolchain.actions.install") }),
        );
      }
      if (row.installed && row.uninstallable) {
        buttons.push(
          h(NButton, {
            size: "small",
            ghost: true,
            loading: busyTool.value === row.id,
            onClick: () => void handleUninstall(row),
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
  void serversStore.fetchServers().catch(() => undefined);
  if (serversStore.servers.length > 0 && store.serverId === "") {
    void selectNode(serversStore.servers[0].id);
  } else if (store.serverId !== "") {
    void store.refresh().catch((err: unknown) => message.error(describeToolchainError(err)));
  }
});

watch(
  () => serversStore.servers.length,
  () => {
    if (store.serverId === "" && serversStore.servers.length > 0) {
      void selectNode(serversStore.servers[0].id);
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
          <span v-if="store.activeJobs.length > 0" class="nav-count">
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
          <span v-if="selectableIds.length > 0" class="nav-count">
            {{ selectableIds.length }}
          </span>
        </NButton>
      </div>
    </div>

    <NCard>
      <NSpace vertical :size="12">
        <NSpace :size="12" align="center">
          <NSelect
            :value="store.serverId === '' ? null : store.serverId"
            :options="nodeOptions"
            :placeholder="$t('toolchain.page.selectNodePlaceholder')"
            :aria-label="$t('toolchain.page.selectNode')"
            style="min-width: 240px"
            @update:value="(id: string) => void selectNode(id)"
          />
          <NInput
            v-model:value="searchQuery"
            :placeholder="$t('toolchain.page.searchPlaceholder')"
            clearable
            style="max-width: 280px"
          />
        </NSpace>

        <NAlert v-if="errorText" type="error" :show-icon="true">
          {{ errorText }}
        </NAlert>

        <NSpin v-if="store.loading" :show="true" />

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
  gap: 16px;
}
.page-head {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 16px;
  flex-wrap: wrap;
}
.eyebrow {
  color: var(--fg-3);
  font-size: var(--text-xs);
  text-transform: uppercase;
  letter-spacing: 0.08em;
  margin: 0 0 4px;
}
.page-head h1 {
  margin: 0;
  font-size: var(--text-2xl);
  color: var(--fg-1);
}
.page-desc {
  color: var(--fg-3);
  margin: 8px 0 0;
  max-width: 640px;
}
.page-actions {
  display: flex;
  gap: 8px;
  align-items: center;
}
.group-title {
  font-size: var(--text-sm);
  font-weight: 600;
  color: var(--fg-2);
  text-transform: uppercase;
  letter-spacing: 0.06em;
  margin: 8px 0 4px;
}
.nav-count {
  margin-left: 6px;
}
</style>
