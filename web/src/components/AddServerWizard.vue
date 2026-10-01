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
import type { CheckResult, Server, ServerCheckName } from "../api/servers";
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
  passphrase: string;
  keyId: string;
}

/** Display state of one fixed validation check. */
type CheckState = "idle" | "running" | "ok" | "fail";

interface FixedCheck {
  name: ServerCheckName;
  label: string;
  state: CheckState;
  detail: string;
}

const props = defineProps<Props>();
const emit = defineEmits<{
  "update:show": [value: boolean];
  created: [server: Server];
}>();

const serversStore = useServersStore();
const message = useMessage();

// The installer is not a release asset and needs its sibling files
// (deploy/release-verify.sh, gotham-agent-updater.conf, ...), so the wizard
// points at the documented checkout flow instead of a `curl | sh` one-liner
// that would 404. Keep this a single copy-pasteable block.
const installCommand =
  "git clone --depth 1 https://github.com/justindeelux/gotham /tmp/gotham " +
  "&& sudo /tmp/gotham/deploy/install-agent.sh";

const stepNames = ["Connect", "Validate", "Install", "Finish"];

/** Fixed probe list — exactly the checks the validate API reports. */
const FIXED_CHECK_LABELS: Array<{ name: ServerCheckName; label: string }> = [
  { name: "docker", label: "Docker Engine installed and running" },
  { name: "cpu", label: "CPU architecture and core count" },
  { name: "ram", label: "Available RAM" },
  { name: "disk", label: "Free disk space" },
];

const step = ref(0);
const formRef = ref<FormInst | null>(null);
const creating = ref(false);
const validating = ref(false);
const errorMessage = ref("");
const validateMessage = ref("");
const validationPassed = ref(false);
const fixedChecks = ref<FixedCheck[]>(makeIdleChecks());
const createdServer = ref<Server | null>(null);

