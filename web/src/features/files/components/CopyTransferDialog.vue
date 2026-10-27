<script setup lang="ts">
import { ArrowRight, RefreshCw } from "@lucide/vue";
import {
  NAlert,
  NButton,
  NCheckbox,
  NInput,
  NModal,
  NProgress,
  NSelect,
  NSpin,
  useMessage,
} from "naive-ui";
import { computed, nextTick, ref, watch } from "vue";
import { useI18n } from "vue-i18n";

import {
  describeFilesError,
  listRoots,
} from "@/features/files/api/files";
import type { FilesRoot } from "@/features/files/schemas/files";
import type {
  TransferOutcome,
  TransferPhase,
} from "@/features/files/schemas/transfer";
import { useFilesStore } from "@/features/files/stores/files";
import { useFilesTransferStore } from "@/features/files/stores/transfer";
import { copyTargetProblem } from "@/features/files/utils/transfer";
import { isApiError, useServersStore } from "@/features/servers";
import { formatBytes } from "@/shared/utils/format";

const { t } = useI18n();
const message = useMessage();
const transfer = useFilesTransferStore();
const servers = useServersStore();

const source = computed(() => transfer.source);
const shownNames = computed<string[]>(
  () => transfer.target?.source.names ?? source.value?.names ?? [],
);
const isPaneOrigin = computed<boolean>(() => transfer.origin === "pane");
const sourceNodeName = computed<string>(() => nodeName(source.value?.serverId ?? ""));
const destinationNodeName = computed<string>(() =>
  nodeName(pendingServerId.value),
);

function nodeName(id: string): string {
  return servers.servers.find((server) => server.id === id)?.name ?? id;
}

const pendingServerId = ref("");
const pendingRoot = ref("");
const pendingPath = ref("");
const roots = ref<FilesRoot[]>([]);
const rootsLoading = ref(false);
const rootsError = ref<string>("");
const formError = ref<string>("");

const serverOptions = computed(() =>
  servers.servers.map((server) => ({
    label:
      server.status === "ready"
        ? server.name
        : `${server.name} · ${String(t(`servers.status.${server.status}`))}`,
    value: server.id,
    disabled:
      server.status !== "ready" && server.id !== (source.value?.serverId ?? ""),
  })),
);

const rootOptions = computed(() =>
  roots.value.map((root) => ({
    label: `${root.name} · ${t(root.writable ? "files.browser.writable" : "files.browser.readOnly")}`,
    value: root.name,
  })),
);

const selectedRoot = computed<FilesRoot | null>(
  () => roots.value.find((root) => root.name === pendingRoot.value) ?? null,
);

const destination = computed(() => ({
  serverId: pendingServerId.value,
  root: pendingRoot.value,
  path: pendingPath.value.trim().replace(/^\/+|\/+$/g, ""),
}));

const targetMatchesDraft = computed<boolean>(() => {
  const target = transfer.target;
  if (target === null) {
    return false;
  }
  return (
    target.destination.serverId === destination.value.serverId &&
    target.destination.root === destination.value.root &&
    target.destination.path === destination.value.path
  );
});

