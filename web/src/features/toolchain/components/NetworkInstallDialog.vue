<script setup lang="ts">
import { NAlert, NButton, NModal, NSpace } from "naive-ui";
import { computed } from "vue";
import { useI18n } from "vue-i18n";

const props = defineProps<{
  show: boolean;
  tools: string[];
}>();

const emit = defineEmits<{
  (_event: "update:show", _value: boolean): void;
  (_event: "confirm"): void;
}>();

const { t } = useI18n();

const toolList = computed<string>(() => props.tools.join(", "));
</script>

<template>
  <NModal
    :show="props.show"
    preset="card"
    :title="t('toolchain.networkConfirm.title')"
    class="app-modal"
    style="width: 460px; max-width: 94vw"
    :mask-closable="false"
    @update:show="(value: boolean) => emit('update:show', value)"
  >
    <NSpace vertical :size="16">
      <p class="lead">
        {{ t("toolchain.networkConfirm.lead", { tools: toolList }) }}
      </p>
      <NAlert type="warning" :show-icon="true">
        {{ t("toolchain.networkConfirm.warning") }}
      </NAlert>
    </NSpace>
    <template #footer>
      <NSpace justify="end" :size="12">
        <NButton @click="emit('update:show', false)">
          {{ t("toolchain.actions.cancel") }}
        </NButton>
        <NButton type="primary" @click="emit('confirm')">
          {{ t("toolchain.networkConfirm.confirm") }}
        </NButton>
      </NSpace>
    </template>
  </NModal>
</template>

<style scoped>
.lead {
  margin: 0;
}
</style>
