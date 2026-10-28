<script setup lang="ts">
import {
  NAlert,
  NButton,
  NForm,
  NFormItem,
  NInput,
  NInputNumber,
  NModal,
  NSpace,
  NSwitch,
  NText,
} from "naive-ui";
import { reactive, watch } from "vue";
import { useI18n } from "vue-i18n";

import type {
  WebsiteCheck,
  WebsiteCheckInput,
} from "@/features/applications/api/applications";

interface Props {
  show: boolean;
  domain: string;
  check: WebsiteCheck;
  saving: boolean;
  error: string | null;
}

const props = defineProps<Props>();

const emit = defineEmits<{
  "update:show": [value: boolean];
  save: [input: WebsiteCheckInput];
}>();

const { t } = useI18n();

const draft = reactive<WebsiteCheckInput>({
  enabled: true,
  path: "/",
  expected_min: 200,
  expected_max: 399,
  timeout_seconds: 5,
});

watch(
  () => [props.show, props.check] as const,
  () => {
    draft.enabled = props.check.enabled;
    draft.path = props.check.path;
    draft.expected_min = props.check.expected_min;
    draft.expected_max = props.check.expected_max;
    draft.timeout_seconds = props.check.timeout_seconds;
  },
  { immediate: true },
);

function save(): void {
  emit("save", {
    enabled: draft.enabled,
    path: draft.path,
    expected_min: draft.expected_min,
    expected_max: draft.expected_max,
    timeout_seconds: draft.timeout_seconds,
  });
}
</script>

<template>
  <NModal
    :show="props.show"
    preset="card"
    :title="t('applications.websiteCheck.configureTitle', { domain: props.domain })"
    class="app-modal website-check-modal"
    style="width: 520px; max-width: 94vw"
    :mask-closable="false"
    @update:show="(value: boolean) => emit('update:show', value)"
  >
    <NSpace vertical :size="12">
      <NAlert v-if="props.error" type="error" :show-icon="true">
        {{ props.error }}
      </NAlert>
      <NForm label-placement="top" :show-feedback="false">
        <NFormItem :label="t('applications.websiteCheck.enabled')">
          <div class="switch-row">
            <NSwitch
              v-model:value="draft.enabled"
              :aria-label="t('applications.websiteCheck.enabled')"
            />
            <NText depth="3" class="hint">
              {{ t("applications.websiteCheck.enabledHint") }}
            </NText>
          </div>
        </NFormItem>
        <NFormItem :label="t('applications.websiteCheck.path')">
          <NInput
            v-model:value="draft.path"
            class="mono"
            placeholder="/"
            :input-props="{
              'aria-label': t('applications.websiteCheck.path'),
            }"
          />
        </NFormItem>
        <div class="range-row">
          <NFormItem :label="t('applications.websiteCheck.expectedMin')">
            <NInputNumber
              v-model:value="draft.expected_min"
              :min="100"
              :max="599"
              :input-props="{
                'aria-label': t('applications.websiteCheck.expectedMin'),
              }"
            />
          </NFormItem>
          <NFormItem :label="t('applications.websiteCheck.expectedMax')">
            <NInputNumber
              v-model:value="draft.expected_max"
              :min="100"
              :max="599"
              :input-props="{
                'aria-label': t('applications.websiteCheck.expectedMax'),
              }"
            />
          </NFormItem>
          <NFormItem :label="t('applications.websiteCheck.timeout')">
            <NInputNumber
              v-model:value="draft.timeout_seconds"
              :min="1"
              :max="30"
              :input-props="{
                'aria-label': t('applications.websiteCheck.timeout'),
              }"
            />
          </NFormItem>
        </div>
        <NText depth="3" class="hint">
          {{ t("applications.websiteCheck.authenticatedHint") }}
        </NText>
        <NText depth="3" class="hint">
          {{ t("applications.websiteCheck.graceHint") }}
        </NText>
      </NForm>
    </NSpace>
    <template #footer>
      <div class="modal-actions">
        <NButton @click="() => emit('update:show', false)">
          {{ t("applications.websiteCheck.cancel") }}
        </NButton>
        <NButton type="primary" :loading="props.saving" @click="save">
          {{ t("applications.websiteCheck.save") }}
        </NButton>
      </div>
    </template>
  </NModal>
</template>

<style scoped>
.switch-row,
.range-row {
  display: flex;
  gap: var(--space-3);
  align-items: center;
  flex-wrap: wrap;
}

.range-row {
  align-items: flex-start;
}

.range-row :deep(.n-form-item) {
  min-width: 120px;
  flex: 1 1 120px;
}

.hint {
  font-size: var(--text-xs);
  max-width: 56ch;
}

.modal-actions {
  display: flex;
  justify-content: flex-end;
  gap: var(--space-2);
}
</style>
