<script setup lang="ts">
import {
  NAlert,
  NButton,
  NForm,
  NFormItem,
  NInput,
  NInputNumber,
  NModal,
  NRadioButton,
  NRadioGroup,
  NSpace,
  type FormInst,
  type FormRules,
} from "naive-ui";
import { computed, onBeforeUnmount, reactive, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import type { Server, UpdateServerInput } from "@/features/servers/api/servers";
import { failureText } from "@/features/servers/api/servers";
import { editRules } from "@/features/servers/schemas/servers";
import { useServersStore } from "@/features/servers/stores/servers";
import { onLocaleChange } from "@/shared/i18n";

interface Props {
  show: boolean;
  server: Server | null;
}

interface EditForm {
  name: string;
  ip: string;
  port: number | null;
  sshUser: string;
  authMode: "keep" | "key" | "password";
  keyId: string;
  password: string;
}

const props = defineProps<Props>();
const emit = defineEmits<{
  "update:show": [value: boolean];
  updated: [server: Server];
}>();

const serversStore = useServersStore();
const { locale, t } = useI18n();
const formRef = ref<FormInst | null>(null);
/**
 * saveFailure keeps the raw last save failure. The banner display derives
 * from it in the current locale, so a language switch re-renders a retained
 * failure without resubmitting or touching the draft.
 */
const saveFailure = ref<unknown>(null);
/** errorMessage renders the retained save failure, empty while healthy. */
const errorMessage = computed<string>(() =>
  saveFailure.value === null || saveFailure.value === undefined
    ? ""
    : failureText(saveFailure.value, locale.value),
);
const saving = ref(false);
/**
 * validationAttempted records that the edit form has been validated at
 * least once, so a language switch can refresh already-visible feedback
 * without ever surfacing errors on a pristine form.
 */
const validationAttempted = ref(false);

const form = reactive<EditForm>({
  name: "",
  ip: "",
  port: 22,
  sshUser: "root",
  authMode: "keep",
  keyId: "",
  password: "",
});

const rules = computed<FormRules>(() => editRules(form.authMode));

/** currentAuth describes the stored credential without revealing it. */
const currentAuth = computed<string>(() => {
  if (!props.server) {
    return "";
  }
  if (props.server.ssh_key_id) {
    return t("servers.edit.currentKey", { id: props.server.ssh_key_id.slice(0, 8) });
  }
  if (props.server.has_password) {
    return t("servers.edit.currentPassword");
  }
  return t("servers.edit.currentNone");
});

/** resetsPin warns when the edit invalidates the pinned host key. */
const resetsPin = computed<boolean>(() => {
  const server = props.server;
  if (!server) {
    return false;
  }
  return (
    form.ip.trim() !== server.ip ||
    (form.port ?? 22) !== server.port ||
    form.sshUser.trim() !== server.ssh_user ||
    (form.authMode === "key" && form.keyId.trim() !== (server.ssh_key_id ?? "")) ||
    (form.authMode === "password" && form.password !== "")
  );
});

/** prefill copies the server into the form; the password always stays blank. */
function prefill(): void {
  const server = props.server;
  if (!server) {
    return;
  }
  form.name = server.name;
  form.ip = server.ip;
  form.port = server.port;
  form.sshUser = server.ssh_user;
  form.authMode = "keep";
  form.keyId = server.ssh_key_id ?? "";
  form.password = "";
  saveFailure.value = null;
  validationAttempted.value = false;
  formRef.value?.restoreValidation();
}

watch(
  () => [props.show, props.server?.id] as const,
  ([show]) => {
    if (show) {
      prefill();
    }
  },
  { immediate: true },
);

/** closeModal closes the dialog without saving. */
function closeModal(): void {
  emit("update:show", false);
}

/** handleSave validates and PATCHes the server. */
async function handleSave(): Promise<void> {
  if (!props.server) {
    return;
  }
  saveFailure.value = null;
  validationAttempted.value = true;
  try {
    await formRef.value?.validate();
  } catch {
    return;
  }

  const input: UpdateServerInput = {
    name: form.name.trim(),
    ip: form.ip.trim(),
    port: form.port ?? 22,
    ssh_user: form.sshUser.trim(),
  };
  if (form.authMode === "key") {
    input.ssh_key_id = form.keyId.trim();
  } else if (form.authMode === "password" && form.password !== "") {
    // A blank password leaves the stored secret unchanged.
    input.password = form.password;
  }

  saving.value = true;
  try {
    const updated = await serversStore.updateServer(props.server.id, input);
    // The secret never leaves this form: drop it the moment the save lands.
    form.password = "";
    emit("updated", updated);
    emit("update:show", false);
  } catch (error) {
    saveFailure.value = error;
  } finally {
    saving.value = false;
  }
}

/**
 * A language switch refreshes already-visible edit feedback without
 * touching the draft. A pristine form is never validated, so no errors
 * surface on untouched fields.
 */
const stopLocaleWatch = onLocaleChange(() => {
  if (validationAttempted.value && props.show && formRef.value) {
    void formRef.value.validate().catch(() => {});
  }
});
onBeforeUnmount(stopLocaleWatch);
</script>

<template>
  <NModal
    :show="props.show"
    preset="card"
    :title="$t('servers.edit.title')"
    :mask-closable="false"
    class="edit-server-modal"
    style="width: 560px; max-width: 96vw"
    @update:show="(value: boolean) => emit('update:show', value)"
  >
    <NAlert v-if="errorMessage" type="error" :show-icon="true">
      {{ errorMessage }}
    </NAlert>

    <NForm
      ref="formRef"
      :model="form"
      :rules="rules"
      label-placement="top"
      @submit.prevent="handleSave"
    >
      <div class="connect-form">
        <section class="connect-group" :aria-label="$t('servers.edit.identity')">
          <h4 class="connect-group__title">{{ $t("servers.edit.identity") }}</h4>
          <NFormItem :label="$t('servers.edit.nodeName')" path="name">
            <NInput v-model:value="form.name" placeholder="build-node-03" :input-props="{ 'aria-label': $t('servers.edit.nodeName') }" />
            <span class="field-hint">{{ $t("servers.edit.nodeNameHint") }}</span>
          </NFormItem>
        </section>

        <section class="connect-group" :aria-label="$t('servers.edit.address')">
          <h4 class="connect-group__title">{{ $t("servers.edit.address") }}</h4>
          <div class="addr-row">
            <NFormItem :label="$t('servers.edit.ipLabel')" path="ip">
              <NInput v-model:value="form.ip" placeholder="203.0.113.90" :input-props="{ 'aria-label': $t('servers.edit.ipLabel') }" />
              <span class="field-hint">{{ $t("servers.edit.ipHint") }}</span>
            </NFormItem>
            <NFormItem :label="$t('servers.edit.portLabel')" path="port">
              <NInputNumber v-model:value="form.port" :min="1" :max="65535" placeholder="22" :input-props="{ 'aria-label': $t('servers.edit.portLabel') }" />
              <span class="field-hint">{{ $t("servers.edit.portHint") }}</span>
            </NFormItem>
          </div>
        </section>

        <section class="connect-group" :aria-label="$t('servers.edit.access')">
          <h4 class="connect-group__title">{{ $t("servers.edit.access") }}</h4>
          <NFormItem :label="$t('servers.edit.sshUser')" path="sshUser">
            <NInput v-model:value="form.sshUser" placeholder="root" :input-props="{ 'aria-label': $t('servers.edit.sshUser') }" />
            <span class="field-hint">{{ $t("servers.edit.sshUserHint") }}</span>
          </NFormItem>
        </section>

        <section class="connect-group" :aria-label="$t('servers.edit.credentials')">
          <h4 class="connect-group__title">{{ $t("servers.edit.credentials") }}</h4>
          <NFormItem :label="$t('servers.edit.credentialLabel')">
            <NRadioGroup v-model:value="form.authMode" size="small" :aria-label="$t('servers.edit.credentialChange')">
              <NRadioButton value="keep">{{ $t("servers.edit.keepCurrent", { current: currentAuth }) }}</NRadioButton>
              <NRadioButton value="key">{{ $t("servers.edit.keyOption") }}</NRadioButton>
              <NRadioButton value="password">{{ $t("servers.edit.passwordOption") }}</NRadioButton>
            </NRadioGroup>
            <span class="field-hint">{{ $t("servers.edit.credentialHint") }}</span>
          </NFormItem>

          <NFormItem v-if="form.authMode === 'key'" :label="$t('servers.edit.keyId')" path="keyId">
            <NInput v-model:value="form.keyId" placeholder="00000000-0000-0000-0000-000000000000" :input-props="{ 'aria-label': $t('servers.edit.keyId') }" />
            <span class="field-hint">{{ $t("servers.edit.keyIdHint") }}</span>
          </NFormItem>

          <NFormItem v-if="form.authMode === 'password'" :label="$t('servers.edit.newPassword')" path="password">
            <NInput
              v-model:value="form.password"
              type="password"
              show-password-on="click"
              :placeholder="$t('servers.edit.newPasswordHint')"
              :input-props="{ autocomplete: 'new-password', 'aria-label': $t('servers.edit.newPassword') }"
            />
            <span class="field-hint">{{ $t("servers.edit.newPasswordHint") }}</span>
          </NFormItem>
        </section>

        <NAlert v-if="resetsPin" type="warning" :show-icon="false">
          {{ $t("servers.edit.pinResetWarning") }}
        </NAlert>
      </div>
    </NForm>

    <template #footer>
      <NSpace justify="end" :size="8">
        <NButton @click="closeModal">{{ $t("servers.edit.cancel") }}</NButton>
        <NButton type="primary" :loading="saving" @click="handleSave">
          {{ $t("servers.edit.save") }}
        </NButton>
      </NSpace>
    </template>
  </NModal>
</template>

<style scoped>
.edit-server-modal :deep(.n-card-content) {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}
</style>