const form = reactive<ConnectionForm>({
  name: "",
  ip: "",
  port: 22,
  sshUser: "root",
  keyMode: "new",
  keyName: "",
  privateKey: "",
  passphrase: "",
  keyId: "",
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
      validator: (_rule, value: string) => isValidHost(value),
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
  keyName:
    form.keyMode === "new"
      ? { required: true, message: "Enter a key name.", trigger: ["input", "blur"] }
      : [],
  privateKey:
    form.keyMode === "new"
      ? { required: true, message: "Paste the PEM-encoded private key.", trigger: ["input", "blur"] }
      : [],
  keyId:
    form.keyMode === "existing"
      ? { required: true, message: "Enter an existing key ID.", trigger: ["input", "blur"] }
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

/** SSH status line for the validate step (connection facts only). */
const sshStatusLine = computed<string>(() => {
  const server = currentServer.value;
  if (!server) {
    return "";
  }
  return `SSH ${server.ip}:${server.port} · user ${server.ssh_user}`;
});

const passedCount = computed<number>(
  () => fixedChecks.value.filter((check) => check.state === "ok").length,
);

/** checkSummary reports the real outcome — never a fabricated duration. */
const checkSummary = computed<string>(() => {
  if (validating.value) {
    return "Running probes…";
  }
  if (!hasRunChecks.value) {
    return "No probes have run yet.";
  }
  const total = fixedChecks.value.length;
  const passed = passedCount.value;
  if (validationPassed.value) {
    return `${passed}/${total} probes passed — the node is ready for the agent.`;
  }
  return `${passed}/${total} probes passed — fix the failing checks, then retry.`;
});

const hasRunChecks = computed<boolean>(
  () => fixedChecks.value.some((check) => check.state === "ok" || check.state === "fail"),
);

/** nodeInitials renders the avatar on the Finish step. */
const nodeInitials = computed<string>(() => {
  const name = (currentServer.value?.name ?? form.name).trim();
  if (name === "") {
    return "··";
  }
  const compact = name.replace(/[^a-zA-Z0-9]/g, "");
  return (compact.slice(0, 2) || name.slice(0, 2)).toUpperCase();
});

const resourceSummary = computed<string>(() => {
  const server = currentServer.value;
  if (!server) {
    return "";
  }
  const parts: string[] = [];
  if (server.total_mem !== null) {
    parts.push(formatBytes(server.total_mem));
  }
  if (server.total_disk !== null) {
    parts.push(formatBytes(server.total_disk));
  }
  if (server.arch !== null && server.arch !== "") {
    parts.push(server.arch);
  }
  return parts.join(" · ");
});

// Entering the validate step kicks off the first probe automatically; the
// Retry button repeats it on demand.
watch(step, (value) => {
  if (value === 1 && createdServer.value && !hasRunChecks.value && !validating.value) {
    void handleValidate();
  }
});

/** makeIdleChecks returns the fixed probe list in the idle state. */
function makeIdleChecks(): FixedCheck[] {
  return FIXED_CHECK_LABELS.map((item) => ({
    name: item.name,
    label: item.label,
    state: "idle" as CheckState,
    detail: "Pending",
  }));
}

/** applyCheckResults maps the API outcome onto the fixed check list. */
function applyCheckResults(results: CheckResult[]): void {
  const byName = new Map(results.map((item) => [item.name, item]));
  fixedChecks.value = FIXED_CHECK_LABELS.map((item) => {
    const result = byName.get(item.name);
    if (!result) {
      return { name: item.name, label: item.label, state: "idle" as CheckState, detail: "Pending" };
    }
    return {
      name: item.name,
      label: item.label,
      state: (result.ok ? "ok" : "fail") as CheckState,
      detail: result.detail,
    };
  });
}

/** isValidHost accepts an IPv4 literal or a DNS-style hostname. */
function isValidHost(value: string): boolean {
  const trimmed = value.trim();
  if (trimmed === "" || !HOST_PATTERN.test(trimmed)) {
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
  fixedChecks.value = FIXED_CHECK_LABELS.map((item) => ({
    name: item.name,
    label: item.label,
    state: "running" as CheckState,
    detail: "Running…",
  }));
  try {
    const outcome = await serversStore.validate(server.id);
    applyCheckResults(outcome.checks);
    validateMessage.value = outcome.message;
    validationPassed.value = outcome.ok;
    if (outcome.ok) {
      message.success("Validation passed");
    }
  } catch (error) {
    fixedChecks.value = makeIdleChecks();
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
  form.passphrase = "";
  form.keyId = "";
  errorMessage.value = "";
  validateMessage.value = "";
  validationPassed.value = false;
  fixedChecks.value = makeIdleChecks();
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
    class="wizard-modal"
    style="width: 880px; max-width: 96vw"
    @update:show="handleShowChange"
  >
    <NText depth="3">
      Probe the node over SSH (Docker, CPU, RAM, disk), then install the agent.
      Probes run for at most 15 seconds.
    </NText>

    <div class="wizard">
      <div class="wizard-rail">
        <ol>
          <li
            v-for="(label, index) in stepNames"
            :key="label"
            :class="{
              'is-active': step === index,
              'is-done': step > index,
            }"
          >
            <span class="idx">{{ index + 1 }}</span>{{ label }}
          </li>
        </ol>
        <p class="wizard-rail-note">
          The private key is encrypted before it is stored and is never returned
          by the API.
        </p>
      </div>

      <div class="wizard-main">
        <div class="wizard-body">
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
              <NFormItem label="Node name" path="name">
                <NInput v-model:value="form.name" placeholder="build-node-03" />
                <template #feedback>
                  <span class="field-hint">A short unique name, e.g. build-node-03.</span>
                </template>
              </NFormItem>

              <NSpace :size="12">
                <NFormItem label="IP address / hostname" path="ip" class="grow">
                  <NInput v-model:value="form.ip" placeholder="203.0.113.90" />
                  <template #feedback>
                    <span class="field-hint">IPv4 or hostname, e.g. 203.0.113.90 or node3.internal.</span>
                  </template>
                </NFormItem>
                <NFormItem label="SSH port" path="port" style="width: 120px">
                  <NInputNumber
                    v-model:value="form.port"
                    :min="1"
                    :max="65535"
                    placeholder="22"
                  />
                  <template #feedback>
                    <span class="field-hint">Usually 22.</span>
                  </template>
                </NFormItem>
              </NSpace>

              <NFormItem label="SSH user" path="sshUser">
                <NInput v-model:value="form.sshUser" placeholder="root" />
                <template #feedback>
                  <span class="field-hint">The Unix user the control plane connects as.</span>
                </template>
              </NFormItem>

              <NFormItem label="SSH key">
                <NRadioGroup v-model:value="form.keyMode" size="small">
                  <NRadioButton value="new">Paste a new key</NRadioButton>
                  <NRadioButton value="existing">Use an existing key ID</NRadioButton>
                </NRadioGroup>
                <template #feedback>
                  <span class="field-hint">Key listing is not exposed by the API yet — paste the key material or a known key ID.</span>
                </template>
              </NFormItem>

              <template v-if="form.keyMode === 'new'">
                <NFormItem label="Key name" path="keyName">
                  <NInput v-model:value="form.keyName" placeholder="deploy-key" />
                  <template #feedback>
                    <span class="field-hint">A label so you can reuse the key for other nodes.</span>
                  </template>
                </NFormItem>
                <NFormItem label="Private key (PEM)" path="privateKey">
                  <NInput
                    v-model:value="form.privateKey"
                    type="textarea"
                    :autosize="{ minRows: 4, maxRows: 10 }"
                    placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
                  />
                  <template #feedback>
                    <span class="field-hint">Ed25519 or RSA in PEM format. Stored encrypted, never returned.</span>
                  </template>
                </NFormItem>
                <NFormItem label="Key passphrase (if any)" path="passphrase">
                  <NInput
                    v-model:value="form.passphrase"
                    type="password"
                    show-password-on="click"
                    placeholder="Leave empty for unencrypted keys"
                  />
                  <template #feedback>
                    <span class="field-hint">The API does not accept a passphrase yet — passphrase-protected keys will fail validation.</span>
                  </template>
                </NFormItem>
              </template>

              <NFormItem v-else label="Key ID" path="keyId">
                <NInput
                  v-model:value="form.keyId"
                  placeholder="00000000-0000-0000-0000-000000000000"
                />
                <template #feedback>
                  <span class="field-hint">The UUID of a key already stored on the control plane.</span>
                </template>
              </NFormItem>

              <NText depth="3">
                Password-auth servers are not supported in this release.
              </NText>
            </NSpace>
          </NForm>

          <!-- Step 2: validation checklist -->
          <NSpace v-else-if="step === 1" vertical :size="12">
            <div class="status-line-row">
              <NText strong>Validate node</NText>
              <span v-if="sshStatusLine" class="status-line">
                <span class="dot" :class="validating ? 'dot--running' : hasRunChecks ? (validationPassed ? 'dot--ok' : 'dot--fail') : 'dot--idle'" />
                {{ sshStatusLine }}
              </span>
            </div>

            <NAlert v-if="validateMessage && !validationPassed" type="error" :show-icon="true">
              {{ validateMessage }}
            </NAlert>

            <div class="check-list">
              <div
                v-for="check in fixedChecks"
                :key="check.name"
                class="check-row"
                :class="`is-${check.state}`"
              >
                <span class="mark">
                  <span v-if="check.state === 'ok'" class="glyph">✓</span>
                  <span v-else-if="check.state === 'fail'" class="glyph">✕</span>
                </span>
                <span class="grow">{{ check.label }}</span>
                <span class="detail">{{ check.detail }}</span>
              </div>
            </div>
            <NText depth="3">{{ checkSummary }}</NText>

            <NAlert v-if="validationPassed" type="success" :show-icon="true">
              All checks passed. Continue to install the node agent.
            </NAlert>
          </NSpace>

          <!-- Step 3: install the agent (real states only) -->
          <NSpace v-else-if="step === 2" vertical :size="12">
            <NText strong>Install the agent</NText>

            <div class="check-list">
              <div class="check-row is-ok">
                <span class="mark"><span class="glyph">✓</span></span>
                <span class="grow">Server registered on the control plane</span>
                <span class="detail">{{ currentServer?.name ?? "" }}</span>
              </div>
              <div class="check-row" :class="validationPassed ? 'is-ok' : 'is-idle'">
                <span class="mark"><span v-if="validationPassed" class="glyph">✓</span></span>
                <span class="grow">Readiness probes passed</span>
                <span class="detail">{{ passedCount }}/{{ fixedChecks.length }}</span>
              </div>
              <div
                class="check-row"
                :class="isReady ? 'is-ok' : validating ? 'is-running' : 'is-idle'"
              >
                <span class="mark"><span v-if="isReady" class="glyph">✓</span></span>
                <span class="grow">Agent installed and reporting</span>
                <span class="detail">{{ isReady ? "heartbeat received" : "waiting for first heartbeat" }}</span>
              </div>
            </div>

            <NSpace align="center" :size="8">
              <NText depth="2">Current status:</NText>
              <ServerStatusTag v-if="currentServer" :status="currentServer.status" />
            </NSpace>

            <NAlert v-if="isReady" type="success" :show-icon="true">
              The agent registered and the server is ready.
            </NAlert>

            <NText depth="2">
              SSH into the node and run the installer from the checkout. It
              downloads the agent, installs the systemd unit, and registers with
              the control plane. Pass the control plane's CA certificate with
              --ca (copy it from the control plane first) — the installer fails
              closed without it.
            </NText>

            <NSpace align="center" :size="8">
              <NInput :value="installCommand" readonly class="grow" />
              <NButton @click="copyInstallCommand">Copy</NButton>
            </NSpace>

            <NText depth="3">
              The page keeps polling every 5 seconds; the status badge flips to
              Ready once the agent checks in. No install progress stream exists
              yet, so this screen only shows live states — never a simulated
              percentage.
            </NText>

            <NText v-if="currentServer" depth="3">
              Detected memory: {{ formatBytes(currentServer.total_mem) }} · disk:
              {{ formatBytes(currentServer.total_disk) }}
            </NText>
          </NSpace>

          <!-- Step 4: finish -->
          <div v-else class="finish">
            <span class="avatar avatar--lg">{{ nodeInitials }}</span>
            <h4>{{ currentServer?.name ?? form.name }} is ready</h4>
            <NText depth="3">
              The node passed validation. Finish the agent install on the node
              and it will start sending heartbeats.
            </NText>
            <div class="finish-tags">
              <ServerStatusTag v-if="currentServer" :status="currentServer.status" />
              <NTag v-if="currentServer?.docker_version" size="small" round>
                Docker {{ currentServer.docker_version }}
              </NTag>
              <NTag v-if="resourceSummary" size="small" round>
                {{ resourceSummary }}
              </NTag>
            </div>
          </div>
        </div>

        <div class="wizard-foot">
          <NButton v-if="step > 0" tertiary @click="step -= 1">Back</NButton>
          <span class="step-counter">Step {{ step + 1 }} / 4</span>
          <span class="grow" />
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
          <template v-else-if="step === 2">
            <NButton type="primary" @click="step = 3">Continue to finish</NButton>
          </template>
          <template v-else>
            <NButton type="primary" @click="closeWizard">Done</NButton>
          </template>
        </div>
      </div>
    </div>
  </NModal>
</template>

<style scoped>
.field-hint {
  font-size: var(--text-xs);
  color: var(--meta);
}

.wizard {
  display: grid;
  grid-template-columns: 208px minmax(0, 1fr);
  min-height: 420px;
  margin-top: var(--space-3);
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  overflow: hidden;
}

.wizard-rail {
  border-right: 1px solid var(--border);
  padding: var(--space-4) var(--space-3);
  background: color-mix(in oklab, var(--surface) 70%, var(--surface-warm));
}

.wizard-rail ol {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  margin: 0;
  padding: 0;
  list-style: none;
}

.wizard-rail li {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: 7px var(--space-2);
  border-radius: var(--radius-sm);
  font-size: var(--text-sm);
  color: var(--muted);
}

.wizard-rail li .idx {
  width: 20px;
  height: 20px;
  border-radius: var(--radius-pill);
  border: 1.5px solid var(--meta);
  display: grid;
  place-items: center;
  font-family: var(--font-mono);
  font-size: 10px;
  flex: 0 0 auto;
}

.wizard-rail li.is-done {
  color: var(--fg);
}

.wizard-rail li.is-done .idx {
  background: var(--success);
  border-color: var(--success);
  color: var(--accent-on);
}

.wizard-rail li.is-active {
  background: var(--selected-row);
  color: var(--fg-2);
  font-weight: 600;
}

.wizard-rail li.is-active .idx {
  border-color: var(--accent);
  color: var(--accent-ink);
}

.wizard-rail-note {
  margin: var(--space-4) 0 0;
  font-size: var(--text-xs);
  color: var(--meta);
  line-height: var(--leading-body);
}

.wizard-main {
  display: flex;
  flex-direction: column;
  min-height: 0;
  min-width: 0;
}

.wizard-body {
  padding: var(--space-4);
  overflow-y: auto;
  flex: 1 1 auto;
  min-height: 0;
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}

.wizard-foot {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-3) var(--space-4);
  border-top: 1px solid var(--border);
  flex: 0 0 auto;
}

.step-counter {
  font-size: var(--text-xs);
  color: var(--muted);
  font-family: var(--font-mono);
}

.status-line-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-2);
  flex-wrap: wrap;
}

.status-line {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--text-xs);
  color: var(--muted);
  font-family: var(--font-mono);
}

