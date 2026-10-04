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
  NText,
  useMessage,
} from "naive-ui";
import { computed, reactive, ref, watch } from "vue";

import {
  createApplication,
  describeApplicationError,
} from "../api/applications";
import type {
  Application,
  BuildPack,
  CreateApplicationInput,
  EnvVar,
  StorageMapping,
} from "../api/applications";
import { useProvidersStore } from "../stores/providers";
import type { ProviderRepo } from "../api/providers";
import { useServersStore } from "../stores/servers";
import { useApplicationsStore } from "../stores/applications";
import {
  countDroppedEnvRows,
  hasEnvKeyWarnings,
} from "../utils/wizardValidation";
import EnvEditor from "./EnvEditor.vue";
import StorageEditor from "./StorageEditor.vue";

interface Props {
  show: boolean;
}

interface WizardForm {
  providerId: string;
  publicCloneUrl: string;
  repoFullName: string;
  cloneUrl: string;
  branch: string;
  name: string;
  buildPack: BuildPack;
  serverId: string;
  port: number | null;
  hostPort: number | null;
  baseDomain: string;
  env: EnvVar[];
  storage: StorageMapping[];
}

const props = defineProps<Props>();
const emit = defineEmits<{
  "update:show": [value: boolean];
  created: [application: Application];
}>();

const providersStore = useProvidersStore();
const serversStore = useServersStore();
const appsStore = useApplicationsStore();
const message = useMessage();

const PUBLIC_PROVIDER = "public";

const stepNames = ["Source", "Build pack", "Runtime", "Env & storage", "Deploy"];

const NAME_PATTERN = /^[a-z][a-z0-9-]{2,30}$/;
const DOMAIN_PATTERN = /^[a-z0-9.-]+\.[a-z]{2,}$/;

const BUILD_PACKS: Array<{ value: BuildPack; label: string; hint: string }> = [
  {
    value: "",
    label: "Auto-detect (recommended)",
    hint: "The control plane picks Dockerfile, Railpack, Buildpacks or static from the repo layout.",
  },
  {
    value: "railpack",
    label: "Railpack",
    hint: "Language auto-detection without a Dockerfile.",
  },
  {
    value: "dockerfile",
    label: "Dockerfile",
    hint: "Uses the Dockerfile in the repo. Full control over the build.",
  },
  {
    value: "buildpacks",
    label: "Buildpacks",
    hint: "Heroku-style buildpacks for legacy apps.",
  },
  {
    value: "static",
    label: "Static",
    hint: "Builds a static directory and serves it via Traefik. No process runs.",
  },
];

const step = ref(0);
const submitting = ref(false);
const errorMessage = ref("");
const sourceError = ref("");

const form = reactive<WizardForm>({
  providerId: "",
  publicCloneUrl: "",
  repoFullName: "",
  cloneUrl: "",
  branch: "main",
  name: "",
  buildPack: "",
  serverId: "",
  port: 3000,
  hostPort: null,
  baseDomain: "",
  env: [{ key: "NODE_ENV", value: "production" }],
  storage: [],
});

const providerOptions = computed<Array<{ label: string; value: string }>>(() => [
  ...providersStore.providers.map((item) => ({
    label: `${item.provider} · ${item.connected ? "connected" : "not connected"}`,
    value: item.id,
  })),
  { label: "Public repository · paste URL", value: PUBLIC_PROVIDER },
]);

const isPublicRepo = computed<boolean>(() => form.providerId === PUBLIC_PROVIDER);

const repoOptions = computed<Array<{ label: string; value: string }>>(() =>
  providersStore.reposOf(form.providerId).map((repo) => ({
    label: `${repo.full_name}${repo.private ? " (private)" : ""}`,
    value: repo.full_name,
  })),
);

const serverOptions = computed<Array<{ label: string; value: string }>>(() =>
  serversStore.servers.map((server) => ({
    label: `${server.name} · ${server.ip}`,
    value: server.id,
  })),
);

