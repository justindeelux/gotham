<script setup lang="ts">
import { NAlert, NButton, NCard, NForm, NFormItem, NInput, NSelect, NSwitch } from "naive-ui";
import type { FormInst, FormRules } from "naive-ui";
import { computed, reactive, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import { saveNodeSystem } from "@/features/servers/api/host";
import type { NodeHostState } from "@/features/servers/api/host";
import { useHostUnlock } from "@/features/servers/composables/useHostUnlock";
import { timezoneOptions } from "@/features/servers/composables/useTimezoneOptions";
import {
  hostnameSchema,
  ntpListSchema,
  splitList,
  timezoneSchema,
} from "@/features/servers/schemas/host";
import { useSectionForm } from "@/shared/composables/useSectionForm";
import { ruleFrom } from "@/shared/validation/naiveAdapter";

const props = defineProps<{ serverId: string; state: NodeHostState }>();
const emit = defineEmits<{ saved: [serverId: string, state: NodeHostState] }>();
const { t } = useI18n();

const formRef = ref<FormInst | null>(null);
const form = reactive({ hostname: "", ntpEnabled: true, ntpServers: "", timezone: "" });
const zoneOptions = computed(() => timezoneOptions(form.timezone));

watch(
  () => props.state.system,
  (value) => {
    form.hostname = value.hostname;
    form.ntpEnabled = value.ntp_enabled;
    form.ntpServers = value.ntp_servers.join(", ");
    form.timezone = value.timezone;
  },
  { immediate: true },
);

const rules: FormRules = {
  hostname: ruleFrom(hostnameSchema),
  ntpServers: ruleFrom(ntpListSchema),
  timezone: ruleFrom(timezoneSchema, { required: true }),
};

const { submitting, serverErrors, errorMessage, submit } = useSectionForm(
  () =>
    saveNodeSystem(props.serverId, {
      hostname: form.hostname,
      ntp_enabled: form.ntpEnabled,
      ntp_servers: splitList(form.ntpServers),
      timezone: form.timezone,
    }),
  (next) => emit("saved", props.serverId, next),
  () => t("servers.settings.host.system.saved"),
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

const disabled = (): boolean => props.state.capability !== "ready";

const { focusTarget, unlocked } = useHostUnlock(() => props.state.capability === "ready");
</script>

<template>
  <NCard :title="t('servers.settings.host.system.title')">
    <p class="sr-only" role="status" aria-live="polite">
      {{ unlocked ? t("servers.settings.host.helper.installed") : "" }}
    </p>
    <NAlert v-if="errorMessage" type="error" :show-icon="true" class="form-alert">{{ errorMessage }}</NAlert>
    <NForm ref="formRef" :model="form" :rules="rules" class="host-form" @submit.prevent="handleSubmit">
      <NFormItem
        path="hostname"
        :label="t('servers.settings.host.system.hostname')"
        :validation-status="status('hostname')"
        :feedback="serverErrors.hostname"
      >
        <NInput
          ref="focusTarget"
          v-model:value="form.hostname"
          :disabled="disabled()"
          placeholder="gotham-1"
        />
      </NFormItem>
      <NFormItem :label="t('servers.settings.host.system.ntpEnabled')">
        <NSwitch v-model:value="form.ntpEnabled" :disabled="disabled()" />
      </NFormItem>
      <NFormItem
        path="ntpServers"
        :label="t('servers.settings.host.system.ntpServers')"
        :validation-status="status('ntp_servers')"
        :feedback="serverErrors.ntp_servers"
      >
        <NInput
          v-model:value="form.ntpServers"
          :disabled="disabled()"
          placeholder="pool.ntp.org, time.cloudflare.com"
        />
        <template #feedback v-if="!serverErrors.ntp_servers">
          {{ t("servers.settings.host.system.ntpHint") }}
        </template>
      </NFormItem>
      <NFormItem
        path="timezone"
        :label="t('servers.settings.host.system.timezone')"
        :validation-status="status('timezone')"
        :feedback="serverErrors.timezone"
      >
        <NSelect
          v-model:value="form.timezone"
          filterable
          :options="zoneOptions"
          :disabled="disabled()"
        />
        <template #feedback v-if="!serverErrors.timezone">
          {{ t("servers.settings.host.system.timezoneHint") }}
        </template>
      </NFormItem>
      <NButton
        type="primary"
        :loading="submitting"
        :disabled="disabled()"
        @click="handleSubmit"
      >
        {{ t("servers.settings.host.system.submit") }}
      </NButton>
    </NForm>
  </NCard>
</template>

<style scoped>
.host-form {
  max-width: 640px;
}

.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  margin: -1px;
  padding: 0;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
  border: 0;
}
</style>