const destinationProblemText = computed<string>(() => {
  const origin = source.value;
  if (origin === null || transfer.isBusy) {
    return "";
  }
  if (rootsLoading.value) {
    return "";
  }
  if (rootsError.value !== "") {
    return String(
      t("files.transfer.unavailableRoots", { reason: rootsError.value }),
    );
  }
  const server = servers.servers.find(
    (item) => item.id === destination.value.serverId,
  );
  const problem = copyTargetProblem({
    sourceServerId: origin.serverId,
    sourceRoot: origin.root,
    sourcePath: origin.path,
    sourceDirectories: origin.directories,
    destination: {
      serverId: destination.value.serverId,
      root: destination.value.root,
      path: destination.value.path,
      serverStatus: server?.status,
      writable: selectedRoot.value?.writable,
    },
  });
  switch (problem) {
    case "no-server":
    case "offline":
      return String(
        t("files.transfer.unavailableNodeOffline", {
          status: String(t(`servers.status.${server?.status ?? "offline"}`)),
        }),
      );
    case "outdated":
      return String(
        t("files.transfer.unavailableNodeOffline", {
          status: String(t(`servers.status.${server?.status ?? "error"}`)),
        }),
      );
    case "not-ready":
      return String(t("files.transfer.unavailableLoading"));
    case "disabled":
      return String(t("files.transfer.unavailableNodeOffline", { status: "disabled" }));
    case "no-root":
      return String(t("files.transfer.unavailableNoRoot"));
    case "read-only":
      return String(t("files.transfer.unavailableReadOnly"));
    case "invalid-path":
      return String(t("files.transfer.unavailableInvalidPath"));
    case "same-cross-root":
      return String(t("files.transfer.unavailableSameCrossRoot"));
    case "same-folder":
      return String(t("files.transfer.unavailableSameFolder"));
    case "into-itself":
      return String(t("files.transfer.unavailableIntoItself"));
    default:
      return "";
  }
});

const sameNodeSameRoot = computed<boolean>(() => {
  const origin = source.value;
  return (
    origin !== null &&
    destination.value.serverId === origin.serverId &&
    destination.value.root === origin.root &&
    destinationProblemText.value === ""
  );
});

const canCheck = computed<boolean>(
  () =>
    !transfer.isBusy &&
    source.value !== null &&
    destinationProblemText.value === "" &&
    pendingServerId.value !== "" &&
    pendingRoot.value !== "",
);

const canStart = computed<boolean>(
  () =>
    transfer.canStart &&
    transfer.plan !== null &&
    targetMatchesDraft.value &&
    !allUnavailable.value,
);

const plan = computed(() => transfer.plan);
const allUnavailable = computed<boolean>(() => {
  const current = transfer.plan;
  if (current === null || current.sources.length === 0) {
    return false;
  }
  return current.sources.every(
    (item) =>
      item.conflict ||
      current.unsupported.some((entry) => entry.name === item.name),
  );
});

const maxSelected = computed<{ name: string; size: number } | null>(() => {
  const origin = source.value;
  if (origin === null) {
    return null;
  }
  let best: { name: string; size: number } | null = null;
  for (const name of shownNames.value) {
    const size = origin.sizes[name] ?? 0;
    if (best === null || size > best.size) {
      best = { name, size };
    }
  }
  return best;
});

const planFailureText = computed<string>(() => {
  if (transfer.planError === null) {
    return "";
  }
  const error = transfer.planError;
  const code = isApiError(error) ? error.code : undefined;
  if (code === "too_large" || (isApiError(error) && error.status === 413)) {
    const file = maxSelected.value;
    const available = selectedRoot.value?.free_bytes;
    const bound =
      available !== undefined && file !== null && available >= file.size
        ? available
        : undefined;
    if (file !== null && bound !== undefined) {
      return String(
        t("files.transfer.planTooLarge", {
          name: file.name,
          bound: formatBytes(bound),
        }),
      );
    }
    if (file !== null) {
      return String(
        t("files.transfer.planTooLargeNoBound", {
          name: file.name,
        }),
      );
    }
  }
  return String(
    t("files.transfer.planFailed", {
      reason: describeFilesError(error),
    }),
  );
});

const showPlan = computed<boolean>(
  () =>
    plan.value !== null &&
    !transfer.isBusy &&
    transfer.state !== "finished" &&
    targetMatchesDraft.value,
);

const conflictsText = computed<string>(() => {
  const current = plan.value;
  if (current === null || current.conflicts.length === 0) {
    return "";
  }
  return String(
    t("files.transfer.conflicts", {
      count: current.conflicts.length,
      names: current.conflicts.join(", "),
    }),
  );
});

