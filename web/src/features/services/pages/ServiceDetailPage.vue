<script setup lang="ts">
import {
  NAlert,
  NButton,
  NCard,
  NDataTable,
  NDescriptions,
  NDescriptionsItem,
  NEmpty,
  NInput,
  NPopconfirm,
  NSpace,
  NTag,
  NText,
  useMessage,
} from "naive-ui";
import type { DataTableColumns } from "naive-ui";
import { computed, h, onMounted, ref, watch } from "vue";
import type { VNode } from "vue";
import { RouterLink, useRoute, useRouter } from "vue-router";

import {
  deployStateTagType,
  describeServiceError,
  listServiceContainers,
  serviceStatusTagType,
} from "@/features/services/api/services";
import type {
  ComposeServiceContainer,
  ServiceDeploy,
} from "@/features/services/api/services";
import { isApiError } from "@/features/servers";
import ComposeEditor from "@/features/services/components/ComposeEditor.vue";
import ServiceLogs from "@/features/services/components/ServiceLogs.vue";
import { useServersStore } from "@/features/servers";
import { useServicesStore } from "@/features/services/stores/services";
import { relativeTime } from "@/shared/utils/format";

/**
 * One compose service: the stored document (view/edit → PATCH), its
 * environment, the node's containers and logs, the lifecycle actions and the
 * deploy history.
 *
 * Containers and logs are loaded on demand: both dial the node agent, which
 * answers 502 when no agent is connected, so a page render never probes the
 * node on its own. The per-deploy step timeline has no API and is rendered as
 * an explicit backend-pending stub.
 */

/**
 * One editable environment row. Values are masked (see the template note).
 * Rows carry a stable id so add/remove keeps focus and input state attached to
 * the row it belongs to instead of its position.
 */
interface EnvRow {
  id: number;
  key: string;
  value: string;
}

const route = useRoute();
const router = useRouter();
const message = useMessage();
const servicesStore = useServicesStore();
const serversStore = useServersStore();

/** envReference is the compose `${VAR}` substitution form shown in copy. */
const envReference = "${VAR}";

/** Monotonic env-row id for stable list keys. */
let nextEnvRowId = 0;

const serviceId = computed<string>(() => String(route.params.id ?? ""));

const error = ref<string | null>(null);
const notFound = ref(false);

const composeYaml = ref("");
const composeEditing = ref(false);
const composeSaving = ref(false);
const composeError = ref<string | null>(null);

const envDraft = ref<EnvRow[]>([]);
const envSaving = ref(false);
const envError = ref<string | null>(null);

const containers = ref<ComposeServiceContainer[]>([]);
const containersLoading = ref(false);
const containersError = ref<string | null>(null);
const containersLoaded = ref(false);

const busy = ref<string | null>(null);
const actionError = ref<string | null>(null);

const service = computed(() => servicesStore.serviceOf(serviceId.value));
const deploys = computed<ServiceDeploy[]>(() =>
  servicesStore.deploysOf(serviceId.value),
);
const latestDeploy = computed<ServiceDeploy | null>(() => deploys.value[0] ?? null);

/**
 * loadedServiceId is the service whose configuration is in the editors right
 * now. It is set only by a successful detail read for the id the load started
 * with, so a pending or failed load can never leave another service's drafts
 * editable — the compose/env saves are gated on it.
 */
const loadedServiceId = ref<string>("");

/**
 * loadToken invalidates obsolete detail loads: a newer load (route change,
 * reload) bumps it, and a completion whose token no longer matches is dropped
 * before it can touch a page-local draft.
 */
let loadToken = 0;

/** canEditCurrent reports whether the drafts belong to the displayed service. */
const canEditCurrent = computed<boolean>(
  () => loadedServiceId.value !== "" && loadedServiceId.value === serviceId.value,
);

/**
 * history is the per-service deploy-history state. A missing entry means the
 * history was never read: the UI must not claim it is empty then, and only
 * `loaded` (set by a successful read) allows the empty/zero copy.
 */
const history = computed(() => servicesStore.historyOf(serviceId.value));
const historyLoaded = computed<boolean>(() => history.value?.loaded ?? false);
const historyUnavailable = computed<string | null>(() => history.value?.error ?? null);

const serverName = computed<string>(() => {
  const current = service.value;
  if (!current) {
    return "—";
  }
  const server = serversStore.servers.find((item) => item.id === current.server_id);
  return server ? server.name : "unknown node";
});

