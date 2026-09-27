<script setup lang="ts">
import {
  NAlert,
  NButton,
  NFormItem,
  NInput,
  NInputNumber,
  NModal,
  NRadio,
  NRadioGroup,
  NSelect,
  NSpace,
  NSwitch,
  NText,
  useMessage,
} from "naive-ui";
import { computed, reactive, ref, watch } from "vue";

import {
  describeDatabaseError,
} from "../api/databases";
import type {
  CreatedDatabase,
  DatabaseCredentials,
} from "../api/databases";
import { useDatabasesStore } from "../stores/databases";
import { useServersStore } from "../stores/servers";

interface Props {
  show: boolean;
}

interface WizardForm {
  engine: string;
  version: string;
  serverId: string;
  name: string;
  exposePublic: boolean;
  publicPort: number | null;
}

const props = defineProps<Props>();
const emit = defineEmits<{
  "update:show": [value: boolean];
  created: [created: CreatedDatabase];
}>();

const databasesStore = useDatabasesStore();
const serversStore = useServersStore();
const message = useMessage();

/** Backend name rule from internal/databases/service.go (namePattern). */
const NAME_PATTERN = /^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,62}$/;

interface EngineMeta {
  value: string;
  label: string;
  repo: string;
  defaultVersion: string;
  versions: string[];
  port: number;
}

/**
 * Engine catalogue mirroring internal/databases (*Engine.Image/PortSpec).
 * The version list offers known-good tags; any tag matching the backend
 * version pattern is accepted via the custom input is not needed — the
 * select covers the defaults and recent majors.
 */
const ENGINES: EngineMeta[] = [
  {
    value: "postgres",
    label: "PostgreSQL",
    repo: "postgres",
    defaultVersion: "16-alpine",
    versions: ["16-alpine", "15-alpine", "14-alpine"],
    port: 5432,
  },
  {
    value: "mysql",
    label: "MySQL",
    repo: "mysql",
    defaultVersion: "8.4",
    versions: ["8.4", "8.0"],
    port: 3306,
  },
  {
    value: "mariadb",
    label: "MariaDB",
    repo: "mariadb",
    defaultVersion: "11.4",
    versions: ["11.4", "11", "10.11"],
    port: 3306,
  },
  {
    value: "mongodb",
    label: "MongoDB",
    repo: "mongo",
    defaultVersion: "7.0",
    versions: ["7.0", "6.0"],
    port: 27017,
  },
  {
    value: "redis",
    label: "Redis",
    repo: "redis",
    defaultVersion: "7.2-alpine",
    versions: ["7.2-alpine", "7.0-alpine"],
    port: 6379,
  },
];

const stepNames = ["Engine", "Configure", "Review"];

const step = ref(0);
const submitting = ref(false);
const errorMessage = ref("");
const created = ref<CreatedDatabase | null>(null);

const form = reactive<WizardForm>({
  engine: "postgres",
  version: "",
  serverId: "",
  name: "",
  exposePublic: false,
  publicPort: null,
});

const selectedEngine = computed<EngineMeta>(
  () => ENGINES.find((item) => item.value === form.engine) ?? ENGINES[0],
);

const versionOptions = computed<Array<{ label: string; value: string }>>(
  () => selectedEngine.value.versions.map((tag) => ({
    label: `${selectedEngine.value.repo}:${tag}`,
    value: tag,
  })),
);

const effectiveVersion = computed<string>(
  () => form.version || selectedEngine.value.defaultVersion,
);

const imagePreview = computed<string>(
  () => `${selectedEngine.value.repo}:${effectiveVersion.value}`,
);

const serverOptions = computed<Array<{ label: string; value: string }>>(() =>
  serversStore.servers.map((server) => ({
    label: `${server.name} · ${server.ip}`,
    value: server.id,
  })),
);

/** engineValid gates the Engine step: an engine and a node. */
const engineValid = computed<boolean>(() => form.serverId !== "");

/** configureValid gates the Configure step: backend name rule + port range. */
const configureValid = computed<boolean>(() => {
  if (!NAME_PATTERN.test(form.name.trim())) {
    return false;
  }
  if (form.exposePublic) {
    return (
      form.publicPort !== null &&
      Number.isInteger(form.publicPort) &&
      form.publicPort >= 1 &&
      form.publicPort <= 65535
    );
  }
  return true;
});