const unsupportedText = computed<string>(() => {
  const current = plan.value;
  if (current === null || current.unsupported.length === 0) {
    return "";
  }
  return String(
    t("files.transfer.unsupported", {
      count: current.unsupported.length,
      names: current.unsupported.map((entry) => entry.name).join(", "),
    }),
  );
});

const boundsText = computed<string>(() => {
  const current = plan.value;
  if (current === null) {
    return "";
  }
  return String(
    t("files.transfer.bounds", {
      bundle: formatBytes(current.bounds.max_bundle_bytes),
      expanded: formatBytes(current.bounds.max_expanded_bytes),
      entries: current.bounds.max_entries,
      sources: current.bounds.max_sources,
    }),
  );
});

const availableText = computed<string>(() => {
  const current = plan.value;
  if (current?.available_bytes === undefined) {
    return "";
  }
  return String(
    t("files.transfer.available", {
      free: formatBytes(current.available_bytes),
    }),
  );
});

const fitsText = computed<string>(() => {
  const current = plan.value;
  if (current?.fits === undefined) {
    return "";
  }
  return String(
    t(
      current.fits
        ? "files.transfer.fits"
        : "files.transfer.doesNotFit",
    ),
  );
});

const phaseLabel = computed<string>(() => {
  const current = transfer.phase;
  if (current === null) {
    return String(t("files.transfer.indeterminate"));
  }
  const labels: Record<TransferPhase, string> = {
    preparing: String(t("files.transfer.preparing")),
    transferring: String(t("files.transfer.transferring")),
    extracting: String(t("files.transfer.extracting")),
    publishing: String(t("files.transfer.publishing")),
    cleaning_up: String(t("files.transfer.cleaning")),
  };
  return labels[current];
});

const progressPercent = computed<number | null>(() => {
  const total = transfer.totalBytes;
  if (total === null || total <= 0) {
    return null;
  }
  return Math.min(100, Math.round((transfer.bytes / total) * 100));
});

const progressText = computed<string>(() => {
  const total = transfer.totalBytes;
  if (total === null) {
    return String(
      t("files.transfer.transferredUnknown", {
        bytes: formatBytes(transfer.bytes),
      }),
    );
  }
  return String(
    t("files.transfer.transferred", {
      bytes: formatBytes(transfer.bytes),
      total: formatBytes(total),
    }),
  );
});

const summary = computed(() => transfer.summary);
const copiedWithIssues = computed<boolean>(() => {
  const current = summary.value;
  return (
    current?.status === "copied" &&
    (current.skipped_count > 0 || (current.cleanup_failures ?? 0) > 0)
  );
});

const reportTitle = computed<string>(() => {
  const current = summary.value;
  if (current === null) {
    return String(t("files.transfer.runErrorTitle"));
  }
  if (transfer.interrupted) {
    return String(t("files.transfer.interruptedTitle"));
  }
  if (current.reason === "timeout") {
    return String(t("files.transfer.timeoutTitle"));
  }
  if (current.status === "cancelled") {
    return String(t("files.transfer.cancelledTitle"));
  }
  if (copiedWithIssues.value) {
    return String(t("files.transfer.partialTitle"));
  }
  if (current.status === "copied") {
    return String(t("files.transfer.doneTitle"));
  }
  return String(t("files.transfer.partialTitle"));
});

const reportMessage = computed<string>(() => {
  const current = summary.value;
  if (current === null) {
    return "";
  }
  if (transfer.interrupted) {
    return String(t("files.transfer.interrupted"));
  }
  if (current.reason === "timeout") {
    return String(t("files.transfer.timeout"));
  }
  if (current.status === "cancelled") {
    return String(t("files.transfer.cancelled"));
  }
  if (copiedWithIssues.value) {
    if (current.skipped_count > 0) {
      return String(
        t("files.transfer.doneWithNested", {
          count: current.copied,
          skipped: current.skipped_count,
        }),
      );
    }
    return String(
      t("files.transfer.doneWithCleanup", {
        count: current.copied,
        failures: current.cleanup_failures ?? 0,
      }),
    );
  }
  if (current.status === "copied") {
    return String(t("files.transfer.done", { count: current.copied }));
  }
  return String(
    t("files.transfer.counts", {
      copied: current.copied,
      conflicts: current.conflicts,
      skipped: current.skipped,
      failed: current.failed,
      cancelled: current.cancelled,
      uncertain: current.uncertain,
    }),
  );
});