/** selectedProviderName resolves the provider slug for the create payload. */
const selectedProviderName = computed<string>(() => {
  if (isPublicRepo.value) {
    return "public";
  }
  return (
    providersStore.providers.find((item) => item.id === form.providerId)
      ?.provider ?? ""
  );
});

/** sourceValid gates the Source step: provider, repo (or URL), branch, name. */
const sourceValid = computed<boolean>(() => {
  if (form.providerId === "") {
    return false;
  }
  if (isPublicRepo.value) {
    if (form.publicCloneUrl.trim() === "") {
      return false;
    }
  } else {
    if (form.repoFullName === "") {
      return false;
    }
    // A private repository without a provider ssh_url is rejected rather than
    // silently degraded to https: the keyed cloner would rewrite that URL and
    // drop a self-hosted SSH port. sourceError names the gap.
    if (form.cloneUrl.trim() === "") {
      return false;
    }
  }
  return form.branch.trim() !== "" && NAME_PATTERN.test(form.name.trim());
});

/** runtimeValid gates the Runtime step: a node and a valid port. */
const runtimeValid = computed<boolean>(() => {
  if (form.serverId === "") {
    return false;
  }
  if (form.port === null || !Number.isInteger(form.port) || form.port < 1 || form.port > 65535) {
    return false;
  }
  if (
    form.hostPort !== null &&
    (!Number.isInteger(form.hostPort) || form.hostPort < 0 || form.hostPort > 65535)
  ) {
    return false;
  }
  const domain = form.baseDomain.trim();
  return domain === "" || DOMAIN_PATTERN.test(domain);
});

/**
 * Environment names are warn-only: the API accepts any structurally valid
 * name, so the wizard must not block one it accepts. The alert names rows that
 * deviate from the convention; nameless rows are reported because the payload
 * drops them.
 */
const envKeyWarnings = computed<boolean>(() => hasEnvKeyWarnings(form.env));

const droppedEnvRows = computed<number>(() => countDroppedEnvRows(form.env));

const canContinue = computed<boolean>(() => {
  switch (step.value) {
    case 0:
      return sourceValid.value;
    case 2:
      return runtimeValid.value;
    default:
      return true;
  }
});

const buildPackLabel = computed<string>(
  () => BUILD_PACKS.find((item) => item.value === form.buildPack)?.label ?? "Auto-detect",
);

/** reviewSource renders the repo headline on the review step. */
const reviewSource = computed<string>(() => {
  const repo = isPublicRepo.value ? form.publicCloneUrl.trim() : form.repoFullName;
  return `${repo} · ${form.branch.trim()}`;
});

// Entering the wizard loads providers and nodes; changing provider loads repos.
watch(
  () => props.show,
  (visible) => {
    if (visible) {
      void providersStore.fetchProviders().catch(() => undefined);
      void serversStore.fetchServers().catch(() => undefined);
    } else {
      resetWizard();
    }
  },
);

watch(
  () => form.providerId,
  (providerId) => {
    form.repoFullName = "";
    form.cloneUrl = "";
    sourceError.value = "";
    if (providerId !== "" && providerId !== PUBLIC_PROVIDER) {
      void loadRepos();
    }
  },
);

/** loadRepos fetches the selected provider's repositories; the store exposes any error. */
async function loadRepos(): Promise<void> {
  if (form.providerId === "" || form.providerId === PUBLIC_PROVIDER) {
    return;
  }
  await providersStore.fetchRepos(form.providerId).catch(() => undefined);
}

/**
 * cloneUrlFor returns the clone URL stored for a provider repository
 * (BE-4.4b). A private repository is cloned with an SSH deploy key, so the
 * provider's own ssh_url — authoritative for the host and any non-default SSH
 * port — is stored. An empty ssh_url for a private repo returns "" and the
 * wizard rejects the step (sourceError): falling back to https would let the
 * keyed cloner rewrite it and silently drop the port. A public repository
 * keeps its https URL, which the cloner fetches anonymously.
 */