/**
 * loggableServices lists the compose service selectors the UI knows about:
 * the routed services declared by the stored document, plus the project's
 * container services once they were read from the node.
 */
const loggableServices = computed<string[]>(() => {
  const names = new Set<string>();
  for (const route of service.value?.domains ?? []) {
    names.add(route.service);
  }
  for (const container of containers.value) {
    names.add(container.service);
  }
  return [...names].sort();
});

/** durationText renders created→finished (or created→now) as a short span. */
function durationText(deploy: ServiceDeploy): string {
  const start = new Date(deploy.created_at).getTime();
  const end = deploy.finished_at
    ? new Date(deploy.finished_at).getTime()
    : Date.now();
  if (Number.isNaN(start) || Number.isNaN(end) || end < start) {
    return "—";
  }
  const seconds = Math.round((end - start) / 1000);
  if (seconds < 60) {
    return `${seconds}s`;
  }
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) {
    return `${minutes}m${seconds % 60}s`;
  }
  return `${Math.floor(minutes / 60)}h${minutes % 60}m`;
}

/** stateCell renders one deploy state as a tag. */
function stateCell(deploy: ServiceDeploy): VNode {
  return h(
    NTag,
    { size: "small", type: deployStateTagType(deploy.state) },
    { default: () => deploy.state },
  );
}

/** errorCell renders the redacted failure message, or an em dash. */
function errorCell(deploy: ServiceDeploy): VNode {
  if (!deploy.error) {
    return h(NText, { depth: 3 }, { default: () => "—" });
  }
  return h("span", { class: "mono error-text" }, deploy.error);
}

const deployColumns: DataTableColumns<ServiceDeploy> = [
  {
    title: "Deploy",
    key: "id",
    width: 110,
    render: (row) => h("span", { class: "mono" }, row.id.slice(0, 8)),
  },
  {
    title: "State",
    key: "state",
    width: 120,
    render: (row) => stateCell(row),
  },
  {
    title: "Error",
    key: "error",
    minWidth: 200,
    ellipsis: { tooltip: true },
    render: (row) => errorCell(row),
  },
  {
    title: "Duration",
    key: "duration",
    width: 100,
    render: (row) => h("span", { class: "num" }, durationText(row)),
  },
  {
    title: "Created",
    key: "created_at",
    width: 120,
    render: (row) => relativeTime(row.created_at),
  },
  {
    title: "Finished",
    key: "finished_at",
    width: 120,
    render: (row) => relativeTime(row.finished_at),
  },
];

/** deployRowKey identifies a history row by its deploy id. */
function deployRowKey(row: ServiceDeploy): string {
  return row.id;
}

/** containerRowKey identifies a container row by its container id. */
function containerRowKey(row: ComposeServiceContainer): string {
  return row.container_id;
}

const containerColumns: DataTableColumns<ComposeServiceContainer> = [
  { title: "Service", key: "service", width: 140 },
  { title: "Container", key: "name", minWidth: 220 },
  {
    title: "Image",
    key: "image",
    minWidth: 200,
    ellipsis: { tooltip: true },
  },
  { title: "State", key: "state", width: 110 },
  { title: "Status", key: "status", width: 160 },
  { title: "Health", key: "health", width: 110 },
];

/** envRows converts the API's environment map into editable rows. */
function envRows(env: Record<string, string>): EnvRow[] {
  return Object.entries(env).map(([key, value]) => ({
    id: ++nextEnvRowId,
    key,
    value,
  }));
}

/** load fetches one service, its history and the node list. */
async function load(): Promise<void> {
  const token = ++loadToken;
  // Capture the id this load belongs to: the route may change while it awaits,
  // and an obsolete completion must never write another service's drafts.
  const id = serviceId.value;
  loadedServiceId.value = "";
  error.value = null;
  notFound.value = false;
  actionError.value = null;
  composeError.value = null;
  composeEditing.value = false;
  envError.value = null;
  containers.value = [];
  containersError.value = null;
  containersLoaded.value = false;
  composeYaml.value = "";
  envDraft.value = [];
  try {
    const fetched = await servicesStore.fetchService(id);
    if (token !== loadToken) {
      return;
    }
    composeYaml.value = fetched.compose_yaml ?? "";
    envDraft.value = envRows(fetched.env ?? {});
    loadedServiceId.value = id;
  } catch (err) {
    if (token !== loadToken) {
      return;
    }
    error.value = describeServiceError(err);
    notFound.value = isApiError(err) && err.status === 404;
    return;
  }
  // Dependent reads use the captured id: they belong to the service this load
  // started for, never to whatever the route shows now.
  await servicesStore.fetchDeploys(id).catch(() => undefined);
  void serversStore.fetchServers().catch(() => undefined);
}

