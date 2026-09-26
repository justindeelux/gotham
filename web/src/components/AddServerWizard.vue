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
  NStep,
  NSteps,
  NTag,
  NText,
  useMessage,
} from "naive-ui";
import type { FormInst, FormRules } from "naive-ui";
import { computed, reactive, ref, watch } from "vue";

import {
  createPrivateKey,
  describeServerError,
} from "../api/servers";
import type { CheckResult, Server } from "../api/servers";
import ServerStatusTag from "./ServerStatusTag.vue";
import { useServersStore } from "../stores/servers";
import { formatBytes } from "../utils/format";

interface Props {
  show: boolean;
}

interface ConnectionForm {
  name: string;
  ip: string;
  port: number | null;
  sshUser: string;
  keyMode: "new" | "existing";
  keyName: string;
  privateKey: string;
  keyId: string;
}

const props = defineProps<Props>();
const emit = defineEmits<{
  "update:show": [value: boolean];
  created: [server: Server];
}>();

const serversStore = useServersStore();
const message = useMessage();

// The install one-liner mirrors deploy/install-agent.sh's release base URL.
const releaseBaseUrl =
  "https://github.com/justindeelux/gotham/releases/latest/download";
const installCommand = `curl -fsSL ${releaseBaseUrl}/install-agent.sh | sudo sh`;

const step = ref(0);
const formRef = ref<FormInst | null>(null);
const creating = ref(false);
const validating = ref(false);
const errorMessage = ref("");
const validateMessage = ref("");
const validationPassed = ref(false);
const checks = ref<CheckResult[]>([]);
const createdServer = ref<Server | null>(null);

const form = reactive<ConnectionForm>({
  name: "",
  ip: "",
  port: 22,
  sshUser: "root",
  keyMode: "new",
  keyName: "",
  privateKey: "",
  keyId: "",
});

const rules = computed<FormRules>(() => ({
  name: { required: true, message: "Name is required", trigger: ["input", "blur"] },
  ip: [
    { required: true, message: "IP address is required", trigger: ["input", "blur"] },
    {
      validator: (_rule, value: string) => isValidHost(value),
      message: "Enter a valid IPv4 address or hostname",
      trigger: ["input", "blur"],
    },
  ],
  port: {
    type: "number",
    required: true,
    message: "Port is required",
    trigger: ["input", "blur"],
  },
  sshUser: {
    required: true,
    message: "SSH user is required",
    trigger: ["input", "blur"],
  },
  keyName:
    form.keyMode === "new"
      ? { required: true, message: "Key name is required", trigger: ["input", "blur"] }
      : [],
  privateKey:
    form.keyMode === "new"
      ? { required: true, message: "Private key is required", trigger: ["input", "blur"] }
      : [],
  keyId:
    form.keyMode === "existing"
      ? { required: true, message: "Key ID is required", trigger: ["input", "blur"] }
      : [],
}));

/** currentServer prefers the polled store copy so status flips live. */
const currentServer = computed<Server | null>(() => {
  const created = createdServer.value;
  if (!created) {
    return null;
  }
  return serversStore.servers.find((item) => item.id === created.id) ?? created;
});

const isReady = computed<boolean>(() => currentServer.value?.status === "ready");

// Entering the validate step kicks off the first probe automatically; the
// Retry button repeats it on demand.
watch(step, (value) => {
  if (value === 1 && createdServer.value && checks.value.length === 0) {
    void handleValidate();
  }
});

/** isValidHost accepts an IPv4 literal or a DNS-style hostname. */
function isValidHost(value: string): boolean {
  const trimmed = value.trim();
  if (trimmed === "") {
    return false;
  }
  const ipv4 =
    /^(25[0-5]|2[0-4]\d|1?\d?\d)(\.(25[0-5]|2[0-4]\d|1?\d?\d)){3}$/;
  const hostname = /^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$/;
  return ipv4.test(trimmed) || hostname.test(trimmed);
}

