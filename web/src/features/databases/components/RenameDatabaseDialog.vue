<script setup lang="ts">
import { NButton, NInput, NModal, NSpace, NText } from "naive-ui";
import { inject } from "vue";

import { databaseDetailKey } from "@/features/databases/composables/useDatabaseDetail";
import { isValidDatabaseName } from "@/features/databases/utils/databaseNames";

const detail = inject(databaseDetailKey)!;
</script>

<template>
  <NModal
    v-model:show="detail.renameOpen.value"
    preset="card"
    title="Rename database"
    style="width: 480px; max-width: 94vw"
  >
    <NSpace vertical :size="12">
      <NText depth="3">
        Only the display name changes — the container, volume and credentials
        stay untouched.
      </NText>
      <NInput
        v-model:value="detail.renameValue.value"
        class="mono"
        placeholder="New database name"
        @keyup.enter="() => void detail.handleRename()"
      />
      <NSpace justify="end" :size="8">
        <NButton @click="detail.renameOpen.value = false">Cancel</NButton>
        <NButton
          type="primary"
          :loading="detail.renaming.value"
          :disabled="!isValidDatabaseName(detail.renameValue.value)"
          @click="() => void detail.handleRename()"
        >
          Rename
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
