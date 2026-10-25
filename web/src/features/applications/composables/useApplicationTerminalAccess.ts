import { computed } from "vue";
import type { ComputedRef } from "vue";
import { useRoute } from "vue-router";

import { useAuthStore } from "@/features/auth";

export interface ApplicationTerminalAccess {
  accessKnown: ComputedRef<boolean>;
  canWrite: ComputedRef<boolean>;
  accessError: ComputedRef<boolean>;
  retryAccess: () => void;
  showTab: ComputedRef<boolean>;
  blocked: ComputedRef<boolean>;
  resolveTab: (_tab: string) => string;
}

export const ACCESS_CHECK_TIMEOUT_MS = 8_000;

export function useApplicationTerminalAccess(
  applicationLoaded: () => boolean,
): ApplicationTerminalAccess {
  const route = useRoute();
  const authStore = useAuthStore();

  const accessKnown = computed<boolean>(() => true);
  const accessError = computed<boolean>(() => false);
  const canWrite = computed<boolean>(() => authStore.isOwner);
  const showTab = computed<boolean>(
    () => applicationLoaded() && canWrite.value,
  );
  const blocked = computed<boolean>(
    () =>
      applicationLoaded() &&
      !canWrite.value &&
      route?.query?.tab === "terminal",
  );

  function resolveTab(tab: string): string {
    if (tab === "terminal" && !canWrite.value) {
      return "overview";
    }
    return tab;
  }

  return {
    accessKnown,
    accessError,
    retryAccess: () => undefined,
    canWrite,
    showTab,
    blocked,
    resolveTab,
  };
}
