<script setup lang="ts">
import { NAlert, NButton, NCard, NForm, NFormItem, NInput } from "naive-ui";
import type { FormInst, FormRules } from "naive-ui";
import { computed, reactive, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import { saveGeneral } from "@/features/instance-settings/api/instance";
import type { InstanceState } from "@/features/instance-settings/api/instance";
import ControlPlaneDomainStatus from "@/features/instance-settings/components/ControlPlaneDomainStatus.vue";
import {
  controlPlaneUrlSchema,
  instanceNameSchema,
} from "@/features/instance-settings/schemas/instance";
import { useSectionForm } from "@/shared/composables/useSectionForm";
import { ruleFrom } from "@/shared/validation/naiveAdapter";

const props = defineProps<{ state: InstanceState }>();
const emit = defineEmits<{ saved: [state: InstanceState] }>();
const { t } = useI18n();

const formRef = ref<FormInst | null>(null);
const form = reactive({ url: "", name: "" });
const general = computed(() => props.state.general);

watch(
  general,
  (value) => {
    form.url = value.control_plane_url.value;
    form.name = value.instance_name.value;
  },
  { immediate: true },
);

const rules: FormRules = {
  url: ruleFrom(controlPlaneUrlSchema),
  name: ruleFrom(instanceNameSchema, { required: true }),
};

const { submitting, serverErrors, errorMessage, submit } = useSectionForm(
  () =>
    saveGeneral({
      control_plane_url: form.url,
      instance_name: form.name,
    }),
  (next) => emit("saved", next),
  () => t("instance-settings.general.saved"),
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
  <NCard :title="t('instance-settings.general.title')">
    <NAlert v-if="errorMessage" type="error" :show-icon="true" class="form-alert">{{ errorMessage }}</NAlert>
    <NForm ref="formRef" :model="form" :rules="rules" class="instance-form" @submit.prevent="handleSubmit">
      <NFormItem
        path="url"
        :label="t('instance-settings.general.url')"
        :validation-status="status('control_plane_url')"
        :feedback="serverErrors.control_plane_url"
      >
        <NInput
          v-model:value="form.url"
          :disabled="general.control_plane_url.locked"
          placeholder="https://gotham.example.com"
        />
        <template #feedback v-if="!serverErrors.control_plane_url">
          {{ t("instance-settings.general.urlHint") }}
        </template>
      </NFormItem>
      <ControlPlaneDomainStatus
        :url="form.url"
        :saved-url="general.control_plane_url.value"
      />
      <p v-if="general.control_plane_url.locked" class="lock-note">
        {{ t("instance-settings.locked", { name: general.control_plane_url.env_var }) }}
      </p>

      <NFormItem
        path="name"
        :label="t('instance-settings.general.name')"
        :validation-status="status('instance_name')"
        :feedback="serverErrors.instance_name"
      >
        <NInput v-model:value="form.name" :disabled="general.instance_name.locked" maxlength="64" />
      </NFormItem>
      <p v-if="general.instance_name.locked" class="lock-note">
        {{ t("instance-settings.locked", { name: general.instance_name.env_var }) }}
      </p>

      <NButton type="primary" :loading="submitting" @click="handleSubmit">
        {{ t("instance-settings.general.submit") }}
      </NButton>
    </NForm>
  </NCard>
</template>

<style scoped>
.instance-form {
  max-width: 640px;
}

.lock-note {
  margin: -12px 0 12px;
  font-size: var(--text-xs, 12px);
  color: var(--muted, #8b8f99);
}
</style>
