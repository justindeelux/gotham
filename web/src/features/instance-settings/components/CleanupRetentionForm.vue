<script setup lang="ts">
import { NAlert, NButton, NCard, NForm, NFormItem, NInputNumber } from "naive-ui";
import type { FormInst, FormRules } from "naive-ui";
import { computed, reactive, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import { saveCleanupRetention } from "@/features/instance-settings/api/instance";
import type { InstanceState } from "@/features/instance-settings/api/instance";
import {
  cleanupBackupFileMinAgeDaysSchema,
  cleanupHistoryDaysSchema,
} from "@/features/instance-settings/schemas/instance";
import { useSectionForm } from "@/shared/composables/useSectionForm";
import { ruleFrom } from "@/shared/validation/naiveAdapter";

const props = defineProps<{ state: InstanceState }>();
const emit = defineEmits<{ saved: [state: InstanceState] }>();
const { t } = useI18n();

const formRef = ref<FormInst | null>(null);
const form = reactive<{
  historyDays: number | null;
  backupFileMinAgeDays: number | null;
}>({ historyDays: null, backupFileMinAgeDays: null });

const cleanup = computed(() => props.state.cleanup);

watch(
  cleanup,
  (value) => {
    form.historyDays = value.history_days;
    form.backupFileMinAgeDays = value.backup_file_min_age_days;
  },
  { immediate: true },
);

const rules: FormRules = {
  historyDays: ruleFrom(cleanupHistoryDaysSchema),
  backupFileMinAgeDays: ruleFrom(cleanupBackupFileMinAgeDaysSchema),
};

const { submitting, serverErrors, errorMessage, submit } = useSectionForm(
  () =>
    saveCleanupRetention({
      history_days: form.historyDays ?? 0,
      backup_file_min_age_days: form.backupFileMinAgeDays ?? 0,
    }),
  (next) => emit("saved", next),
  () => t("instance-settings.cleanup.saved"),
);

async function handleSubmit(): Promise<void> {
  try {
    await formRef.value?.validate();
  } catch {
    return;
  }
  await submit();
}

function status(path: string): "error" | undefined {
  return serverErrors.value[path] ? "error" : undefined;
}
</script>

<template>
  <NCard :title="t('instance-settings.cleanup.title')">
    <NAlert v-if="errorMessage" type="error" :show-icon="true" class="instance-cleanup-alert">
      {{ errorMessage }}
    </NAlert>
    <NForm
      ref="formRef"
      :model="form"
      :rules="rules"
      class="instance-cleanup-form"
      @submit.prevent="handleSubmit"
    >
      <NFormItem
        path="historyDays"
        :label="t('instance-settings.cleanup.history')"
        :validation-status="status('history_days')"
        :feedback="serverErrors.history_days"
      >
        <NInputNumber
          v-model:value="form.historyDays"
          :min="7"
          :max="3650"
          :precision="0"
          class="instance-cleanup-input"
        />
        <template #feedback v-if="!serverErrors.history_days">
          {{ t("instance-settings.cleanup.historyHint") }}
        </template>
      </NFormItem>
      <NFormItem
        path="backupFileMinAgeDays"
        :label="t('instance-settings.cleanup.backupAge')"
        :validation-status="status('backup_file_min_age_days')"
        :feedback="serverErrors.backup_file_min_age_days"
      >
        <NInputNumber
          v-model:value="form.backupFileMinAgeDays"
          :min="1"
          :max="365"
          :precision="0"
          class="instance-cleanup-input"
        />
        <template #feedback v-if="!serverErrors.backup_file_min_age_days">
          {{ t("instance-settings.cleanup.backupAgeHint") }}
        </template>
      </NFormItem>
      <NButton type="primary" :loading="submitting" @click="handleSubmit">
        {{ t("instance-settings.cleanup.submit") }}
      </NButton>
    </NForm>
  </NCard>
</template>

<style scoped>
.instance-cleanup-form {
  max-width: 640px;
}

.instance-cleanup-input {
  width: 180px;
}

.instance-cleanup-alert {
  margin-bottom: 16px;
}
</style>
