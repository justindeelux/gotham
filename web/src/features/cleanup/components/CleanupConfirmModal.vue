<script setup lang="ts">
import { NAlert, NButton, NCheckbox, NModal, NTag } from "naive-ui";
import { ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import { formatBytes } from "@/shared/utils/format";

import type { NodeTotal } from "@/features/cleanup/utils/selection";

interface Props {
  show: boolean;
  nodes: NodeTotal[];
  totalBytes: number;
  hasDangerous: boolean;
  serverName: (_serverId: string) => string;
}

const props = defineProps<Props>();
const emit = defineEmits<{
  "update:show": [value: boolean];
  confirm: [];
}>();

const { t } = useI18n();
const understood = ref(false);

watch(
  () => props.show,
  (show) => {
    if (show) {
      understood.value = false;
    }
  },
);
</script>

<template>
  <NModal
    :show="show"
    preset="card"
    :title="t('cleanup.confirm.title')"
    :mask-closable="false"
    class="app-modal cleanup-confirm-modal"
    style="width: 560px; max-width: 96vw"
    @update:show="(value: boolean) => emit('update:show', value)"
  >
    <p class="cleanup-confirm-body">{{ t("cleanup.confirm.body") }}</p>

    <div class="cleanup-confirm-list">
      <div
        v-for="node in nodes"
        :key="node.serverId"
        class="cleanup-confirm-row"
      >
        <span class="cleanup-confirm-row__name">{{ serverName(node.serverId) }}</span>
        <span class="cleanup-confirm-row__meta">
          {{ t("cleanup.results.categoryTotal", {
            count: node.count,
            size: formatBytes(node.bytes),
          }) }}
        </span>
      </div>
    </div>

    <p class="cleanup-confirm-total">
      {{ t("cleanup.confirm.total") }}
      <NTag type="info" size="small">{{ formatBytes(totalBytes) }}</NTag>
    </p>

    <template v-if="hasDangerous">
      <NAlert type="error" :show-icon="true" :title="t('cleanup.confirm.dangerTitle')">
        {{ t("cleanup.confirm.dangerBody") }}
      </NAlert>
      <NCheckbox v-model:checked="understood" class="cleanup-confirm-understand">
        {{ t("cleanup.confirm.understand") }}
      </NCheckbox>
    </template>

    <template #footer>
      <div class="cleanup-confirm-footer">
        <NButton @click="emit('update:show', false)">
          {{ t("cleanup.confirm.cancel") }}
        </NButton>
        <NButton
          type="error"
          :disabled="hasDangerous && !understood"
          @click="emit('confirm')"
        >
          {{ t("cleanup.confirm.delete") }}
        </NButton>
      </div>
    </template>
  </NModal>
</template>

<style scoped>
.cleanup-confirm-body {
  margin: 0 0 var(--space-3);
  color: var(--muted);
}

.cleanup-confirm-list {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  margin-bottom: var(--space-3);
}

.cleanup-confirm-row {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--space-2);
  min-width: 0;
}

.cleanup-confirm-row__name {
  font-weight: 600;
  color: var(--fg-2);
}

.cleanup-confirm-row__meta {
  min-width: 0;
  overflow-wrap: anywhere;
  font-size: var(--text-xs);
  color: var(--muted);
}

.cleanup-confirm-total {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--space-2);
  margin: 0 0 var(--space-3);
  font-weight: 600;
  color: var(--fg-2);
}

.cleanup-confirm-understand {
  margin-top: var(--space-3);
}

.cleanup-confirm-footer {
  display: flex;
  justify-content: flex-end;
  flex-wrap: wrap;
  gap: var(--space-2);
}
</style>