/** handleCreate optionally stores a key, then registers the server. */
async function handleCreate(): Promise<void> {
  errorMessage.value = "";
  try {
    await formRef.value?.validate();
  } catch {
    return;
  }

  creating.value = true;
  try {
    let keyId: string | null = null;
    if (form.keyMode === "new") {
      const key = await createPrivateKey({
        name: form.keyName.trim(),
        private_key: form.privateKey,
      });
      keyId = key.id;
    } else {
      keyId = form.keyId.trim() || null;
    }

    const server = await serversStore.addServer({
      name: form.name.trim(),
      ip: form.ip.trim(),
      port: form.port ?? 22,
      ssh_user: form.sshUser.trim(),
      ssh_key_id: keyId,
    });

    createdServer.value = server;
    emit("created", server);
    step.value = 1;
  } catch (error) {
    errorMessage.value = describeServerError(error);
  } finally {
    creating.value = false;
  }
}

/** handleValidate runs the probe and stores the per-check results. */
async function handleValidate(): Promise<void> {
  const server = createdServer.value;
  if (!server) {
    return;
  }

  validating.value = true;
  validateMessage.value = "";
  try {
    const outcome = await serversStore.validate(server.id);
    checks.value = outcome.checks;
    validateMessage.value = outcome.message;
    validationPassed.value = outcome.ok;
    if (outcome.ok) {
      message.success("Validation passed");
    }
  } catch (error) {
    validateMessage.value = describeServerError(error);
  } finally {
    validating.value = false;
  }
}

/** copyInstallCommand copies the agent one-liner to the clipboard. */
async function copyInstallCommand(): Promise<void> {
  try {
    await navigator.clipboard.writeText(installCommand);
    message.success("Install command copied");
  } catch {
    message.error("Could not copy to clipboard");
  }
}

/** closeWizard closes the modal and resets the wizard state. */
function closeWizard(): void {
  emit("update:show", false);
  resetWizard();
}

/** handleShowChange mirrors the modal visibility and resets when closing. */
function handleShowChange(value: boolean): void {
  emit("update:show", value);
  if (!value) {
    resetWizard();
  }
}

/** resetWizard returns every field to its initial value. */
function resetWizard(): void {
  step.value = 0;
  form.name = "";
  form.ip = "";
  form.port = 22;
  form.sshUser = "root";
  form.keyMode = "new";
  form.keyName = "";
  form.privateKey = "";
  form.keyId = "";
  errorMessage.value = "";
  validateMessage.value = "";
  validationPassed.value = false;
  checks.value = [];
  createdServer.value = null;
  formRef.value?.restoreValidation();
}
</script>