const canContinue = computed<boolean>(() => {
  switch (step.value) {
    case 0:
      return engineValid.value;
    case 1:
      return configureValid.value;
    default:
      return true;
  }
});

/** copyText copies a secret to the clipboard and confirms with a toast. */
async function copyText(value: string, label: string): Promise<void> {
  try {
    await navigator.clipboard.writeText(value);
    message.success(`${label} copied to clipboard`);
  } catch {
    message.error(`Could not copy ${label.toLowerCase()}`);
  }
}

/** credentialRows renders the generated credentials as label/value pairs. */
function credentialRows(
  credentials: DatabaseCredentials,
): Array<{ label: string; value: string; secret: boolean }> {
  const rows: Array<{ label: string; value: string; secret: boolean }> = [
    { label: "Username", value: credentials.username, secret: false },
    { label: "Password", value: credentials.password, secret: true },
    { label: "Database", value: credentials.database, secret: false },
  ];
  if (credentials.root_password) {
    rows.push({
      label: "Root password",
      value: credentials.root_password,
      secret: true,
    });
  }
  return rows;
}

/** resetWizard clears the form back to its defaults. */
function resetWizard(): void {
  step.value = 0;
  submitting.value = false;
  errorMessage.value = "";
  created.value = null;
  form.engine = "postgres";
  form.version = "";
  form.serverId = "";
  form.name = "";
  form.exposePublic = false;
  form.publicPort = null;
}

/** goNext advances one step, or submits on the review step. */
function goNext(): void {
  if (step.value < stepNames.length - 1) {
    step.value += 1;
    return;
  }
  void handleSubmit();
}

/** goBack returns to the previous step. */
function goBack(): void {
  if (step.value > 0) {
    step.value -= 1;
  }
}

/**
 * handleSubmit provisions the database. Credentials are generated
 * server-side — the wizard never asks for a password, it shows the generated
 * set on the success step.
 */
async function handleSubmit(): Promise<void> {
  submitting.value = true;
  errorMessage.value = "";
  try {
    created.value = await databasesStore.provision({
      name: form.name.trim(),
      engine: form.engine,
      version: form.version || undefined,
      server_id: form.serverId,
      public_port: form.exposePublic ? (form.publicPort ?? undefined) : undefined,
    });
    message.success(`Database "${created.value.database.name}" created`);
    emit("created", created.value);
  } catch (error) {
    errorMessage.value = describeDatabaseError(error);
  } finally {
    submitting.value = false;
  }
}

/** handleClose emits the visibility update; the watcher resets the form. */
function handleClose(value: boolean): void {
  emit("update:show", value);
}

// Entering the wizard loads the node list; closing resets the form.
watch(
  () => props.show,
  (visible) => {
    if (visible) {
      void serversStore.fetchServers().catch(() => undefined);
    } else {
      resetWizard();
    }
  },
);

// Switching engine resets the version pick to the engine default.
watch(
  () => form.engine,
  () => {
    form.version = "";
  },
);
</script>