const reportCounts = computed<string>(() => {
  const current = summary.value;
  if (current === null) {
    return "";
  }
  return String(
    t("files.transfer.counts", {
      copied: current.copied,
      conflicts: current.conflicts,
      skipped: current.skipped,
      failed: current.failed,
      cancelled: current.cancelled,
      uncertain: current.uncertain,
    }),
  );
});

function entryStatusLabel(entry: TransferOutcome): string {
  const labels: Record<TransferOutcome["status"], string> = {
    copied: String(t("files.transfer.statusCopied")),
    conflict: String(t("files.transfer.statusConflict")),
    skipped: String(t("files.transfer.statusSkipped")),
    failed: String(t("files.transfer.statusFailed")),
    cancelled: String(t("files.transfer.statusCancelled")),
    uncertain: String(t("files.transfer.statusUncertain")),
  };
  return labels[entry.status];
}

const retryNames = computed<string[]>(() =>
  transfer.entries
    .filter(
      (entry) =>
        entry.status === "skipped" ||
        entry.status === "failed" ||
        entry.status === "cancelled",
    )
    .map((entry) => entry.name),
);

const retryChecked = ref<Set<string>>(new Set());

watch(
  () => transfer.summary,
  () => {
    retryChecked.value = new Set(retryNames.value);
  },
);

const canRetry = computed<boolean>(
  () => retryChecked.value.size > 0 && !transfer.isBusy,
);

function toggleRetry(name: string, checked: boolean): void {
  const next = new Set(retryChecked.value);
  if (checked) {
    next.add(name);
  } else {
    next.delete(name);
  }
  retryChecked.value = next;
}

async function loadRootsFor(serverId: string): Promise<void> {
  roots.value = [];
  rootsError.value = "";
  if (serverId === "") {
    return;
  }
  rootsLoading.value = true;
  try {
    roots.value = await listRoots(serverId);
    if (
      pendingRoot.value === "" ||
      !roots.value.some((root) => root.name === pendingRoot.value)
    ) {
      pendingRoot.value =
        roots.value.find((root) => root.writable)?.name ??
        roots.value[0]?.name ??
        "";
    }
  } catch (err) {
    rootsError.value = describeFilesError(err);
  } finally {
    rootsLoading.value = false;
  }
}

watch(
  () => transfer.source,
  async (current) => {
    if (current === null) {
      return;
    }
    const target = transfer.target;
    if (isPaneOrigin.value && target !== null) {
      pendingServerId.value = target.destination.serverId;
      pendingRoot.value = target.destination.root;
      pendingPath.value = target.destination.path;
      await loadRootsFor(pendingServerId.value);
      return;
    }
    const fallback =
      servers.servers.find(
        (server) =>
          server.id !== current.serverId && server.status === "ready",
      )?.id ?? current.serverId;
    pendingServerId.value = fallback;
    pendingRoot.value = "";
    pendingPath.value = "";
    await loadRootsFor(fallback);
  },
  { immediate: true },
);

watch(pendingServerId, (next, previous) => {
  if (next === previous) {
    return;
  }
  if (isPaneOrigin.value) {
    return;
  }
  pendingRoot.value = "";
  void loadRootsFor(next);
});

function buildTarget() {
  const current = source.value;
  if (current === null) {
    return null;
  }
  return {
    source: { ...current, names: [...current.names] },
    destination: {
      serverId: destination.value.serverId,
      root: destination.value.root,
      path: destination.value.path,
    },
  };
}