<template>
  <NModal
    :show="props.show"
    preset="card"
    title="Add server"
    :mask-closable="false"
    class="wizard"
    style="width: 680px; max-width: 94vw"
    @update:show="handleShowChange"
  >
    <NSpace vertical :size="20">
      <NSteps :current="step + 1" size="small">
        <NStep title="Connection" description="Host and credentials" />
        <NStep title="Validate" description="Probe the node" />
        <NStep title="Install" description="Install the agent" />
      </NSteps>

      <NAlert v-if="errorMessage" type="error" :show-icon="true">
        {{ errorMessage }}
      </NAlert>

      <!-- Step 1: connection details -->
      <NForm
        v-if="step === 0"
        ref="formRef"
        :model="form"
        :rules="rules"
        label-placement="top"
        @submit.prevent="handleCreate"
      >
        <NSpace vertical :size="4">
          <NFormItem label="Name" path="name">
            <NInput v-model:value="form.name" placeholder="web-1" />
          </NFormItem>

          <NSpace :size="12">
            <NFormItem label="IP address" path="ip" class="grow">
              <NInput v-model:value="form.ip" placeholder="10.0.0.5" />
            </NFormItem>
            <NFormItem label="Port" path="port" style="width: 120px">
              <NInputNumber
                v-model:value="form.port"
                :min="1"
                :max="65535"
                placeholder="22"
              />
            </NFormItem>
          </NSpace>

          <NFormItem label="SSH user" path="sshUser">
            <NInput v-model:value="form.sshUser" placeholder="root" />
          </NFormItem>

          <NFormItem label="SSH key">
            <NRadioGroup v-model:value="form.keyMode" size="small">
              <NRadioButton value="new">Paste a new key</NRadioButton>
              <NRadioButton value="existing">Use an existing key ID</NRadioButton>
            </NRadioGroup>
          </NFormItem>

          <template v-if="form.keyMode === 'new'">
            <NFormItem label="Key name" path="keyName">
              <NInput v-model:value="form.keyName" placeholder="deploy-key" />
            </NFormItem>
            <NFormItem label="Private key (PEM)" path="privateKey">
              <NInput
                v-model:value="form.privateKey"
                type="textarea"
                :autosize="{ minRows: 4, maxRows: 10 }"
                placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
              />
            </NFormItem>
            <NText depth="3">
              The key is encrypted at rest by the control plane. It is never
              returned by the API.
            </NText>
          </template>

          <NFormItem v-else label="Key ID" path="keyId">
            <NInput
              v-model:value="form.keyId"
              placeholder="00000000-0000-0000-0000-000000000000"
            />
          </NFormItem>

          <NText depth="3">
            Key listing is not exposed by the API yet, so paste the key material
            or an existing key ID. Password-auth servers are not supported in
            this release.
          </NText>
        </NSpace>
      </NForm>

      <!-- Step 2: validation checklist -->
      <NSpace v-else-if="step === 1" vertical :size="12">
        <NText depth="2">
          Running readiness probes over SSH. Docker must be installed and
          reachable.
        </NText>

        <NAlert v-if="validateMessage && !validationPassed" type="error" :show-icon="true">
          {{ validateMessage }}
        </NAlert>

        <NSpace v-if="checks.length" vertical :size="8">
          <div v-for="check in checks" :key="check.name" class="check-row">
            <NTag :type="check.ok ? 'success' : 'error'" size="small" round>
              {{ check.ok ? "ok" : "fail" }}
            </NTag>
            <NText strong class="check-name">{{ check.name.toUpperCase() }}</NText>
            <NText depth="2" class="check-detail">{{ check.detail }}</NText>
          </div>
        </NSpace>

        <NText v-else-if="!validating" depth="3">No checks have run yet.</NText>

        <NAlert v-if="validationPassed" type="success" :show-icon="true">
          All checks passed. Continue to install the node agent.
        </NAlert>
      </NSpace>

      <!-- Step 3: install the agent -->
      <NSpace v-else vertical :size="12">
        <NSpace align="center" :size="8">
          <NText depth="2">Current status:</NText>
          <ServerStatusTag v-if="currentServer" :status="currentServer.status" />
        </NSpace>

        <NAlert v-if="isReady" type="success" :show-icon="true">
          The agent registered and the server is ready.
        </NAlert>

        <NText depth="2">
          SSH into the node and run the installer. It downloads the agent,
          installs the systemd unit, and registers with the control plane.
        </NText>

        <NSpace align="center" :size="8">
          <NInput :value="installCommand" readonly class="grow" />
          <NButton @click="copyInstallCommand">Copy</NButton>
        </NSpace>

        <NText depth="3">
          The page keeps polling every 5 seconds; the status badge flips to Ready
          once the agent checks in.
        </NText>

        <NText v-if="currentServer" depth="3">
          Detected memory: {{ formatBytes(currentServer.total_mem) }} · disk:
          {{ formatBytes(currentServer.total_disk) }}
        </NText>
      </NSpace>
    </NSpace>

    <template #footer>
      <NSpace justify="end" :size="8">
        <template v-if="step === 0">
          <NButton @click="closeWizard">Cancel</NButton>
          <NButton type="primary" :loading="creating" @click="handleCreate">
            Create &amp; continue
          </NButton>
        </template>
        <template v-else-if="step === 1">
          <NButton :loading="validating" @click="handleValidate">
            Retry validation
          </NButton>
          <NButton
            type="primary"
            :disabled="!validationPassed"
            @click="step = 2"
          >
            Continue
          </NButton>
        </template>
        <template v-else>
          <NButton type="primary" @click="closeWizard">Done</NButton>
        </template>
      </NSpace>
    </template>
  </NModal>
</template>

<style scoped>
.check-row {
  display: flex;
  align-items: baseline;
  gap: 8px;
}

.check-name {
  min-width: 64px;
}

.check-detail {
  word-break: break-word;
}

.grow {
  flex: 1;
}

.wizard :deep(.n-card__content) {
  max-height: 60vh;
  overflow-y: auto;
}
</style>