function cloneUrlFor(repo: ProviderRepo): string {
  const ssh = repo.ssh_url?.trim() ?? "";
  if (repo.private) {
    return ssh;
  }
  return repo.clone_url;
}

/** handleRepoSelect prefills branch, clone URL and a name from the repo. */
function handleRepoSelect(fullName: string): void {
  const repo = providersStore.reposOf(form.providerId).find((item) => item.full_name === fullName);
  if (!repo) {
    return;
  }
  form.cloneUrl = cloneUrlFor(repo);
  sourceError.value = "";
  if (repo.private && form.cloneUrl === "") {
    sourceError.value =
      "The provider reported no SSH URL for this private repository, so a deploy key cannot be used. Pick another repository or make the SSH URL available on the provider.";
  }
  if (repo.default_branch) {
    form.branch = repo.default_branch;
  }
  if (form.name.trim() === "") {
    form.name = repo.name.toLowerCase().replace(/[^a-z0-9-]+/g, "-").slice(0, 31);
  }
}

/** buildPayload assembles the create-application body from the wizard state. */
function buildPayload(): CreateApplicationInput {
  return {
    name: form.name.trim(),
    provider: selectedProviderName.value,
    repo: isPublicRepo.value ? form.publicCloneUrl.trim() : form.repoFullName,
    clone_url: isPublicRepo.value ? form.publicCloneUrl.trim() : form.cloneUrl,
    branch: form.branch.trim(),
    build_pack: form.buildPack,
    base_domain: form.baseDomain.trim(),
    port: form.port ?? 3000,
    host_port: form.hostPort ?? 0,
    server_id: form.serverId,
    env: form.env.filter((row) => row.key.trim() !== ""),
    storage: form.storage.filter((row) => row.name.trim() !== ""),
  };
}

