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
import { computed, onMounted } from "vue";

import StorageDialog from "@/features/storages/components/StorageDialog.vue";
import { useStoragesPage } from "@/features/storages/composables/useStoragesPage";
import { formatBytes } from "@/shared/utils/format";

import { i18n } from "@/shared/i18n";

function t(key: string, params?: Record<string, string | number>): string {
  return String(i18n.global.t(key, params ?? {}));
}

const page = useStoragesPage();
const storages = computed(() => page.store.items);

function locationLabel(
  kind: string,
  endpoint?: string,
  bucket?: string,
  prefix?: string,
): string {
  if (kind !== "s3") {
    return t("storages.controlPlaneDisk");
  }
  return [endpoint, bucket, prefix].filter(Boolean).join(" · ");
}

onMounted(() => {
  void page.load();
});
</script>

<template>
  <NCard :title="t('storages.title')">
    <template #header-extra>
      <NButton type="primary" size="small" @click="page.openCreate()">
        {{ t("storages.page.add") }}
      </NButton>
    </template>
    <NAlert
      v-if="page.store.error"
      type="error"
      :show-icon="true"
      style="margin-bottom: 12px"
    >
      {{ page.store.error }}
    </NAlert>
    <NSpin :show="page.store.loading">
      <NSpace v-if="storages.length > 0" vertical :size="12" style="width: 100%">
        <div
          v-for="storage in storages"
          :id="`storage-${storage.id}`"
          :key="storage.id"
          class="storage-row"
        >
          <div class="storage-row__main">
            <NSpace align="center" :size="8">
              <NText strong>{{ storage.name }}</NText>
              <NTag size="small" :bordered="false">
                {{
                  storage.kind === "s3"
                    ? t("storages.kindS3")
                    : t("storages.kindLocal")
                }}
              </NTag>
              <NTag v-if="storage.builtin" size="small" type="info">
                {{ t("storages.builtin") }}
              </NTag>
              <NTag v-if="storage.has_credentials" size="small" type="success">
                {{ t("storages.credsOk") }}
              </NTag>
            </NSpace>
            <NText class="mono storage-row__location" depth="3">
              {{
                locationLabel(
                  storage.kind,
                  storage.endpoint,
                  storage.bucket,
                  storage.prefix,
                )
              }}
            </NText>
            <NText depth="3">
              {{
                t("storages.usage", {
                  count: storage.backup_count,
                  size: formatBytes(storage.total_size),
                })
              }}
            </NText>
            <NText
              v-if="storage.kind === 'local' && storage.free_space != null"
              depth="3"
            >
              {{
                t("storages.freeSpace", {
                  free: formatBytes(storage.free_space),
                  total: formatBytes(storage.total_space ?? 0),
                })
              }}
            </NText>
            <NText
              v-if="page.checks.value[storage.id] && !page.checks.value[storage.id].checking"
              :type="page.checks.value[storage.id].ok ? 'success' : 'error'"
            >
              {{ page.checkMessage(storage.id) }}
            </NText>
          </div>
          <NSpace class="storage-row__actions" align="center" :size="8">
            <NButton
              size="small"
              secondary
              :aria-label="t('storages.aria.test', { name: storage.name })"
              :loading="page.checks.value[storage.id]?.checking"
              @click="() => void page.test(storage.id)"
            >
              {{ t("storages.test") }}
            </NButton>
            <NButton
              size="small"
              secondary
              :disabled="storage.builtin"
              :aria-label="t('storages.aria.edit', { name: storage.name })"
              @click="page.openEdit(storage.id)"
            >
              {{ t("common.actions.edit") }}
            </NButton>
            <NPopconfirm
              v-if="!storage.builtin"
              @positive-click="() => void page.remove(storage.id, storage.name)"
            >
              <template #trigger>
                <NButton
                  size="small"
                  type="error"
                  ghost
                  :aria-label="t('storages.aria.delete', { name: storage.name })"
                >
                  {{ t("common.actions.delete") }}
                </NButton>
              </template>
              {{ t("storages.deleteConfirm", { name: storage.name }) }}
            </NPopconfirm>
          </NSpace>
        </div>
      </NSpace>
      <NEmpty
        v-else-if="!page.store.loading"
        :description="t('storages.empty')"
      >
        <template #extra>
          <NButton type="primary" @click="page.openCreate()">
            {{ t("storages.page.add") }}
          </NButton>
        </template>
      </NEmpty>
    </NSpin>
    <NAlert
      v-if="page.onlyBuiltin.value"
      type="info"
      :show-icon="false"
      style="margin-top: 12px"
    >
      {{ t("storages.onlyBuiltin") }}
    </NAlert>
    <StorageDialog :state="page" />
  </NCard>
</template>

<style scoped src="./storagesPanel.css"></style>
