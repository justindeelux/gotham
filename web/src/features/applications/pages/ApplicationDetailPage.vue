<script setup lang="ts">
import {
  NAlert,
  NAvatar,
  NButton,
  NCard,
  NDataTable,
  NDescriptions,
  NDescriptionsItem,
  NEmpty,
  NModal,
  NPopconfirm,
  NRadio,
  NRadioGroup,
  NSelect,
  NSpace,
  NSpin,
  NTabPane,
  NTabs,
  NTag,
  NText,
  NTooltip,
  useMessage,
} from "naive-ui";
import type { DataTableColumns } from "naive-ui";
import { computed, h, onMounted, onUnmounted, ref, watch } from "vue";
import type { VNode } from "vue";
import { RouterLink, useRoute } from "vue-router";

import { describeApplicationError, isActiveDeployment } from "@/features/applications/api/applications";
import type {
  Application,
  Deployment,
  EnvVar,
  StorageMapping,
} from "@/features/applications/api/applications";
import type { Preview } from "@/features/applications/api/previews";
import {
  describePreviewError,
  isFeatureDisabled as isPreviewsDisabled,
  listPreviews,
  previewStateTagType,
  previewURL,
} from "@/features/applications/api/previews";
import DeployLogs from "@/features/applications/components/DeployLogs.vue";
import DeploymentStatusTag from "@/features/applications/components/DeploymentStatusTag.vue";
import DomainEditor from "@/features/applications/components/DomainEditor.vue";
import EnvEditor from "@/features/applications/components/EnvEditor.vue";
import StorageEditor from "@/features/applications/components/StorageEditor.vue";
import { useMediaQuery } from "@/shared/composables/useMediaQuery";
import { useApplicationsStore } from "@/features/applications/stores/applications";
import { useServersStore } from "@/features/servers";
import { pipelineStepsFor } from "@/features/applications/utils/deployPipeline";
import { relativeTime } from "@/shared/utils/format";
import { createRequestGeneration } from "@/shared/utils/requestGeneration";

const route = useRoute();
const message = useMessage();
const appsStore = useApplicationsStore();
const serversStore = useServersStore();

const appId = computed<string>(() => String(route.params.id ?? ""));
const activeTab = ref("overview");
const logDeploymentId = ref<string>("");
const logServerId = ref<string>("");
const rollbackOpen = ref(false);
const rollbackTarget = ref<string>("");
const rollingBack = ref(false);
const envDraft = ref<EnvVar[]>([]);
const envLoading = ref(false);
const envError = ref<string | null>(null);
// `envLoadedFor` names the application whose environment actually loaded. Save
// is only enabled while it matches the current application, so an empty draft
// from a failed read (or a draft left over from another application) can never
// be written over unknown data.
const envLoadedFor = ref<string>("");
const storagesDraft = ref<StorageMapping[]>([]);
const storagesLoading = ref(false);
const storagesError = ref<string | null>(null);
const storagesLoadedFor = ref<string>("");

// Invalidates in-flight config reads when the route's application changes, so
// a late response cannot overwrite the new application's draft.
const draftGeneration = createRequestGeneration();

// Previews (FE-8.1). `previewsLoaded` is only set by a successful read, so an
// unavailable list can never render as a confirmed-empty one: a failure keeps
// the previously loaded rows and shows an explicit error with a retry, and a
// feature-flag 404 hides the whole tab instead of erroring.
const previews = ref<Preview[]>([]);
const previewsLoading = ref(false);
const previewsLoaded = ref(false);
const previewsError = ref<string | null>(null);
const previewsAvailable = ref(true);

/** isNarrow stacks the two-column descriptions on small screens. */
const isNarrow = useMediaQuery("(max-width: 640px)");

/** descColumns renders descriptions in one column below 640px. */
const descColumns = computed<number>(() => (isNarrow.value ? 1 : 2));

const deployments = computed<Deployment[]>(() => appsStore.deploymentsOf(appId.value));

const application = computed<Application | null>(() =>
  appsStore.applicationOf(appId.value),
);

/** displayName prefers the stored name, falling back to the id head. */
const displayName = computed<string>(() => application.value?.name ?? shortId.value);

/** hasContainer reports whether any deployment started a container. */
const hasContainer = computed<boolean>(() =>
  deployments.value.some((item) => item.container_id !== ""),
);

