<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NForm,
  NFormItem,
  NInputNumber,
  NModal,
  NSwitch,
} from "naive-ui";
import type { FormInst, FormRules } from "naive-ui";
import { computed, reactive, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import { saveNodeSSH } from "@/features/servers/api/host";
import type { NodeHostState } from "@/features/servers/api/host";
import { sshPortSchema } from "@/features/servers/schemas/host";
import { useSectionForm } from "@/shared/composables/useSectionForm";
import { ruleFrom } from "@/shared/validation/naiveAdapter";

const props = defineProps<{ serverId: string; state: NodeHostState }>();
const emit = defineEmits<{ saved: [serverId: string, state: NodeHostState] }>();
const { t } = useI18n();

const formRef = ref<FormInst | null>(null);
const showConfirm = ref(false);
const form = reactive<{ port: number | null; passwordAuth: boolean }>({
  port: 22,
  passwordAuth: true,
});

watch(
  () => props.state.ssh,
  (ssh) => {
    form.port = ssh.port > 0 ? ssh.port : 22;
    form.passwordAuth = ssh.password_authentication;
  },
  { immediate: true },
);

const rules: FormRules = {
  port: ruleFrom(sshPortSchema, { required: true }),
};

const disabled = computed(
  () => props.state.capability !== "ready" || !props.state.ssh.available,
);
const portChanged = computed(() => form.port !== props.state.ssh.port);
const passwordChanged = computed(
  () => form.passwordAuth !== props.state.ssh.password_authentication,
);
const passwordBlocked = computed(
  () =>
    props.state.ssh.password_authentication &&
    !form.passwordAuth &&
    !props.state.ssh.key_auth_available,
);
const dirty = computed(() => portChanged.value || passwordChanged.value);

const { submitting, serverErrors, errorMessage, submit } = useSectionForm(
  () =>
    saveNodeSSH(props.serverId, {
      port: form.port ?? props.state.ssh.port,
      password_authentication: form.passwordAuth,
    }),
  (next) => emit("saved", props.serverId, next),
  () => t("servers.settings.host.ssh.saved"),
);

async function requestApply(): Promise<void> {
  try {
    await formRef.value?.validate();
  } catch {
    return;
  }
  if (!dirty.value) {
    return;
  }
  if (passwordBlocked.value) {
    errorMessage.value = t("servers.settings.host.ssh.noKeys");
    return;
  }
  showConfirm.value = true;
}

async function confirmApply(): Promise<void> {
  await submit();
  showConfirm.value = false;
}

function status(path: string): "error" | undefined {
  return serverErrors.value[path] ? "error" : undefined;
}
</script>

<template>
  <NCard :title="t('servers.settings.host.ssh.title')">
    <NAlert v-if="!state.ssh.available" type="info" :show-icon="true" class="form-alert">
      {{ t("servers.settings.host.ssh.unavailable") }}
    </NAlert>
    <NAlert
      v-else-if="!state.ssh.key_auth_available"
      type="warning"
      :show-icon="true"
      class="form-alert"
    >
      {{ t("servers.settings.host.ssh.noKeys") }}
    </NAlert>
    <NAlert v-if="errorMessage" type="error" :show-icon="true" class="form-alert form-alert-multiline">
      {{ errorMessage }}
    </NAlert>

    <NForm ref="formRef" :model="form" :rules="rules" class="host-form" @submit.prevent="requestApply">
      <NFormItem
        path="port"
        :label="t('servers.settings.host.ssh.port')"
        :validation-status="status('port')"
        :feedback="serverErrors.port"
      >
        <NInputNumber
          v-model:value="form.port"
          :min="1"
          :max="65535"
          :disabled="disabled"
          placeholder="22"
        />
        <template #feedback v-if="!serverErrors.port">
          {{ t("servers.settings.host.ssh.portHint") }}
        </template>
      </NFormItem>
      <NFormItem :label="t('servers.settings.host.ssh.passwordLogin')">
        <NSwitch v-model:value="form.passwordAuth" :disabled="disabled" />
        <template #feedback>
          {{ t("servers.settings.host.ssh.passwordHint") }}
        </template>
      </NFormItem>
      <NButton
        type="primary"
        :loading="submitting"
        :disabled="disabled || !dirty"
        @click="requestApply"
      >
        {{ t("servers.settings.host.ssh.submit") }}
      </NButton>
    </NForm>

    <NModal
      v-model:show="showConfirm"
      preset="card"
      :title="t('servers.settings.host.ssh.confirmTitle')"
      class="app-modal"
      style="width: 520px; max-width: 96vw"
    >
      <p>{{ t("servers.settings.host.ssh.confirmIntro") }}</p>
      <ul>
        <li v-if="portChanged">
          {{
            t("servers.settings.host.ssh.confirmPort", {
              from: state.ssh.port,
              to: form.port,
            })
          }}
        </li>
        <li v-if="passwordChanged">
          {{
            form.passwordAuth
              ? t("servers.settings.host.ssh.confirmPasswordOn")
              : t("servers.settings.host.ssh.confirmPasswordOff")
          }}
        </li>
      </ul>
      <NAlert v-if="portChanged" type="warning" :show-icon="true">
        {{ t("servers.settings.host.ssh.portWarning") }}
      </NAlert>
      <template #footer>
        <NButton @click="showConfirm = false">
          {{ t("servers.settings.host.ssh.cancel") }}
        </NButton>
        <NButton type="primary" :loading="submitting" @click="confirmApply">
          {{ t("servers.settings.host.ssh.confirmApply") }}
        </NButton>
      </template>
    </NModal>
  </NCard>
</template>

<style scoped>
.host-form {
  max-width: 640px;
}
</style>
