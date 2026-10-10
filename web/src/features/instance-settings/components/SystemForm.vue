<script setup lang="ts">
import { NAlert, NButton, NCard, NForm, NFormItem, NInput, NSwitch } from "naive-ui";
import type { FormInst, FormRules } from "naive-ui";
import { reactive, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import { saveSystem } from "@/features/instance-settings/api/instance";
import type { InstanceState } from "@/features/instance-settings/api/instance";
import { useSectionForm } from "@/features/instance-settings/composables/useSectionForm";
import {
  hostnameSchema,
  ntpListSchema,
  splitList,
} from "@/features/instance-settings/schemas/instance";
import { ruleFrom } from "@/shared/validation/naiveAdapter";

const props = defineProps<{ state: InstanceState }>();
const emit = defineEmits<{ saved: [state: InstanceState] }>();
const { t } = useI18n();

const formRef = ref<FormInst | null>(null);
const form = reactive({ hostname: "", ntpEnabled: true, ntpServers: "" });

watch(
  () => props.state.system,
  (value) => {
    form.hostname = value.hostname;
    form.ntpEnabled = value.ntp_enabled;
    form.ntpServers = value.ntp_servers.join(", ");
  },
  { immediate: true },
);

const rules: FormRules = {
  hostname: ruleFrom(hostnameSchema),
  ntpServers: ruleFrom(ntpListSchema),
};

const { submitting, serverErrors, errorMessage, submit } = useSectionForm(
  () =>
    saveSystem({
      hostname: form.hostname,
      ntp_enabled: form.ntpEnabled,
      ntp_servers: splitList(form.ntpServers),
    }),
  (next) => emit("saved", next),
  () => t("instance-settings.system.saved"),
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
  <NCard :title="t('instance-settings.system.title')">
    <NAlert v-if="!state.capabilities.system" type="info" :show-icon="true" class="form-alert">
      {{ t("instance-settings.unsupported") }}
    </NAlert>
    <NAlert v-if="errorMessage" type="error" :show-icon="true" class="form-alert">{{ errorMessage }}</NAlert>
    <NForm ref="formRef" :model="form" :rules="rules" class="instance-form" @submit.prevent="handleSubmit">
      <NFormItem
        path="hostname"
        :label="t('instance-settings.system.hostname')"
        :validation-status="status('hostname')"
        :feedback="serverErrors.hostname"
      >
        <NInput v-model:value="form.hostname" :disabled="!state.capabilities.system" placeholder="gotham-1" />
      </NFormItem>
      <NFormItem :label="t('instance-settings.system.ntpEnabled')">
        <NSwitch v-model:value="form.ntpEnabled" :disabled="!state.capabilities.system" />
      </NFormItem>
      <NFormItem
        path="ntpServers"
        :label="t('instance-settings.system.ntpServers')"
        :validation-status="status('ntp_servers')"
        :feedback="serverErrors.ntp_servers"
      >
        <NInput
          v-model:value="form.ntpServers"
          :disabled="!state.capabilities.system"
          placeholder="pool.ntp.org, time.cloudflare.com"
        />
        <template #feedback v-if="!serverErrors.ntp_servers">
          {{ t("instance-settings.system.ntpHint") }}
        </template>
      </NFormItem>
      <NButton
        type="primary"
        :loading="submitting"
        :disabled="!state.capabilities.system"
        @click="handleSubmit"
      >
        {{ t("instance-settings.system.submit") }}
      </NButton>
    </NForm>
  </NCard>
</template>

<style scoped>
.instance-form {
  max-width: 640px;
}
</style>
