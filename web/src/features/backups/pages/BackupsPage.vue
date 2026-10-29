<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NEmpty,
  NInputNumber,
  NModal,
  NPopconfirm,
  NSelect,
  NSpace,
  NSpin,
  NSwitch,
  NTabPane,
  NTabs,
  NTag,
  NText,
} from "naive-ui";
import { onMounted, ref, watch } from "vue";
import { useRoute } from "vue-router";

import { BrandIcon } from "@/features/add-resource";
import BackupProvidersPanel from "@/features/backups/components/BackupProvidersPanel.vue";
import { useBackupsOverview } from "@/features/backups/composables/useBackupsOverview";
import type { BackupRun } from "@/features/backups/api/backups";
import { CRON_PRESETS, describeCron, runDisplay, runDuration, statusTagType } from "@/features/backups/utils/backupStatus";
import { useAuthStore } from "@/features/auth";
import { formatBytes, relativeTime } from "@/shared/utils/format";

import { i18n } from "@/shared/i18n";

function t(key: string, params?: Record<string, string | number>): string {
  return String(i18n.global.t(key, params ?? {}));
}

const route = useRoute();
const authStore = useAuthStore();
const overview = useBackupsOverview();

const activeTab = ref(
  route.query.tab === "storage" && authStore.isOwner ? "storage" : "backups",
);

const presetOptions = CRON_PRESETS.map((cron) => ({
  label: describeCron(cron),
  value: cron,
}));

watch(
  () => authStore.isOwner,
  (isOwner) => {
    if (!isOwner) {
      activeTab.value = "backups";
    }
  },
);

onMounted(() => {
  void overview.load();
});

function keepLastLabel(keepLast?: number | null): string {
  return keepLast === null || keepLast === undefined
    ? t("backups.schedules.keepAll")
    : t("backups.schedules.keepLast", { count: keepLast });
}

function restorePrompt(backup: BackupRun): string {
  return t("backups.restore.prompt", {
    name: overview.resolveResource(backup.resource_type, backup.resource_id)
      .name,
    backup: backup.location || backup.id,
    when: relativeTime(backup.created_at),
    size: formatBytes(backup.size),
  });
}
</script>

