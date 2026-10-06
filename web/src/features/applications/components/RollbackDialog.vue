<script setup lang="ts">
import { NButton, NModal, NPopconfirm, NRadio, NRadioGroup, NSpace, NText } from "naive-ui";
import { computed } from "vue";
import { useI18n } from "vue-i18n";

import type { Deployment } from "@/features/applications/api/applications";
import { relativeTime } from "@/shared/utils/format";

interface Props {
  show: boolean;
  rollingBack: boolean;
  rollbackTarget: string;
  runningDeployments: Deployment[];
}

const props = defineProps<Props>();

const emit = defineEmits<{
  "update:show": [value: boolean];
  "update:target": [value: string];
  confirm: [];
}>();

const { t } = useI18n();

/** confirmText names the actual rollback target; plain text, never HTML. */
const confirmText = computed<string>(() =>
  String(t("applications.rollback.confirm", { id: props.rollbackTarget.slice(0, 8) })),
);
</script>

<template>
  <NModal
    :show="props.show"
    preset="card"
    :title="t('applications.rollback.title')"
    style="width: 560px; max-width: 94vw"
    @update:show="emit('update:show', $event)"
  >
    <NSpace vertical :size="12">
      <NText depth="3">
        {{ t("applications.rollback.hint") }}
      </NText>
      <NRadioGroup :value="props.rollbackTarget" @update:value="emit('update:target', $event)">
        <NSpace vertical :size="8">
          <NRadio
            v-for="item in props.runningDeployments"
            :key="item.id"
            :value="item.id"
          >
            <span class="mono">{{ item.id.slice(0, 8) }}</span>
            ·
            <span class="mono">{{ item.image_tag || t("applications.rollback.untagged") }}</span>
            · {{ relativeTime(item.created_at) }}
          </NRadio>
        </NSpace>
      </NRadioGroup>
      <NPopconfirm
        :positive-button-props="{ type: 'primary' }"
        @positive-click="emit('confirm')"
      >
        <template #trigger>
          <NButton
            type="primary"
            :loading="props.rollingBack"
            :disabled="props.rollbackTarget === ''"
          >
            {{ t("applications.header.rollback") }}
          </NButton>
        </template>
        {{ confirmText }}
      </NPopconfirm>
    </NSpace>
  </NModal>
</template>

<style scoped>
.mono {
  font-family: var(--font-mono);
}
</style>
