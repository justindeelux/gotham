<script setup lang="ts">
import { NAlert, NButton, NCard, NDataTable, NEmpty, NSpace, NSpin, NTag, NText } from "naive-ui";
import type { DataTableColumns } from "naive-ui";
import { computed, h } from "vue";
import { useI18n } from "vue-i18n";
import { useRouter } from "vue-router";

import type { StorageMapping } from "@/features/applications/api/applications";
import type {
  ApplicationVolume,
  VolumeListing,
  VolumeMount,
} from "@/features/applications/schemas/volumes";
import { filesDeepLinkRoute, isFilesDeepLink } from "@/features/files";
import type { FilesDeepLink } from "@/features/files";
import StorageEditor from "@/features/applications/components/StorageEditor.vue";

interface Props {
  serverId: string | null;
  sourceType: string;
  volumes: VolumeListing | null;
  volumesLoading: boolean;
  volumesError: string | null;
  storagesDraft: StorageMapping[];
  storagesLoading: boolean;
  storagesError: string | null;
  saveDisabled: boolean;
  saving: boolean;
}

const props = defineProps<Props>();

const emit = defineEmits<{
  "update:draft": [value: StorageMapping[]];
  save: [];
  retry: [];
  refresh: [];
}>();

const { t } = useI18n();
const router = useRouter();

const volumeTableScrollX = 880;

const isCompose = computed<boolean>(() => props.sourceType === "compose");
const rows = computed<ApplicationVolume[]>(() => props.volumes?.volumes ?? []);

function kindLabel(kind: string): string {
  const key = `applications.storageTab.kind.${kind}`;
  const label = t(key);
  return label === key ? kind : label;
}

function stateLabel(state: string): string {
  return t(`applications.storageTab.state.${state}`);
}

function openReasonText(reason: string): string {
  const key = `applications.storageTab.reason.${reason}`;
  const text = t(key);
  return text === key ? reason : text;
}

function mountTarget(mount: VolumeMount): string {
  const consumer = mount.service || mount.container;
  return consumer ? `${mount.destination} · ${consumer}` : mount.destination;
}

function mountMode(mount: VolumeMount): string {
  return mount.read_only
    ? t("applications.storageTab.readOnly")
    : t("applications.storageTab.readWrite");
}

function openRoute(row: ApplicationVolume): ReturnType<typeof filesDeepLinkRoute> | null {
  const target = row.file_manager;
  if (!target) {
    return null;
  }
  const link: FilesDeepLink = {
    serverId: target.server_id,
    root: target.root,
    path: target.path,
  };
  if (!isFilesDeepLink(link, props.serverId ?? "")) {
    return null;
  }
  return filesDeepLinkRoute(link);
}

function unavailableText(row: ApplicationVolume): string {
  if (row.file_manager) {
    return t("applications.storageTab.invalidTarget");
  }
  return row.open_reason
    ? openReasonText(row.open_reason)
    : t("applications.storageTab.openUnavailable");
}

const volumeColumns = computed<DataTableColumns<ApplicationVolume>>(() => [
  {
    title: String(t("applications.storageTab.name")),
    key: "name",
    minWidth: 120,
    ellipsis: { tooltip: true },
    render: (row) => h("span", { class: "volume-mono" }, row.name),
  },
  {
    title: String(t("applications.storageTab.kindLabel")),
    key: "kind",
    width: 95,
    render: (row) => h(NTag, { size: "small", round: true }, { default: () => kindLabel(row.kind) }),
  },
  {
    title: String(t("applications.storageTab.hostPath")),
    key: "host_path",
    minWidth: 180,
    ellipsis: { tooltip: true },
    render: (row) =>
      row.host_path
        ? h("span", { class: "volume-mono" }, row.host_path)
        : h(NText, { depth: 3 }, { default: () => "—" }),
  },
  {
    title: String(t("applications.storageTab.mounts")),
    key: "mounts",
    minWidth: 205,
    render: (row) =>
      h(
        "div",
        { class: "volume-mounts" },
        row.mounts.length > 0
          ? row.mounts.map((mount) =>
              h("div", { class: "volume-mount" }, [
                h("span", { class: "volume-mount__target" }, mountTarget(mount)),
                h(NText, { depth: 3, class: "volume-mount__mode" }, { default: () => mountMode(mount) }),
              ]),
            )
          : [h(NText, { depth: 3 }, { default: () => "—" })],
      ),
  },
  {
    title: String(t("applications.storageTab.stateLabel")),
    key: "state",
    width: 100,
    render: (row) =>
      h(
        NTag,
        { size: "small", round: true, type: row.state === "running" ? "success" : "default" },
        { default: () => stateLabel(row.state) },
      ),
  },
  {
    title: String(t("applications.storageTab.open")),
    key: "open",
    width: 175,
    render: (row) => {
      const route = openRoute(row);
      return route === null
        ? h(
            NText,
            { depth: 3, class: "storage-open-reason" },
            { default: () => unavailableText(row) },
          )
        : h(
            NButton,
            {
              size: "small",
              quaternary: true,
              "aria-label": t("applications.storageTab.openAria", { name: row.name }),
              onClick: () => void router.push(route),
            },
            { default: () => t("applications.storageTab.openAction") },
          );
    },
  },
]);

