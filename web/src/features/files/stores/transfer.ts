import { defineStore } from "pinia";
import { computed, ref } from "vue";

import {
  isTransferAborted,
  isTransferIncomplete,
  planTransfer,
  streamTransfer,
} from "@/features/files/api/transfer";
import type {
  TransferEntryStatus,
  TransferMode,
  TransferOutcome,
  TransferPhase,
  TransferPlan,
  TransferRequest,
  TransferSummary,
  TransferSummaryStatus,
} from "@/features/files/schemas/transfer";
import { refreshAllPaneListings } from "@/features/files/stores/paneSession";
import type { FilesPaneId } from "@/features/files/stores/paneSession";

export interface TransferSource {
  serverId: string;
  pane: FilesPaneId;
  root: string;
  path: string;
  names: string[];
  sizes: Record<string, number>;
  directories: string[];
}

export interface TransferTarget {
  source: TransferSource;
  destination: {
    serverId: string;
    root: string;
    path: string;
  };
}

export type TransferOrigin = "pane" | "node";

export type TransferState =
  | "idle"
  | "planning"
  | "ready"
  | "running"
  | "finished";

function cloneSource(source: TransferSource): TransferSource {
  return {
    serverId: source.serverId,
    pane: source.pane,
    root: source.root,
    path: source.path,
    names: [...source.names],
    sizes: { ...source.sizes },
    directories: [...source.directories],
  };
}

function cloneTarget(target: TransferTarget): TransferTarget {
  return {
    source: cloneSource(target.source),
    destination: { ...target.destination },
  };
}

function requestFor(target: TransferTarget): TransferRequest {  return {
    source_root: target.source.root,
    source_directory: target.source.path,
    sources: target.source.names,
    destination: {
      server_id: target.destination.serverId,
      root: target.destination.root,
      path: target.destination.path,
    },
  };
}

function entryCounts(entries: TransferOutcome[]): {
  copied: number;
  conflicts: number;
  skipped: number;
  failed: number;
  cancelled: number;
  uncertain: number;
} {
  const counts = {
    copied: 0,
    conflicts: 0,
    skipped: 0,
    failed: 0,
    cancelled: 0,
    uncertain: 0,
  };
  for (const entry of entries) {
    switch (entry.status) {
      case "copied":
        counts.copied += 1;
        break;
      case "conflict":
        counts.conflicts += 1;
        break;
      case "skipped":
        counts.skipped += 1;
        break;
      case "cancelled":
        counts.cancelled += 1;
        break;
      case "uncertain":
        counts.uncertain += 1;
        break;
      default:
        counts.failed += 1;
    }
  }
  return counts;
}

function syntheticSummary(
  names: string[],
  status: TransferEntryStatus,
  reason: string,
  mode: TransferMode,
): TransferSummary {
  const entries = names.map((name) => ({ name, status, reason: reason || undefined }));
  const counts = entryCounts(entries);
  const summaryStatus: TransferSummaryStatus =
    status === "uncertain" ? "uncertain" : "cancelled";
  return {
    transfer_id: "",
    mode,
    status: summaryStatus,
    reason,
    ...counts,
    entries,
    skipped_count: 0,
  };
}

