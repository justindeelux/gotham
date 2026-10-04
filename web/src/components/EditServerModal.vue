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
import { computed, reactive, ref, watch } from "vue";

import type { Server, UpdateServerInput } from "../api/servers";
import { useServersStore } from "../stores/servers";

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
const formRef = ref<FormInst | null>(null);
const errorMessage = ref("");
const saving = ref(false);

const form = reactive<EditForm>({
  name: "",
  ip: "",
  port: 22,
  sshUser: "root",
  authMode: "keep",
  keyId: "",
  password: "",
});

const HOST_PATTERN = /^[0-9a-zA-Z.-]+$/;
const NAME_PATTERN = /^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,62}$/;
const USER_PATTERN = /^[a-z_][a-z0-9_-]*[$]?$/;

const rules = computed<FormRules>(() => ({
  name: [
    { required: true, message: "Enter a node name.", trigger: ["input", "blur"] },
    {
      validator: (_rule, value: string) =>
        value.trim() === "" || NAME_PATTERN.test(value.trim()),
      message: "Letters, digits, dots, dashes, and underscores only.",
      trigger: ["input", "blur"],
    },
  ],
  ip: [
    { required: true, message: "Enter an IP address or hostname.", trigger: ["input", "blur"] },
    {
      validator: (_rule, value: string) => {
        const trimmed = value.trim();
        if (trimmed === "" || !HOST_PATTERN.test(trimmed)) {
          return false;
        }
        const ipv4 =
          /^(25[0-5]|2[0-4]\d|1?\d?\d)(\.(25[0-5]|2[0-4]\d|1?\d?\d)){3}$/;
        const hostname =
          /^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$/;
        return ipv4.test(trimmed) || hostname.test(trimmed);
      },
      message: "Enter a valid IPv4 address or hostname.",
      trigger: ["input", "blur"],
    },
  ],
  port: [
    {
      type: "number",
      required: true,
      message: "Enter an SSH port (1-65535).",
      trigger: ["input", "blur"],
    },
    {
      validator: (_rule, value: number | null) =>
        value === null || (Number.isInteger(value) && value >= 1 && value <= 65535),
      message: "Port must be a number from 1 to 65535.",
      trigger: ["input", "blur"],
    },
  ],
  sshUser: [
    { required: true, message: "Enter the SSH user.", trigger: ["input", "blur"] },
    {
      validator: (_rule, value: string) =>
        value.trim() === "" || USER_PATTERN.test(value.trim()),
      message: "Enter a valid Unix username (lowercase, digits, _, -).",
      trigger: ["input", "blur"],
    },
  ],
  keyId:
    form.authMode === "key"
      ? { required: true, message: "Enter a key ID.", trigger: ["input", "blur"] }
      : [],
}));

/** currentAuth describes the stored credential without revealing it. */
const currentAuth = computed<string>(() => {
  if (!props.server) {
    return "";
  }
  if (props.server.ssh_key_id) {
    return `SSH key ${props.server.ssh_key_id.slice(0, 8)}…`;
  }
  if (props.server.has_password) {
    return "Password stored";
  }
  return "No credentials stored";
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
  errorMessage.value = "";
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
  errorMessage.value = "";
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
    errorMessage.value =
      error instanceof Error ? error.message : "Something went wrong. Please try again.";
  } finally {
    saving.value = false;
  }
}
</script>

<template>
  <NModal
    :show="props.show"
    preset="card"
    title="Edit server"
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
        <section class="connect-group" aria-label="Identity">
          <h4 class="connect-group__title">Identity</h4>
          <NFormItem label="Node name" path="name">
            <NInput v-model:value="form.name" placeholder="build-node-03" :input-props="{ 'aria-label': 'Node name' }" />
            <span class="field-hint">A short unique name, e.g. build-node-03.</span>
          </NFormItem>
        </section>

        <section class="connect-group" aria-label="Address">
          <h4 class="connect-group__title">Address</h4>
          <div class="addr-row">
            <NFormItem label="IP address / hostname" path="ip">
              <NInput v-model:value="form.ip" placeholder="203.0.113.90" :input-props="{ 'aria-label': 'IP address or hostname' }" />
              <span class="field-hint">IPv4 or hostname.</span>
            </NFormItem>
            <NFormItem label="SSH port" path="port">
              <NInputNumber v-model:value="form.port" :min="1" :max="65535" placeholder="22" :input-props="{ 'aria-label': 'SSH port' }" />
              <span class="field-hint">Usually 22.</span>
            </NFormItem>
          </div>
        </section>

        <section class="connect-group" aria-label="Access">
          <h4 class="connect-group__title">Access</h4>
          <div class="form-row">
            <NFormItem label="SSH user" path="sshUser">
              <NInput v-model:value="form.sshUser" placeholder="root" :input-props="{ 'aria-label': 'SSH user' }" />
              <span class="field-hint">The Unix user the control plane connects as.</span>
            </NFormItem>
            <NFormItem label="Credentials">
              <NRadioGroup v-model:value="form.authMode" size="small" aria-label="Credential change">
                <NRadioButton value="keep">Keep ({{ currentAuth }})</NRadioButton>
                <NRadioButton value="key">SSH key</NRadioButton>
                <NRadioButton value="password">Password</NRadioButton>
              </NRadioGroup>
              <span class="field-hint">Switching the credential replaces the stored one.</span>
            </NFormItem>
          </div>
        </section>

        <section class="connect-group" aria-label="Credentials">
          <h4 class="connect-group__title">Credentials</h4>

          <NFormItem v-if="form.authMode === 'key'" label="Key ID" path="keyId">
            <NInput v-model:value="form.keyId" placeholder="00000000-0000-0000-0000-000000000000" :input-props="{ 'aria-label': 'Key ID' }" />
            <span class="field-hint">The UUID of a key already stored on the control plane.</span>
          </NFormItem>

          <NFormItem v-if="form.authMode === 'password'" label="New password" path="password">
            <NInput
              v-model:value="form.password"
              type="password"
              show-password-on="click"
              placeholder="Leave blank to keep the stored password"
              :input-props="{ autocomplete: 'new-password', 'aria-label': 'New password' }"
            />
            <span class="field-hint">Blank keeps the stored password. Stored encrypted, never returned.</span>
          </NFormItem>
        </section>

        <NAlert v-if="resetsPin" type="warning" :show-icon="false">
          Changing the address, user, or credential resets the pinned host key and
          returns the node to pending — revalidate it afterwards.
        </NAlert>
      </div>
    </NForm>

    <template #footer>
      <NSpace justify="end" :size="8">
        <NButton @click="closeModal">Cancel</NButton>
        <NButton type="primary" :loading="saving" @click="handleSave">
          Save changes
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
