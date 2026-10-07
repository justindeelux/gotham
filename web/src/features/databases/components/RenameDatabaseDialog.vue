<script setup lang="ts">
import { NButton, NInput, NModal, NSpace, NText } from "naive-ui";
import { inject } from "vue";

import { databaseDetailKey } from "@/features/databases/composables/useDatabaseDetail";
import { isDatabaseNameValid } from "@/features/databases/schemas/databases";

import { i18n } from "@/shared/i18n";

/** t resolves a databases/common message in the current locale. */
function t(key: string, params?: Record<string, string | number>): string {
  return String(i18n.global.t(key, params ?? {}));
}

const detail = inject(databaseDetailKey)!;
</script>

<template>
  <NModal
    v-model:show="detail.renameOpen.value"
    preset="card"
    :title="t('databases.detail.rename.title')"
    style="width: 480px; max-width: 94vw"
  >
    <NSpace vertical :size="12">
      <NText depth="3">
        {{ t("databases.detail.rename.hint") }}
      </NText>
      <NInput
        v-model:value="detail.renameValue.value"
        class="mono"
        :placeholder="t('databases.detail.rename.placeholder')"
        @keyup.enter="() => void detail.handleRename()"
      />
      <NSpace justify="end" :size="8">
        <NButton @click="detail.renameOpen.value = false">{{ t("common.actions.cancel") }}</NButton>
        <NButton
          type="primary"
          :loading="detail.renaming.value"
          :disabled="!isDatabaseNameValid(detail.renameValue.value)"
          @click="() => void detail.handleRename()"
        >
          {{ t("databases.detail.actions.rename") }}
        </NButton>
      </NSpace>
    </NSpace>
  </NModal>
</template>

<style scoped>
.mono {
  font-family: var(--font-mono);
}
</style>