export const useFilesTransferStore = defineStore("filesTransfer", () => {
  const state = ref<TransferState>("idle");
  const dialogOpen = ref(false);
  const origin = ref<TransferOrigin>("node");
  const source = ref<TransferSource | null>(null);
  const target = ref<TransferTarget | null>(null);
  const plan = ref<TransferPlan | null>(null);
  const planError = ref<unknown>(null);
  const runError = ref<unknown>(null);
  const summary = ref<TransferSummary | null>(null);
  const phase = ref<TransferPhase | null>(null);
  const bytes = ref(0);
  const totalBytes = ref<number | null>(null);
  const sawProgress = ref(false);
  const interrupted = ref(false);
  const cancelledByUser = ref(false);

  const leaveOpen = ref(false);
  let leaveResolve: ((_leave: boolean) => void) | null = null;
  let leavePromise: Promise<boolean> | null = null;

  let planController: AbortController | null = null;
  let runController: AbortController | null = null;
  let generation = 0;

  const entries = computed<TransferOutcome[]>(
    () => summary.value?.entries ?? [],
  );
  const isBusy = computed<boolean>(
    () => state.value === "planning" || state.value === "running",
  );
  const canStart = computed<boolean>(
    () => state.value === "ready" && plan.value !== null && target.value !== null,
  );

  function abortPlan(): void {
    planController?.abort();
    planController = null;
  }

  function resetRun(): void {
    plan.value = null;
    planError.value = null;
    runError.value = null;
    summary.value = null;
    phase.value = null;
    bytes.value = 0;
    totalBytes.value = null;
    sawProgress.value = false;
    interrupted.value = false;
    cancelledByUser.value = false;
  }

  function openNodeCopy(nextSource: TransferSource): void {
    if (isBusy.value) {
      dialogOpen.value = true;
      return;
    }
    generation += 1;
    abortPlan();
    resetRun();
    origin.value = "node";
    source.value = cloneSource(nextSource);
    target.value = null;
    state.value = "idle";
    dialogOpen.value = true;
  }

  function openPaneCopy(
    nextSource: TransferSource,
    destination: TransferTarget["destination"],
  ): void {
    if (isBusy.value) {
      dialogOpen.value = true;
      return;
    }
    openNodeCopy(nextSource);
    origin.value = "pane";
    if (destination.serverId !== nextSource.serverId) {
      void requestPlan({ source: nextSource, destination });
      return;
    }
    target.value = cloneTarget({ source: nextSource, destination });
  }

  async function requestPlan(nextTarget: TransferTarget): Promise<boolean> {
    if (state.value === "running") {
      return false;
    }
    const run = ++generation;
    abortPlan();
    resetRun();
    const captured = cloneTarget(nextTarget);
    target.value = captured;
    state.value = "planning";
    const controller = new AbortController();
    planController = controller;
    try {
      const planned = await planTransfer(
        captured.source.serverId,
        requestFor(captured),
        controller.signal,
      );
      if (run !== generation) {
        return false;
      }
      plan.value = planned;
      state.value = "ready";
      return true;
    } catch (err) {
      if (run !== generation || isTransferAborted(err)) {
        return false;
      }
      planError.value = err;
      state.value = "ready";
      return false;
    } finally {
      if (planController === controller) {
        planController = null;
      }
    }
  }

  function requestPlanFor(names: string[]): Promise<boolean> {
    const current = target.value;
    if (current === null || names.length === 0) {
      return Promise.resolve(false);
    }
    return requestPlan({
      source: { ...current.source, names: [...names] },
      destination: { ...current.destination },
    });
  }

  async function start(): Promise<void> {
    const current = target.value;
    const plannedMode = plan.value?.mode ?? "bundle";
    if (current === null || plan.value === null || state.value !== "ready") {
      return;
    }
    const run = ++generation;
    resetRun();
    state.value = "running";
    const controller = new AbortController();
    runController = controller;
    cancelledByUser.value = false;
    try {
      const result = await streamTransfer(
        current.source.serverId,
        requestFor(current),
        {
          signal: controller.signal,
          onEvent: (event) => {
            if (run !== generation) {
              return;
            }
            if (event.type === "summary") {
              summary.value = event.summary;
              return;
            }
            sawProgress.value = true;
            phase.value = event.phase;
            bytes.value = event.bytes ?? 0;
            totalBytes.value = event.total_bytes ?? null;
          },
        },
      );
      if (run !== generation) {
        return;
      }
      summary.value = result;
    } catch (err) {
      if (run !== generation) {
        return;
      }
      if (isTransferAborted(err)) {
        cancelledByUser.value = true;
        summary.value = syntheticSummary(
          current.source.names,
          "cancelled",
          "cancelled",
          plannedMode,
        );
      } else if (isTransferIncomplete(err) || sawProgress.value) {
        interrupted.value = true;
        summary.value = syntheticSummary(
          current.source.names,
          "uncertain",
          "interrupted",
          plannedMode,
        );
      } else {
        runError.value = err;
      }
    } finally {
      if (run === generation) {
        state.value = "finished";
        runController = null;
        await refreshAllPaneListings(
          current.destination.serverId,
          current.destination.root,
          [current.destination.path],
        );
      }
    }
  }

  function cancel(): void {
    if (state.value === "planning") {
      generation += 1;
      abortPlan();
      state.value = "idle";
      return;
    }
    if (state.value === "running") {
      cancelRequested();
      runController?.abort();
    }
  }

  function cancelRequested(): void {
    cancelledByUser.value = true;
  }

  function dismiss(): void {
    dialogOpen.value = false;
  }

  function openDialog(): void {
    if (source.value !== null) {
      dialogOpen.value = true;
    }
  }

  function reset(): void {
    generation += 1;
    abortPlan();
    runController?.abort();
    runController = null;
    dialogOpen.value = false;
    source.value = null;
    target.value = null;
    resetRun();
    state.value = "idle";
    abortLeave();
  }

  function confirmLeave(): Promise<boolean> {
    if (!isBusy.value) {
      return Promise.resolve(true);
    }
    if (leavePromise !== null) {
      return leavePromise;
    }
    leaveOpen.value = true;
    leavePromise = new Promise<boolean>((resolve) => {
      leaveResolve = resolve;
    });
    return leavePromise;
  }

  function resolveLeave(leave: boolean): void {
    leaveOpen.value = false;
    if (leave) {
      cancel();
    }
    leaveResolve?.(leave);
    leaveResolve = null;
    leavePromise = null;
  }

  function abortLeave(): void {
    if (leavePromise === null) {
      return;
    }
    leaveOpen.value = false;
    leaveResolve?.(false);
    leaveResolve = null;
    leavePromise = null;
  }

  return {
    state,
    dialogOpen,
    origin,
    source,
    target,
    plan,
    planError,
    runError,
    summary,
    phase,
    bytes,
    totalBytes,
    sawProgress,
    interrupted,
    cancelledByUser,
    entries,
    isBusy,
    canStart,
    leaveOpen,
    openNodeCopy,
    openPaneCopy,
    requestPlan,
    requestPlanFor,
    start,
    cancel,
    dismiss,
    openDialog,
    reset,
    confirmLeave,
    resolveLeave,
  };
});
