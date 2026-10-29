<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NEmpty,
  NPopconfirm,
  NSpace,
  NSpin,
  NTag,
  NText,
} from "naive-ui";
import { computed, provide } from "vue";

import BackupProviderDialog from "@/features/backups/components/BackupProviderDialog.vue";
import {
  providerCatalogKey,
  useProviderCatalog,
} from "@/features/backups/composables/useProviderCatalog";

import { i18n } from "@/shared/i18n";

function t(key: string, params?: Record<string, string | number>): string {
  return String(i18n.global.t(key, params ?? {}));
}

const catalog = useProviderCatalog();
provide(providerCatalogKey, catalog);

const providers = computed(() => catalog.backupsStore.providers);
const onlyBuiltin = computed(
  () => providers.value.length > 0 && providers.value.every((item) => item.builtin),
);

function locationLabel(kind: string, endpoint?: string, bucket?: string, prefix?: string): string {
  if (kind !== "s3") {
    return t("databases.backups.targets.controlPlaneDisk");
  }
  return [endpoint, bucket, prefix].filter(Boolean).join(" · ");
}

async function handleDelete(providerId: string, name: string): Promise<void> {
  await catalog.remove(providerId, name);
}
</script>

<template>
  <NCard :title="t('backups.storage.title')">
    <template #header-extra>
      <NButton type="primary" size="small" @click="catalog.openCreate()">
        {{ t("backups.storage.add") }}
      </NButton>
    </template>
    <NAlert
      v-if="catalog.backupsStore.providersError"
      type="error"
      :show-icon="true"
      style="margin-bottom: 12px"
    >
      {{ catalog.backupsStore.providersError }}
    </NAlert>
    <NSpin :show="catalog.backupsStore.providersLoading">
      <NSpace v-if="providers.length > 0" vertical :size="12" style="width: 100%">
        <div
          v-for="provider in providers"
          :id="`backup-provider-${provider.id}`"
          :key="provider.id"
          class="backup-row"
        >
          <div class="backup-row__main">
            <NSpace align="center" :size="8">
              <NText strong>{{ provider.name }}</NText>
              <NTag size="small" :bordered="false">
                {{ provider.kind === "s3" ? t("databases.backups.targets.kindS3") : t("databases.backups.targets.kindLocal") }}
              </NTag>
              <NTag v-if="provider.builtin" size="small" type="info">
                {{ t("backups.storage.builtin") }}
              </NTag>
              <NTag
                v-if="provider.has_credentials"
                size="small"
                type="success"
              >
                {{ t("databases.backups.targets.credsOk") }}
              </NTag>
            </NSpace>
            <NText class="mono backup-row__location" depth="3">
              {{
                locationLabel(
                  provider.kind,
                  provider.endpoint,
                  provider.bucket,
                  provider.prefix,
                )
              }}
            </NText>
            <NText
              v-if="catalog.checks.value[provider.id] && !catalog.checks.value[provider.id].checking"
              :type="catalog.checks.value[provider.id].ok ? 'success' : 'error'"
            >
              {{ catalog.checkMessage(provider.id) }}
            </NText>
          </div>
          <NSpace class="backup-row__actions" align="center" :size="8">
            <NButton
              size="small"
              secondary
              :aria-label="t('databases.backups.targets.testAria', { name: provider.name })"
              :loading="catalog.checks.value[provider.id]?.checking"
              @click="() => void catalog.test(provider.id)"
            >
              {{ t("databases.backups.targets.test") }}
            </NButton>
            <NButton
              size="small"
              secondary
              :disabled="provider.builtin"
              :aria-label="t('databases.backups.targets.editAria', { name: provider.name })"
              @click="catalog.openEdit(provider.id)"
            >
              {{ t("common.actions.edit") }}
            </NButton>
            <NPopconfirm
              v-if="!provider.builtin"
              @positive-click="() => void handleDelete(provider.id, provider.name)"
            >
              <template #trigger>
                <NButton
                  size="small"
                  type="error"
                  ghost
                  :aria-label="t('databases.backups.targets.deleteAria', { name: provider.name })"
                >
                  {{ t("common.actions.delete") }}
                </NButton>
              </template>
              {{ t("databases.backups.targets.deleteConfirm", { name: provider.name }) }}
            </NPopconfirm>
          </NSpace>
        </div>
      </NSpace>
      <NEmpty
        v-else-if="!catalog.backupsStore.providersLoading"
        :description="t('backups.storage.empty')"
      >
        <template #extra>
          <NButton type="primary" @click="catalog.openCreate()">
            {{ t("backups.storage.add") }}
          </NButton>
        </template>
      </NEmpty>
    </NSpin>
    <NAlert
      v-if="onlyBuiltin"
      type="info"
      :show-icon="false"
      style="margin-top: 12px"
    >
      {{ t("backups.storage.onlyBuiltin") }}
    </NAlert>
    <BackupProviderDialog />
  </NCard>
</template>

<style scoped src="./backupRows.css">
</style>
