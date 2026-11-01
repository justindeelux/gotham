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
import { computed } from "vue";

import type { StoragesPageState } from "@/features/storages/composables/useStoragesPage";
import { i18n } from "@/shared/i18n";

function t(key: string): string {
  return String(i18n.global.t(key));
}

const props = defineProps<{ state: StoragesPageState }>();
const state = props.state;

const kindOptions = computed(() => [
  { label: t("storages.kindS3"), value: "s3" },
  { label: t("storages.kindLocal"), value: "local" },
]);
</script>

<template>
  <NModal
    v-model:show="state.dialogOpen.value"
    preset="card"
    class="app-modal"
    :title="
      state.editingId.value === null
        ? t('storages.addTitle')
        : t('storages.editTitle')
    "
    style="width: 560px; max-width: 94vw"
    @after-leave="state.resetForm()"
  >
    <NForm
      label-placement="top"
      :show-feedback="false"
      class="storage-form form-container"
      @submit.prevent="() => void state.save()"
    >
      <div class="form-row">
        <NFormItem :label="t('storages.fields.name')" class="field-name">
          <NInput
            v-model:value="state.name.value"
            :placeholder="t('storages.placeholders.name')"
            :aria-label="t('storages.aria.name')"
          />
        </NFormItem>
        <NFormItem :label="t('storages.fields.kind')" class="field-kind">
          <NSelect
            v-model:value="state.kind.value"
            :options="kindOptions"
            :aria-label="t('storages.aria.kind')"
          />
        </NFormItem>
      </div>
      <template v-if="state.kind.value === 's3'">
        <div class="form-row">
          <NFormItem :label="t('storages.fields.endpoint')" class="field-endpoint">
            <NInput
              v-model:value="state.endpoint.value"
              class="mono"
              :placeholder="t('storages.placeholders.endpoint')"
              :aria-label="t('storages.aria.endpoint')"
            />
          </NFormItem>
          <NFormItem :label="t('storages.fields.region')" class="field-region">
            <NInput
              v-model:value="state.region.value"
              class="mono"
              :placeholder="t('storages.placeholders.region')"
              :aria-label="t('storages.aria.region')"
            />
          </NFormItem>
        </div>
        <div class="form-row">
          <NFormItem :label="t('storages.fields.bucket')" class="field-bucket">
            <NInput
              v-model:value="state.bucket.value"
              class="mono"
              :placeholder="t('storages.placeholders.bucket')"
              :aria-label="t('storages.aria.bucket')"
            />
          </NFormItem>
          <NFormItem :label="t('storages.fields.prefix')" class="field-prefix">
            <NInput
              v-model:value="state.prefix.value"
              class="mono"
              :placeholder="t('storages.placeholders.prefix')"
              :aria-label="t('storages.aria.prefix')"
            />
          </NFormItem>
        </div>
        <div class="form-row">
          <NFormItem :label="t('storages.fields.accessKey')" class="field-key">
            <NInput
              v-model:value="state.accessKey.value"
              type="password"
              class="mono"
              :placeholder="
                state.editingId.value === null
                  ? t('storages.placeholders.accessKey')
                  : t('storages.placeholders.accessKeyEdit')
              "
              :aria-label="t('storages.aria.accessKey')"
            />
          </NFormItem>
          <NFormItem :label="t('storages.fields.secretKey')" class="field-key">
            <NInput
              v-model:value="state.secretKey.value"
              type="password"
              class="mono"
              :placeholder="
                state.editingId.value === null
                  ? t('storages.placeholders.secretKey')
                  : t('storages.placeholders.secretKeyEdit')
              "
              :aria-label="t('storages.aria.secretKey')"
            />
          </NFormItem>
        </div>
        <NAlert type="info" :show-icon="false">
          {{ t("storages.secretsNote") }}
        </NAlert>
      </template>
      <NText v-else depth="3">
        {{ t("storages.localNote") }}
      </NText>
    </NForm>
    <template #footer>
      <NSpace justify="end" :size="8">
        <NButton @click="state.close()">
          {{ t("common.actions.cancel") }}
        </NButton>
        <NButton
          type="primary"
          attr-type="submit"
          :loading="state.store.acting"
          @click="() => void state.save()"
        >
          {{
            state.editingId.value === null
              ? t("storages.page.add")
              : t("storages.save")
          }}
        </NButton>
      </NSpace>
    </template>
  </NModal>
</template>

<style scoped>
.storage-form {
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
