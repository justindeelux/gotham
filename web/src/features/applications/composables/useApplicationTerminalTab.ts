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

export interface ApplicationTerminalTabOptions {
  applicationId: () => string;
  applicationName: () => string;
  canUse: () => boolean;
  active: () => boolean;
}

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
      return "loading";
    }
    if (discovery.loaded.value && targetCount.value > 1) {
      return "select";
    }
    return "idle";
  });

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

  function resetLocal(): void {
    tabId.value = "";
    starting.value = false;
    closed.value = false;
    selection.value = "";
    discovery.reset();
  }

  async function openTarget(target: ApplicationTerminalTarget): Promise<void> {
    const applicationId = options.applicationId();
    if (!canUse.value || applicationId === "") {
      return;
    }
    const current = tab.value;
    if (current !== null && current.mode !== "window") {
      return;
    }
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

  async function applyFreshTargets(
    result: TargetsLoadResult,
    autoOpen: boolean,
  ): Promise<void> {
    if (result === null || !result.ok) {
      return;
    }
    const live = tab.value;
    if (live !== null) {
      selection.value = tabTargetKey(live);
      return;
    }
    const stillSelected = selectedTarget(result.targets);
    if (stillSelected === null) {
      selection.value = "";
    }
    if (closed.value) {
      return;
    }
    if (result.targets.length === 1 && autoOpen) {
      await openTarget(result.targets[0]!);
    }
  }

  async function discover(autoOpen: boolean): Promise<void> {
    const result = await discovery.load();
    await applyFreshTargets(result, autoOpen);
  }

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

  async function startNew(): Promise<void> {
    await restart();
  }

  async function retry(): Promise<void> {
    await discover(true);
  }

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
      if (current !== "" && tabMode === "page") {
        unbindRetry = terminal.registerRetryHandler(current, retryFromPane);
      }
    },
    { immediate: true },
  );

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
    if (live !== null && live.mode === "page") {
      if (tabTargetKey(live) === key) {
        selection.value = key;
        return;
      }
      const confirmed = await terminal.requestClose([live.id]);
      if (!confirmed) {
        selection.value = tabTargetKey(live);
        return;
      }
      tabId.value = "";
    }
    selection.value = key;
    closed.value = false;
    await openTarget(target);
  }

  watch(
    () => options.applicationId(),
    () => {
      resetLocal();
    },
  );

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
        tabId.value = existing.id;
        selection.value = tabTargetKey(existing);
        void discover(false);
        return;
      }
      const dismissed = terminal.containerRestartTarget(applicationId);
      if (dismissed !== null) {
        closed.value = true;
        selection.value = dismissed;
        discovery.markAttempted();
        return;
      }
      void discover(true);
    },
    { immediate: true },
  );

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