async function check(): Promise<void> {
  const target = buildTarget();
  if (target === null || !canCheck.value) {
    return;
  }
  if (sameNodeSameRoot.value) {
    await runLocalCopy();
    return;
  }
  await transfer.requestPlan(target);
}

async function start(): Promise<void> {
  if (!canStart.value) {
    return;
  }
  await transfer.start();
}

async function runLocalCopy(): Promise<void> {
  const current = source.value;
  if (current === null) {
    return;
  }
  const local = useFilesStore(current.pane);
  if (local.rootName !== current.root) {
    formError.value = String(t("files.transfer.localSourceChanged", { root: current.root }));
    return;
  }
  formError.value = "";
  const result = await local.promptTransfer(
    "copy",
    current.path,
    destination.value.path,
    current.names,
  );
  if (result.failed.length > 0) {
    message.warning(
      String(
        t("files.dialogs.transferPartial", {
          failed: result.failed.length,
          error: result.failed[0]?.error ?? "",
        }),
      ),
    );
    return;
  }
  if (result.conflicts.length > 0) {
    close();
    return;
  }
  if (result.done.length > 0) {
    message.success(
      String(t("files.dialogs.copied", { count: result.done.length })),
    );
  }
  close();
}

function cancelOrHide(): void {
  if (transfer.isBusy) {
    transfer.cancel();
    return;
  }
  close();
}

function close(): void {
  if (transfer.isBusy) {
    transfer.dismiss();
    return;
  }
  if (transfer.state === "finished") {
    transfer.dismiss();
    return;
  }
  transfer.reset();
}

async function retry(): Promise<void> {
  if (!canRetry.value) {
    return;
  }
  await transfer.requestPlanFor([...retryChecked.value]);
}

const runErrorText = computed<string>(() => {
  if (transfer.runError === null) {
    return "";
  }
  if (transfer.runError instanceof Error) {
    return transfer.runError.message;
  }
  return describeFilesError(transfer.runError);
});

const dialogBody = ref<HTMLElement | null>(null);
let restoreFocus: HTMLElement | null = null;
let restorePane = "left";

function focusDialog(): void {
  const body = dialogBody.value;
  if (body === null) {
    return;
  }
  const field = body.querySelector<HTMLElement>(
    ".transfer-node-select input, .transfer-field--path input",
  );
  if (field !== null) {
    field.focus();
    return;
  }
  const primary = Array.from(
    body.querySelectorAll<HTMLElement>("button"),
  ).find((button) => {
    const label = button.getAttribute("aria-label") ?? "";
    return (
      label === String(t("files.transfer.check")) ||
      label === String(t("files.transfer.start"))
    );
  });
  (primary ?? body.querySelector<HTMLElement>("button"))?.focus();
}

function restoreDialogFocus(): void {
  const target = restoreFocus;
  restoreFocus = null;
  if (target !== null && target !== document.body && target.isConnected) {
    target.focus();
    return;
  }
  document
    .querySelector<HTMLElement>(
      `.files-browser[data-pane="${restorePane}"] button[aria-label="File operations"]`,
    )
    ?.focus();
}

watch(
  () => transfer.dialogOpen,
  async (open) => {
    if (open) {
      restorePane = transfer.source?.pane ?? "left";
      restoreFocus =
        document.activeElement instanceof HTMLElement
          ? document.activeElement
          : null;
      await nextTick();
      focusDialog();
      return;
    }
    await nextTick();
    restoreDialogFocus();
  },
  { flush: "post" },
);

const announcement = computed<string>(() => {
  if (transfer.state === "running") {
    return phaseLabel.value;
  }
  if (transfer.state === "finished") {
    return `${reportTitle.value} · ${reportMessage.value || runErrorText.value}`;
  }
  if (transfer.state === "planning") {
    return String(t("files.transfer.checking"));
  }
  return "";
});
</script>