<template>
  <NModal
    :show="show"
    preset="card"
    title="Create database"
    style="width: 640px; max-width: 94vw"
    :mask-closable="false"
    @update:show="handleClose"
  >
    <NSpace vertical :size="16">
      <NText depth="3">
        Step {{ step + 1 }} of {{ stepNames.length }} · {{ stepNames[step] }}.
        Each database is a container with its own volume on one node; the
        credentials are generated server-side and stored encrypted.
      </NText>

      <NAlert v-if="errorMessage" type="error" :show-icon="true">
        {{ errorMessage }}
      </NAlert>

      <template v-if="step === 0">
        <NFormItem label="Engine" :show-feedback="false">
          <NRadioGroup v-model:value="form.engine">
            <NSpace vertical :size="8">
              <NRadio
                v-for="engine in ENGINES"
                :key="engine.value"
                :value="engine.value"
              >
                {{ engine.label }}
                <NText depth="3" class="mono">
                  · {{ engine.repo }}:{{ engine.defaultVersion }} · port
                  {{ engine.port }}
                </NText>
              </NRadio>
            </NSpace>
          </NRadioGroup>
        </NFormItem>
        <NFormItem label="Version" :show-feedback="false">
          <NSelect
            v-model:value="form.version"
            :options="versionOptions"
            :placeholder="`Default: ${selectedEngine.defaultVersion}`"
            clearable
          />
        </NFormItem>
        <NFormItem
          label="Node"
          feedback="The container and its volume live on this node."
        >
          <NSelect
            v-model:value="form.serverId"
            :options="serverOptions"
            placeholder="Select a node"
            :loading="serversStore.loading"
          />
        </NFormItem>
      </template>

      <template v-else-if="step === 1">
        <NFormItem
          label="Name"
          :feedback="
            form.name === '' || NAME_PATTERN.test(form.name.trim())
              ? 'Used for the container and the credentials; 1-63 chars: letters, digits, ., _ or -.'
              : 'Name must be 1-63 characters of letters, digits, ., _ or -.'
          "
          :validation-status="
            form.name === '' || NAME_PATTERN.test(form.name.trim())
              ? undefined
              : 'error'
          "
        >
          <NInput
            v-model:value="form.name"
            class="mono"
            placeholder="pg-orders"
          />
        </NFormItem>
        <NFormItem :show-feedback="false">
          <NSpace align="center" :size="12">
            <NSwitch v-model:value="form.exposePublic" />
            <NText>Expose a public port</NText>
          </NSpace>
        </NFormItem>
        <NAlert v-if="form.exposePublic" type="warning" :show-icon="true">
          A public port is an attack surface and cannot change later — Docker
          port bindings are fixed at creation. Leave it off unless an external
          client requires it.
        </NAlert>
        <NFormItem
          v-if="form.exposePublic"
          label="Public port"
          feedback="Host port forwarding to the engine port."
        >
          <NInputNumber
            v-model:value="form.publicPort"
            :min="1"
            :max="65535"
            placeholder="e.g. 15432"
            style="width: 100%"
          />
        </NFormItem>
      </template>

      <template v-else>
        <div v-if="created === null">
          <NText>Review the database before creating it:</NText>
          <ul class="review-list mono">
            <li>Engine / image · {{ imagePreview }}</li>
            <li>
              Node ·
              {{
                serverOptions.find((item) => item.value === form.serverId)
                  ?.label ?? form.serverId
              }}
            </li>
            <li>Name · {{ form.name.trim() }}</li>
            <li>
              Public port ·
              {{
                form.exposePublic
                  ? `${form.publicPort} → ${selectedEngine.port}`
                  : `off · internal network only`
              }}
            </li>
            <li>Volume · kept 7 days after deletion</li>
          </ul>
        </div>
        <div v-else class="success-panel">
          <NAlert type="success" :show-icon="true" title="Database created">
            Container provisioning started on the node. Save these credentials
            — they stay available on the database detail page.
          </NAlert>
          <div
            v-for="row in credentialRows(created.credentials)"
            :key="row.label"
            class="credential-row"
          >
            <NText depth="3">{{ row.label }}</NText>
            <NText class="mono">{{ row.value }}</NText>
            <NButton
              size="small"
              secondary
              @click="() => void copyText(row.value, row.label)"
            >
              Copy
            </NButton>
          </div>
        </div>
      </template>

      <NSpace justify="end" :size="8">
        <NButton :disabled="step === 0 || submitting" @click="goBack">
          Back
        </NButton>
        <NButton
          v-if="created === null"
          type="primary"
          :disabled="!canContinue"
          :loading="submitting"
          @click="goNext"
        >
          {{ step === stepNames.length - 1 ? "Create database" : "Continue" }}
        </NButton>
        <NButton v-else type="primary" @click="handleClose(false)">
          Done
        </NButton>
      </NSpace>
    </NSpace>
  </NModal>
</template>

<style scoped>
.mono {
  font-family: var(--font-mono);
}

.review-list {
  list-style: none;
  margin: var(--space-3) 0 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  color: var(--muted);
}

.success-panel {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}

.credential-row {
  display: flex;
  align-items: center;
  gap: var(--space-3);
}

.credential-row .mono {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
}
</style>