/** retryHistory re-reads the deploy history after an unavailable result. */
async function retryHistory(): Promise<void> {
  await servicesStore.fetchDeploys(serviceId.value).catch(() => undefined);
}

/** handleSaveCompose persists the edited document (PATCH → 200). */
async function handleSaveCompose(text: string): Promise<void> {
  // A save may only fire for the service whose configuration is displayed: the
  // drafts are cleared while a load is pending, so an unguarded save could
  // write an empty or foreign document onto the route's service.
  const id = serviceId.value;
  if (!canEditCurrent.value || id === "") {
    return;
  }
  const token = loadToken;
  composeSaving.value = true;
  composeError.value = null;
  try {
    const updated = await servicesStore.update(id, { compose_yaml: text });
    if (token !== loadToken || id !== serviceId.value) {
      // The route moved on while the write was in flight; the response belongs
      // to the previous service and must not seed the new drafts.
      return;
    }
    composeYaml.value = updated.compose_yaml ?? text;
    composeEditing.value = false;
    message.success("Compose document saved. Deploy to apply it on the node.");
  } catch (err) {
    if (token === loadToken && id === serviceId.value) {
      composeError.value = describeServiceError(err);
    }
  } finally {
    composeSaving.value = false;
  }
}

/** addEnvRow appends an empty variable row. */
function addEnvRow(): void {
  envDraft.value = [
    ...envDraft.value,
    { id: ++nextEnvRowId, key: "", value: "" },
  ];
}

/** updateEnvRow replaces one row immutably, addressed by its stable id. */
function updateEnvRow(id: number, patch: Partial<EnvRow>): void {
  envDraft.value = envDraft.value.map((row) =>
    row.id === id ? { ...row, ...patch } : row,
  );
}

/** removeEnvRow drops one row, addressed by its stable id. */
function removeEnvRow(id: number): void {
  envDraft.value = envDraft.value.filter((row) => row.id !== id);
}

/** handleSaveEnv replaces the whole environment map (PATCH → 200). */
async function handleSaveEnv(): Promise<void> {
  // Same draft-ownership guard as the compose save.
  const id = serviceId.value;
  if (!canEditCurrent.value || id === "") {
    return;
  }
  const env: Record<string, string> = {};
  for (const row of envDraft.value) {
    const key = row.key.trim();
    if (key !== "") {
      env[key] = row.value;
    }
  }
  const token = loadToken;
  envSaving.value = true;
  envError.value = null;
  try {
    const updated = await servicesStore.update(id, { env });
    if (token !== loadToken || id !== serviceId.value) {
      return;
    }
    envDraft.value = envRows(updated.env ?? {});
    message.success("Environment saved. It applies to the next deploy.");
  } catch (err) {
    if (token === loadToken && id === serviceId.value) {
      envError.value = describeServiceError(err);
    }
  } finally {
    envSaving.value = false;
  }
}

/** loadContainers reads the project's containers from the node on demand. */
async function loadContainers(): Promise<void> {
  containersLoading.value = true;
  containersError.value = null;
  try {
    containers.value = await listServiceContainers(serviceId.value);
    containersLoaded.value = true;
  } catch (err) {
    containers.value = [];
    containersError.value = describeServiceError(err);
  } finally {
    containersLoading.value = false;
  }
}

/** handleDeploy renders the stored document and starts the project. */
async function handleDeploy(): Promise<void> {
  busy.value = "deploy";
  actionError.value = null;
  try {
    const outcome = await servicesStore.deploy(serviceId.value);
    message.success(`Deploy ${outcome.deploy.state}.`);
  } catch (err) {
    actionError.value = describeServiceError(err);
  } finally {
    busy.value = null;
  }
}

/** handleStop takes the project down; named volumes keep their data. */
async function handleStop(): Promise<void> {
  busy.value = "stop";
  actionError.value = null;
  try {
    await servicesStore.stop(serviceId.value);
    message.success("Service stopped. Named volumes were kept.");
  } catch (err) {
    actionError.value = describeServiceError(err);
  } finally {
    busy.value = null;
  }
}