<template>
  <div class="backups-page">
    <div class="page-head">
      <div>
        <p class="eyebrow">{{ t("backups.page.eyebrow") }}</p>
        <h1>{{ t("backups.page.title") }}</h1>
        <p class="page-desc">{{ t("backups.page.description") }}</p>
      </div>
      <div class="page-actions">
        <NButton
          v-if="activeTab === 'backups'"
          type="primary"
          @click="overview.openBackupNow()"
        >
          {{ t("backups.backupNow.action") }}
        </NButton>
      </div>
    </div>

    <NTabs v-model:value="activeTab" type="line" animated class="tabs">
      <NTabPane name="backups" :tab="t('backups.tabs.backups')">
        <div class="filters">
          <NSelect
            v-model:value="overview.typeFilter.value"
            :options="overview.typeFilterOptions.value"
            :aria-label="t('backups.filters.typeAria')"
            style="width: 200px"
          />
          <NSelect
            v-model:value="overview.statusFilter.value"
            :options="overview.statusFilterOptions.value"
            :aria-label="t('backups.filters.statusAria')"
            style="width: 200px"
          />
          <NButton
            secondary
            :loading="overview.backupsStore.allLoading"
            @click="() => void overview.refresh()"
          >
            {{ t("backups.filters.refresh") }}
          </NButton>
        </div>

        <NAlert
          v-if="overview.backupsStore.backupsError"
          type="error"
          :show-icon="true"
          style="margin-bottom: 12px"
        >
          {{ overview.backupsStore.backupsError }}
        </NAlert>

        <NCard :title="t('backups.runs.title')">
          <NSpin :show="overview.backupsStore.allLoading">
            <NEmpty
              v-if="overview.filteredBackups.value.length === 0"
              :description="
                overview.backupsStore.allBackups.length === 0
                  ? t('backups.runs.empty')
                  : t('backups.runs.emptyFiltered')
              "
            >
              <template #extra>
                <p class="empty-hint">{{ t("backups.runs.emptyHint") }}</p>
                <NButton type="primary" @click="overview.openBackupNow()">
                  {{ t("backups.backupNow.action") }}
                </NButton>
              </template>
            </NEmpty>
            <table v-else class="backups-table">
              <thead>
                <tr>
                  <th scope="col">{{ t("backups.runs.columns.resource") }}</th>
                  <th scope="col">{{ t("backups.runs.columns.status") }}</th>
                  <th scope="col">{{ t("backups.runs.columns.size") }}</th>
                  <th scope="col">{{ t("backups.runs.columns.provider") }}</th>
                  <th scope="col">{{ t("backups.runs.columns.started") }}</th>
                  <th scope="col">{{ t("backups.runs.columns.duration") }}</th>
                  <th scope="col">
                    <span class="visually-hidden">{{ t("backups.runs.columns.actions") }}</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="backup in overview.filteredBackups.value" :key="backup.id">
                  <td :data-label="t('backups.runs.columns.resource')">
                    <div class="resource-cell">
                      <BrandIcon
                        v-if="overview.resolveResource(backup.resource_type, backup.resource_id).brand"
                        v-bind="overview.resolveResource(backup.resource_type, backup.resource_id).brand!"
                        size="sm"
                      />
                      <RouterLink
                        v-if="overview.resolveResource(backup.resource_type, backup.resource_id).link"
                        :to="overview.resolveResource(backup.resource_type, backup.resource_id).link!"
                      >
                        {{ overview.resolveResource(backup.resource_type, backup.resource_id).name }}
                      </RouterLink>
                      <NText v-else>
                        {{ overview.resolveResource(backup.resource_type, backup.resource_id).name }}
                      </NText>
                      <NTag size="small" :bordered="false">
                        {{ overview.resourceTypeLabel(backup.resource_type) }}
                      </NTag>
                    </div>
                  </td>
                  <td :data-label="t('backups.runs.columns.status')">
                    <NTag
                      v-if="backup.cleanup_pending"
                      type="warning"
                      size="small"
                    >
                      {{ t("backups.runs.cleanupPending") }}
                    </NTag>
                    <NTag v-else :type="statusTagType(backup.status)" size="small">
                      {{ runDisplay("runStatus", backup.status) }}
                    </NTag>
                    <NText
                      v-if="backup.cleanup_pending && backup.cleanup_error"
                      depth="3"
                      class="cleanup-error"
                    >
                      {{ backup.cleanup_error }}
                    </NText>
                  </td>
                  <td :data-label="t('backups.runs.columns.size')">
                    <NText class="mono">
                      {{
                        backup.status === "running"
                          ? t("databases.backups.runs.sizePending")
                          : formatBytes(backup.size)
                      }}
                    </NText>
                  </td>
                  <td :data-label="t('backups.runs.columns.provider')">
                    {{
                      overview.providerName(
                        backup.provider_id,
                        backup.provider_name,
                        backup.provider_removed,
                      )
                    }}
                  </td>
                  <td :data-label="t('backups.runs.columns.started')">
                    <span :title="backup.created_at">{{ relativeTime(backup.created_at) }}</span>
                  </td>
                  <td :data-label="t('backups.runs.columns.duration')">
                    {{ runDuration(backup.created_at, backup.finished_at) }}
                  </td>
                  <td class="actions-cell" :data-label="t('backups.runs.columns.actions')">
                    <NSpace v-if="backup.cleanup_pending" :size="6">
                      <NButton
                        size="tiny"
                        secondary
                        @click="() => void overview.handleRetryCleanup(backup)"
                      >
                        {{ t("backups.runs.retryCleanup") }}
                      </NButton>
                      <NPopconfirm
                        @positive-click="
                          () => void overview.handleDiscardCleanup(backup)
                        "
                      >
                        <template #trigger>
                          <NButton size="tiny" type="error" ghost>
                            {{ t("backups.runs.discardCleanup") }}
                          </NButton>
                        </template>
                        {{ t("backups.runs.discardCleanupConfirm") }}
                      </NPopconfirm>
                    </NSpace>
                    <NSpace v-else :size="6">
                      <NButton
                        size="tiny"
                        secondary
                        :disabled="backup.status !== 'completed'"
                        @click="overview.openRestore(backup)"
                      >
                        {{ t("backups.runs.restore") }}
                      </NButton>
                      <NPopconfirm
                        @positive-click="() => void overview.handleDeleteBackup(backup)"
                      >
                        <template #trigger>
                          <NButton
                            size="tiny"
                            type="error"
                            ghost
                            :disabled="backup.status === 'running'"
                          >
                            {{ t("common.actions.delete") }}
                          </NButton>
                        </template>
                        {{ t("databases.backups.runs.deleteConfirm") }}
                      </NPopconfirm>
                    </NSpace>
                  </td>
                </tr>
              </tbody>
            </table>
          </NSpin>
        </NCard>

        <NCard :title="t('backups.schedules.title')" class="schedules-card">
          <template #header-extra>
            <NButton size="small" secondary @click="overview.openScheduleCreate()">
              {{ t("backups.schedules.add") }}
            </NButton>
          </template>
          <NSpin :show="overview.backupsStore.schedulesLoading">
            <NEmpty
              v-if="overview.schedules.value.length === 0"
              :description="t('backups.schedules.empty')"
            />
            <NSpace v-else vertical :size="12" style="width: 100%">
              <div
                v-for="schedule in overview.schedules.value"
                :key="schedule.id"
                class="backup-row"
              >
                <div class="backup-row__main">
                  <NSpace align="center" :size="8">
                    <NText strong>{{ describeCron(schedule.cron) }}</NText>
                    <NTag size="small" :bordered="false">
                      {{ overview.resolveResource(schedule.resource_type, schedule.resource_id).name }}
                    </NTag>
                    <NTag size="small" :bordered="false">
                      {{ overview.providerName(schedule.provider_id) }}
                    </NTag>
                  </NSpace>
                  <NText depth="3">
                    {{ t("databases.backups.schedules.nextRun") }}
                    <span :title="schedule.next_run_at">{{ relativeTime(schedule.next_run_at) }}</span>
                    <span v-if="schedule.last_run_at">
                      · {{ t("databases.backups.schedules.lastRun") }}
                      <span :title="schedule.last_run_at">{{ relativeTime(schedule.last_run_at) }}</span>
                    </span>
                    <span> · {{ keepLastLabel(schedule.keep_last) }}</span>
                  </NText>
                </div>
                <NSpace class="backup-row__actions" align="center" :size="8">
                  <NSwitch
                    :value="schedule.enabled"
                    :aria-label="t('databases.backups.schedules.enableAria', { cron: schedule.cron })"
                    :loading="overview.backupsStore.schedulesActing"
                    @update:value="
                      (enabled: boolean) =>
                        void overview.handleToggleSchedule(schedule, enabled)
                    "
                  >
                    <template #checked>{{ t("databases.backups.schedules.on") }}</template>
                    <template #unchecked>{{ t("databases.backups.schedules.off") }}</template>
                  </NSwitch>
                  <NPopconfirm
                    @positive-click="() => void overview.handleDeleteSchedule(schedule)"
                  >
                    <template #trigger>
                      <NButton
                        size="small"
                        type="error"
                        ghost
                        :aria-label="t('databases.backups.schedules.deleteAria', { cron: schedule.cron })"
                      >
                        {{ t("common.actions.delete") }}
                      </NButton>
                    </template>
                    {{ t("databases.backups.schedules.deleteConfirm") }}
                  </NPopconfirm>
                </NSpace>
              </div>
            </NSpace>
          </NSpin>
        </NCard>
      </NTabPane>

      <NTabPane
        v-if="authStore.isOwner"
        name="storage"
        :tab="t('backups.tabs.storage')"
      >
        <BackupProvidersPanel />
      </NTabPane>
    </NTabs>

    <NModal
      v-model:show="overview.backupDialogOpen.value"
      preset="card"
      class="app-modal"
      :title="t('backups.backupNow.title')"
      style="width: 520px; max-width: 94vw"
    >
      <NSpace vertical :size="12">
        <NSelect
          v-model:value="overview.newResourceKey.value"
          :options="overview.resourceSelectOptions.value"
          :placeholder="t('backups.backupNow.resource')"
          :aria-label="t('backups.backupNow.resource')"
        />
        <NSelect
          v-if="overview.isOwner.value"
          v-model:value="overview.newProviderId.value"
          :options="overview.providerOptions.value"
          :placeholder="t('backups.backupNow.provider')"
          :aria-label="t('backups.backupNow.provider')"
        />
        <NText depth="3">{{ t("backups.backupNow.note") }}</NText>
        <NSpace justify="end" :size="8">
          <NButton @click="overview.backupDialogOpen.value = false">
            {{ t("common.actions.cancel") }}
          </NButton>
          <NButton
            type="primary"
            :loading="overview.backupsStore.backupsActing"
            @click="() => void overview.submitBackupNow()"
          >
            {{ t("backups.backupNow.submit") }}
          </NButton>
        </NSpace>
      </NSpace>
    </NModal>

    <NModal
      v-model:show="overview.scheduleDialogOpen.value"
      preset="card"
      class="app-modal"
      :title="t('backups.schedules.add')"
      style="width: 520px; max-width: 94vw"
    >
      <NSpace vertical :size="12">
        <NSelect
          v-model:value="overview.scheduleResourceKey.value"
          :options="overview.resourceSelectOptions.value"
          :placeholder="t('backups.schedule.chooseResource')"
          :aria-label="t('backups.schedule.chooseResource')"
        />
        <NSelect
          v-if="overview.isOwner.value"
          v-model:value="overview.scheduleProviderId.value"
          :options="overview.providerOptions.value"
          :placeholder="t('backups.backupNow.provider')"
          :aria-label="t('backups.backupNow.provider')"
        />
        <NSelect
          v-model:value="overview.scheduleCron.value"
          :options="presetOptions"
          :aria-label="t('backups.schedule.preset')"
        />
        <NSpace align="center" :size="8">
          <NText depth="3">{{ t("databases.backups.protection.maxRunLabel") }}</NText>
          <NInputNumber
            v-model:value="overview.scheduleMaxRunMinutes.value"
            :min="15"
            :max="1440"
            :step="15"
            style="width: 150px"
            :aria-label="t('databases.backups.protection.maxRunAria')"
          >
            <template #suffix>
              {{ t("databases.backups.protection.minutesSuffix") }}
            </template>
          </NInputNumber>
          <NText depth="3">{{ t("databases.backups.schedules.on") }}</NText>
          <NSwitch v-model:value="overview.scheduleEnabled.value" />
        </NSpace>
        <NSpace align="center" :size="8">
          <NText depth="3">{{ t("backups.schedules.keepLastLabel") }}</NText>
          <NInputNumber
            v-model:value="overview.scheduleKeepLast.value"
            :min="1"
            :max="365"
            style="width: 120px"
            :aria-label="t('backups.schedules.keepLastLabel')"
          />
        </NSpace>
        <NText depth="3">{{ t("backups.schedules.keepLastHint") }}</NText>
        <NSpace justify="end" :size="8">
          <NButton @click="overview.scheduleDialogOpen.value = false">
            {{ t("common.actions.cancel") }}
          </NButton>
          <NButton
            type="primary"
            :loading="overview.backupsStore.schedulesActing"
            @click="() => void overview.submitSchedule()"
          >
            {{ t("backups.schedules.add") }}
          </NButton>
        </NSpace>
      </NSpace>
    </NModal>

    <NModal
      v-model:show="overview.restoreOpen.value"
      preset="card"
      class="app-modal"
      :title="t('backups.restore.title')"
      style="width: 520px; max-width: 94vw"
    >
      <NSpace v-if="overview.restoreCandidate.value" vertical :size="12">
        <NText>{{ restorePrompt(overview.restoreCandidate.value) }}</NText>
        <NAlert type="warning" :show-icon="true">
          {{
            t(
              `backups.restore.warning${
                overview.restoreCandidate.value.resource_type === "database"
                  ? "Database"
                  : overview.restoreCandidate.value.resource_type === "service"
                    ? "Service"
                    : "Application"
              }`,
            )
          }}
        </NAlert>
        <NSpace justify="end" :size="8">
          <NButton @click="overview.restoreOpen.value = false">
            {{ t("common.actions.cancel") }}
          </NButton>
          <NButton
            type="warning"
            :loading="overview.restoring.value"
            @click="() => void overview.handleRestoreConfirm()"
          >
            {{ t("backups.restore.confirm") }}
          </NButton>
        </NSpace>
      </NSpace>
    </NModal>
  </div>
</template>

<style scoped src="./backupsPage.css">
</style>