.dot {
  width: 8px;
  height: 8px;
  border-radius: var(--radius-pill);
  flex: 0 0 auto;
}

.dot--idle {
  background: var(--meta);
}

.dot--running {
  background: var(--accent);
  animation: wizard-pulse 900ms ease-in-out infinite alternate;
}

.dot--ok {
  background: var(--success);
}

.dot--fail {
  background: var(--danger);
}

@keyframes wizard-pulse {
  from {
    opacity: 0.4;
  }
  to {
    opacity: 1;
  }
}

.check-list {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.check-row {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-2) var(--space-3);
  border-radius: var(--radius-sm);
  background: var(--surface-warm);
}

.check-row .mark {
  width: 18px;
  height: 18px;
  border-radius: var(--radius-pill);
  display: grid;
  place-items: center;
  flex: 0 0 auto;
  border: 1.5px solid var(--meta);
}

.check-row .mark .glyph {
  font-size: 11px;
  line-height: 1;
  font-weight: 700;
}

.check-row.is-ok .mark {
  background: var(--success);
  border-color: var(--success);
  color: var(--accent-on);
}

.check-row.is-fail .mark {
  background: var(--danger);
  border-color: var(--danger);
  color: var(--accent-on);
}

.check-row.is-running .mark {
  border-color: var(--accent);
  border-top-color: transparent;
  animation: wizard-spin 700ms linear infinite;
}