<template>
  <NModal
    class="app-modal transfer-modal"
    :show="transfer.dialogOpen"
    preset="card"
    style="width: 620px; max-width: 94vw"
    :title="isPaneOrigin ? t('files.transfer.titlePane') : t('files.transfer.title')"
    :mask-closable="!transfer.isBusy"
    @update:show="(value: boolean) => { if (!value) close(); }"
  >
    <div v-if="source" ref="dialogBody" class="transfer-form">
      <section class="transfer-block" data-testid="transfer-source">
        <h4 class="transfer-block__title">{{ t("files.transfer.source") }}</h4>
        <p class="transfer-line">
          {{ t("files.transfer.sourceNode", { node: sourceNodeName, root: source.root }) }}
        </p>
        <p class="transfer-line muted">
          {{ t("files.transfer.sourcePath", { path: source.path === "" ? "/" : source.path }) }}
        </p>
        <p class="transfer-line">
          {{ t("files.transfer.selection", { count: shownNames.length, names: shownNames.join(", ") }) }}
        </p>
      </section>

      <section class="transfer-block" data-testid="transfer-destination">
        <h4 class="transfer-block__title">{{ t("files.transfer.destination") }}</h4>
        <template v-if="isPaneOrigin">
          <p class="transfer-line">
            {{ t("files.transfer.sourceNode", { node: destinationNodeName, root: destination.root }) }}
          </p>
          <p class="transfer-line muted">
            {{ t("files.transfer.sourcePath", { path: destination.path === "" ? "/" : destination.path }) }}
          </p>
        </template>
        <template v-else>
          <div class="transfer-fields">
            <label class="transfer-field">
              <span class="transfer-field__label">{{ t("files.transfer.destinationNode") }}</span>
              <NSelect
                v-model:value="pendingServerId"
                class="transfer-node-select"
                size="small"
                :options="serverOptions"
                :disabled="transfer.isBusy"
                :aria-label="t('files.transfer.destinationNode')"
              />
            </label>
            <label class="transfer-field">
              <span class="transfer-field__label">{{ t("files.transfer.destinationRoot") }}</span>
              <NSelect
                v-model:value="pendingRoot"
                class="transfer-root-select"
                size="small"
                :options="rootOptions"
                :loading="rootsLoading"
                :disabled="transfer.isBusy"
                :aria-label="t('files.transfer.destinationRoot')"
              />
            </label>
            <label class="transfer-field transfer-field--path">
              <span class="transfer-field__label">{{ t("files.transfer.destinationPath") }}</span>
              <NInput
                v-model:value="pendingPath"
                size="small"
                :placeholder="t('files.transfer.destinationPathPlaceholder')"
                :disabled="transfer.isBusy"
                :aria-label="t('files.transfer.destinationPath')"
              />
            </label>
          </div>
        </template>
        <p v-if="destinationProblemText" class="transfer-problem" role="alert">
          {{ destinationProblemText }}
        </p>
        <p v-else-if="formError" class="transfer-problem" role="alert">
          {{ formError }}
        </p>
        <p v-else-if="sameNodeSameRoot" class="transfer-line muted">
          {{ t("files.transfer.sameNodeSameRoot") }}
        </p>
      </section>

      <NAlert
        v-if="transfer.state === 'planning'"
        type="info"
        :show-icon="true"
      >
        <span class="transfer-inline">
          <NSpin size="small" />
          {{ t("files.transfer.checking") }}
        </span>
      </NAlert>

      <NAlert
        v-else-if="transfer.state === 'ready' && transfer.planError"
        type="error"
        :show-icon="true"
        :title="t('files.transfer.unavailableTitle')"
      >
        <p class="transfer-line">{{ planFailureText }}</p>
      </NAlert>

      <NAlert
        v-else-if="showPlan"
        type="info"
        :show-icon="true"
        :title="t('files.transfer.preflight')"
      >
        <p class="transfer-line">
          {{ plan?.mode === "direct" ? t("files.transfer.modeDirect") : t("files.transfer.modeBundle") }}
        </p>
        <p class="transfer-line muted">{{ boundsText }}</p>
        <p v-if="availableText" class="transfer-line muted">{{ availableText }}</p>
        <p v-if="fitsText" class="transfer-line">{{ fitsText }}</p>
        <p v-if="conflictsText" class="transfer-line">{{ conflictsText }}</p>
        <p v-if="unsupportedText" class="transfer-line">{{ unsupportedText }}</p>
        <p v-if="conflictsText || unsupportedText" class="transfer-line muted">
          {{ t("files.transfer.skippedNote") }}
        </p>
        <p v-if="allUnavailable" class="transfer-problem" role="alert">
          {{ t("files.transfer.allUnavailable") }}
        </p>
      </NAlert>

      <section
        v-if="transfer.state === 'running'"
        class="transfer-progress"
      >
        <h4 class="transfer-block__title">{{ phaseLabel }}</h4>
        <NProgress
          v-if="progressPercent !== null"
          type="line"
          :percentage="progressPercent"
          :show-indicator="false"
        />
        <p class="transfer-line muted">{{ progressText }}</p>
        <p v-if="progressPercent === null" class="transfer-line muted">
          {{ t("files.transfer.indeterminate") }}
        </p>
      </section>

      <section v-if="transfer.state === 'finished'" class="transfer-report">
        <h4 class="transfer-block__title">{{ reportTitle }}</h4>
        <p class="transfer-line">{{ reportMessage }}</p>
        <p v-if="reportCounts" class="transfer-line muted">{{ reportCounts }}</p>

        <NAlert
          v-if="transfer.runError"
          type="error"
          :show-icon="true"
          :title="t('files.transfer.runErrorTitle')"
        >
          <p class="transfer-line">
            {{ t("files.transfer.runError", { reason: runErrorText }) }}
          </p>
        </NAlert>

        <p
          v-if="summary && summary.reason === 'timeout'"
          class="transfer-line transfer-problem"
        >
          {{ t("files.transfer.reasonTimeout") }}
        </p>

        <p
          v-if="transfer.interrupted || summary?.uncertain"
          class="transfer-line transfer-problem"
        >
          {{ t("files.transfer.uncertainNote") }}
        </p>

        <p v-if="summary && summary.skipped_count > 0" class="transfer-line muted">
          {{ t("files.transfer.nestedSkipped", { count: summary.skipped_count }) }}
        </p>
        <p v-if="summary && (summary.cleanup_failures ?? 0) > 0" class="transfer-line muted">
          {{ t("files.transfer.cleanupFailures", { count: summary.cleanup_failures }) }}
        </p>

        <div v-if="transfer.entries.length > 0" class="transfer-entries">
          <h5 class="transfer-entries__title">{{ t("files.transfer.entries") }}</h5>
          <ul class="transfer-list">
            <li
              v-for="entry in transfer.entries"
              :key="entry.name"
              class="transfer-list__item"
              :data-status="entry.status"
            >
              <span class="mono grow">{{ entry.name }}</span>
              <span class="transfer-status" :data-status="entry.status">
                {{ entryStatusLabel(entry) }}
              </span>
            </li>
          </ul>
        </div>

        <div v-if="retryNames.length > 0" class="transfer-retry">
          <h5 class="transfer-entries__title">{{ t("files.transfer.retryTitle") }}</h5>
          <p class="transfer-line muted">{{ t("files.transfer.retryHint") }}</p>
          <ul class="transfer-list">
            <li v-for="name in retryNames" :key="name" class="transfer-list__item">
              <NCheckbox
                :checked="retryChecked.has(name)"
                :aria-label="name"
                @update:checked="(checked: boolean) => toggleRetry(name, checked)"
              >
                <span class="mono">{{ name }}</span>
              </NCheckbox>
            </li>
          </ul>
          <NButton
            size="small"
            secondary
            :disabled="!canRetry"
            :aria-label="t('files.transfer.retry')"
            @click="() => void retry()"
          >
            <template #icon>
              <RefreshCw :size="14" :stroke-width="1.8" aria-hidden="true" />
            </template>
            {{ t("files.transfer.retry") }}
          </NButton>
        </div>
      </section>
    </div>

    <span class="fm-sr-only" role="status" aria-live="polite" aria-atomic="true">
      {{ announcement }}
    </span>

    <template #footer>
      <div class="transfer-foot">
        <span class="transfer-arrow" aria-hidden="true">
          <ArrowRight :size="14" :stroke-width="1.8" />
        </span>
        <NButton
          v-if="transfer.state !== 'finished'"
          @click="cancelOrHide"
        >
          {{ transfer.isBusy ? t("files.transfer.cancel") : t("common.actions.cancel") }}
        </NButton>
        <NButton
          v-if="transfer.state === 'running'"
          type="primary"
          @click="transfer.dismiss()"
        >
          {{ t("files.transfer.hide") }}
        </NButton>
        <NButton
          v-if="transfer.state === 'idle' || transfer.state === 'ready'"
          type="primary"
          :disabled="!canCheck"
          :aria-label="t('files.transfer.check')"
          @click="() => void check()"
        >
          {{ t("files.transfer.check") }}
        </NButton>
        <NButton
          v-if="transfer.state === 'ready' && transfer.plan"
          type="primary"
          :disabled="!canStart"
          :aria-label="t('files.transfer.start')"
          @click="() => void start()"
        >
          {{ t("files.transfer.start") }}
        </NButton>
        <NButton
          v-if="transfer.state === 'finished'"
          type="primary"
          @click="close"
        >
          {{ t("files.transfer.close") }}
        </NButton>
      </div>
    </template>
  </NModal>