/** handleRestart restarts a running project, or starts a stopped one. */
async function handleRestart(): Promise<void> {
  busy.value = "restart";
  actionError.value = null;
  try {
    await servicesStore.restart(serviceId.value);
    message.success("Restart sent to the node agent.");
  } catch (err) {
    actionError.value = describeServiceError(err);
  } finally {
    busy.value = null;
  }
}

/** handleDelete stops the project and removes the row. */
async function handleDelete(): Promise<void> {
  busy.value = "delete";
  actionError.value = null;
  try {
    await servicesStore.remove(serviceId.value);
    message.success("Service deleted. Named volumes were kept on the node.");
    await router.push({ name: "services" });
  } catch (err) {
    actionError.value = describeServiceError(err);
  } finally {
    busy.value = null;
  }
}

onMounted(() => {
  void load();
});

// The detail route is reused when navigating between services: reload when the
// id changes so the page never shows the previous service's data.
watch(serviceId, () => {
  void load();
});
</script>

<template>
  <div class="service-detail-page">
    <div class="page-head">
      <div class="page-head__title">
        <RouterLink :to="{ name: 'services' }" class="back">
          ← Services
        </RouterLink>
        <h1 class="mono">{{ service?.name ?? serviceId.slice(0, 8) }}</h1>
        <NSpace :size="8" align="center">
          <NTag
            v-if="service"
            size="small"
            :type="serviceStatusTagType(service.status)"
          >
            {{ service.status }}
          </NTag>
          <NTag v-if="service" size="small">{{ serverName }}</NTag>
          <NTag
            v-for="route in service?.domains ?? []"
            :key="route.domain"
            size="small"
            class="mono"
          >
            {{ route.domain }}:{{ route.port }}
          </NTag>
          <NTag v-if="service" size="small" class="mono">
            {{ service.project_name }}
          </NTag>
        </NSpace>
      </div>
      <div class="page-actions">
        <NButton
          v-if="service"
          type="primary"
          :loading="busy === 'deploy'"
          :disabled="busy !== null"
          @click="handleDeploy"
        >
          Deploy
        </NButton>
        <NButton
          v-if="service"
          :loading="busy === 'restart'"
          :disabled="busy !== null"
          @click="handleRestart"
        >
          Restart
        </NButton>
        <NButton
          v-if="service"
          :loading="busy === 'stop'"
          :disabled="busy !== null"
          @click="handleStop"
        >
          Stop
        </NButton>
        <NPopconfirm
          v-if="service"
          :positive-button-props="{ type: 'error' }"
          @positive-click="handleDelete"
        >
          <template #trigger>
            <NButton type="error" ghost :disabled="busy !== null">Delete</NButton>
          </template>
          Delete the service {{ service.name }}? The project goes down; its
          named volumes stay on the node.
        </NPopconfirm>
      </div>
    </div>

    <NAlert v-if="error" type="error" :show-icon="true">
      {{ error }}
    </NAlert>

    <NEmpty
      v-if="notFound"
      description="This service does not exist (or belongs to another account)."
    >
      <template #extra>
        <RouterLink :to="{ name: 'services' }">
          <NButton>Back to services</NButton>
        </RouterLink>
      </template>
    </NEmpty>

    <template v-else-if="service">
      <NAlert v-if="actionError" type="error" :show-icon="true">
        {{ actionError }}
      </NAlert>

      <NCard title="Overview">
        <NDescriptions :column="2" label-placement="top" bordered size="small">
          <NDescriptionsItem label="Node">{{ serverName }}</NDescriptionsItem>
          <NDescriptionsItem label="Status">
            {{ service.status }}
          </NDescriptionsItem>
          <NDescriptionsItem label="Compose project">
            <span class="mono">{{ service.project_name }}</span>
          </NDescriptionsItem>
          <NDescriptionsItem label="Service id">
            <span class="mono">{{ service.id }}</span>
          </NDescriptionsItem>
          <NDescriptionsItem label="Created">
            {{ relativeTime(service.created_at) }}
          </NDescriptionsItem>
          <NDescriptionsItem label="Updated">
            {{ relativeTime(service.updated_at) }}
          </NDescriptionsItem>
          <NDescriptionsItem label="Domains" :span="2">
            <template v-if="service.domains.length > 0">
              <span
                v-for="route in service.domains"
                :key="route.domain"
                class="mono domain-chip"
              >
                {{ route.service }} → {{ route.domain }}:{{ route.port }}
              </span>
            </template>
            <NText v-else depth="3">
              No routed domain. A compose service is routed by the
              <span class="mono">gotham.domain</span> label.
            </NText>
          </NDescriptionsItem>
        </NDescriptions>

        <div class="stub">
          <h4>Deploy step timeline — backend pending</h4>
          <p>
            The API records one row per deploy (state, error, timestamps) and
            exposes no per-step progress, so the history below is the whole
            picture. Service TLS/certificate status is out of scope for this
            page.
          </p>
        </div>
      </NCard>

      <NCard title="compose.yaml">
        <template #header-extra>
          <NText depth="3" class="small">
            saving creates a new version; the running project switches on deploy
          </NText>
        </template>
        <NSpace vertical :size="12">
          <NAlert v-if="composeError" type="error" :show-icon="true">
            {{ composeError }}
          </NAlert>
          <ComposeEditor
            v-if="canEditCurrent"
            v-model="composeYaml"
            v-model:editing="composeEditing"
            :saving="composeSaving"
            :label="`compose_yaml · ${service.name}`"
            @save="handleSaveCompose"
          />
          <NText v-else depth="3" class="small">
            {{
              error
                ? "The compose document is unavailable."
                : "Loading the service configuration…"
            }}
          </NText>
        </NSpace>
      </NCard>

      <NCard title="Environment">
        <template #header-extra>
          <NText depth="3" class="small">
            <span class="mono">{{ envReference }}</span> substitution input
          </NText>
        </template>
        <NSpace vertical :size="12">
          <NAlert v-if="envError" type="error" :show-icon="true">
            {{ envError }}
          </NAlert>
          <template v-if="canEditCurrent">
            <p class="small muted">
              Values are masked here: a service created from a template keeps
              its secret values in this map, and they must never be displayed in
              clear. The control plane redacts every value from errors and from
              the deploy history.
            </p>
            <div v-if="envDraft.length > 0" class="env-rows">
              <div v-for="row in envDraft" :key="row.id" class="env-row">
                <NInput
                  :value="row.key"
                  class="mono"
                  placeholder="MYSQL_PASSWORD"
                  aria-label="Variable name"
                  @update:value="(value: string) => updateEnvRow(row.id, { key: value })"
                />
                <NInput
                  :value="row.value"
                  type="password"
                  show-password-on="click"
                  :input-props="{ autocomplete: 'new-password' }"
                  class="mono"
                  placeholder="value"
                  aria-label="Variable value"
                  @update:value="(value: string) => updateEnvRow(row.id, { value })"
                />
                <NButton
                  quaternary
                  type="error"
                  aria-label="Remove variable"
                  @click="removeEnvRow(row.id)"
                >
                  Remove
                </NButton>
              </div>
            </div>
            <NText v-else depth="3">
              The environment is empty. Saving an empty environment clears it.
            </NText>
            <NSpace :size="8" align="center">
              <NButton size="small" @click="addEnvRow">Add variable</NButton>
              <NButton
                size="small"
                type="primary"
                :loading="envSaving"
                @click="handleSaveEnv"
              >
                Save environment
              </NButton>
            </NSpace>
          </template>
          <NText v-else depth="3" class="small">
            {{
              error
                ? "The environment is unavailable."
                : "Loading the environment…"
            }}
          </NText>
        </NSpace>
      </NCard>

      <NCard title="Containers">
        <template #header-extra>
          <NSpace :size="8" align="center">
            <NText depth="3" class="small">observed from the node agent</NText>
            <NButton size="small" :loading="containersLoading" @click="loadContainers">
              Refresh containers
            </NButton>
          </NSpace>
        </template>
        <NSpace vertical :size="12">
          <NAlert v-if="containersError" type="warning" :show-icon="true">
            {{ containersError }}
          </NAlert>
          <NDataTable
            v-if="containers.length > 0"
            :data="containers"
            :columns="containerColumns"
            :row-key="containerRowKey"
            :bordered="false"
            :scroll-x="1000"
            size="small"
          />
          <NEmpty
            v-else-if="!containersLoading"
            description="No container list loaded."
          >
            <template #extra>
              <p class="empty-hint">
                Reading the project's containers dials the node agent, so it
                happens on demand. Without a connected agent the API answers
                502.
                <template v-if="containersLoaded">
                  The last read returned no containers for this project.
                </template>
              </p>
            </template>
          </NEmpty>
        </NSpace>
      </NCard>

      <NCard title="Logs">
        <template #header-extra>
          <NText depth="3" class="small">
            docker compose logs -f via the node agent
          </NText>
        </template>
        <ServiceLogs
          :service-id="serviceId"
          :services="loggableServices"
          :title="service.name"
        />
      </NCard>

      <NCard title="Deploy history">
        <template #header-extra>
          <NText v-if="historyLoaded" depth="3" class="small">
            {{ deploys.length }} attempts · newest first
          </NText>
          <NText v-else-if="historyUnavailable" depth="3" class="small">
            unavailable
          </NText>
        </template>
        <NAlert
          v-if="historyUnavailable"
          type="warning"
          :show-icon="true"
          data-testid="history-unavailable"
        >
          <div class="history-error">
            <span>
              Deploy history unavailable: {{ historyUnavailable }}
              <template v-if="deploys.length > 0">
                — showing the last successful read.
              </template>
            </span>
            <NButton
              size="small"
              :loading="history?.loading ?? false"
              @click="retryHistory"
            >
              Retry
            </NButton>
          </div>
        </NAlert>
        <NDataTable
          v-if="deploys.length > 0"
          :data="deploys"
          :columns="deployColumns"
          :row-key="deployRowKey"
          :bordered="false"
          :scroll-x="900"
        />
        <NEmpty
          v-else-if="historyLoaded"
          description="No deploys yet."
          data-testid="history-empty"
        >
          <template #extra>
            <p class="empty-hint">
              Deploy renders the stored document on the node and records the
              attempt here. The rendered snapshot of each deploy is what a
              rollback would redeploy.
            </p>
          </template>
        </NEmpty>
        <NEmpty
          v-else-if="history?.loading"
          description="Reading deploy history…"
        />
        <NEmpty
          v-else
          description="Deploy history not loaded."
        />
        <template #footer>
          <NText depth="3" class="small">
            <template v-if="latestDeploy">
              Newest attempt:
              <span class="mono">{{ latestDeploy.id.slice(0, 8) }}</span> ·
              {{ latestDeploy.state }} · {{ relativeTime(latestDeploy.created_at) }}.
            </template>
            <template v-else-if="historyLoaded">
              Nothing has been deployed yet.
            </template>
            <template v-else-if="historyUnavailable">
              The history could not be read, so nothing is claimed about earlier
              attempts.
            </template>
            <template v-else>
              Reading the history…
            </template>
            The API does not expose a per-deploy log or a step timeline; logs
            stream from the running project instead.
          </NText>
        </template>
      </NCard>
    </template>
  </div>