@keyframes wizard-spin {
  to {
    transform: rotate(360deg);
  }
}

.check-row .detail {
  margin-left: auto;
  font-family: var(--font-mono);
  font-size: var(--text-xs);
  color: var(--muted);
  text-align: right;
  word-break: break-word;
}

.finish {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--space-3);
  text-align: center;
  padding: var(--space-6) 0;
}

.finish h4 {
  margin: 0;
  color: var(--fg-2);
  font-size: var(--text-xl);
}

.finish-tags {
  display: flex;
  gap: var(--space-2);
  flex-wrap: wrap;
  justify-content: center;
}

.avatar {
  display: inline-grid;
  place-items: center;
  background: var(--accent-soft);
  color: var(--accent-ink);
  font-weight: 700;
  border-radius: var(--radius-md);
}

.avatar--lg {
  width: 48px;
  height: 48px;
  font-size: var(--text-lg);
}

.grow {
  flex: 1;
  min-width: 0;
}

.wizard-modal :deep(.n-card__content) {
  max-height: 72vh;
  overflow-y: auto;
}

@media (max-width: 720px) {
  .wizard {
    grid-template-columns: minmax(0, 1fr);
  }

  .wizard-rail {
    border-right: 0;
    border-bottom: 1px solid var(--border);
  }

  .wizard-rail ol {
    flex-direction: row;
    flex-wrap: wrap;
  }
}
</style>
