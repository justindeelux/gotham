<script setup lang="ts">
import { NButton, NModal } from "naive-ui";
import { useI18n } from "vue-i18n";

import { useFilesTransferStore } from "@/features/files/stores/transfer";

const { t } = useI18n();
const transfer = useFilesTransferStore();
</script>

<template>
  <NModal
    class="app-modal"
    :show="transfer.leaveOpen"
    preset="card"
    style="width: 460px; max-width: 94vw"
    :title="t('files.transfer.leaveTitle')"
    :mask-closable="false"
    @update:show="(value: boolean) => { if (!value) transfer.resolveLeave(false); }"
  >
    <p class="leave-message">{{ t("files.transfer.leaveMessage") }}</p>
    <template #footer>
      <div class="leave-foot">
        <NButton @click="transfer.resolveLeave(false)">
          {{ t("files.transfer.leaveStay") }}
        </NButton>
        <NButton type="warning" @click="transfer.resolveLeave(true)">
          {{ t("files.transfer.leaveCancel") }}
        </NButton>
      </div>
    </template>
  </NModal>
</template>

<style scoped>
.leave-message {
  margin: 0;
  font-size: var(--text-sm);
}

.leave-foot {
  display: flex;
  justify-content: flex-end;
  gap: var(--space-2);
}
</style>
