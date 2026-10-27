<script setup lang="ts">
import { NButton, NModal } from "naive-ui";
import { nextTick, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import { useFilesTransferStore } from "@/features/files/stores/transfer";

const { t } = useI18n();
const transfer = useFilesTransferStore();
const stayButton = ref<InstanceType<typeof NButton> | null>(null);
let restoreFocus: HTMLElement | null = null;

watch(
  () => transfer.leaveOpen,
  async (open) => {
    if (open) {
      restoreFocus =
        document.activeElement instanceof HTMLElement
          ? document.activeElement
          : null;
      await nextTick();
      stayButton.value?.$el?.focus?.();
      return;
    }
    await nextTick();
    if (restoreFocus !== null && restoreFocus.isConnected) {
      restoreFocus.focus();
    }
    restoreFocus = null;
  },
  { flush: "post" },
);
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
    <div>
      <p class="leave-message">{{ t("files.transfer.leaveMessage") }}</p>
    </div>
    <template #footer>
      <div class="leave-foot">
        <NButton ref="stayButton" @click="transfer.resolveLeave(false)">
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
