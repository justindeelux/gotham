<script setup lang="ts">
import {
  NAlert,
  NButton,
  NForm,
  NFormItem,
  NInput,
  NModal,
  NSelect,
  NSpace,
  NText,
} from "naive-ui";
import { computed, inject } from "vue";

import { providerCatalogKey } from "@/features/backups/composables/useProviderCatalog";
import { i18n } from "@/shared/i18n";

function t(key: string): string {
  return String(i18n.global.t(key));
}

const catalog = inject(providerCatalogKey)!;

const kindOptions = computed(() => [
  { label: t("databases.backups.targets.kindS3"), value: "s3" },
  { label: t("databases.backups.targets.kindLocal"), value: "local" },
]);
</script>

<template>
  <NModal
    v-model:show="catalog.dialogOpen.value"
    preset="card"
    class="app-modal"
    :title="catalog.editingId.value === null ? t('backups.storage.addTitle') : t('backups.storage.editTitle')"
    style="width: 560px; max-width: 94vw"
    @after-leave="catalog.resetForm()"
  >
    <NForm label-placement="top" :show-feedback="false" class="provider-form form-container">
      <div class="form-row">
        <NFormItem :label="t('backups.storage.name')" class="field-name">
          <NInput
            v-model:value="catalog.name.value"
            :placeholder="t('databases.backups.targets.namePlaceholder')"
            :aria-label="t('databases.backups.targets.nameAria')"
          />
        </NFormItem>
        <NFormItem :label="t('backups.storage.kind')" class="field-kind">
          <NSelect
            v-model:value="catalog.kind.value"
            :options="kindOptions"
            :aria-label="t('databases.backups.targets.kindAria')"
          />
        </NFormItem>
      </div>
      <template v-if="catalog.kind.value === 's3'">
        <div class="form-row">
          <NFormItem :label="t('backups.storage.endpoint')" class="field-endpoint">
            <NInput
              v-model:value="catalog.endpoint.value"
              class="mono"
              :placeholder="t('databases.backups.targets.endpointPlaceholder')"
              :aria-label="t('databases.backups.targets.endpointAria')"
            />
          </NFormItem>
          <NFormItem :label="t('backups.storage.region')" class="field-region">
            <NInput
              v-model:value="catalog.region.value"
              class="mono"
              :placeholder="t('databases.backups.targets.regionPlaceholder')"
              :aria-label="t('databases.backups.targets.regionAria')"
            />
          </NFormItem>
        </div>
        <div class="form-row">
          <NFormItem :label="t('backups.storage.bucket')" class="field-bucket">
            <NInput
              v-model:value="catalog.bucket.value"
              class="mono"
              :placeholder="t('databases.backups.targets.bucketPlaceholder')"
              :aria-label="t('databases.backups.targets.bucketAria')"
            />
          </NFormItem>
          <NFormItem :label="t('backups.storage.prefix')" class="field-prefix">
            <NInput
              v-model:value="catalog.prefix.value"
              class="mono"
              :placeholder="t('databases.backups.targets.prefixPlaceholder')"
              :aria-label="t('databases.backups.targets.prefixAria')"
            />
          </NFormItem>
        </div>
        <div class="form-row">
          <NFormItem :label="t('backups.storage.accessKey')" class="field-key">
            <NInput
              v-model:value="catalog.accessKey.value"
              type="password"
              class="mono"
              :placeholder="
                catalog.editingId.value === null
                  ? t('databases.backups.targets.accessKeyPlaceholder')
                  : t('databases.backups.targets.accessKeyEditPlaceholder')
              "
              :aria-label="t('databases.backups.targets.accessKeyAria')"
            />
          </NFormItem>
          <NFormItem :label="t('backups.storage.secretKey')" class="field-key">
            <NInput
              v-model:value="catalog.secretKey.value"
              type="password"
              class="mono"
              :placeholder="
                catalog.editingId.value === null
                  ? t('databases.backups.targets.secretKeyPlaceholder')
                  : t('databases.backups.targets.secretKeyEditPlaceholder')
              "
              :aria-label="t('databases.backups.targets.secretKeyAria')"
            />
          </NFormItem>
        </div>
        <NAlert type="info" :show-icon="false">
          {{ t("databases.backups.targets.secretsNote") }}
        </NAlert>
      </template>
      <NText v-else depth="3">
        {{ t("backups.storage.localNote") }}
      </NText>
    </NForm>
    <template #footer>
      <NSpace justify="end" :size="8">
        <NButton @click="catalog.close()">
          {{ t("common.actions.cancel") }}
        </NButton>
        <NButton
          type="primary"
          :loading="catalog.backupsStore.providersActing"
          @click="() => void catalog.save()"
        >
          {{
            catalog.editingId.value === null
              ? t("backups.storage.add")
              : t("common.actions.save")
          }}
        </NButton>
      </NSpace>
    </template>
  </NModal>
</template>

<style scoped>
.provider-form {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}

.form-row {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: var(--space-3);
}

.mono {
  font-family: var(--font-mono);
}

@media (max-width: 640px) {
  .form-row {
    grid-template-columns: 1fr;
  }
}
</style>
