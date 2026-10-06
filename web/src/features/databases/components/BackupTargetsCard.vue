<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NEmpty,
  NInput,
  NPopconfirm,
  NSelect,
  NSpace,
  NSpin,
  NTag,
  NText,
} from "naive-ui";
import { inject } from "vue";

import { databaseBackupsKey } from "@/features/databases/composables/useDatabaseBackups";

import { i18n } from "@/shared/i18n";

/** t resolves a databases/common message in the current locale. */
function t(key: string, params?: Record<string, string | number>): string {
  return String(i18n.global.t(key, params ?? {}));
}

const backups = inject(databaseBackupsKey)!;
</script>

<template>
  <NCard :title="t('databases.backups.targets.title')">
    <template #header-extra>
      <NText depth="3">{{ t("databases.backups.targets.subtitle") }}</NText>
    </template>
    <NAlert
      v-if="backups.backupsStore.targetsError"
      type="error"
      :show-icon="true"
      style="margin-bottom: 12px"
    >
      {{ backups.backupsStore.targetsError }}
    </NAlert>
    <NSpin :show="backups.backupsStore.targetsLoading">
      <NSpace
        v-if="backups.backupsStore.targets.length > 0"
        vertical
        :size="12"
        style="width: 100%"
      >
        <div
          v-for="target in backups.backupsStore.targets"
          :key="target.id"
          class="backup-row"
        >
          <div class="backup-row__main">
            <NSpace align="center" :size="8">
              <NText strong>{{ target.name }}</NText>
              <NTag size="small" :bordered="false">
                {{ target.kind === "s3" ? t("databases.backups.targets.kindS3") : t("databases.backups.targets.kindLocal") }}
              </NTag>
              <NTag
                v-if="target.has_credentials"
                size="small"
                type="success"
              >
                {{ t("databases.backups.targets.credsOk") }}
              </NTag>
              <NTag v-else size="small" type="warning">
                {{ t("databases.backups.targets.credsMissing") }}
              </NTag>
            </NSpace>
            <NText class="mono backup-row__location" depth="3">
              {{
                target.kind === "s3"
                  ? `${target.endpoint ?? ""} · ${target.bucket ?? ""}${target.prefix ? ` · ${target.prefix}` : ""}`
                  : t("databases.backups.targets.controlPlaneDisk")
              }}
            </NText>
            <NText
              v-if="
                backups.targetTests.value[target.id] &&
                !backups.targetTests.value[target.id].checking
              "
              :type="
                backups.targetTests.value[target.id].ok ? 'success' : 'error'
              "
            >
              {{ backups.targetTests.value[target.id].message }}
            </NText>
          </div>
          <NSpace class="backup-row__actions" align="center" :size="8">
            <NButton
              size="small"
              secondary
              :aria-label="t('databases.backups.targets.testAria', { name: target.name })"
              :loading="backups.targetTests.value[target.id]?.checking"
              @click="() => void backups.handleTestTarget(target.id)"
            >
              {{ t("databases.backups.targets.test") }}
            </NButton>
            <NButton
              size="small"
              secondary
              :aria-label="t('databases.backups.targets.editAria', { name: target.name })"
              @click="backups.openTargetEdit(target.id)"
            >
              {{ t("common.actions.edit") }}
            </NButton>
            <NPopconfirm
              @positive-click="
                () => void backups.handleDeleteTarget(target.id, target.name)
              "
            >
              <template #trigger>
                <NButton
                  size="small"
                  type="error"
                  ghost
                  :aria-label="t('databases.backups.targets.deleteAria', { name: target.name })"
                >
                  {{ t("common.actions.delete") }}
                </NButton>
              </template>
              {{ t("databases.backups.targets.deleteConfirm", { name: target.name }) }}
            </NPopconfirm>
          </NSpace>
        </div>
      </NSpace>
      <NEmpty
        v-else-if="!backups.backupsStore.targetsLoading"
        :description="t('databases.backups.targets.empty')"
      >
        <template #extra>
          <p class="empty-hint">
            {{ t("databases.backups.targets.emptyHint") }}
          </p>
        </template>
      </NEmpty>
    </NSpin>
    <div class="target-form">
      <NText strong>
        {{ backups.targetEditingId.value === null ? t("databases.backups.targets.newTitle") : t("databases.backups.targets.editTitle") }}
      </NText>
      <div class="target-form__grid">
        <NInput
          v-model:value="backups.targetName.value"
          :placeholder="t('databases.backups.targets.namePlaceholder')"
          :aria-label="t('databases.backups.targets.nameAria')"
        />
        <NSelect
          v-model:value="backups.targetKind.value"
          :options="backups.targetKindOptions.value"
          :aria-label="t('databases.backups.targets.kindAria')"
        />
      </div>
      <template v-if="backups.targetKind.value === 's3'">
        <div class="target-form__grid">
          <NInput
            v-model:value="backups.targetEndpoint.value"
            class="mono"
            :placeholder="t('databases.backups.targets.endpointPlaceholder')"
            :aria-label="t('databases.backups.targets.endpointAria')"
          />
          <NInput
            v-model:value="backups.targetRegion.value"
            class="mono"
            :placeholder="t('databases.backups.targets.regionPlaceholder')"
            :aria-label="t('databases.backups.targets.regionAria')"
          />
        </div>
        <div class="target-form__grid">
          <NInput
            v-model:value="backups.targetBucket.value"
            class="mono"
            :placeholder="t('databases.backups.targets.bucketPlaceholder')"
            :aria-label="t('databases.backups.targets.bucketAria')"
          />
          <NInput
            v-model:value="backups.targetPrefix.value"
            class="mono"
            :placeholder="t('databases.backups.targets.prefixPlaceholder')"
            :aria-label="t('databases.backups.targets.prefixAria')"
          />
        </div>
        <div class="target-form__grid">
          <NInput
            v-model:value="backups.targetAccessKey.value"
            type="password"
            class="mono"
            :placeholder="
              backups.targetEditingId.value === null
                ? t('databases.backups.targets.accessKeyPlaceholder')
                : t('databases.backups.targets.accessKeyEditPlaceholder')
            "
            :aria-label="t('databases.backups.targets.accessKeyAria')"
          />
          <NInput
            v-model:value="backups.targetSecretKey.value"
            type="password"
            class="mono"
            :placeholder="
              backups.targetEditingId.value === null
                ? t('databases.backups.targets.secretKeyPlaceholder')
                : t('databases.backups.targets.secretKeyEditPlaceholder')
            "
            :aria-label="t('databases.backups.targets.secretKeyAria')"
          />
        </div>
        <NText depth="3">
          {{ t("databases.backups.targets.secretsNote") }}
        </NText>
      </template>
      <NSpace justify="end" :size="8">
        <NButton
          v-if="backups.targetEditingId.value !== null"
          @click="backups.resetTargetForm()"
        >
          {{ t("common.actions.cancel") }}
        </NButton>
        <NButton
          type="primary"
          :loading="backups.backupsStore.targetsActing"
          @click="() => void backups.handleSaveTarget()"
        >
          {{
            backups.targetEditingId.value === null ? t("databases.backups.targets.add") : t("databases.backups.targets.save")
          }}
        </NButton>
      </NSpace>
    </div>
  </NCard>
</template>

<style scoped src="./backupRows.css">
</style>

<style scoped>
.target-form {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
  margin-top: var(--space-4);
  padding-top: var(--space-4);
  border-top: 1px dashed var(--border);
}

.target-form__grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: var(--space-2);
}

@media (max-width: 640px) {
  .target-form__grid {
    grid-template-columns: 1fr;
  }
}
</style>