</template>

<style scoped>
.service-detail-page {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
}

.page-head {
  display: flex;
  align-items: flex-start;
  gap: var(--space-4);
  flex-wrap: wrap;
}

.page-head__title {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  min-width: 0;
}

.back {
  font-size: var(--text-xs);
  color: var(--muted);
}

.page-head h1 {
  margin: 0;
  font-size: var(--text-2xl);
  line-height: 1.25;
  color: var(--fg-2);
  word-break: break-all;
}

.page-actions {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex-wrap: wrap;
}

.domain-chip {
  display: inline-block;
  margin-right: var(--space-3);
}

.stub {
  margin-top: var(--space-4);
  border-left: 4px solid var(--warn);
  background: var(--surface);
  border-radius: var(--radius-sm);
  padding: var(--space-2) var(--space-3);
}

.stub h4 {
  margin: 0;
  font-size: var(--text-xs);
  color: var(--fg-2);
}

.stub p {
  margin: var(--space-1) 0 0;
  font-size: var(--text-xs);
  color: var(--muted);
  max-width: 80ch;
}

.history-error {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  flex-wrap: wrap;
}

.env-rows {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.env-row {
  display: grid;
  grid-template-columns: minmax(0, 220px) minmax(0, 1fr) auto;
  gap: var(--space-2);
  align-items: center;
}

.small {
  font-size: var(--text-xs);
}

.muted {
  color: var(--muted);
}

.mono {
  font-family: var(--font-mono);
}

.empty-hint {
  color: var(--muted);
  font-size: var(--text-sm);
  margin: 0;
  max-width: 70ch;
}

@media (max-width: 860px) {
  .page-actions {
    margin-left: 0;
    width: 100%;
  }

  .env-row {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
