<script setup lang="ts">
import { NAlert, NButton, NCard, NDataTable, NEmpty, NSpace, NTag, NText } from "naive-ui";
import type { DataTableColumns } from "naive-ui";
import { computed, h } from "vue";
import { useI18n } from "vue-i18n";

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

const { t } = useI18n();

/**
 * Preview rows shown in the Previews tab, oldest closed last. Computed so a
 * language switch relabels headers without losing table state; hosts, SHAs
 * and branch names stay verbatim.
 */
const previewColumns = computed<DataTableColumns<Preview>>(() => [
  {
    title: String(t("applications.previewsTab.pr")),
    key: "pr_number",
    width: 90,
    render: (row) => h("span", { class: "mono" }, `#${row.pr_number}`),
  },
  {
    title: String(t("applications.previewsTab.branch")),
    key: "branch",
    minWidth: 160,
    ellipsis: { tooltip: true },
    render: (row) =>
      row.branch
        ? h("span", { class: "mono" }, row.branch)
        : h(NText, { depth: 3 }, { default: () => "—" }),
  },
  {
    title: String(t("applications.previewsTab.url")),
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
    title: String(t("applications.previewsTab.state")),
    key: "state",
    width: 120,
    render: (row) =>
      h(
        NTag,
        { type: previewStateTagType(row.state), size: "small", round: true },
        { default: () => String(t(`applications.previewState.${row.state}`)) },
      ),
  },
  {
    title: String(t("applications.previewsTab.head")),
    key: "head_sha",
    width: 100,
    render: (row) =>
      row.head_sha
        ? h("span", { class: "mono" }, row.head_sha.slice(0, 8))
        : h(NText, { depth: 3 }, { default: () => "—" }),
  },
  {
    title: String(t("applications.previewsTab.created")),
    key: "created_at",
    width: 110,
    render: (row) => relativeTime(row.created_at),
  },
  {
    title: String(t("applications.previewsTab.deleted")),
    key: "deleted_at",
    width: 110,
    render: (row) =>
      row.deleted_at
        ? relativeTime(row.deleted_at)
        : h(NText, { depth: 3 }, { default: () => "—" }),
  },
]);

/** previewRowKey identifies a preview row by its binding id. */
function previewRowKey(row: Preview): string {
  return row.id;
}
</script>

<template>
  <NCard style="margin-top: 16px" :title="t('applications.previewsTab.title')">
    <template #header-extra>
      <NButton
        size="small"
        :loading="props.previewsLoading"
        @click="emit('refresh')"
      >
        {{ t("applications.previewsTab.refresh") }}
      </NButton>
    </template>
    <NSpace vertical :size="12">
      <NAlert v-if="props.previewsError" type="error" :show-icon="true">
        <NSpace align="center" :size="12" wrap>
          <span>{{ props.previewsError }}</span>
          <NButton size="small" @click="emit('refresh')">
            {{ t("common.actions.retry") }}
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
        :description="t('applications.previewsTab.empty')"
      >
        <template #extra>
          <NText depth="3">
            {{ t("applications.previewsTab.emptyStart") }}
            <span class="mono">{{ props.branch || t("applications.previewsTab.emptyBranchFallback") }}</span>,
            {{ t("applications.previewsTab.emptyEnd") }}
          </NText>
        </template>
      </NEmpty>
    </NSpace>
    <template #footer>
      <NText depth="3">
        {{ t("applications.previewsTab.footer") }}
      </NText>
    </template>
  </NCard>
</template>

<style scoped>
.mono {
  font-family: var(--font-mono);
}
</style>