/**
 * controlHint explains why stop/start are unavailable, if they are: an
 * in-flight deployment owns the container right now, or no deployment ever
 * started one (the backend would answer 404).
 */
const controlHint = computed<string | null>(() => {
  if (active.value !== null) {
    return "A deployment is in progress. Wait for it to finish.";
  }
  if (!hasContainer.value) {
    return "No container to control yet. Deploy the application first.";
  }
  return null;
});

const latest = computed<Deployment | null>(() => appsStore.latestDeployment(appId.value));

const active = computed<Deployment | null>(() => appsStore.activeDeployment(appId.value));

/**
 * containerStopped tracks a stop/start the deployment row cannot express:
 * stopping leaves the deployment state "running" (the release still describes
 * the container), so the header tag and the Stop/Start buttons read this local
 * truth instead of `latest.state` alone (C4-19).
 */
const containerStopped = ref(false);

/**
 * stoppedForContainer names the container the local stop override applies to.
 * stopApp → refreshDeployments must not clear the override when the refresh
 * merely re-keys the row for the same container.
 */
const stoppedForContainer = ref<string>("");

/** containerIsRunning is the live view of the newest deployment's container. */
const containerIsRunning = computed<boolean>(
  () => latest.value?.state === "running" && !containerStopped.value,
);

/** shortId renders the head of the application UUID for the header. */
const shortId = computed<string>(() => appId.value.slice(0, 8));

/** initials derives a two-letter avatar from the application id. */
const initials = computed<string>(() => (shortId.value.slice(0, 2) || "AP").toUpperCase());

/** runningDeployments lists terminal running rows eligible for rollback. */
const runningDeployments = computed<Deployment[]>(() =>
  deployments.value.filter((item) => item.state === "running"),
);

/** logTarget resolves the deployment selected in the Logs tab. */
const logTarget = computed<Deployment | null>(
  () => deployments.value.find((item) => item.id === logDeploymentId.value) ?? active.value ?? latest.value,
);

/** effectiveLogServerId prefers the application's node, then the selection. */
const effectiveLogServerId = computed<string>(
  () =>
    logServerId.value ||
    application.value?.server_id ||
    serversStore.servers[0]?.id ||
    "",
);

const serverOptions = computed<Array<{ label: string; value: string }>>(() =>
  serversStore.servers.map((server) => ({
    label: `${server.name} · ${server.ip}`,
    value: server.id,
  })),
);

const deploymentOptions = computed<Array<{ label: string; value: string }>>(() =>
  deployments.value.map((item) => ({
    label: `${item.id.slice(0, 8)} · ${item.kind} · ${item.state}`,
    value: item.id,
  })),
);

/** pipelineSteps maps the latest deployment onto done/active/todo/failed. */
const pipelineSteps = computed(() => pipelineStepsFor(latest.value));

