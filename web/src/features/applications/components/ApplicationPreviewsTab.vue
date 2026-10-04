<script setup lang="ts">
import { NAlert, NButton, NCard, NDataTable, NEmpty, NSpace, NTag, NText } from "naive-ui";
import type { DataTableColumns } from "naive-ui";
import { h } from "vue";

import type { Preview } from "@/features/applications/api/previews";
import { previewStateTagType, previewURL } from "@/features/applications/api/previews";
import { relativeTime } from "@/shared/utils/format";

interface Props {
  previews: Preview[];
  previewsLoading: boolean;
  previewsLoaded: boolean;
  previewsError: string | null;
  branch: string;
}

const props = defineProps<Props>();

const emit = defineEmits<{
  refresh: [];
}>();

/** Preview rows shown in the Previews tab, oldest closed last. */
const previewColumns: DataTableColumns<Preview> = [
  {
    title: "PR",
    key: "pr_number",
    width: 90,
    render: (row) => h("span", { class: "mono" }, `#${row.pr_number}`),
  },
  {
    title: "Branch",
    key: "branch",
    minWidth: 160,
    ellipsis: { tooltip: true },
    render: (row) =>
      row.branch
        ? h("span", { class: "mono" }, row.branch)
        : h(NText, { depth: 3 }, { default: () => "—" }),
  },
  {
    title: "Preview URL",
    key: "host",
    minWidth: 300,
    ellipsis: { tooltip: true },
    render: (row) =>
      row.host
        ? h(
            "a",
            {
              class: "mono",
              href: previewURL(row.host),
              target: "_blank",
              rel: "noopener noreferrer",
            },
            row.host,
          )
        : h(NText, { depth: 3 }, { default: () => "—" }),
  },
  {
    title: "State",
    key: "state",
    width: 120,
    render: (row) =>
      h(
        NTag,
        { type: previewStateTagType(row.state), size: "small", round: true },
        { default: () => row.state },
      ),
  },
  {
    title: "Head",
    key: "head_sha",
    width: 100,
    render: (row) =>
      row.head_sha
        ? h("span", { class: "mono" }, row.head_sha.slice(0, 8))
        : h(NText, { depth: 3 }, { default: () => "—" }),
  },
  {
    title: "Created",
    key: "created_at",
    width: 110,
    render: (row) => relativeTime(row.created_at),
  },
  {
    title: "Deleted",
    key: "deleted_at",
    width: 110,
    render: (row) =>
      row.deleted_at
        ? relativeTime(row.deleted_at)
        : h(NText, { depth: 3 }, { default: () => "—" }),
  },
];

/** previewRowKey identifies a preview row by its binding id. */
function previewRowKey(row: Preview): string {
  return row.id;
}
</script>

<template>
  <NCard style="margin-top: 16px" title="Preview deployments">
    <template #header-extra>
      <NButton
        size="small"
        :loading="props.previewsLoading"
        @click="emit('refresh')"
      >
        Refresh
      </NButton>
    </template>
    <NSpace vertical :size="12">
      <NAlert v-if="props.previewsError" type="error" :show-icon="true">
        <NSpace align="center" :size="12" wrap>
          <span>{{ props.previewsError }}</span>
          <NButton size="small" @click="emit('refresh')">
            Retry
          </NButton>
        </NSpace>
      </NAlert>
      <NDataTable
        v-if="props.previews.length > 0"
        :columns="previewColumns"
        :data="props.previews"
        :loading="props.previewsLoading"
        :row-key="previewRowKey"
        :bordered="false"
        :scroll-x="1000"
        :pagination="false"
      />
      <NEmpty
        v-else-if="!props.previewsLoading && props.previewsLoaded && !props.previewsError"
        description="No previews for this application yet."
      >
        <template #extra>
          <NText depth="3">
            A preview is created when a pull request opens against
            <span class="mono">{{ props.branch || "the watched branch" }}</span>,
            and torn down when it closes or merges.
          </NText>
        </template>
      </NEmpty>
    </NSpace>
    <template #footer>
      <NText depth="3">
        Previews are sibling applications: they build the PR head
        branch on a temporary host, and the base application's sealed
        secrets and volumes are deliberately not shared with them.
      </NText>
    </template>
  </NCard>
</template>

<style scoped>
.mono {
  font-family: var(--font-mono);
}
</style>
