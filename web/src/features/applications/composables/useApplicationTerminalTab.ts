import { computed, onScopeDispose, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";

import { useApplicationTerminalTargets } from "@/features/applications/composables/useApplicationTerminalTargets";
import type { TargetsLoadResult } from "@/features/applications/composables/useApplicationTerminalTargets";
import { useTerminalStore } from "@/features/terminal";
import type { TerminalTab } from "@/features/terminal";
import type { ApplicationTerminalTarget } from "@/features/terminal/schemas/terminal";
import {
  chooseRecoveryTarget,
  containerTabTitle,
  containerTargetKey,
} from "@/features/terminal/utils/containerRecovery";
import { isContainerTerminalTarget } from "@/features/terminal/utils/terminal";

/**
 * Session lifecycle for the application Terminal tab (JUS-247).
 *
 * Discovery is lazy and owned by `useApplicationTerminalTargets`; this module
 * decides what a fresh target set means: exactly one runnable container opens
 * automatically, several show the explicit selector, none render the empty
 * state. Every recovery path (pane Retry, Start new session, Refresh) reads the
 * targets again first, so a replaced container is never re-POSTed by id. A
 * service switch runs the shared close confirmation, and a close leaves the
 * explicit restart card (even across a route remount).
 */
export interface ApplicationTerminalTabOptions {
  applicationId: () => string;
  applicationName: () => string;
  /** canUse mirrors the known, write-level access gate. */
  canUse: () => boolean;
  /** active is true only while the Terminal tab is selected. */
  active: () => boolean;
}

/** tabTargetKey is the key read back from a live tab. */
function tabTargetKey(tab: TerminalTab): string {
  return containerTargetKey({
    service: tab.service,
    containerId: tab.containerId,
  });
}

export function useApplicationTerminalTab(
  options: ApplicationTerminalTabOptions,
) {
  const route = useRoute();
  const router = useRouter();
  const terminal = useTerminalStore();

  const discovery = useApplicationTerminalTargets({
    applicationId: options.applicationId,
    enabled: options.canUse,
  });

  const selection = ref("");
  const tabId = ref("");
  const starting = ref(false);
  /** closed marks a tab that vanished under the mounted pane. */
  const closed = ref(false);

  const tab = computed<TerminalTab | null>(() =>
    tabId.value === "" ? null : terminal.findTab(tabId.value),
  );
  const targetOptions = computed(() =>
    discovery.targets.value.map((target) => ({
      label: target.label || target.name,
      value: containerTargetKey({
        service: target.service,
        containerId: target.container_id,
      }),
    })),
  );
  const targetCount = computed<number>(() => discovery.targets.value.length);
  const showPicker = computed<boolean>(() => targetCount.value > 1);
  const canUse = computed<boolean>(() => options.canUse());
  const mode = computed<
    "idle" | "loading" | "error" | "empty" | "select" | "session"
  >(() => {
    if (closed.value || tab.value !== null || starting.value) {
      return "session";
    }
    if (!canUse.value) {
      return "idle";
    }
    if (discovery.loading.value) {
      return "loading";
    }
    if (discovery.error.value !== null) {
      return "error";
    }
    if (discovery.loaded.value && targetCount.value === 0) {
      return "empty";
    }
    if (discovery.loaded.value && targetCount.value === 1) {
      // The single target is opening; never flash an empty selector.
      return "loading";
    }
    if (discovery.loaded.value && targetCount.value > 1) {
      return "select";
    }
    return "idle";
  });

  /** sourcePath is the route the app dock resumes for this page session. */
  function sourcePath(): string {
    return router.resolve({
      name: "application-detail",
      params: {
        projectId: String(route.params.projectId ?? ""),
        environmentId: String(route.params.environmentId ?? ""),
        id: options.applicationId(),
      },
      query: { tab: "terminal" },
    }).fullPath;
  }

  /** resetLocal forgets the pane but keeps the store tab untouched. */
  function resetLocal(): void {
    tabId.value = "";
    starting.value = false;
    closed.value = false;
    selection.value = "";
    discovery.reset();
  }

  /** openTarget creates (or reuses) the page session for one exact target. */
  async function openTarget(target: ApplicationTerminalTarget): Promise<void> {
    const applicationId = options.applicationId();
    if (!canUse.value || applicationId === "") {
      return;
    }
    const current = tab.value;
    if (current !== null && current.mode !== "window") {
      return;
    }
    // A detached tab is explicitly retained in the window: a restart opens a
    // NEW page session for the two together (one-way close stays automatic
    // only through the shared confirmation).
    const detached = current !== null;
    closed.value = false;
    terminal.clearContainerRestart(applicationId);
    starting.value = true;
    try {
      const opened = await terminal.openPageSession({
        serverId: discovery.serverId.value,
        serverName: options.applicationName() || applicationId,
        target: "container",
        applicationId,
        applicationName: options.applicationName(),
        service: target.service,
        containerId: target.container_id,
        containerName: target.name,
        targetLabel: target.label || target.name,
        title: containerTabTitle(
          options.applicationName(),
          target.label || target.name,
        ),
        sourcePath: sourcePath(),
        ...(detached ? { fresh: true } : {}),
      });
      if (options.applicationId() !== applicationId) {
        return;
      }
      tabId.value = opened.id;
      selection.value = containerTargetKey({
        service: target.service,
        containerId: target.container_id,
      });
    } finally {
      starting.value = false;
    }
  }

  /** selectedTarget resolves the current selection in one target list. */
  function selectedTarget(
    targets: ApplicationTerminalTarget[],
  ): ApplicationTerminalTarget | null {
    return (
      targets.find(
        (target) =>
          containerTargetKey({
            service: target.service,
            containerId: target.container_id,
          }) === selection.value,
      ) ?? null
    );
  }

  /**
   * applyFreshTargets turns one fresh discovery into the next state:
   * a single runnable container reopens, several keep the selector, none
   * surface the empty/error state. It never creates more than one shell.
   */
  async function applyFreshTargets(
    result: TargetsLoadResult,
    autoOpen: boolean,
  ): Promise<void> {
    if (result === null || !result.ok) {
      return;
    }
    const live = tab.value;
    if (live !== null) {
      // A refresh never starts a second shell; the live tab stays selected
      // even when the list was reordered or a replica was replaced.
      selection.value = tabTargetKey(live);
      return;
    }
    const stillSelected = selectedTarget(result.targets);
    if (stillSelected === null) {
      selection.value = "";
    }
    if (closed.value) {
      return; // the explicit restart card owns the next create
    }
    if (result.targets.length === 1 && autoOpen) {
      await openTarget(result.targets[0]!);
    }
  }

  /** discover loads and applies the targets (no duplicates on flapping). */
  async function discover(autoOpen: boolean): Promise<void> {
    const result = await discovery.load();
    await applyFreshTargets(result, autoOpen);
  }

  /**
   * restart re-reads the targets before reconnecting. A replaced container is
   * resolved from the fresh response (one target opens, several show the
   * selector, none stays empty); a refused id is never re-POSTed.
   */
  async function restart(): Promise<void> {
    closed.value = false;
    terminal.clearContainerRestart(options.applicationId());
    selection.value = "";
    const result = await discovery.load();
    if (result === null) {
      return;
    }
    if (!result.ok) {
      return;
    }
    const choice = chooseRecoveryTarget(result.targets, null, false);
    if (choice.target !== null) {
      await openTarget(choice.target);
    } else {
      selection.value = "";
    }
  }

  /** startNew is the closed-card action; it always rediscovers first. */
  async function startNew(): Promise<void> {
    await restart();
  }

  /** retry is the explicit recovery from a bounded discovery error. */
  async function retry(): Promise<void> {
    await discover(true);
  }

  /**
   * refresh re-reads the list. A healthy/live shell is never recreated; a
   * failed container tab is recovered through the same rediscovery path as
   * Retry, so the visible "Refresh services" control can never re-POST a
   * refused container id either.
   */
  async function refresh(): Promise<void> {
    const live = tab.value;
    if (
      live !== null &&
      live.mode === "page" &&
      live.status !== "connected" &&
      live.status !== "connecting"
    ) {
      await retryFromPane();
      return;
    }
    await discover(live === null && !closed.value);
  }

  /**
   * retryFromPane is registered with the store for this tab: Retry on a
   * container pane must release the failed tab, rediscover and re-resolve the
   * target instead of re-POSTing the refused container id.
   */
  async function retryFromPane(): Promise<void> {
    if (tabId.value !== "") {
      terminal.closeTabs([tabId.value]);
    }
    tabId.value = "";
    await restart();
  }

  let unbindRetry: (() => void) | null = null;
  watch(
    [tabId, () => tab.value?.mode ?? ""],
    ([current, tabMode]) => {
      unbindRetry?.();
      unbindRetry = null;
      // A detached tab is retried by the floating window; the store's bounded
      // recovery keeps its window mode. This page resolver owns page tabs only.
      if (current !== "" && tabMode === "page") {
        unbindRetry = terminal.registerRetryHandler(current, retryFromPane);
      }
    },
    { immediate: true },
  );

  /**
   * selectTarget is the explicit service/replica choice. While a shell is
   * live it runs the shared confirmation: Cancel keeps the current session
   * (and restores the selector), Confirm releases exactly that session and
   * opens the chosen target.
   */
  async function selectTarget(key: string): Promise<void> {
    const target =
      discovery.targets.value.find(
        (candidate) =>
          containerTargetKey({
            service: candidate.service,
            containerId: candidate.container_id,
          }) === key,
      ) ?? null;
    if (target === null) {
      return;
    }
    const live = tab.value;
    // A detached window shell is retained, never closed by a selection: the
    // chosen target opens as a new page session beside it. A visible page
    // shell still uses the shared confirmation (Cancel keeps it).
    if (live !== null && live.mode === "page") {
      if (tabTargetKey(live) === key) {
        selection.value = key;
        return;
      }
      const confirmed = await terminal.requestClose([live.id]);
      if (!confirmed) {
        // Cancel keeps the live shell: restore the visible selection to it.
        selection.value = tabTargetKey(live);
        return;
      }
      tabId.value = "";
    }
    selection.value = key;
    closed.value = false;
    await openTarget(target);
  }

  // Reset before the activation watcher for the same id change: otherwise a
  // remount adoption (which sets tabId) would be cleared by this watcher.
  watch(
    () => options.applicationId(),
    () => {
      resetLocal();
    },
  );

  // First activation discovers. A revisit reuses `attempted`, and a closed
  // tab keeps its restart card with zero creates until the operator acts.
  watch(
    [
      () => options.active(),
      canUse,
      () => options.applicationId(),
      () => options.applicationName(),
    ],
    () => {
      const applicationId = options.applicationId();
      if (
        !options.active() ||
        !canUse.value ||
        applicationId === "" ||
        closed.value ||
        tabId.value !== "" ||
        discovery.attempted.value
      ) {
        return;
      }
      const existing = terminal.tabs.find(
        (candidate) =>
          isContainerTerminalTarget(candidate.target) &&
          candidate.applicationId === applicationId,
      );
      if (existing) {
        // A remount (route change and back) adopts the live store tab before
        // discovery, so re-entering never opens a second shell.
        tabId.value = existing.id;
        selection.value = tabTargetKey(existing);
        void discover(false);
        return;
      }
      const dismissed = terminal.containerRestartTarget(applicationId);
      if (dismissed !== null) {
        // The explicit close survives the remount: show the restart card and
        // create nothing until the operator asks.
        closed.value = true;
        selection.value = dismissed;
        discovery.markAttempted();
        return;
      }
      void discover(true);
    },
    { immediate: true },
  );

  // A tab removed by any route (close confirmation, logout reset) must not
  // leave the host pointing at a dead id. The selection survives so the
  // explicit restart card can offer the same target.
  watch(tab, (current) => {
    if (tabId.value !== "" && current === null) {
      tabId.value = "";
      starting.value = false;
      closed.value = true;
      const applicationId = options.applicationId();
      if (applicationId !== "") {
        terminal.dismissContainerRestart(applicationId, selection.value);
      }
    }
  });

  onScopeDispose(() => {
    unbindRetry?.();
    unbindRetry = null;
  });

  return {
    targetOptions,
    targetsLoading: discovery.loading,
    targetsError: discovery.error,
    mode,
    selection,
    tabId,
    starting,
    closed,
    showPicker,
    refresh,
    retry,
    startNew,
    selectTarget,
  };
}
