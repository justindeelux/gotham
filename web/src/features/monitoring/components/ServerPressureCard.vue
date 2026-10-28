<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NDataTable,
  NEmpty,
  NSpace,
  NSpin,
  NTag,
  NText,
} from "naive-ui";
import type { DataTableColumns } from "naive-ui";
import { computed, h, onMounted, ref, watch } from "vue";
import { useRouter } from "vue-router";
import { useI18n } from "vue-i18n";

import type { FilesystemStatus, NodeStatus } from "@/features/monitoring/api/monitoring";
import {
  describeMonitoringError,
  getNodeStatus,
} from "@/features/monitoring/api/monitoring";
import { formatBytes, relativeTime } from "@/shared/utils/format";

const props = defineProps<{ serverId: string }>();

const { t } = useI18n();
const router = useRouter();
const status = ref<NodeStatus | null>(null);
const loading = ref(false);
const error = ref("");

const active = computed(() => status.value?.active ?? []);
const coverage = computed(() => status.value?.coverage ?? "unknown");

function detectorLabel(detector: string): string {
  switch (detector) {
    case "host_cpu":
      return t("monitoring.detectors.cpu");
    case "host_memory":
      return t("monitoring.detectors.memory");
    case "filesystem_space":
      return t("monitoring.detectors.space");
    case "filesystem_inode":
      return t("monitoring.detectors.inode");
    case "filesystem_forecast":
      return t("monitoring.detectors.forecast");
    default:
      return detector;
  }
}

function horizonLabel(hours: number | undefined): string {
  if (!hours || hours <= 0) {
    return "—";
  }
  if (hours < 48) {
    return `${Math.max(1, Math.round(hours))}h`;
  }
  const days = hours / 24;
  return days < 10 ? `${days.toFixed(1)}d` : `${Math.round(days)}d`;
}

function forecastText(row: FilesystemStatus): { label: string; detail?: string } {
  const forecast = row.forecast;
  if (!forecast) {
    return { label: t("monitoring.forecast.unavailable") };
  }
  switch (forecast.status) {
    case "estimated":
      return {
        label: t("monitoring.forecast.estimated", {
          horizon: horizonLabel(forecast.horizon_hours),
        }),
        detail: t("monitoring.forecast.detail", {
          available: formatBytes(forecast.available_bytes),
          hours: forecast.span_hours,
          fit: Math.round(forecast.fit * 100),
        }),
      };
    case "non_growing":
      return { label: t("monitoring.forecast.nonGrowing") };
    case "stale":
      return { label: t("monitoring.forecast.stale") };
    case "low_confidence":
      return { label: t("monitoring.forecast.lowConfidence") };
    default:
      return { label: t("monitoring.forecast.insufficient") };
  }
}

const columns = computed<DataTableColumns<FilesystemStatus>>(() => [
  {
    title: t("monitoring.filesystems.mount"),
    key: "path",
    render: (row) => h("span", { class: "mono" }, row.path),
  },
  {
    title: t("monitoring.filesystems.purpose"),
    key: "purpose",
  },
  {
    title: t("monitoring.filesystems.space"),
    key: "space",
    render: (row) => {
      const space =
        row.status === "ok"
          ? `${Math.round(row.used_percent)}% · ${t("monitoring.filesystems.available", {
              available: formatBytes(row.available_bytes),
              total: formatBytes(row.total_bytes),
            })}`
          : t("monitoring.filesystems.unsupported");
      const forecast = forecastText(row);
      return h("div", { class: "space-cell" }, [
        h("span", space),
        h("span", { class: "forecast-label" }, forecast.label),
        forecast.detail
          ? h("span", { class: "forecast-detail" }, forecast.detail)
          : null,
      ]);
    },
  },
  {
    title: t("monitoring.filesystems.inodes"),
    key: "inodes",
    render: (row) =>
      row.inode_status === "ok"
        ? `${Math.round(row.inode_used_percent)}% · ${t("monitoring.filesystems.available", {
            available: row.free_inodes.toLocaleString(),
            total: row.total_inodes.toLocaleString(),
          })}`
        : t("monitoring.filesystems.unsupported"),
  },
]);

async function load(): Promise<void> {
  if (!props.serverId) {
    return;
  }
  loading.value = true;
  error.value = "";
  try {
    status.value = await getNodeStatus(props.serverId);
  } catch (err) {
    error.value = describeMonitoringError(err);
  } finally {
    loading.value = false;
  }
}

function openMetrics(): void {
  void router.push({
    name: "server-detail",
    params: { id: props.serverId },
    query: { tab: "metrics" },
  });
}

watch(() => props.serverId, load);
onMounted(load);
</script>

<template>
  <NCard :title="t('monitoring.status.title')">
    <template #header-extra>
      <NSpace align="center" :size="8">
        <NTag
          :type="coverage === 'fresh' ? 'success' : 'warning'"
          :bordered="false"
          size="small"
        >
          {{
            coverage === "fresh"
              ? t("monitoring.status.fresh")
              : coverage === "stale"
                ? t("monitoring.status.stale")
                : t("monitoring.status.unknown")
          }}
        </NTag>
        <NButton size="small" :loading="loading" @click="load">
          {{ t("monitoring.status.refresh") }}
        </NButton>
      </NSpace>
    </template>

    <NSpin :show="loading">
      <NSpace vertical :size="12">
        <NAlert v-if="error" type="error" :show-icon="true">{{ error }}</NAlert>

        <NText depth="3">
          {{ t("monitoring.status.lastObservation") }}
          {{ relativeTime(status?.observed_at) }}
        </NText>

        <NAlert
          v-if="coverage !== 'fresh'"
          type="warning"
          :show-icon="true"
          data-testid="pressure-unknown"
        >
          {{ t("monitoring.status.unknownHint") }}
        </NAlert>

        <template v-if="active.length > 0">
          <NAlert
            v-for="item in active"
            :key="item.detector + item.resource_name"
            :type="item.severity === 'critical' ? 'error' : 'warning'"
            :show-icon="true"
            :data-testid="`pressure-${item.detector}`"
          >
            <template #header>
              {{ detectorLabel(item.detector) }} ·
              {{ item.resource_name }}
              <NTag size="small" :bordered="false">
                {{ t(`monitoring.severity.${item.severity}`) }}
              </NTag>
            </template>
            {{ item.evidence }}
          </NAlert>
        </template>
        <NEmpty
          v-else
          size="small"
          :description="t('monitoring.status.noActive')"
        />

        <NDataTable
          v-if="status?.filesystems?.length"
          size="small"
          :columns="columns"
          :data="status.filesystems"
          :bordered="false"
        />

        <NText depth="3">{{ t("monitoring.status.suggestion") }}</NText>
        <NSpace>
          <NButton size="small" @click="openMetrics">
            {{ t("monitoring.status.openMetrics") }}
          </NButton>
        </NSpace>
      </NSpace>
    </NSpin>
  </NCard>
</template>

<style scoped>
.space-cell {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.forecast-label {
  font-size: var(--text-xs);
  color: var(--fg-2);
}

.forecast-detail {
  font-size: var(--text-xs);
  color: var(--muted);
}
</style>