/** durationText renders started→finished (or started→now) as a short span. */
function durationText(deployment: Deployment): string {
  if (!deployment.started_at) {
    return "—";
  }
  const start = new Date(deployment.started_at).getTime();
  const end = deployment.finished_at ? new Date(deployment.finished_at).getTime() : Date.now();
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

/** errorText renders the deployment error, falling back to an em dash. */
function errorText(deployment: Deployment): VNode {
  if (!deployment.error) {
    return h(NText, { depth: 3 }, { default: () => "—" });
  }
  return h("span", { class: "mono error-text" }, deployment.error);
}

/** actionsCell renders per-row Logs / Rollback controls. */
function actionsCell(row: Deployment): VNode {
  const children: VNode[] = [
    h(
      NButton,
      {
        size: "small",
        quaternary: true,
        onClick: () => {
          logDeploymentId.value = row.id;
          activeTab.value = "logs";
        },
      },
      { default: () => "Logs" },
    ),
  ];
  if (row.state === "running") {
    children.push(
      h(
        NButton,
        {
          size: "small",
          onClick: () => {
            rollbackTarget.value = row.id;
            rollbackOpen.value = true;
          },
        },
        { default: () => "Rollback" },
      ),
    );
  }
  return h(NSpace, { size: 8, align: "center", wrap: false }, { default: () => children });
}

const columns: DataTableColumns<Deployment> = [
  {
    title: "Deploy",
    key: "id",
    width: 110,
    render: (row) => h("span", { class: "mono" }, row.id.slice(0, 8)),
  },
  {
    title: "Kind",
    key: "kind",
    width: 100,
    render: (row) => h("span", { class: "mono" }, row.kind),
  },
  {
    title: "Image",
    key: "image_tag",
    minWidth: 160,
    ellipsis: { tooltip: true },
    render: (row) =>
      row.image_tag
        ? h("span", { class: "mono" }, row.image_tag)
        : h(NText, { depth: 3 }, { default: () => "—" }),
  },
  {
    title: "Duration",
    key: "duration",
    width: 90,
    render: (row) => h("span", { class: "tnum" }, durationText(row)),
  },
  {
    title: "State",
    key: "state",
    width: 130,
    render: (row) => h(DeploymentStatusTag, { state: row.state }),
  },
  {
    title: "Error",
    key: "error",
    minWidth: 160,
    ellipsis: { tooltip: true },
    render: (row) => errorText(row),
  },
  {
    title: "Created",
    key: "created_at",
    width: 110,
    render: (row) => relativeTime(row.created_at),
  },
  {
    title: "Actions",
    key: "actions",
    width: 190,
    render: (row) => actionsCell(row),
  },
];

/** rowKey identifies a row by its deployment id. */
function rowKey(row: Deployment): string {
  return row.id;
}

/** Preview rows shown in the Previews tab, oldest closed last. */
const previewColumns: DataTableColumns<Preview> = [
  {
    title: "PR",
    key: "pr_number",
    width: 90,
    render: (row) => h("span", { class: "mono" }, `#${row.pr_number}`),
  },
  {
    title: "Branch",
    key: "branch",
    minWidth: 160,
    ellipsis: { tooltip: true },
    render: (row) =>
      row.branch
        ? h("span", { class: "mono" }, row.branch)
        : h(NText, { depth: 3 }, { default: () => "—" }),
  },
  {
    title: "Preview URL",
    key: "host",
    minWidth: 300,
    ellipsis: { tooltip: true },
    render: (row) =>
      row.host
        ? h(
            "a",
            {
              class: "mono",
              href: previewURL(row.host),
              target: "_blank",
              rel: "noopener noreferrer",
            },
            row.host,
          )
        : h(NText, { depth: 3 }, { default: () => "—" }),
  },
  {
    title: "State",
    key: "state",
    width: 120,
    render: (row) =>
      h(
        NTag,
        { type: previewStateTagType(row.state), size: "small", round: true },
        { default: () => row.state },
      ),
  },
  {
    title: "Head",
    key: "head_sha",
    width: 100,
    render: (row) =>
      row.head_sha
        ? h("span", { class: "mono" }, row.head_sha.slice(0, 8))
        : h(NText, { depth: 3 }, { default: () => "—" }),
  },
  {
    title: "Created",
    key: "created_at",
    width: 110,
    render: (row) => relativeTime(row.created_at),
  },
  {
    title: "Deleted",
    key: "deleted_at",
    width: 110,
    render: (row) =>
      row.deleted_at
        ? relativeTime(row.deleted_at)
        : h(NText, { depth: 3 }, { default: () => "—" }),
  },
];

/** previewRowKey identifies a preview row by its binding id. */
function previewRowKey(row: Preview): string {
  return row.id;
}

/** fetchAll loads the application, its history, config and node list. */
async function fetchAll(): Promise<void> {
  if (!appId.value) {
    return;
  }
  try {
    await appsStore.fetchApplication(appId.value);
  } catch {
    // The store already exposes the error; the alert renders it.
  }
  try {
    await appsStore.fetchDeployments(appId.value);
  } catch {
    // The store already exposes the error; the alert renders it.
  }
  void loadEnv();
  void loadStorages();
  void loadPreviews();
  void serversStore.fetchServers().catch(() => undefined);
}

/** loadEnv refreshes the environment draft shown in the editor. */
async function loadEnv(): Promise<void> {
  const target = appId.value;
  if (target === "") {
    return;
  }
  const token = draftGeneration.current();
  envLoading.value = true;
  envError.value = null;
  try {
    const env = await appsStore.fetchEnv(target);
    if (!draftGeneration.isCurrent(token) || target !== appId.value) {
      return; // superseded by an application switch
    }
    envDraft.value = [...env];
    envLoadedFor.value = target;
  } catch (error) {
    if (!draftGeneration.isCurrent(token) || target !== appId.value) {
      return;
    }
    // Never present a failed read as an empty collection: keep the draft in an
    // error state and clear envLoadedFor so Save stays disabled until a
    // successful read. An unknown server state is never overwritten.
    envError.value = describeApplicationError(error);
    envLoadedFor.value = "";
  } finally {
    if (draftGeneration.isCurrent(token) && target === appId.value) {
      envLoading.value = false;
    }
  }
}

/** loadStorages refreshes the volume draft shown in the editor. */
async function loadStorages(): Promise<void> {
  const target = appId.value;
  if (target === "") {
    return;
  }
  const token = draftGeneration.current();
  storagesLoading.value = true;
  storagesError.value = null;
  try {
    const storages = await appsStore.fetchStorages(target);
    if (!draftGeneration.isCurrent(token) || target !== appId.value) {
      return;
    }
    storagesDraft.value = [...storages];
    storagesLoadedFor.value = target;
  } catch (error) {
    if (!draftGeneration.isCurrent(token) || target !== appId.value) {
      return;
    }
    storagesError.value = describeApplicationError(error);
    storagesLoadedFor.value = "";
  } finally {
    if (draftGeneration.isCurrent(token) && target === appId.value) {
      storagesLoading.value = false;
    }
  }
}

/**
 * loadPreviews refreshes the application's preview bindings. A feature-flag
 * 404 hides the tab; any other failure keeps the rows already shown and
 * surfaces an explicit error with a retry (an unavailable list is never
 * rendered as an empty one).
 */
async function loadPreviews(): Promise<void> {
  if (!appId.value) {
    return;
  }
  previewsLoading.value = true;
  previewsError.value = null;
  try {
    previews.value = await listPreviews(appId.value);
    previewsLoaded.value = true;
    previewsAvailable.value = true;
  } catch (error) {
    if (isPreviewsDisabled(error)) {
      previews.value = [];
      previewsLoaded.value = false;
      previewsAvailable.value = false;
      return;
    }
    previewsError.value = describePreviewError(error);
  } finally {
    previewsLoading.value = false;
  }
}

/** handleSaveEnv replaces the whole environment collection. */
async function handleSaveEnv(): Promise<void> {
  const target = appId.value;
  // Refuse to write a draft that does not belong to the current application:
  // an empty or stale draft must never replace unknown server-side data.
  if (target === "" || envLoadedFor.value !== target) {
    return;
  }
  const token = draftGeneration.current();
  envError.value = null;
  try {
    const saved = await appsStore.saveEnv(target, envDraft.value);
    // Guard by generation as well as id: A→B→A must not let A's old save
    // response overwrite the draft B's navigation reloaded.
    if (target !== appId.value || !draftGeneration.isCurrent(token)) {
      return;
    }
    envDraft.value = [...saved];
    message.success("Environment saved. New variables apply to the next deploy.");
  } catch (error) {
    if (target !== appId.value || !draftGeneration.isCurrent(token)) {
      return;
    }
    envError.value = describeApplicationError(error);
  }
}

/** handleSaveStorages replaces the whole storage collection. */
async function handleSaveStorages(): Promise<void> {
  const target = appId.value;
  if (target === "" || storagesLoadedFor.value !== target) {
    return;
  }
  const token = draftGeneration.current();
  storagesError.value = null;
  try {
    const saved = await appsStore.saveStorages(target, storagesDraft.value);
    if (target !== appId.value || !draftGeneration.isCurrent(token)) {
      return;
    }
    storagesDraft.value = [...saved];
    message.success("Volumes saved. They persist on the node across deploys.");
  } catch (error) {
    if (target !== appId.value || !draftGeneration.isCurrent(token)) {
      return;
    }
    storagesError.value = describeApplicationError(error);
  }
}

/** handleDeploy queues a redeploy of the current revision. */
async function handleDeploy(): Promise<void> {
  try {
    await appsStore.deploy(appId.value);
    message.success("Deploy queued");
  } catch (error) {
    message.error(describeApplicationError(error));
  }
}

/** handleRollback queues a rollback to the selected running deployment. */
async function handleRollback(): Promise<void> {
  rollingBack.value = true;
  try {
    await appsStore.rollback(appId.value, rollbackTarget.value || undefined);
    message.success("Rollback queued");
    rollbackOpen.value = false;
  } catch (error) {
    message.error(describeApplicationError(error));
  } finally {
    rollingBack.value = false;
  }
}

/** handleStop stops the container of the newest deployment. */
async function handleStop(): Promise<void> {
  try {
    await appsStore.stopApp(appId.value);
    // A stop leaves the deployment row "running"; reflect the live container
    // so the tag flips and Stop disables (C4-19).
    containerStopped.value = true;
    stoppedForContainer.value = latest.value?.container_id ?? "";
    message.success("Stop signal sent");
  } catch (error) {
    message.error(describeApplicationError(error, "stop"));
  }
}

/** handleStart restarts the container of the newest deployment. */
async function handleStart(): Promise<void> {
  try {
    await appsStore.startApp(appId.value);
    containerStopped.value = false;
    stoppedForContainer.value = "";
    message.success("Start signal sent");
  } catch (error) {
    message.error(describeApplicationError(error, "start"));
  }
}

/** openRollback preselects the previous release and opens the dialog. */
function openRollback(): void {
  const candidates = runningDeployments.value;
  // Default to the previous successful release; fall back to the only one.
  rollbackTarget.value = candidates[1]?.id ?? candidates[0]?.id ?? "";
  rollbackOpen.value = true;
}

watch(appId, () => {
  // Invalidate any in-flight config read for the previous application.
  draftGeneration.bump();
  activeTab.value = "overview";
  containerStopped.value = false;
  stoppedForContainer.value = "";
  logDeploymentId.value = "";
  logServerId.value = "";
  envDraft.value = [];
  envError.value = null;
  envLoadedFor.value = "";
  envLoading.value = false;
  storagesDraft.value = [];
  storagesError.value = null;
  storagesLoadedFor.value = "";
  storagesLoading.value = false;
  previews.value = [];
  previewsLoaded.value = false;
  previewsError.value = null;
  previewsAvailable.value = true;
  appsStore.stopAllPolling();
  void fetchAll();
});

// A new deployment's container supersedes any local stop/start override —
// but a refresh that merely re-keys the row for the same stopped container
// keeps it.
watch(
  () => latest.value?.id,
  () => {
    const row = latest.value;
    if (
      row &&
      !isActiveDeployment(row) &&
      stoppedForContainer.value !== "" &&
      row.container_id !== "" &&
      row.container_id === stoppedForContainer.value
    ) {
      return;
    }
    containerStopped.value = false;
  },
);

onMounted(() => {
  void fetchAll();
});

onUnmounted(() => {
  appsStore.stopAllPolling();
});
</script>

<template>
  <NSpace vertical :size="16">
    <nav class="breadcrumb" aria-label="Breadcrumb">
      <RouterLink to="/applications">Applications</RouterLink>
      <span class="breadcrumb__sep">/</span>
      <span class="muted mono">{{ shortId || appId }}</span>
    </nav>

    <NSpin :show="appsStore.loading">
      <NAlert
        v-if="appsStore.error"
        type="error"
        :show-icon="true"
        style="margin-bottom: 12px"
      >
        {{ appsStore.error }}
      </NAlert>

      <div class="page-head">
        <NAvatar round :size="48">{{ initials }}</NAvatar>
        <div class="page-head__title">
          <NSpace align="center" :size="10">
            <NText strong style="font-size: 20px" class="mono">{{ displayName || "Application" }}</NText>
            <DeploymentStatusTag
              v-if="latest && !containerStopped"
              :state="latest.state"
              size="medium"
            />
            <NTag v-else-if="containerStopped" type="default" size="medium" round>
              stopped
            </NTag>
          </NSpace>
          <NText depth="3" class="mono">{{ appId }}</NText>
        </div>
        <NSpace class="page-head__actions" align="center" :size="8">
          <NButton
            :loading="appsStore.acting"
            :disabled="active !== null"
            @click="handleDeploy"
          >
            {{ active ? "Deploying…" : "Redeploy" }}
          </NButton>
          <NButton
            :disabled="runningDeployments.length === 0 || appsStore.acting"
            @click="openRollback"
          >
            Rollback
          </NButton>
          <NTooltip trigger="hover" :disabled="controlHint === null">
            <template #trigger>
              <NButton
                :loading="appsStore.acting"
                :disabled="appsStore.acting || controlHint !== null || !containerIsRunning"
                @click="handleStop"
              >
                Stop
              </NButton>
            </template>
            {{ controlHint }}
          </NTooltip>
          <NTooltip trigger="hover" :disabled="controlHint === null">
            <template #trigger>
              <NButton
                :loading="appsStore.acting"
                :disabled="appsStore.acting || controlHint !== null || containerIsRunning"
                @click="handleStart"
              >
                Start
              </NButton>
            </template>
            {{ controlHint }}
          </NTooltip>
        </NSpace>
      </div>

      <NTabs v-model:value="activeTab" type="line" animated>
        <NTabPane name="overview" tab="Overview">
          <NSpace vertical :size="16" style="margin-top: 16px">
            <NCard v-if="application" title="Application">
              <NDescriptions :column="descColumns" bordered label-placement="left">
                <NDescriptionsItem label="Name">
                  <span class="mono">{{ application.name }}</span>
                </NDescriptionsItem>
                <NDescriptionsItem label="Branch">
                  <span class="mono">{{ application.branch || "—" }}</span>
                </NDescriptionsItem>
                <NDescriptionsItem label="Build pack">
                  <span class="mono">{{ application.build_pack || "auto" }}</span>
                </NDescriptionsItem>
                <NDescriptionsItem label="Domain">
                  <span class="mono">{{ application.base_domain || "—" }}</span>
                </NDescriptionsItem>
                <NDescriptionsItem label="Port">
                  <span class="mono">{{ application.port }}:{{ application.host_port }}</span>
                </NDescriptionsItem>
                <NDescriptionsItem label="Node">
                  <span class="mono">{{ application.server_id ? application.server_id.slice(0, 8) : "unassigned" }}</span>
                </NDescriptionsItem>
              </NDescriptions>
            </NCard>
            <NCard v-if="latest" :title="`Deploy ${latest.id.slice(0, 8)}`">
              <template #header-extra>
                <DeploymentStatusTag :state="latest.state" />
              </template>
              <div class="pipeline" role="list" aria-label="Deploy pipeline">
                <template v-for="(step, index) in pipelineSteps" :key="step.name">
                  <span
                    v-if="index > 0"
                    class="pipeline__arrow"
                    aria-hidden="true"
                  >→</span>
                  <span class="pipeline__node" :class="step.mood" role="listitem">
                    <span class="dot" aria-hidden="true" />
                    {{ step.name }}
                  </span>
                </template>
              </div>
              <NDescriptions :column="descColumns" bordered label-placement="left">
                <NDescriptionsItem label="Kind">
                  <span class="mono">{{ latest.kind }}</span>
                </NDescriptionsItem>
                <NDescriptionsItem label="Image">
                  <span class="mono">{{ latest.image_tag || "—" }}</span>
                </NDescriptionsItem>
                <NDescriptionsItem label="Registry image">
                  <span class="mono">{{ latest.registry_image || "—" }}</span>
                </NDescriptionsItem>
                <NDescriptionsItem label="Container">
                  <span class="mono">{{ latest.container_id || "—" }}</span>
                </NDescriptionsItem>
                <NDescriptionsItem label="Duration">
                  {{ durationText(latest) }}
                </NDescriptionsItem>
                <NDescriptionsItem label="Created">
                  {{ relativeTime(latest.created_at) }}
                </NDescriptionsItem>
              </NDescriptions>
              <NAlert
                v-if="latest.error"
                type="error"
                :show-icon="true"
                style="margin-top: 12px"
              >
                {{ latest.error }}
              </NAlert>
            </NCard>
            <NCard v-else title="No deployments yet">
              <NEmpty description="Queue the first deploy to start the pipeline.">
                <template #extra>
                  <NButton
                    type="primary"
                    :loading="appsStore.acting"
                    @click="handleDeploy"
                  >
                    Deploy now
                  </NButton>
                </template>
              </NEmpty>
            </NCard>

            <NCard title="Recent deployments">
              <template #header-extra>
                <NButton
                  v-if="deployments.length > 0"
                  quaternary
                  size="small"
                  @click="activeTab = 'deployments'"
                >
                  View all
                </NButton>
              </template>
              <NDataTable
                v-if="deployments.length > 0"
                :columns="columns"
                :data="deployments.slice(0, 4)"
                :row-key="rowKey"
                :bordered="false"
                :scroll-x="1000"
                :pagination="false"
              />
              <NEmpty v-else description="No deployments recorded for this application." />
            </NCard>
          </NSpace>
        </NTabPane>

        <NTabPane name="deployments" :tab="`Deployments (${deployments.length})`">
          <NCard style="margin-top: 16px">
            <NDataTable
              v-if="deployments.length > 0 || appsStore.loading"
              :columns="columns"
              :data="deployments"
              :loading="appsStore.loading"
              :row-key="rowKey"
              :bordered="false"
              :scroll-x="1000"
              :pagination="{ pageSize: 10 }"
            />
            <NEmpty v-else description="No deployments recorded for this application." />
            <template #footer>
              <NText depth="3">
                Rollback only switches the image tag — the old image stays in
                the internal registry.
              </NText>
            </template>
          </NCard>
        </NTabPane>

        <NTabPane name="logs" tab="Logs">
          <NCard style="margin-top: 16px">
            <NSpace vertical :size="12">
              <NSpace :size="12">
                <NSelect
                  v-model:value="logServerId"
                  :options="serverOptions"
                  placeholder="Select node"
                  style="width: 260px"
                />
                <NSelect
                  v-model:value="logDeploymentId"
                  :options="deploymentOptions"
                  :placeholder="
                    active
                      ? `Streaming: ${active.id.slice(0, 8)}`
                      : 'Select deployment'
                  "
                  style="width: 280px"
                />
              </NSpace>
              <NText depth="3">
                Logs default to the application's node and fall back to the
                first known one — they stream on
                <span class="mono">logs:{node}:{deployment}</span>.
              </NText>
              <DeployLogs
                :server-id="effectiveLogServerId"
                :deployment="logTarget"
              />
            </NSpace>
          </NCard>
        </NTabPane>

        <NTabPane name="env" tab="Environment">
          <NCard style="margin-top: 16px" title="Environment variables">
            <template #header-extra>
              <NButton
                type="primary"
                size="small"
                :loading="appsStore.savingEnv"
                :disabled="envLoading || envLoadedFor !== appId"
                @click="handleSaveEnv"
              >
                Save
              </NButton>
            </template>
            <NSpace vertical :size="12">
              <NAlert
                v-if="envError"
                type="error"
                :show-icon="true"
              >
                <NSpace align="center" :size="12" wrap>
                  <span>{{ envError }}</span>
                  <NButton size="small" @click="void loadEnv()">Retry</NButton>
                </NSpace>
              </NAlert>
              <NSpin :show="envLoading">
                <EnvEditor v-model="envDraft" />
              </NSpin>
            </NSpace>
            <template #footer>
              <NText depth="3">
                Saving replaces the whole collection. Sealed secrets stay
                sealed, and new variables apply to the next deploy.
              </NText>
            </template>
          </NCard>
        </NTabPane>

        <NTabPane name="storage" tab="Storage">
          <NCard style="margin-top: 16px" title="Volumes">
            <template #header-extra>
              <NButton
                type="primary"
                size="small"
                :loading="appsStore.savingStorages"
                :disabled="storagesLoading || storagesLoadedFor !== appId"
                @click="handleSaveStorages"
              >
                Save
              </NButton>
            </template>
            <NSpace vertical :size="12">
              <NAlert
                v-if="storagesError"
                type="error"
                :show-icon="true"
              >
                <NSpace align="center" :size="12" wrap>
                  <span>{{ storagesError }}</span>
                  <NButton size="small" @click="void loadStorages()">Retry</NButton>
                </NSpace>
              </NAlert>
              <NSpin :show="storagesLoading">
                <StorageEditor v-model="storagesDraft" />
              </NSpin>
            </NSpace>
            <template #footer>
              <NText depth="3">
                Saving replaces the whole collection. Volumes live on the node,
                so data survives redeploys and rollbacks.
              </NText>
            </template>
          </NCard>
        </NTabPane>

        <NTabPane name="domains" tab="Domains">
          <div style="margin-top: 16px">
            <DomainEditor v-if="application" :application="application" />
            <NCard v-else>
              <NEmpty description="Loading the application…" />
            </NCard>
          </div>
        </NTabPane>

        <NTabPane
          v-if="previewsAvailable"
          name="previews"
          :tab="previewsLoaded ? `Previews (${previews.length})` : 'Previews'"
        >
          <NCard style="margin-top: 16px" title="Preview deployments">
            <template #header-extra>
              <NButton
                size="small"
                :loading="previewsLoading"
                @click="void loadPreviews()"
              >
                Refresh
              </NButton>
            </template>
            <NSpace vertical :size="12">
              <NAlert v-if="previewsError" type="error" :show-icon="true">
                <NSpace align="center" :size="12" wrap>
                  <span>{{ previewsError }}</span>
                  <NButton size="small" @click="void loadPreviews()">
                    Retry
                  </NButton>
                </NSpace>
              </NAlert>
              <NDataTable
                v-if="previews.length > 0"
                :columns="previewColumns"
                :data="previews"
                :loading="previewsLoading"
                :row-key="previewRowKey"
                :bordered="false"
                :scroll-x="1000"
                :pagination="false"
              />
              <NEmpty
                v-else-if="!previewsLoading && previewsLoaded && !previewsError"
                description="No previews for this application yet."
              >
                <template #extra>
                  <NText depth="3">
                    A preview is created when a pull request opens against
                    <span class="mono">{{ application?.branch || "the watched branch" }}</span>,
                    and torn down when it closes or merges.
                  </NText>
                </template>
              </NEmpty>
            </NSpace>
            <template #footer>
              <NText depth="3">
                Previews are sibling applications: they build the PR head
                branch on a temporary host, and the base application's sealed
                secrets and volumes are deliberately not shared with them.
              </NText>
            </template>
          </NCard>
        </NTabPane>
      </NTabs>
    </NSpin>

    <NModal
      v-model:show="rollbackOpen"
      preset="card"
      title="Rollback to a previous release"
      style="width: 560px; max-width: 94vw"
    >
      <NSpace vertical :size="12">
        <NText depth="3">
          The control plane switches the image tag and restarts the container.
          Environment and volumes stay unchanged.
        </NText>
        <NRadioGroup v-model:value="rollbackTarget">
          <NSpace vertical :size="8">
            <NRadio
              v-for="item in runningDeployments"
              :key="item.id"
              :value="item.id"
            >
              <span class="mono">{{ item.id.slice(0, 8) }}</span>
              ·
              <span class="mono">{{ item.image_tag || "untagged" }}</span>
              · {{ relativeTime(item.created_at) }}
            </NRadio>
          </NSpace>
        </NRadioGroup>
        <NPopconfirm
          :positive-button-props="{ type: 'primary' }"
          @positive-click="handleRollback"
        >
          <template #trigger>
            <NButton
              type="primary"
              :loading="rollingBack"
              :disabled="rollbackTarget === ''"
            >
              Rollback
            </NButton>
          </template>
          Roll back to {{ rollbackTarget.slice(0, 8) }}? The current container
          is kept for a roll-forward.
        </NPopconfirm>
      </NSpace>
    </NModal>
  </NSpace>
</template>

<style scoped>
.breadcrumb {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  font-size: var(--text-xs);
}

.breadcrumb__sep {
  color: var(--meta);
}

.muted {
  color: var(--muted);
}

.mono {
  font-family: var(--font-mono);
}

.tnum {
  font-variant-numeric: tabular-nums;
}

.error-text {
  color: var(--danger);
}

.page-head {
  display: flex;
  align-items: center;
  gap: var(--space-4);
  flex-wrap: wrap;
}

.page-head__title {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  flex: 1;
  min-width: 0;
}

.page-head__actions {
  margin-left: auto;
  flex-shrink: 0;
}

.pipeline {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  flex-wrap: wrap;
  margin-bottom: var(--space-4);
}

.pipeline__arrow {
  color: var(--meta);
}

.pipeline__node {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  font-family: var(--font-mono);
  font-size: var(--text-xs);
  color: var(--muted);
  background: var(--surface-warm);
  border: 1px solid var(--border);
  border-radius: var(--radius-pill);
  padding: 4px 12px;
}

.pipeline__node .dot {
  width: 8px;
  height: 8px;
  border-radius: var(--radius-pill);
  background: var(--meta);
}

.pipeline__node.is-done {
  color: var(--fg);
}

.pipeline__node.is-done .dot {
  background: var(--success);
}

.pipeline__node.is-active {
  color: var(--fg-2);
  border-color: var(--accent);
}

.pipeline__node.is-active .dot {
  background: var(--accent);
  animation: pipeline-pulse 1.2s ease-in-out infinite;
}

.pipeline__node.is-failed {
  color: var(--danger);
  border-color: var(--danger);
}

.pipeline__node.is-failed .dot {
  background: var(--danger);
}

@keyframes pipeline-pulse {
  0%,
  100% {
    opacity: 1;
  }
  50% {
    opacity: 0.35;
  }
}
</style>