</template>

<style scoped>
.transfer-form {
  display: flex;
  flex-direction: column;
  gap: var(--space-3);
}

.transfer-block {
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  padding: var(--space-3);
}

.transfer-block__title {
  margin: 0 0 var(--space-2);
  font-size: var(--text-sm);
  color: var(--fg-2);
}

.transfer-line {
  margin: 0;
  font-size: var(--text-sm);
  overflow-wrap: anywhere;
}

.transfer-fields {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  gap: var(--space-2);
}

.transfer-field {
  display: flex;
  flex-direction: column;
  gap: var(--space-1);
  min-width: 0;
}

.transfer-field--path {
  grid-column: 1 / -1;
}

.transfer-field__label {
  font-size: var(--text-xs);
  color: var(--muted);
}

.transfer-problem {
  color: var(--danger, #c0392b);
  font-size: var(--text-sm);
}

.transfer-inline {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
}

.transfer-progress,
.transfer-report {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.transfer-entries,
.transfer-retry {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
}

.transfer-entries__title {
  margin: var(--space-2) 0 0;
  font-size: var(--text-xs);
  color: var(--fg-2);
}

.transfer-list {
  margin: 0;
  padding: 0;
  list-style: none;
  max-height: 220px;
  overflow-y: auto;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
}

.transfer-list__item {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-1) var(--space-2);
  font-size: var(--text-sm);
}

.transfer-list__item + .transfer-list__item {
  border-top: 1px solid var(--border);
}

.transfer-status {
  flex: 0 0 auto;
  font-size: var(--text-xs);
  color: var(--muted);
}

.transfer-status[data-status="copied"] {
  color: var(--success, #2e7d32);
}

.transfer-status[data-status="failed"],
.transfer-status[data-status="uncertain"] {
  color: var(--danger, #c0392b);
}

.transfer-foot {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: var(--space-2);
}

.transfer-arrow {
  margin-right: auto;
  color: var(--muted);
}

.fm-sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
  border: 0;
}

@media (max-width: 480px) {
  .transfer-fields {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
