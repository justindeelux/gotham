import { useMessage } from "naive-ui";
import { computed, onMounted, ref, watch } from "vue";
import type { ComputedRef } from "vue";
import { useI18n } from "vue-i18n";

import { describeApplicationError } from "@/features/applications/api/applications";
import type {
  ApplicationContainer,
  ApplicationContainerAction,
} from "@/features/applications/api/containers";
import {
  applicationContainerAction,
  listApplicationContainers,
} from "@/features/applications/api/containers";
import { useApplicationsStore } from "@/features/applications/stores/applications";
import { activeLocale } from "@/shared/i18n/locale";

export function useApplicationContainers(appId: ComputedRef<string>) {
  const message = useMessage();
  const { t } = useI18n();
  const appsStore = useApplicationsStore();

  const containers = ref<ApplicationContainer[]>([]);
  const loading = ref(false);
  const loaded = ref(false);
  const errorRaw = ref<unknown>(null);
  const pending = ref<Record<string, boolean>>({});

  const error = computed<string | null>(() => {
    if (errorRaw.value === null || errorRaw.value === undefined) {
      return null;
    }
    void activeLocale.value;
    return describeApplicationError(errorRaw.value);
  });

  function isPending(containerId: string, action: ApplicationContainerAction): boolean {
    return pending.value[`${containerId}:${action}`] === true;
  }

  function isBusy(containerId: string): boolean {
    return Object.keys(pending.value).some((key) =>
      key.startsWith(`${containerId}:`),
    );
  }

  async function fetchContainers(showLoading: boolean): Promise<void> {
    if (!appId.value) {
      return;
    }
    if (showLoading) {
      loading.value = true;
    }
    try {
      containers.value = await listApplicationContainers(appId.value);
      errorRaw.value = null;
      loaded.value = true;
    } catch (err) {
      errorRaw.value = err;
    } finally {
      loading.value = false;
    }
  }

  async function runAction(
    row: ApplicationContainer,
    action: ApplicationContainerAction,
  ): Promise<void> {
    const key = `${row.id}:${action}`;
    pending.value = { ...pending.value, [key]: true };
    try {
      await applicationContainerAction(appId.value, row.id, action);
      message.success(
        t("applications.containers.actionRequested", {
          action: t(`applications.containers.${action}`),
          name: row.name,
        }),
      );
      await fetchContainers(false);
      // The header badge and the persisted stop read the application record.
      await appsStore.fetchApplication(appId.value).catch(() => undefined);
    } catch (err) {
      message.error(describeApplicationError(err));
    } finally {
      const next = { ...pending.value };
      delete next[key];
      pending.value = next;
    }
  }

  function initialize(): void {
    loaded.value = false;
    void fetchContainers(true);
  }

  watch(
    () => appId.value,
    () => {
      containers.value = [];
      errorRaw.value = null;
      initialize();
    },
  );

  onMounted(initialize);

  return {
    containers,
    loading,
    loaded,
    error,
    isPending,
    isBusy,
    fetchContainers,
    runAction,
  };
}