function volumeRowKey(row: ApplicationVolume): string {
  return `${row.kind}|${row.name}|${row.host_path ?? ""}`;
}
</script>

<template>
  <NCard style="margin-top: 16px" :title="t('applications.storageTab.title')">
    <template #header-extra>
      <NButton size="small" :loading="props.volumesLoading" @click="emit('refresh')">
        {{ t("applications.storageTab.refresh") }}
      </NButton>
    </template>
    <NSpace vertical :size="12">
      <NAlert v-if="props.volumesError" type="error" :show-icon="true">
        <NSpace align="center" :size="12" wrap>
          <span>{{ props.volumesError }}</span>
          <NButton size="small" @click="emit('refresh')">{{ t("common.actions.retry") }}</NButton>
        </NSpace>
      </NAlert>
      <NAlert
        v-if="props.volumes && props.volumes.runtime !== 'available' && props.volumes.reason"
        type="warning"
        :show-icon="true"
      >
        {{ props.volumes.reason }}
      </NAlert>
      <NDataTable
        v-if="rows.length > 0"
        :columns="volumeColumns"
        :data="rows"
        :loading="props.volumesLoading"
        :row-key="volumeRowKey"
        :bordered="false"
        :scroll-x="volumeTableScrollX"
        :pagination="false"
      />
      <NEmpty
        v-else-if="!props.volumesLoading && !props.volumesError"
        :description="t('applications.storageTab.empty')"
      >
        <template #extra>
          <NText depth="3">{{ t(isCompose ? "applications.storageTab.emptyCompose" : "applications.storageTab.emptyHint") }}</NText>
        </template>
      </NEmpty>
    </NSpace>
  </NCard>

  <NCard v-if="isCompose" style="margin-top: 16px" :title="t('applications.storageTab.composeTitle')">
    <NText depth="3">{{ t("applications.storageTab.composeHint") }}</NText>
  </NCard>

  <NCard v-else style="margin-top: 16px" :title="t('applications.storageTab.editorTitle')">
    <template #header-extra>
      <NButton
        type="primary"
        size="small"
        :loading="props.saving"
        :disabled="props.saveDisabled"
        @click="emit('save')"
      >
        {{ t("common.actions.save") }}
      </NButton>
    </template>
    <NSpace vertical :size="12">
      <NAlert v-if="props.storagesError" type="error" :show-icon="true">
        <NSpace align="center" :size="12" wrap>
          <span>{{ props.storagesError }}</span>
          <NButton size="small" @click="emit('retry')">{{ t("common.actions.retry") }}</NButton>
        </NSpace>
      </NAlert>
      <NSpin :show="props.storagesLoading">
        <StorageEditor :model-value="props.storagesDraft" @update:model-value="emit('update:draft', $event)" />
      </NSpin>
    </NSpace>
    <template #footer>
      <NText depth="3">
        {{ t("applications.storageTab.footer") }}
      </NText>
    </template>
  </NCard>
</template>

<style scoped>
:deep(.volume-mono),
:deep(.volume-mount__target) {
  font-family: var(--font-mono);
}

:deep(.volume-mounts) {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
}

:deep(.volume-mount) {
  display: flex;
  flex-direction: column;
  line-height: 1.35;
}

:deep(.volume-mount__mode) {
  font-size: var(--text-xs);
}

.storage-open-reason {
  font-size: var(--text-sm);
}
</style>