/** handleSubmit posts the wizard payload, queues the first deploy and reports. */
async function handleSubmit(): Promise<void> {
  errorMessage.value = "";
  submitting.value = true;
  try {
    const { application, webhook } = await createApplication(buildPayload());
    // The create route only stores the row; "Create & deploy" must queue the
    // first deployment explicitly and surface whether it was queued.
    try {
      await appsStore.deploy(application.id);
      message.success(`Application "${application.name}" created and first deploy queued`);
    } catch (deployError) {
      message.warning(
        `Application "${application.name}" was created, but the first deploy could not be queued: ${describeApplicationError(
          deployError,
        )}`,
        { duration: 8000 },
      );
    }
    if (webhook && !webhook.installed) {
      message.warning(
        `Automatic deploys are off: ${
          webhook.error ?? "the provider hook could not be installed"
        }`,
        { duration: 8000 },
      );
    }
    emit("created", application);
    emit("update:show", false);
    resetWizard();
  } catch (error) {
    errorMessage.value = describeApplicationError(error);
  } finally {
    submitting.value = false;
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
  form.providerId = "";
  form.publicCloneUrl = "";
  form.repoFullName = "";
  form.cloneUrl = "";
  form.branch = "main";
  form.name = "";
  form.buildPack = "";
  form.serverId = "";
  form.port = 3000;
  form.hostPort = null;
  form.baseDomain = "";
  form.env = [{ key: "NODE_ENV", value: "production" }];
  form.storage = [];
  errorMessage.value = "";
  sourceError.value = "";
  submitting.value = false;
  // The provider store is a singleton: a stale repo error must not survive
  // into the next wizard with a Retry that no longer applies.
  providersStore.reposError = null;
}
</script>

<template>
  <NModal
    :show="props.show"
    preset="card"
    title="Create application"
    :mask-closable="false"
    class="wizard-modal"
    style="width: 880px; max-width: 96vw"
    @update:show="handleShowChange"
  >
    <NText depth="3">
      Pick a source, build pack, port and domain, then deploy. Source, build
      pack and runtime are fixed once created; environment variables, volumes
      and domains can be changed afterwards.
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
          The webhook is created automatically after the first deploy.
        </p>
      </div>

      <div class="wizard-main">
        <div class="wizard-body">
          <NAlert v-if="errorMessage" type="error" :show-icon="true">
            {{ errorMessage }}
          </NAlert>

          <!-- Step 1: source -->
          <NSpace v-if="step === 0" vertical :size="16">
            <NFormItem label="Provider" :show-feedback="true">
              <NSelect
                v-model:value="form.providerId"
                :options="providerOptions"
                :loading="providersStore.loading"
                placeholder="Select a connected provider"
              />
              <template #feedback>
                <span class="field-hint">Each provider uses its own OAuth app.</span>
              </template>
            </NFormItem>

            <NFormItem v-if="isPublicRepo" label="Clone URL">
              <NInput
                v-model:value="form.publicCloneUrl"
                class="mono"
                placeholder="https://github.com/owner/repo.git"
              />
              <template #feedback>
                <span class="field-hint">Any public repo — no provider connection needed.</span>
              </template>
            </NFormItem>

            <NFormItem v-else label="Repository">
              <NSelect
                v-model:value="form.repoFullName"
                :options="repoOptions"
                :loading="providersStore.reposLoading"
                :disabled="form.providerId === ''"
                placeholder="Select a repository"
                filterable
                @update:value="handleRepoSelect"
              />
              <template #feedback>
                <span class="field-hint">Private repos deploy with an SSH deploy key.</span>
              </template>
              <NAlert
                v-if="providersStore.reposError"
                type="error"
                :show-icon="true"
                style="margin-top: 8px"
              >
                <NSpace align="center" :size="12" wrap>
                  <span>{{ providersStore.reposError }}</span>
                  <NButton size="small" @click="void loadRepos()">Retry</NButton>
                </NSpace>
              </NAlert>
            </NFormItem>

            <NAlert v-if="sourceError" type="warning" :show-icon="true">
              {{ sourceError }}
            </NAlert>

            <NSpace :size="12">
              <NFormItem label="Branch" class="grow">
                <NInput v-model:value="form.branch" class="mono" placeholder="main" />
                <template #feedback>
                  <span class="field-hint">Branch listing is not exposed by the API yet — the default branch is prefilled.</span>
                </template>
              </NFormItem>
              <NFormItem label="Application name" class="grow">
                <NInput v-model:value="form.name" class="mono" placeholder="storefront" />
                <template #feedback>
                  <span class="field-hint">Lowercase, digits and dashes (3-31 chars). Used for the container and image tag.</span>
                </template>
              </NFormItem>
            </NSpace>
          </NSpace>

          <!-- Step 2: build pack -->
          <NSpace v-else-if="step === 1" vertical :size="12">
            <NText strong>Build pack</NText>
            <NText depth="3">
              Detection runs server-side at build time from the repo layout —
              the choice below is a hint, never a guess.
            </NText>
            <NRadioGroup v-model:value="form.buildPack">
              <NSpace vertical :size="8">
                <NRadio v-for="pack in BUILD_PACKS" :key="pack.label" :value="pack.value">
                  <NText strong>{{ pack.label }}</NText>
                  <br />
                  <NText depth="3">{{ pack.hint }}</NText>
                </NRadio>
              </NSpace>
            </NRadioGroup>
          </NSpace>

          <!-- Step 3: runtime -->
          <NSpace v-else-if="step === 2" vertical :size="16">
            <NFormItem label="Node">
              <NSelect
                v-model:value="form.serverId"
                :options="serverOptions"
                placeholder="Select the node that runs the container"
              />
              <template #feedback>
                <span class="field-hint">The agent builds the image on this node.</span>
              </template>
            </NFormItem>

            <NSpace :size="12">
              <NFormItem label="Internal port" class="grow">
                <NInputNumber
                  v-model:value="form.port"
                  :min="1"
                  :max="65535"
                  placeholder="3000"
                />
                <template #feedback>
                  <span class="field-hint">The port the app listens on inside the container.</span>
                </template>
              </NFormItem>
              <NFormItem label="Host port (0 = auto)" class="grow">
                <NInputNumber
                  v-model:value="form.hostPort"
                  :min="0"
                  :max="65535"
                  placeholder="0"
                />
                <template #feedback>
                  <span class="field-hint">Leave empty to let the control plane assign one.</span>
                </template>
              </NFormItem>
            </NSpace>

            <NFormItem label="Domain (optional)">
              <NInput
                v-model:value="form.baseDomain"
                class="mono"
                placeholder="app.gotham.dev"
              />
              <template #feedback>
                <span class="field-hint">Leave empty to reach the app by port first.</span>
              </template>
            </NFormItem>
          </NSpace>

          <!-- Step 4: env & storage -->
          <NSpace v-else-if="step === 3" vertical :size="12">
            <div>
              <NText strong>Environment variables</NText>
              <EnvEditor v-model="form.env" />
            </div>
            <div>
              <NText strong>Volumes</NText>
              <StorageEditor v-model="form.storage" />
            </div>
            <NAlert v-if="envKeyWarnings" type="warning" :show-icon="false">
              One or more variable names do not follow the usual
              ^[A-Z][A-Z0-9_]*$ convention. The API accepts them, so they are
              not blocked — but a non-standard name may not be injected as you
              expect.
            </NAlert>
            <NAlert v-if="droppedEnvRows > 0" type="warning" :show-icon="false">
              {{ droppedEnvRows }} variable row{{ droppedEnvRows === 1 ? "" : "s" }}
              without a name will be ignored on create.
            </NAlert>
          </NSpace>

          <!-- Step 5: review -->
          <NSpace v-else vertical :size="12">
            <NText strong>Summary</NText>
            <dl class="review">
              <dt>Application</dt>
              <dd class="mono">{{ form.name }}</dd>
              <dt>Source</dt>
              <dd class="mono">{{ reviewSource }}</dd>
              <dt>Build pack</dt>
              <dd>{{ buildPackLabel }}</dd>
              <dt>Port / domain</dt>
              <dd class="mono">{{ form.port ?? 3000 }} · {{ form.baseDomain || "—" }}</dd>
              <dt>Env / volumes</dt>
              <dd class="mono">{{ form.env.length }} vars · {{ form.storage.length }} volumes</dd>
            </dl>
            <NText depth="3">
              Creating posts the payload to the applications API, then the
              first deploy queues immediately.
            </NText>
            <NAlert v-if="droppedEnvRows > 0" type="warning" :show-icon="false">
              {{ droppedEnvRows }} nameless variable row{{ droppedEnvRows === 1 ? "" : "s" }}
              will be ignored on create.
            </NAlert>
          </NSpace>
        </div>

        <div class="wizard-foot">
          <NButton v-if="step > 0" tertiary @click="step -= 1">Back</NButton>
          <span class="step-counter">Step {{ step + 1 }} / 5</span>
          <span class="grow" />
          <template v-if="step < 4">
            <NButton @click="closeWizard">Cancel</NButton>
            <NButton type="primary" :disabled="!canContinue" @click="step += 1">
              Continue
            </NButton>
          </template>
          <template v-else>
            <NButton @click="closeWizard">Cancel</NButton>
            <NButton
              type="primary"
              :loading="submitting"
              :disabled="!sourceValid || !runtimeValid"
              @click="handleSubmit"
            >
              Create &amp; deploy
            </NButton>
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

.mono {
  font-family: var(--font-mono);
}

.review {
  display: grid;
  grid-template-columns: 140px minmax(0, 1fr);
  gap: var(--space-2) var(--space-3);
  margin: 0;
}

.review dt {
  color: var(--muted);
  font-size: var(--text-sm);
}

.review dd {
  margin: 0;
  color: var(--fg);
  font-size: var(--text-sm);
  word-break: break-word;
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

.grow {
  flex: 1;
  min-width: 0;
}

.wizard-modal :deep(.n-card-content) {
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
