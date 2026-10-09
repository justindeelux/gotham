<script setup lang="ts">
import { NButton, NInput, NModal, NSpace, NText } from "naive-ui";
import { computed, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

const props = defineProps<{
  show: boolean;
  appName: string;
  deleting: boolean;
}>();

const emit = defineEmits<{
  "update:show": [value: boolean];
  confirm: [];
}>();

const { t } = useI18n();

/** confirmName holds the typed application name; reset on every open. */
const confirmName = ref("");

watch(
  () => props.show,
  (show) => {
    if (show) {
      confirmName.value = "";
    }
  },
);

/** matches enables the confirm button only on an exact name match. */
const matches = computed<boolean>(
  () => props.appName !== "" && confirmName.value === props.appName,
);
</script>

<template>
  <NModal
    :show="props.show"
    preset="card"
    :title="t('applications.delete.modalTitle')"
    class="app-modal"
    style="width: 480px; max-width: 94vw"
    @update:show="emit('update:show', $event)"
  >
    <NSpace vertical :size="12">
      <NText depth="3">
        {{ t("applications.delete.modalHint", { name: props.appName }) }}
      </NText>
      <NInput
        v-model:value="confirmName"
        class="mono"
        :placeholder="t('applications.delete.placeholder')"
      />
      <NSpace justify="end" :size="8">
        <NButton @click="emit('update:show', false)">{{ t("common.actions.cancel") }}</NButton>
        <NButton
          type="error"
          :loading="props.deleting"
          :disabled="!matches"
          @click="emit('confirm')"
        >
          {{ t("applications.delete.confirm") }}
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
